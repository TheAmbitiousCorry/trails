// Package entries is the single write path for entries: it runs the write rules, stores the entry, its anchors and links, and appends a version.
package entries

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/TheAmbitiousCorry/trails/internal/anchor"
	"github.com/TheAmbitiousCorry/trails/internal/content/markdown"
	"github.com/TheAmbitiousCorry/trails/internal/search"
	"github.com/TheAmbitiousCorry/trails/internal/staged"
	"github.com/TheAmbitiousCorry/trails/internal/store"
	"github.com/TheAmbitiousCorry/trails/internal/trail"
	"github.com/TheAmbitiousCorry/trails/internal/view"
)

// BodySchema is the content schema version stored with every body.
const BodySchema = 1

const (
	maxTitle    = 48
	maxReadWhen = 200
)

// WriteRequest is one create (EntryID empty) or update (EntryID set). Actor is a session id, or "user" for a human.
type WriteRequest struct {
	Actor        string
	ParentSignID string
	Kind         string
	Title        string
	ReadWhen     string
	BodyMarkdown string
	Anchors      []anchor.Anchor
	Tags         []string
	Requires     []string
	SeeAlso      []string
	DecidedBy    string
	DecidedAt    string
	EntryID      string
	// Resolution answers a duplicate refusal: merge, replace or distinct. DistinctReason is required with distinct.
	Resolution     string
	DistinctReason string
	// Staged asks for an entry with no anchors to be held on the checkout's branch.
	Staged bool
	// Edit marks a human edit from the browser: an update by actor user that keeps the entry's tier and the hashes of unchanged anchors.
	Edit bool
	// IfMatch is the updated_at the editor loaded; a different stored value refuses the update stale_edit. Empty skips the check.
	IfMatch string
}

// ConflictOffer names the verified entry an agent tried to overwrite and the provisional entry the server stored instead.
type ConflictOffer struct {
	VerifiedID string `json:"verified_id"`
	NewID      string `json:"new_id"`
}

// WriteResult has Refusal set when a rule failed; EntryID and Tier then describe the entry stored anyway, which only a conflict does.
// Merged means nothing was stored: the request's read_when became an alias on EntryID.
// Staged names the branch holding the entry when it was stored staged.
type WriteResult struct {
	EntryID  string
	Tier     string
	Merged   bool
	Staged   string
	Refusal  *trail.Refusal
	Conflict *ConflictOffer
}

type existing struct {
	id, signID, kind, tier string
	// stored is the tier column as stored, where tier is the tier a stale entry had before it went stale.
	stored string
	// stagedBranch is the branch holding the entry while it is staged.
	stagedBranch string
}

// writer carries one request through the rules; a rule fills the fields later rules and the insert read.
type writer struct {
	db      *sql.DB
	checker anchor.Checker
	req     WriteRequest
	prev    *existing
	body    json.RawMessage
	anchors []anchor.Anchor
	// staged is the anchor rule's verdict; srcRepo is the checkout the anchors resolved against, which the paraphrase rule reads the same files from.
	staged  staged.Decision
	srcRepo string
	// over and overVersion are set when this update is stored as a staged copy of entry over, whose version count was overVersion.
	over        string
	overVersion int

	// Set by the duplicate rule from the request's resolution.
	merge, supersede *search.Hit
	distinctReason   string
	scores           scores
}

// scores are what rules 4 and 5 measured; they are logged on every write that reached them, accepted or not.
type scores struct {
	Duplicate     float64  `json:"duplicate"`
	DuplicateOf   string   `json:"duplicate_of,omitempty"`
	DuplicateBM25 float64  `json:"duplicate_bm25,omitempty"`
	Paraphrase    *float64 `json:"paraphrase,omitempty"`
	measured      bool
}

type ruleFunc func(ctx context.Context, w *writer) (*trail.Refusal, error)

// rules run in spec order and the first refusal wins.
var rules = []ruleFunc{schema, wordCap, anchors, duplicate, paraphrase, fanOut, reachability}

// Write runs the rules, then stores the entry. A refusal is returned in the result, an error only for a failure of the store itself.
func Write(ctx context.Context, db *sql.DB, checker anchor.Checker, req WriteRequest) (WriteResult, error) {
	w := &writer{db: db, checker: checker, req: req}
	res, err := w.run(ctx)
	if err == nil && w.scores.measured {
		err = w.logScores(ctx, res)
	}
	return res, err
}

