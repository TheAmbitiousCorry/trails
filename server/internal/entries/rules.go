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

	"github.com/TheAmbitiousCorry/trails/internal/anchor"
	"github.com/TheAmbitiousCorry/trails/internal/content"
	"github.com/TheAmbitiousCorry/trails/internal/content/markdown"
	"github.com/TheAmbitiousCorry/trails/internal/search"
	"github.com/TheAmbitiousCorry/trails/internal/signs"
	"github.com/TheAmbitiousCorry/trails/internal/staged"
	"github.com/TheAmbitiousCorry/trails/internal/trail"
	"github.com/TheAmbitiousCorry/trails/internal/view"
)

var defaultCaps = map[string]int{"summary": 300, "decision": 200, "procedure": 400, "example": 40, "render": 60, "reference": 100}

// RefusalRules names every rule the write path refuses with itself; schema failures from the content walker carry content.RefusalRules.
var RefusalRules = []string{"schema", "unknown_entry", "split this", "anchor", "duplicate", "verified", "paraphrase", "unreachable", "immutable", "detached_checkout", "staged_elsewhere", "stale_edit"}

func refusal(rule, format string, args ...any) *trail.Refusal {
	return &trail.Refusal{Rule: rule, Fix: fmt.Sprintf(format, args...)}
}

