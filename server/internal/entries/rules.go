package entries

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/TheAmbitiousCorry/trails/internal/content"
	"github.com/TheAmbitiousCorry/trails/internal/content/markdown"
	"github.com/TheAmbitiousCorry/trails/internal/signs"
	"github.com/TheAmbitiousCorry/trails/internal/trail"
)

var defaultCaps = map[string]int{"summary": 300, "decision": 200, "example": 40, "render": 60, "reference": 100}

func refusal(rule, format string, args ...any) *trail.Refusal {
	return &trail.Refusal{Rule: rule, Fix: fmt.Sprintf(format, args...)}
}

// schema is rule 1: field shapes, then the body through the Markdown adapter and the content walker.
func schema(ctx context.Context, w *writer) (*trail.Refusal, error) {
	r := w.req
	if _, ok := defaultCaps[r.Kind]; !ok {
		return refusal("schema:kind", "kind %q is not one of summary, decision, example, render, reference", r.Kind), nil
	}
	if r.Title == "" || utf8.RuneCountInString(r.Title) > maxTitle || strings.ContainsAny(r.Title, "\r\n") {
		return refusal("schema:title", "title is one line of 1 to %d characters", maxTitle), nil
	}
	if r.ReadWhen == "" || utf8.RuneCountInString(r.ReadWhen) > maxReadWhen || strings.ContainsAny(r.ReadWhen, "\r\n") {
		return refusal("schema:read_when", "read_when is one line of 1 to %d characters naming the need this entry answers", maxReadWhen), nil
	}
	body, err := markdown.FromMarkdown(r.BodyMarkdown)
	if err != nil {
		var ref *content.Refusal
		if errors.As(err, &ref) {
			return &trail.Refusal{Rule: ref.Rule, Fix: ref.Fix}, nil
		}
		return nil, err
	}
	w.body = body
	for _, id := range append(append([]string{}, r.Requires...), r.SeeAlso...) {
		var n int
		if err := w.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM entries WHERE id = ?`, id).Scan(&n); err != nil {
			return nil, err
		}
		if n == 0 {
			return refusal("unknown_entry", "linked entry %q does not exist; entry ids come from a sign or a read", id), nil
		}
	}
	return nil, nil
}

// wordCap is rule 2.
func wordCap(ctx context.Context, w *writer) (*trail.Refusal, error) {
	limit := defaultCaps[w.req.Kind]
	var v sql.NullString
	err := w.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, "cap_"+w.req.Kind+"_words").Scan(&v)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if n, convErr := strconv.Atoi(strings.TrimSpace(v.String)); convErr == nil && n > 0 {
		limit = n
	}
	if n := content.WordCount(w.body); n > limit {
		return refusal("split this", "the body is %d words and the cap for %s is %d; split it into several entries", n, w.req.Kind, limit), nil
	}
	return nil, nil
}

// anchors is rule 3: the kind's anchor rule, then every anchor resolved against the repository or the web right now.
func anchors(ctx context.Context, w *writer) (*trail.Refusal, error) {
	r := w.req
	as := r.Anchors
	types := map[string]int{}
	for _, a := range as {
		types[a.Type]++
	}
	switch r.Kind {
	case "summary":
		if types["file"]+types["dir"]+types["commit"] == 0 {
			return refusal("anchor", "a summary needs at least one file, dir or commit anchor"), nil
		}
	case "decision":
		if r.DecidedBy == "" || r.DecidedAt == "" {
			return refusal("anchor", "a decision needs decided_by and decided_at"), nil
		}
	case "example":
		if len(as) != 1 || types["range"] != 1 {
			return refusal("anchor", "an example needs exactly one range anchor and no other"), nil
		}
	case "render":
		if len(as) != 1 || types["produced_by"] != 1 {
			return refusal("anchor", "a render needs exactly one produced_by anchor naming the command or URL and the commit"), nil
		}
	case "reference":
		if len(as) != 1 || types["url"]+types["transcript"] != 1 {
			return refusal("anchor", "a reference needs exactly one url or transcript anchor"), nil
		}
	}
	w.anchors = nil
	for _, a := range as {
		got, err := w.checker.Resolve(ctx, a)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return refusal("anchor", "%s does not resolve now: %v", a.Label(), err), nil
		}
		w.anchors = append(w.anchors, got)
	}
	return nil, nil
}

// fanOut is rule 6. Only an entry kind new to its parent adds an edge, so a second entry of a kind already there is never refused.
func fanOut(ctx context.Context, w *writer) (*trail.Refusal, error) {
	if w.prev != nil && w.prev.tier != "verified" {
		return nil, nil
	}
	var n int
	err := w.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM entries WHERE sign_id = ? AND kind = ?`, w.req.ParentSignID, w.req.Kind).Scan(&n)
	if err != nil || n > 0 {
		return nil, err
	}
	err = signs.CheckFanOut(ctx, w.db, w.req.ParentSignID)
	var ref *trail.Refusal
	if errors.As(err, &ref) {
		return ref, nil
	}
	return nil, err
}

// reachability is rule 7: exactly one parent, and the walk up from it ends at the root.
func reachability(ctx context.Context, w *writer) (*trail.Refusal, error) {
	id := w.req.ParentSignID
	if id == "" {
		return refusal("unreachable", "pass parent_sign_id: every entry hangs under exactly one sign"), nil
	}
	seen := map[string]bool{}
	for cur := id; ; {
		if seen[cur] {
			return refusal("unreachable", "sign %q is in a cycle and cannot be reached from the root", id), nil
		}
		seen[cur] = true
		var parent sql.NullString
		err := w.db.QueryRowContext(ctx, `SELECT parent_id FROM signs WHERE id = ?`, cur).Scan(&parent)
		if errors.Is(err, sql.ErrNoRows) {
			return refusal("unreachable", "sign %q does not exist or hangs under a missing sign; sign ids come from look", id), nil
		}
		if err != nil {
			return nil, err
		}
		if !parent.Valid {
			return nil, nil
		}
		cur = parent.String
	}
}