func (w *writer) run(ctx context.Context) (WriteResult, error) {
	db, req := w.db, w.req
	w.req.Title = strings.TrimSpace(req.Title)
	w.req.ReadWhen = strings.TrimSpace(req.ReadWhen)
	w.req.Tags = cleanList(req.Tags)
	w.req.Requires = cleanList(req.Requires)
	w.req.SeeAlso = cleanList(req.SeeAlso)

	if req.Edit && (req.Actor != "user" || req.EntryID == "") {
		return WriteResult{Refusal: refusal("schema", "edit is a human update of an existing entry: it needs actor user and an entry_id")}, nil
	}
	if req.EntryID != "" {
		prev, ref, err := loadExisting(ctx, db, req.EntryID)
		if err != nil || ref != nil {
			return WriteResult{Refusal: ref}, err
		}
		w.prev = prev
		// A session that is not on the entry's branch has no staged copy to edit. An agent with no checkout is not refused, but an Edit is: the browser has no checkout to edit a staged entry from.
		if v := view.From(ctx); prev.stagedBranch != "" && (req.Edit || v.Checkout != "" && prev.stagedBranch != v.Branch) {
			return WriteResult{Refusal: refusal("staged_elsewhere", "%s is staged on branch %s and is edited from a checkout on that branch; it reaches the base branch when that work lands", prev.id, prev.stagedBranch)}, nil
		}
		if err := w.fillOmitted(ctx, req); err != nil {
			return WriteResult{}, err
		}
		if w.req.Kind == "" {
			w.req.Kind = prev.kind
		}
		if w.req.ParentSignID == "" {
			w.req.ParentSignID = prev.signID
		}
		if w.req.Kind != prev.kind || w.req.ParentSignID != prev.signID {
			return WriteResult{Refusal: &trail.Refusal{Rule: "immutable", Fix: "an entry keeps its kind and parent; write a new entry instead"}}, nil
		}
	}

	for _, r := range rules {
		ref, err := r(ctx, w)
		if err != nil || ref != nil {
			return WriteResult{Refusal: ref}, err
		}
		if w.merge != nil {
			return w.mergeAlias(ctx)
		}
	}

	human := w.req.Actor == "user"
	tier := "provisional"
	if human {
		tier = "verified"
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return WriteResult{}, err
	}
	defer tx.Rollback()

	if w.prev != nil && req.IfMatch != "" {
		var at string
		err := tx.QueryRowContext(ctx, `SELECT updated_at FROM entries WHERE id = ?`, w.prev.id).Scan(&at)
		if errors.Is(err, sql.ErrNoRows) {
			return WriteResult{Refusal: &trail.Refusal{Rule: "unknown_entry", Fix: "entry ids come from a sign or a read; check the id"}}, nil
		}
		if err != nil {
			return WriteResult{}, err
		}
		if at != req.IfMatch {
			return WriteResult{Refusal: refusal("stale_edit", "%s changed after you loaded it; reload to see the newer text, then make your edit again", w.prev.id)}, nil
		}
	}

	var res WriteResult
	switch {
	case w.prev != nil && w.staged.Staged && w.prev.stagedBranch == "":
		w.over = w.prev.id
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM versions WHERE entry_id = ?`, w.over).Scan(&w.overVersion); err != nil {
			return WriteResult{}, err
		}
		id, err := w.create(ctx, tx, tier)
		if err != nil {
			return WriteResult{}, err
		}
		res = WriteResult{EntryID: id, Tier: tier, Staged: w.staged.Branch}
	case w.prev == nil:
		id, err := w.create(ctx, tx, tier)
		if err != nil {
			return WriteResult{}, err
		}
		res = WriteResult{EntryID: id, Tier: tier, Staged: w.staged.Branch}
	case !human && w.prev.tier == "verified":
		id, err := w.create(ctx, tx, "provisional")
		if err != nil {
			return WriteResult{}, err
		}
		if err := addLink(ctx, tx, id, w.prev.id, "conflicts_with"); err != nil {
			return WriteResult{}, err
		}
		res = WriteResult{EntryID: id, Tier: "provisional",
			Refusal: &trail.Refusal{Rule: "verified", Fix: fmt.Sprintf(
				"%s is verified and an agent cannot overwrite it; your text was stored as provisional entry %s with a conflicts_with link, and a human resolves the pair in the browser",
				w.prev.id, id)},
			Conflict: &ConflictOffer{VerifiedID: w.prev.id, NewID: id}}
	default:
		if w.req.Edit {
			tier = w.prev.stored
		}
		if err := w.update(ctx, tx, tier); err != nil {
			return WriteResult{}, err
		}
		res = WriteResult{EntryID: w.prev.id, Tier: tier, Staged: w.prev.stagedBranch}
	}
	if err := search.Refresh(ctx, tx, res.EntryID); err != nil {
		return WriteResult{}, err
	}
	if w.supersede != nil && res.Conflict == nil {
		if err := w.markSuperseded(ctx, tx, res.EntryID); err != nil {
			return WriteResult{}, err
		}
	}
	return res, tx.Commit()
}

// mergeAlias stores the request's read_when as an alias on the duplicate hit and stores nothing else.
func (w *writer) mergeAlias(ctx context.Context) (WriteResult, error) {
	hit := w.merge
	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return WriteResult{}, err
	}
	defer tx.Rollback()
	added, err := tx.ExecContext(ctx, `INSERT INTO entry_aliases (entry_id, read_when) SELECT ?1, ?2
		WHERE ?2 != (SELECT read_when FROM entries WHERE id = ?1)
		  AND NOT EXISTS (SELECT 1 FROM entry_aliases WHERE entry_id = ?1 AND read_when = ?2)`, hit.EntryID, w.req.ReadWhen)
	if err != nil {
		return WriteResult{}, err
	}
	if err := search.Refresh(ctx, tx, hit.EntryID); err != nil {
		return WriteResult{}, err
	}
	if n, _ := added.RowsAffected(); n > 0 {
		if err := touch(ctx, tx, hit.EntryID); err != nil {
			return WriteResult{}, err
		}
	}
	return WriteResult{EntryID: hit.EntryID, Tier: hit.Tier, Merged: true}, tx.Commit()
}

// markSuperseded links the new entry over the hit and retires the hit from default search.
func (w *writer) markSuperseded(ctx context.Context, tx *sql.Tx, newID string) error {
	return retire(ctx, tx, w.supersede.EntryID, newID)
}

func retire(ctx context.Context, tx *sql.Tx, old, newID string) error {
	if err := addLink(ctx, tx, newID, old, "supersedes"); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE entries SET superseded_by = ?, tier = 'dormant', updated_at = ? WHERE id = ?`, newID, store.Now(), old)
	return err
}