// schema is rule 1: field shapes, then the body through the Markdown adapter and the content walker.
// Every field problem is reported together, in one fix, so a caller repairs the request once.
func schema(ctx context.Context, w *writer) (*trail.Refusal, error) {
	r := w.req
	var problems []string
	if _, ok := defaultCaps[r.Kind]; !ok {
		problems = append(problems, fmt.Sprintf("kind %q is not one of summary, decision, procedure, example, render, reference", r.Kind))
	}
	if w.prev == nil && r.ParentSignID == "" {
		problems = append(problems, "parent_sign_id is required to create; every entry hangs under exactly one sign")
	}
	if r.Title == "" || utf8.RuneCountInString(r.Title) > maxTitle || strings.ContainsAny(r.Title, "\r\n") {
		problems = append(problems, fmt.Sprintf("title names the subject in at most %d characters, not a sentence", maxTitle))
	}
	if r.ReadWhen == "" || utf8.RuneCountInString(r.ReadWhen) > maxReadWhen || strings.ContainsAny(r.ReadWhen, "\r\n") {
		problems = append(problems, fmt.Sprintf("read_when is one line of 1 to %d characters naming the need this entry answers", maxReadWhen))
	}
	if w.prev == nil && strings.TrimSpace(r.BodyMarkdown) == "" {
		problems = append(problems, "body is required to create an entry; send it as Markdown")
	}
	body, err := markdown.FromMarkdown(r.BodyMarkdown)
	if err != nil {
		var ref *content.Refusal
		if !errors.As(err, &ref) {
			return nil, err
		}
		if len(problems) == 0 {
			return &trail.Refusal{Rule: ref.Rule, Fix: ref.Fix}, nil
		}
		problems = append(problems, ref.Rule+": "+ref.Fix)
	}
	if len(problems) > 0 {
		return refusal("schema", "%s", strings.Join(problems, "; ")), nil
	}
	w.body = body
	for _, id := range append(append([]string{}, r.Requires...), r.SeeAlso...) {
		var n int
		if err := w.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM entries WHERE id = ?`, id).Scan(&n); err != nil {
			return nil, err
		}
		if n == 0 || !view.From(ctx).Sees(id) {
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
	var kept []anchor.Anchor
	if r.Edit {
		var err error
		if kept, err = anchor.Load(ctx, w.db, w.prev.id); err != nil {
			return nil, err
		}
		// An edit that leaves the anchor set alone is judged on the words only: a commit that is no longer reachable, or the anchorless root summary, must not lock the entry.
		if sameAnchors(as, kept) {
			w.anchors, w.srcRepo = detach(kept), w.checker.Repo
			return nil, nil
		}
	}
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
	checker, ref := w.sourceChecker(ctx, as)
	if ref != nil {
		return ref, nil
	}
	w.srcRepo = checker.Repo
	w.anchors = nil
	for _, a := range as {
		if i := indexAnchor(kept, a); i >= 0 {
			w.anchors = append(w.anchors, detach(kept[i : i+1])[0])
			kept = append(kept[:i:i], kept[i+1:]...)
			continue
		}
		got, err := checker.Resolve(ctx, a)
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

// anchorKey is what makes two anchors the same claim for an edit: type, path and range. A commit is its sha, kept in Hash; a transcript is its provider and id, kept in Extra.
// Any other Hash is the server's own reading of the source (produced_by is overwritten with HEAD), so it never decides.
func anchorKey(a anchor.Anchor) string {
	k := fmt.Sprintf("%s|%s|%d|%d", a.Type, a.PathOrURL, a.RangeStart, a.RangeEnd)
	switch a.Type {
	case "commit":
		k += "|" + a.Hash
	case "transcript":
		k += "|" + a.Extra
	}
	return k
}

func sameAnchors(req, stored []anchor.Anchor) bool {
	if len(req) != len(stored) {
		return false
	}
	left := stored
	for _, a := range req {
		i := indexAnchor(left, a)
		if i < 0 {
			return false
		}
		left = append(left[:i:i], left[i+1:]...)
	}
	return true
}

// indexAnchor is the position in from of an anchor with a's key, or -1.
func indexAnchor(from []anchor.Anchor, a anchor.Anchor) int {
	for i, f := range from {
		if anchorKey(f) == anchorKey(a) {
			return i
		}
	}
	return -1
}

// detach copies stored anchors without their row identity, so they insert as new rows that keep hash, ok and check times.
func detach(as []anchor.Anchor) []anchor.Anchor {
	out := make([]anchor.Anchor, len(as))
	for i, a := range as {
		a.ID, a.EntryID = 0, ""
		out[i] = a
	}
	return out
}

// sourceChecker picks what the anchors resolve against. A normal entry resolves against the primary, as the checks that follow it do.
// A write about branch work is staged and resolves against the session's checkout, where the work is; an update to an entry staged on the viewer's own branch does the same and stays staged.
// An update to a normal entry is judged on the union of the request's and the original's anchors, and is stored as a staged copy when that union is changed on the branch.
func (w *writer) sourceChecker(ctx context.Context, as []anchor.Anchor) (anchor.Checker, *trail.Refusal) {
	v := view.From(ctx)
	if v.Checkout == "" {
		return w.checker, nil
	}
	here := anchor.Checker{Repo: v.Checkout, Fetcher: w.checker.Fetcher}
	var original []anchor.Anchor
	if w.prev != nil {
		if w.prev.stagedBranch != "" {
			return here, nil
		}
		var err error
		if original, err = anchor.Load(ctx, w.db, w.prev.id); err != nil {
			return w.checker, refusal("anchor", "the staged check failed: %v", err)
		}
	}
	d, err := staged.DecideFor(ctx, v.Checkout, as, original, w.req.Staged)
	switch {
	case errors.Is(err, staged.ErrDetached):
		return w.checker, refusal("detached_checkout", "this entry describes work on a branch and HEAD is detached, so no branch can hold it; check out a branch, then write again")
	case errors.Is(err, staged.ErrDirAnchor):
		return w.checker, refusal("anchor", "this entry describes work on branch %s and a directory anchor cannot be held with it; anchor the files instead", v.Branch)
	case err != nil:
		return w.checker, refusal("anchor", "the staged check failed: %v", err)
	}
	if !d.Staged {
		return w.checker, nil
	}
	w.staged = d
	return here, nil
}

// maxDuplicateCandidates bounds the self-score lookups: only the best raw matches can be near-identical.
const maxDuplicateCandidates = 3

// duplicate is rule 4. It compares only entries of the request's kind, so a decision never collides with its own summary. It searches title, read_when and the first paragraph, then divides each top hit's bm25 by that hit's score against its own text,
// so the threshold is a fraction of a perfect match and does not move with the length of the entry.
func duplicate(ctx context.Context, w *writer) (*trail.Refusal, error) {
	q := search.DuplicateQuery(w.req.Title, w.req.ReadWhen, content.PlainText(w.body))
	if w.req.Edit {
		// An edit that leaves what the rule searches on alone cannot have made a duplicate, and a stored near-duplicate pair must not lock its entries.
		was, err := search.EntryQuery(ctx, w.db, w.prev.id)
		if err != nil {
			return nil, err
		}
		if was == q {
			return nil, nil
		}
	}
	hits, err := search.Rank(ctx, w.db, q, "")
	if err != nil {
		return nil, err
	}
	threshold, err := search.Setting(ctx, w.db, "duplicate_threshold")
	if err != nil {
		return nil, err
	}
	readWhenThreshold, err := search.Setting(ctx, w.db, "search_threshold")
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
		if w.prev != nil {
			standsIn, err := standsInFor(ctx, w.db, w.prev.id, h.EntryID)
			if err != nil {
				return nil, err
			}
			if standsIn {
				continue
			}
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
		// Neighbours in one domain share nouns; only a read_when asking the same question makes them the same entry.
		// Terms are compared on the read_when lines alone: a bm25 ratio counted the request's words wherever they appeared in the hit.
		// A caller who names a resolution has already chosen the hit, so the read_when gate applies to the automatic refusal only.
		if ratio > threshold && w.req.Resolution == "" {
			if rw := readWhenOverlap(w.req.ReadWhen, h.ReadWhen); rw >= 0 && rw < readWhenThreshold {
				continue
			}
		}
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
		// An update cannot merge (see the merge case below), so its fix must not offer it.
		if w.prev != nil {
			return refusal("duplicate", "%s (%q) already covers this (%.0f%% match). Write again with resolution replace (supersede it) or distinct with a distinct_reason (keep both)",
				hit.EntryID, hit.Title, math.Min(w.scores.Duplicate, 1)*100), nil
		}
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

// standsInFor reports whether the hit is linked to the updated entry by conflicts_with in either direction, or was superseded by it.
func standsInFor(ctx context.Context, db *sql.DB, prevID, hitID string) (bool, error) {
	var n int
	err := db.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(*) FROM links WHERE relation = 'conflicts_with' AND ((from_id = ?1 AND to_id = ?2) OR (from_id = ?2 AND to_id = ?1)))
		+ (SELECT COUNT(*) FROM entries WHERE id = ?2 AND superseded_by = ?1)`, prevID, hitID).Scan(&n)
	return n > 0, err
}

