package mcp_test

import (
	"context"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type agent struct {
	t    *testing.T
	sess *sdk.ClientSession
	sid  string
}

// newAgent connects a client and starts a session.
func newAgent(t *testing.T, addr, runtimeID string) *agent {
	t.Helper()
	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "0"}, nil)
	sess, err := client.Connect(context.Background(), &sdk.StreamableClientTransport{Endpoint: "http://" + addr + "/mcp", HTTPClient: http.DefaultClient}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sess.Close() })
	a := &agent{t: t, sess: sess}
	args := map[string]any{"task": "test walk", "task_type": "report"}
	if runtimeID != "" {
		args["runtime_session_id"] = runtimeID
	}
	a.sid = a.ok("session_start", args)["session_id"].(string)
	return a
}

func (a *agent) raw(tool string, args map[string]any) (*sdk.CallToolResult, error) {
	if _, set := args["session_id"]; !set && tool != "session_start" {
		args["session_id"] = a.sid
	}
	return a.sess.CallTool(context.Background(), &sdk.CallToolParams{Name: tool, Arguments: args})
}

func (a *agent) ok(tool string, args map[string]any) map[string]any {
	a.t.Helper()
	res, err := a.raw(tool, args)
	return structured(a.t, res, err)
}

// refused asserts the call failed with the named rule and returns the full refusal text.
func (a *agent) refused(tool string, args map[string]any, rule string) string {
	a.t.Helper()
	res, err := a.raw(tool, args)
	if err != nil || !res.IsError {
		a.t.Fatalf("%s not refused (err %v)", tool, err)
	}
	txt := res.Content[0].(*sdk.TextContent).Text
	if !strings.HasPrefix(txt, rule+":") {
		a.t.Fatalf("%s refusal = %q, want rule %q", tool, txt, rule)
	}
	return txt
}

func (a *agent) writeDecision(title, body string) string {
	a.t.Helper()
	w := a.ok("write", map[string]any{"parent_sign_id": "root", "kind": "decision", "title": title,
		"read_when": "you study " + title, "body": body, "decided_by": "ana", "decided_at": "2026-10-27"})
	return w["entry_id"].(string)
}

func (a *agent) readFull(id string) {
	a.t.Helper()
	a.ok("read", map[string]any{"entry_id": id, "depth": "full"})
}

func TestConfirmNeedsTwoSessionsAfterFullReads(t *testing.T) {
	st, addr := startServer(t)
	author, b, c := newAgent(t, addr, ""), newAgent(t, addr, ""), newAgent(t, addr, "")
	id := author.writeDecision("Ship weekly", "We ship on Fridays.")

	b.refused("confirm", map[string]any{"entry_id": id}, "unread")
	b.ok("read", map[string]any{"entry_id": id}) // summary depth does not count
	b.refused("confirm", map[string]any{"entry_id": id}, "unread")
	b.readFull(id)
	if got := b.ok("confirm", map[string]any{"entry_id": id}); got["tier"] != "provisional" || got["confirmed_by"] != float64(1) {
		t.Fatalf("one confirm = %v", got)
	}
	if got := b.ok("confirm", map[string]any{"entry_id": id}); got["tier"] != "provisional" || got["confirmed_by"] != float64(1) {
		t.Fatalf("the same session twice = %v", got)
	}

	c.readFull(id)
	if got := c.ok("confirm", map[string]any{"entry_id": id}); got["tier"] != "verified" || got["confirmed_by"] != float64(2) {
		t.Fatalf("second session = %v", got)
	}
	var tier, by string
	st.DB.QueryRow(`SELECT tier, promoted_by FROM entries WHERE id = ?`, id).Scan(&tier, &by)
	if tier != "verified" || by != "agents" {
		t.Errorf("stored tier %q promoted_by %q", tier, by)
	}
}

func TestSelfConfirmRefused(t *testing.T) {
	_, addr := startServer(t)
	author := newAgent(t, addr, "")
	id := author.writeDecision("Ship weekly", "We ship on Fridays.")
	author.readFull(id)
	author.refused("confirm", map[string]any{"entry_id": id}, "self")
}

func gapIDs(t *testing.T, txt string) []int64 {
	t.Helper()
	var out []int64
	for _, m := range regexp.MustCompile(`gap (\d+):`).FindAllStringSubmatch(txt, -1) {
		id, _ := strconv.ParseInt(m[1], 10, 64)
		out = append(out, id)
	}
	if len(out) == 0 {
		t.Fatalf("no gap id in %q", txt)
	}
	return out
}