// touch moves an entry's updated_at, which is the concurrency token of a browser edit: every change to an entry or to what the browser shows of it must call it or set updated_at in its own statement.
func touch(ctx context.Context, tx *sql.Tx, id string) error {
	_, err := tx.ExecContext(ctx, `UPDATE entries SET updated_at = ? WHERE id = ?`, store.Now(), id)
	return err
}

var (
	ErrUnknownEntry   = errors.New("unknown entry")
	ErrNotConflicting = errors.New("the two entries are not linked by conflicts_with")
)

// Supersede is the human resolution of a conflicting pair: old goes dormant in favour of by, and by keeps its tier.
func Supersede(ctx context.Context, db *sql.DB, old, by string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(DISTINCT id) FROM entries WHERE id IN (?, ?)`, old, by).Scan(&n); err != nil {
		return err
	}
	if n != len(map[string]bool{old: true, by: true}) {
		return ErrUnknownEntry
	}
	lo, hi := old, by
	if lo > hi {
		lo, hi = hi, lo
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM links WHERE from_id = ? AND to_id = ? AND relation = 'conflicts_with'`, lo, hi).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return ErrNotConflicting
	}
	if err := retire(ctx, tx, old, by); err != nil {
		return err
	}
	return tx.Commit()
}

// logScores appends the write's measured scores as an events row of its own beside the recorder's write event.
func (w *writer) logScores(ctx context.Context, res WriteResult) error {
	target := res.EntryID
	if target == "" {
		target = w.req.EntryID
	}
	if target == "" {
		target = w.req.ParentSignID
	}
	result := "ok"
	if res.Refusal != nil {
		result = "refused:" + res.Refusal.Rule
	}
	b, err := json.Marshal(w.scores)
	if err != nil {
		return err
	}
	_, err = w.db.ExecContext(ctx, `INSERT INTO events (session_id, at, tool, target, result, shown_json) VALUES (?, ?, 'write_scores', ?, ?, ?)`,
		w.req.Actor, store.Now(), target, result, string(b))
	return err
}

