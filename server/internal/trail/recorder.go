// Package trail records what every reader did: sessions and one event row per MCP call.
package trail

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/TheAmbitiousCorry/trails/internal/store"
)

// RefusalRules names every rule this package refuses with; the mcp golden file pins them and a test keeps the list equal to the source.
var RefusalRules = []string{"task_empty", "unknown_session", "unknown_entry", "unread", "self", "unknown_gap", "gap_closed", "fill_target", "reason", "dismiss_cap", "action", "gaps_open"}

// Refusal is a handler error that the trail logs as refused:<Rule> and the agent sees with its fix.
type Refusal struct {
	Rule string
	Fix  string
}

func (r *Refusal) Error() string { return r.Rule + ": " + r.Fix }

// Call is what the recorder logs about a tool call before the handler runs.
type Call struct {
	SessionID string
	Target    string
	Depth     string
	// Mint asks the recorder to issue the session id; only session_start sets it.
	Mint bool
}

type Input interface{ Call() Call }

type Recorder struct{ DB *sql.DB }

type ctxKey struct{}

type outcomeKey struct{}

// outcome lets a handler change what the recorder logs for a call that returned no error.
type outcome struct {
	result string
	shown  []string
}

// SetResult replaces the logged result of a successful call, for example with "miss".
func SetResult(ctx context.Context, result string) {
	if o, ok := ctx.Value(outcomeKey{}).(*outcome); ok {
		o.result = result
	}
}

// SetShown records the ids a call returned in the event's shown column.
func SetShown(ctx context.Context, ids []string) {
	if o, ok := ctx.Value(outcomeKey{}).(*outcome); ok {
		o.shown = ids
	}
}

// SessionID is the id the recorder logged this call under; session_start must create its session row with it.
func SessionID(ctx context.Context) string {
	s, _ := ctx.Value(ctxKey{}).(string)
	return s
}

// Record wraps a tool handler so the event row is written before it runs and its result after. Register every tool through it.
func Record[In Input, Out any](r *Recorder, tool string, h sdk.ToolHandlerFor[In, Out]) sdk.ToolHandlerFor[In, Out] {
	return func(ctx context.Context, req *sdk.CallToolRequest, in In) (*sdk.CallToolResult, Out, error) {
		var zero Out
		c := in.Call()
		if c.Mint {
			c.SessionID = "s-" + store.RandomSlug(12)
		}
		res, err := r.DB.ExecContext(ctx,
			`INSERT INTO events (session_id, at, tool, target, depth, result) VALUES (?, ?, ?, ?, ?, 'pending')`,
			c.SessionID, store.Now(), tool, c.Target, c.Depth)
		if err != nil {
			return nil, zero, err
		}
		eventID, _ := res.LastInsertId()

		result := "error"
		oc := &outcome{}
		defer func() {
			shown := "[]"
			if oc.shown != nil {
				if b, err := json.Marshal(oc.shown); err == nil {
					shown = string(b)
				}
			}
			// Detached from ctx so a cancelled request still closes its event.
			r.DB.ExecContext(context.WithoutCancel(ctx), `UPDATE events SET result = ?, shown_json = ? WHERE id = ?`, result, shown, eventID)
		}()

		hctx := context.WithValue(context.WithValue(ctx, ctxKey{}, c.SessionID), outcomeKey{}, oc)
		out, o, herr := h(hctx, req, in)
		var ref *Refusal
		switch {
		case herr == nil:
			result = "ok"
			if oc.result != "" {
				result = oc.result
			}
		case errors.As(herr, &ref):
			result = "refused:" + ref.Rule
		default:
			result = "error"
		}
		return out, o, herr
	}
}

// StartSession creates the session row for an id the recorder already issued.
// runtimeID is the agent runtime's own session id, which hook endpoints use to find the session; it may be empty.
func StartSession(ctx context.Context, db *sql.DB, id, actor, task, taskType, runtimeID string) error {
	task = strings.TrimSpace(task)
	if task == "" {
		return &Refusal{Rule: "task_empty", Fix: "pass a one-line task describing what you are about to do"}
	}
	_, err := db.ExecContext(ctx, `INSERT INTO sessions (id, actor, task, task_type, runtime_session_id, started_at) VALUES (?, ?, ?, ?, NULLIF(?,''), ?)`,
		id, actor, task, taskType, strings.TrimSpace(runtimeID), store.Now())
	return err
}

// RequireSession refuses a call whose session id the server never issued.
func RequireSession(ctx context.Context, db *sql.DB, id string) error {
	ok, err := SessionExists(ctx, db, id)
	if err != nil {
		return err
	}
	if !ok {
		return &Refusal{Rule: "unknown_session", Fix: "call session_start and use the session_id it returns"}
	}
	return nil
}

func SessionExists(ctx context.Context, db *sql.DB, id string) (bool, error) {
	var n int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE id = ?`, id).Scan(&n)
	return n > 0, err
}

// LogRead records an entry served inside another call's response as a read event of its own.
func LogRead(ctx context.Context, db *sql.DB, sessionID, entryID, depth string) error {
	_, err := db.ExecContext(ctx, `INSERT INTO events (session_id, at, tool, target, depth, result) VALUES (?, ?, 'read', ?, ?, 'ok')`,
		sessionID, store.Now(), entryID, depth)
	return err
}

// ReadAtFull reports whether the session has already been served the entry at full by a completed read.
func ReadAtFull(ctx context.Context, db *sql.DB, sessionID, entryID string) (bool, error) {
	var n int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE session_id = ? AND tool = 'read' AND depth = 'full' AND target = ? AND result = 'ok'`,
		sessionID, entryID).Scan(&n)
	return n > 0, err
}
