package entries

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/TheAmbitiousCorry/trails/internal/content"
	"github.com/TheAmbitiousCorry/trails/internal/content/markdown"
	"github.com/TheAmbitiousCorry/trails/internal/search"
	"github.com/TheAmbitiousCorry/trails/internal/signs"
	"github.com/TheAmbitiousCorry/trails/internal/trail"
)

var defaultCaps = map[string]int{"summary": 300, "decision": 200, "procedure": 400, "example": 40, "render": 60, "reference": 100}

// RefusalRules names every rule the write path refuses with itself; schema failures from the content walker carry content.RefusalRules.
var RefusalRules = []string{"schema:kind", "schema:title", "schema:read_when", "schema:body", "unknown_entry", "split this", "anchor", "duplicate", "verified", "paraphrase", "unreachable", "immutable"}

func refusal(rule, format string, args ...any) *trail.Refusal {
	return &trail.Refusal{Rule: rule, Fix: fmt.Sprintf(format, args...)}
}

// schema is rule 1: field shapes, then the body through the Markdown adapter and the content walker.
func schema(ctx context.Context, w *writer) (*trail.Refusal, error) {
	r := w.req
	if _, ok := defaultCaps[r.Kind]; !ok {
		return refusal("schema:kind", "kind %q is not one of summary, decision, procedure, example, render, reference", r.Kind), nil
	}
	if r.Title == "" || utf8.RuneCountInString(r.Title) > maxTitle || strings.ContainsAny(r.Title, "\r\n") {
		return refusal("schema:title", "title names the subject in at most %d characters, not a sentence", maxTitle), nil
	}
	if r.ReadWhen == "" || utf8.RuneCountInString(r.ReadWhen) > maxReadWhen || strings.ContainsAny(r.ReadWhen, "\r\n") {
		return refusal("schema:read_when", "read_when is one line of 1 to %d characters naming the need this entry answers", maxReadWhen), nil
	}
	if w.prev == nil && strings.TrimSpace(r.BodyMarkdown) == "" {
		return refusal("schema:body", "body is required to create an entry; send it as Markdown"), nil
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
	case "procedure":
		if types["file"]+types["dir"]+types["commit"] == 0 {
			return refusal("anchor", "a procedure needs at least one file, dir or commit anchor naming what its steps touch"), nil
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

// maxDuplicateCandidates bounds the self-score lookups: only the best raw matches can be near-identical.
const maxDuplicateCandidates = 3

// duplicate is rule 4. It compares only entries of the request's kind, so a decision never collides with its own summary. It searches title, read_when and the first paragraph, then divides each top hit's bm25 by that hit's score against its own text,
// so the threshold is a fraction of a perfect match and does not move with the length of the entry.
func duplicate(ctx context.Context, w *writer) (*trail.Refusal, error) {
	q := search.DuplicateQuery(w.req.Title, w.req.ReadWhen, content.PlainText(w.body))
	hits, err := search.Rank(ctx, w.db, q, "")
	if err != nil {
		return nil, err
	}
	threshold, err := search.Setting(ctx, w.db, "duplicate_threshold")
	if err != nil {
		return nil, err
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	var hit *search.Hit
	seen := 0
	for i := range hits {
		h := &hits[i]
		if (w.prev != nil && h.EntryID == w.prev.id) || h.Kind != w.req.Kind {
			continue
		}
		if seen++; seen > maxDuplicateCandidates {
			break
		}
		self, err := selfScore(ctx, w.db, h.EntryID)
		if err != nil {
			return nil, err
		}
		// A self score near zero means the corpus is too small for idf to say anything.
		if self < 1e-3 {
			continue
		}
		ratio := h.Score / self
		if ratio > w.scores.Duplicate {
			w.scores.Duplicate, w.scores.DuplicateOf, w.scores.DuplicateBM25 = ratio, h.EntryID, h.Score
			hit = h
		}
	}
	w.scores.measured = true
	if w.scores.Duplicate <= threshold {
		hit = nil
	}
	if hit == nil {
		if w.req.Resolution == "distinct" {
			w.distinctReason = strings.TrimSpace(w.req.DistinctReason)
		}
		return nil, nil
	}
	switch w.req.Resolution {
	case "":
		return refusal("duplicate", "%s (%q) already covers this (%.0f%% match). Write again with resolution merge (add your read_when as an alias on it), replace (supersede it) or distinct with a distinct_reason (keep both)",
			hit.EntryID, hit.Title, math.Min(w.scores.Duplicate, 1)*100), nil
	case "merge":
		if w.prev != nil {
			return refusal("duplicate", "merge folds a new entry into %s; this write updates %s, so use replace or distinct", hit.EntryID, w.prev.id), nil
		}
		w.merge = hit
	case "replace":
		var before string
		if err := w.db.QueryRowContext(ctx, `SELECT COALESCE(tier_before_stale, tier) FROM entries WHERE id = ?`, hit.EntryID).Scan(&before); err != nil {
			return nil, err
		}
		if before == "verified" && w.req.Actor != "user" {
			return refusal("verified", "%s is verified and an agent cannot supersede it; use merge or distinct, and a human resolves the pair in the browser", hit.EntryID), nil
		}
		w.supersede = hit
	case "distinct":
		reason := strings.TrimSpace(w.req.DistinctReason)
		if reason == "" {
			return refusal("duplicate", "distinct needs a distinct_reason: one line on why this is not the same entry as %s", hit.EntryID), nil
		}
		w.distinctReason = reason
	default:
		return refusal("duplicate", "resolution %q is not one of merge, replace, distinct", w.req.Resolution), nil
	}
	return nil, nil
}

// selfScore is the bm25 an entry earns when its own title, read_when and first paragraph are the query.
func selfScore(ctx context.Context, db *sql.DB, id string) (float64, error) {
	q, err := search.EntryQuery(ctx, db, id)
	if err != nil {
		return 0, err
	}
	hits, err := search.Rank(ctx, db, q, "")
	if err != nil {
		return 0, err
	}
	for _, h := range hits {
		if h.EntryID == id {
			return h.Score, nil
		}
	}
	return 0, nil
}

var shingleWord = regexp.MustCompile(`[\p{L}\p{N}]+`)

// shingles are the lowercased word 3-grams of text.
func shingles(text string) map[string]bool {
	words := shingleWord.FindAllString(strings.ToLower(text), -1)
	out := map[string]bool{}
	for i := 0; i+3 <= len(words); i++ {
		out[strings.Join(words[i:i+3], " ")] = true
	}
	return out
}

// paraphrase is rule 5, for summaries only: the share of the body's shingles that also occur in the anchored files.
func paraphrase(ctx context.Context, w *writer) (*trail.Refusal, error) {
	if w.req.Kind != "summary" {
		return nil, nil
	}
	var src strings.Builder
	for _, a := range w.anchors {
		if a.Type != "file" {
			continue
		}
		if b, err := os.ReadFile(filepath.Join(w.checker.Repo, a.PathOrURL)); err == nil {
			src.Write(b)
			src.WriteByte('\n')
		}
	}
	body, from := shingles(content.PlainText(w.body)), shingles(src.String())
	score := 0.0
	if len(body) > 0 {
		shared := 0
		for sh := range body {
			if from[sh] {
				shared++
			}
		}
		score = float64(shared) / float64(len(body))
	}
	w.scores.Paraphrase, w.scores.measured = &score, true
	threshold, err := search.Setting(ctx, w.db, "paraphrase_threshold")
	if err != nil {
		return nil, err
	}
	if score > threshold {
		return refusal("paraphrase", "describe the why, point at the what"), nil
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
	if err != nil {
		return nil, err
	}
	// A lone summary is no edge: the first summary adds none and the second adds the summary edge.
	newEdge := n == 0
	if w.req.Kind == "summary" {
		newEdge = n == 1
	}
	if !newEdge {
		return nil, nil
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