// fillOmitted fills every field the update left out from the stored entry, so the rules judge the whole entry. An empty list differs from an absent one: tags and anchors sent as [] clear them.
// requires and see_also are not filled; an update replaces both.
func (w *writer) fillOmitted(ctx context.Context, orig WriteRequest) error {
	var title, readWhen, body, tags string
	var by, at sql.NullString
	err := w.db.QueryRowContext(ctx, `SELECT title, read_when, body_json, tags_json, decided_by, decided_at FROM entries WHERE id = ?`, w.prev.id).
		Scan(&title, &readWhen, &body, &tags, &by, &at)
	if err != nil {
		return err
	}
	r := &w.req
	if r.Title == "" {
		r.Title = title
	}
	if r.ReadWhen == "" {
		r.ReadWhen = readWhen
	}
	if strings.TrimSpace(r.BodyMarkdown) == "" {
		if r.BodyMarkdown, err = markdown.ToMarkdown(json.RawMessage(body)); err != nil {
			return err
		}
	}
	if orig.Tags == nil {
		if err := json.Unmarshal([]byte(tags), &r.Tags); err != nil {
			return err
		}
	}
	if orig.Anchors == nil {
		stored, err := anchor.Load(ctx, w.db, w.prev.id)
		if err != nil {
			return err
		}
		for _, a := range stored {
			a.ID, a.EntryID = 0, ""
			r.Anchors = append(r.Anchors, a)
		}
	}
	if r.DecidedBy == "" {
		r.DecidedBy = by.String
	}
	if r.DecidedAt == "" {
		r.DecidedAt = at.String
	}
	return nil
}

func loadExisting(ctx context.Context, db *sql.DB, id string) (*existing, *trail.Refusal, error) {
	e := &existing{id: id}
	var before sql.NullString
	var stagedBranch sql.NullString
	err := db.QueryRowContext(ctx, `SELECT sign_id, kind, tier, tier_before_stale, staged_branch FROM entries WHERE id = ?`, id).Scan(&e.signID, &e.kind, &e.tier, &before, &stagedBranch)
	e.stagedBranch = stagedBranch.String
	if errors.Is(err, sql.ErrNoRows) {
		return nil, &trail.Refusal{Rule: "unknown_entry", Fix: "entry ids come from a sign or a read; check the id"}, nil
	}
	if err != nil {
		return nil, nil, err
	}
	e.stored = e.tier
	// A stale entry is still the tier it had before its anchors failed.
	if e.tier == "stale" && before.Valid {
		e.tier = before.String
	}
	return e, nil, nil
}