// readWhenOverlap is the share of the shorter read_when's content terms that the other read_when also contains.
// It returns -1 when either side has no content terms, so the combined verdict stands.
func readWhenOverlap(a, b string) float64 {
	ta, tb := search.ContentTerms(a), search.ContentTerms(b)
	if len(ta) == 0 || len(tb) == 0 {
		return -1
	}
	in := make(map[string]bool, len(tb))
	for _, t := range tb {
		in[t] = true
	}
	shared := 0
	for _, t := range ta {
		if in[t] {
			shared++
		}
	}
	return float64(shared) / float64(min(len(ta), len(tb)))
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
	if w.req.Edit {
		// The rule judges the body, and the anchored files can come to hold its words after it was written; an edit that leaves the body alone is not the author.
		var was string
		if err := w.db.QueryRowContext(ctx, `SELECT body_json FROM entries WHERE id = ?`, w.prev.id).Scan(&was); err != nil {
			return nil, err
		}
		if canonical(was) == canonical(string(w.body)) {
			return nil, nil
		}
	}
	var src strings.Builder
	for _, a := range w.anchors {
		if a.Type != "file" {
			continue
		}
		if b, err := os.ReadFile(filepath.Join(w.srcRepo, a.PathOrURL)); err == nil {
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
	// An edit stores no entry; without this the rule counts the edited summary itself as the second one.
	if w.req.Edit {
		return nil, nil
	}
	if w.prev != nil && w.prev.tier != "verified" {
		return nil, nil
	}
	n, err := visibleOfKind(ctx, w.db, w.req.ParentSignID, w.req.Kind)
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

// visibleOfKind counts the entries of one kind under a sign that the writer's Viewer is shown, so a staged entry on another branch does not make a kind look present.
func visibleOfKind(ctx context.Context, db *sql.DB, signID, kind string) (int, error) {
	rows, err := db.QueryContext(ctx, `SELECT id FROM entries WHERE sign_id = ? AND kind = ?`, signID, kind)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	viewer := view.From(ctx)
	n := 0
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return 0, err
		}
		if viewer.Sees(id) {
			n++
		}
	}
	return n, rows.Err()
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