func TestConfirmResolveGapSessionEnd(t *testing.T) {
	st, addr := startServer(t)
	other := newAgent(t, addr, "")
	a := newAgent(t, addr, "runtime-42")

	id := a.writeDecision("Quarterly invoice rounding", "Round each invoice line to the nearest cent before summing totals.")
	a.readFull(id)
	a.ok("look", map[string]any{"sign_id": "root"})
	if miss := a.ok("search", map[string]any{"query": "zqxv ledger cadence"}); miss["miss"] != true {
		t.Fatalf("search = %v", miss)
	}

	txt := a.refused("session_end", map[string]any{}, "gaps_open")
	if !strings.Contains(txt, "zqxv ledger cadence") {
		t.Errorf("refusal does not list the gap: %q", txt)
	}
	gap := gapIDs(t, txt)[0]

	a.ok("resolve_gap", map[string]any{"gap_id": gap, "action": "merge", "target_id": id})
	if hit := a.ok("search", map[string]any{"query": "zqxv ledger cadence"}); hit["miss"] != false || ids(t, hit["hits"])[0] != id {
		t.Fatalf("merged alias is not searchable: %v", hit)
	}

	end := a.ok("session_end", map[string]any{})
	if end["closed"] != true || end["reads"] != float64(1) || end["writes"] != float64(1) || end["confirms"] != float64(0) {
		t.Errorf("session_end = %v", end)
	}
	var endedBy, runtime string
	st.DB.QueryRow(`SELECT ended_by, runtime_session_id FROM sessions WHERE id = ?`, a.sid).Scan(&endedBy, &runtime)
	if endedBy != "agent" || runtime != "runtime-42" {
		t.Errorf("session row: ended_by %q runtime_session_id %q", endedBy, runtime)
	}

	// Another session cannot resolve this session's gap, and a resolved gap stays resolved.
	other.refused("resolve_gap", map[string]any{"gap_id": gap, "action": "merge", "target_id": id}, "unknown_gap")
	a.refused("resolve_gap", map[string]any{"gap_id": gap, "action": "merge", "target_id": id}, "gap_closed")
}

func TestFillDismissAndDismissCap(t *testing.T) {
	_, addr := startServer(t)
	author := newAgent(t, addr, "")
	a := newAgent(t, addr, "")
	theirs := author.writeDecision("Their note", "Written by someone else.")
	a.ok("look", map[string]any{"sign_id": "root"})
	var gaps []int64
	for _, q := range []string{"qaaa", "qbbb", "qccc", "qddd", "qeee"} {
		a.ok("search", map[string]any{"query": q})
		gaps = append(gaps, gapIDs(t, a.refused("session_end", map[string]any{}, "gaps_open"))[len(gaps)])
	}

	a.refused("resolve_gap", map[string]any{"gap_id": gaps[0], "action": "fill", "target_id": theirs}, "fill_target")
	a.refused("resolve_gap", map[string]any{"gap_id": gaps[0], "action": "dismiss"}, "reason")
	a.refused("resolve_gap", map[string]any{"gap_id": gaps[0], "action": "dismiss", "reason": strings.Repeat("x", 201)}, "reason")
	a.refused("resolve_gap", map[string]any{"gap_id": gaps[0], "action": "ignore"}, "action")

	for _, g := range gaps[:3] {
		a.ok("resolve_gap", map[string]any{"gap_id": g, "action": "dismiss", "reason": "out of scope for this task"})
	}
	a.refused("resolve_gap", map[string]any{"gap_id": gaps[3], "action": "dismiss", "reason": "also out of scope"}, "dismiss_cap")

	mine := a.writeDecision("My own note", "Written here.")
	if got := a.ok("resolve_gap", map[string]any{"gap_id": gaps[3], "action": "fill", "target_id": mine}); got["state"] != "resolved" || got["resolution"] != "fill" {
		t.Errorf("fill = %v", got)
	}
}

func TestOpenGapCapStopsGapsNotEvents(t *testing.T) {
	st, addr := startServer(t)
	st.DB.Exec(`UPDATE settings SET value = '1' WHERE key = 'open_gap_cap'`)
	a := newAgent(t, addr, "")
	a.ok("search", map[string]any{"query": "qaaa"})
	a.ok("search", map[string]any{"query": "qbbb"})
	var gaps, misses int
	st.DB.QueryRow(`SELECT COUNT(*) FROM gaps`).Scan(&gaps)
	st.DB.QueryRow(`SELECT COUNT(*) FROM events WHERE tool = 'search' AND result = 'miss'`).Scan(&misses)
	if gaps != 1 || misses != 2 {
		t.Errorf("gaps %d, miss events %d, want 1 and 2", gaps, misses)
	}
	b := a.ok("session_start", map[string]any{"task": "again", "task_type": "report"})
	if n, _ := b["orient"].(map[string]any)["notice"].(string); !strings.Contains(n, "cap") {
		t.Errorf("orient notice = %q", n)
	}
}

func TestOrientCarriesTagsAndHowToWalk(t *testing.T) {
	st, addr := startServer(t)
	st.DB.Exec(`INSERT INTO tags (id, description, defined) VALUES ('report', 'monthly client reports', 1)`)
	a := newAgent(t, addr, "")
	a.ok("write", map[string]any{"parent_sign_id": "root", "kind": "decision", "title": "Tagged", "read_when": "you need tags", "body": "Body text here.",
		"decided_by": "ana", "decided_at": "2026-10-27", "tags": []string{"report", "newtag"}})

	var defined int
	if err := st.DB.QueryRow(`SELECT defined FROM tags WHERE id = 'newtag'`).Scan(&defined); err != nil || defined != 0 {
		t.Fatalf("undefined tag: defined %d err %v", defined, err)
	}
	o := a.ok("session_start", map[string]any{"task": "again", "task_type": "report"})["orient"].(map[string]any)
	tags := o["tags"].([]any)
	if len(tags) != 1 || tags[0].(map[string]any)["id"] != "report" || tags[0].(map[string]any)["description"] != "monthly client reports" {
		t.Errorf("orient tags = %v", tags)
	}
	if n := strings.Count(o["how_to_walk"].(string), "\n") + 1; n >= 15 {
		t.Errorf("how_to_walk is %d lines", n)
	}
}