func cleanList(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func (w *writer) create(ctx context.Context, tx *sql.Tx, tier string) (string, error) {
	id, err := store.FreeID(ctx, tx, store.Slug(w.req.Title, "entry"))
	if err != nil {
		return "", err
	}
	tags, _ := json.Marshal(append([]string{}, w.req.Tags...))
	now := store.Now()
	_, err = tx.ExecContext(ctx, `INSERT INTO entries (id, sign_id, kind, title, read_when, body_json, body_schema, tags_json, tier, decided_by, decided_at, distinct_reason, created_at, updated_at,
			staged_branch, staged_base_name, staged_base, staged_landing, stages_over, stages_over_version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NULLIF(?,''), NULLIF(?,''), NULLIF(?,''), ?, ?, NULLIF(?,''), NULLIF(?,''), NULLIF(?,''), NULLIF(?,''), NULLIF(?,''), NULLIF(?,0))`,
		id, w.req.ParentSignID, w.req.Kind, w.req.Title, w.req.ReadWhen, string(w.body), BodySchema, string(tags), tier,
		w.req.DecidedBy, w.req.DecidedAt, w.distinctReason, now, now,
		w.staged.Branch, w.staged.BaseName, w.staged.Base, w.staged.Landing, w.over, w.overVersion)
	if err != nil {
		return "", err
	}
	if err := w.storeParts(ctx, tx, id); err != nil {
		return "", err
	}
	if err := touchChangedPartners(ctx, tx, id, nil); err != nil {
		return "", err
	}
	return id, appendVersion(ctx, tx, id, w, false)
}

func (w *writer) update(ctx context.Context, tx *sql.Tx, tier string) error {
	id := w.prev.id
	tags, _ := json.Marshal(append([]string{}, w.req.Tags...))
	// An edit leaves tier and tier_before_stale as they are, so a stale entry stays stale until a confirm.
	setTier := `tier = ?, tier_before_stale = NULL,`
	if w.req.Edit {
		setTier = `tier = ?,`
	}
	_, err := tx.ExecContext(ctx, `UPDATE entries SET title = ?, read_when = ?, body_json = ?, body_schema = ?, tags_json = ?, `+setTier+`
		decided_by = NULLIF(?,''), decided_at = NULLIF(?,''), distinct_reason = COALESCE(NULLIF(?,''), distinct_reason), updated_at = ? WHERE id = ?`,
		w.req.Title, w.req.ReadWhen, string(w.body), BodySchema, string(tags), tier, w.req.DecidedBy, w.req.DecidedAt, w.distinctReason, store.Now(), id)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM anchors WHERE entry_id = ?`, id); err != nil {
		return err
	}
	before, err := seeAlsoPartners(ctx, tx, id)
	if err != nil {
		return err
	}
	// requires is directed from this entry; see_also is symmetric, so either end counts.
	if _, err := tx.ExecContext(ctx, `DELETE FROM links WHERE (from_id = ? AND relation = 'requires') OR (relation = 'see_also' AND (from_id = ? OR to_id = ?))`, id, id, id); err != nil {
		return err
	}
	if err := w.storeParts(ctx, tx, id); err != nil {
		return err
	}
	if err := touchChangedPartners(ctx, tx, id, before); err != nil {
		return err
	}
	return appendVersion(ctx, tx, id, w, true)
}

// storeParts writes the resolved anchors and the requested links of entry id, and files any tag the vocabulary lacks as undefined.
func (w *writer) storeParts(ctx context.Context, tx *sql.Tx, id string) error {
	for _, tag := range w.req.Tags {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO tags (id) VALUES (?)`, tag); err != nil {
			return err
		}
	}
	for _, a := range w.anchors {
		a.EntryID = id
		if err := insertAnchor(ctx, tx, a); err != nil {
			return err
		}
	}
	for _, to := range w.req.Requires {
		if to != id {
			if err := addLink(ctx, tx, id, to, "requires"); err != nil {
				return err
			}
		}
	}
	for _, to := range w.req.SeeAlso {
		if to != id {
			if err := addLink(ctx, tx, id, to, "see_also"); err != nil {
				return err
			}
		}
	}
	return nil
}

func insertAnchor(ctx context.Context, tx *sql.Tx, a anchor.Anchor) error {
	ok := 0
	if a.OK {
		ok = 1
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO anchors (entry_id, type, path_or_url, range_start, range_end, hash, fetched_at, last_checked_at, ok, extra)
		VALUES (?, ?, NULLIF(?,''), NULLIF(?,0), NULLIF(?,0), NULLIF(?,''), NULLIF(?,''), NULLIF(?,''), ?, NULLIF(?,''))`,
		a.EntryID, a.Type, a.PathOrURL, a.RangeStart, a.RangeEnd, a.Hash, a.FetchedAt, a.LastCheckedAt, ok, a.Extra)
	return err
}

// seeAlsoPartners lists the entries linked to id by see_also, from either end.
func seeAlsoPartners(ctx context.Context, tx *sql.Tx, id string) (map[string]bool, error) {
	rows, err := tx.QueryContext(ctx, `SELECT CASE WHEN from_id = ?1 THEN to_id ELSE from_id END FROM links WHERE relation = 'see_also' AND (from_id = ?1 OR to_id = ?1)`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out[p] = true
	}
	return out, rows.Err()
}

// touchChangedPartners moves updated_at of every entry that gained or lost a see_also link to id, because the browser shows and re-sends see_also from both ends.
func touchChangedPartners(ctx context.Context, tx *sql.Tx, id string, before map[string]bool) error {
	after, err := seeAlsoPartners(ctx, tx, id)
	if err != nil {
		return err
	}
	for p := range before {
		if !after[p] {
			if err := touch(ctx, tx, p); err != nil {
				return err
			}
		}
	}
	for p := range after {
		if !before[p] {
			if err := touch(ctx, tx, p); err != nil {
				return err
			}
		}
	}
	return nil
}

// addLink stores a link, ordering the ends of a symmetric relation so the pair exists once.
func addLink(ctx context.Context, tx *sql.Tx, from, to, relation string) error {
	if (relation == "see_also" || relation == "conflicts_with") && from > to {
		from, to = to, from
	}
	_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO links (from_id, to_id, relation) VALUES (?, ?, ?)`, from, to, relation)
	return err
}
