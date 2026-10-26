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
	"github.com/TheAmbitiousCorry/trails/internal/store"
	"github.com/TheAmbitiousCorry/trails/internal/trail"
)

// BodySchema is the content schema version stored with every body.
const BodySchema = 1

const (
	maxTitle    = 80
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
}

// ConflictOffer names the verified entry an agent tried to overwrite and the provisional entry the server stored instead.
type ConflictOffer struct {
	VerifiedID string `json:"verified_id"`
	NewID      string `json:"new_id"`
}

// WriteResult has Refusal set when a rule failed; EntryID and Tier then describe the entry stored anyway, which only a conflict does.
type WriteResult struct {
	EntryID  string
	Tier     string
	Refusal  *trail.Refusal
	Conflict *ConflictOffer
}

type existing struct {
	id, signID, kind, tier string
}

// writer carries one request through the rules; a rule fills the fields later rules and the insert read.
type writer struct {
	db      *sql.DB
	checker anchor.Checker
	req     WriteRequest
	prev    *existing
	body    json.RawMessage
	anchors []anchor.Anchor
}

type ruleFunc func(ctx context.Context, w *writer) (*trail.Refusal, error)

// rules run in spec order and the first refusal wins; rules 4 and 5 (duplicate, paraphrase) go between anchors and fanOut.
var rules = []ruleFunc{schema, wordCap, anchors, fanOut, reachability}

// Write runs the rules, then stores the entry. A refusal is returned in the result, an error only for a failure of the store itself.
func Write(ctx context.Context, db *sql.DB, checker anchor.Checker, req WriteRequest) (WriteResult, error) {
	w := &writer{db: db, checker: checker, req: req}
	w.req.Title = strings.TrimSpace(req.Title)
	w.req.ReadWhen = strings.TrimSpace(req.ReadWhen)
	w.req.Tags = cleanList(req.Tags)
	w.req.Requires = cleanList(req.Requires)
	w.req.SeeAlso = cleanList(req.SeeAlso)

	if req.EntryID != "" {
		prev, ref, err := loadExisting(ctx, db, req.EntryID)
		if err != nil || ref != nil {
			return WriteResult{Refusal: ref}, err
		}
		w.prev = prev
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

	var res WriteResult
	switch {
	case w.prev == nil:
		id, err := w.create(ctx, tx, tier)
		if err != nil {
			return WriteResult{}, err
		}
		res = WriteResult{EntryID: id, Tier: tier}
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
		if err := w.update(ctx, tx, tier); err != nil {
			return WriteResult{}, err
		}
		res = WriteResult{EntryID: w.prev.id, Tier: tier}
	}
	return res, tx.Commit()
}

func loadExisting(ctx context.Context, db *sql.DB, id string) (*existing, *trail.Refusal, error) {
	e := &existing{id: id}
	var before sql.NullString
	err := db.QueryRowContext(ctx, `SELECT sign_id, kind, tier, tier_before_stale FROM entries WHERE id = ?`, id).Scan(&e.signID, &e.kind, &e.tier, &before)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, &trail.Refusal{Rule: "unknown_entry", Fix: "entry ids come from a sign or a read; check the id"}, nil
	}
	if err != nil {
		return nil, nil, err
	}
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
	id, err := freeID(ctx, tx, slug(w.req.Title))
	if err != nil {
		return "", err
	}
	tags, _ := json.Marshal(append([]string{}, w.req.Tags...))
	now := store.Now()
	_, err = tx.ExecContext(ctx, `INSERT INTO entries (id, sign_id, kind, title, read_when, body_json, body_schema, tags_json, tier, decided_by, decided_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NULLIF(?,''), NULLIF(?,''), ?, ?)`,
		id, w.req.ParentSignID, w.req.Kind, w.req.Title, w.req.ReadWhen, string(w.body), BodySchema, string(tags), tier,
		w.req.DecidedBy, w.req.DecidedAt, now, now)
	if err != nil {
		return "", err
	}
	if err := w.storeParts(ctx, tx, id); err != nil {
		return "", err
	}
	return id, appendVersion(ctx, tx, id, w, false)
}

func (w *writer) update(ctx context.Context, tx *sql.Tx, tier string) error {
	id := w.prev.id
	tags, _ := json.Marshal(append([]string{}, w.req.Tags...))
	_, err := tx.ExecContext(ctx, `UPDATE entries SET title = ?, read_when = ?, body_json = ?, body_schema = ?, tags_json = ?, tier = ?, tier_before_stale = NULL,
		decided_by = NULLIF(?,''), decided_at = NULLIF(?,''), updated_at = ? WHERE id = ?`,
		w.req.Title, w.req.ReadWhen, string(w.body), BodySchema, string(tags), tier, w.req.DecidedBy, w.req.DecidedAt, store.Now(), id)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM anchors WHERE entry_id = ?`, id); err != nil {
		return err
	}
	// requires is directed from this entry; see_also is symmetric, so either end counts.
	if _, err := tx.ExecContext(ctx, `DELETE FROM links WHERE (from_id = ? AND relation = 'requires') OR (relation = 'see_also' AND (from_id = ? OR to_id = ?))`, id, id, id); err != nil {
		return err
	}
	if err := w.storeParts(ctx, tx, id); err != nil {
		return err
	}
	return appendVersion(ctx, tx, id, w, true)
}

// storeParts writes the resolved anchors and the requested links of entry id.
func (w *writer) storeParts(ctx context.Context, tx *sql.Tx, id string) error {
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

// addLink stores a link, ordering the ends of a symmetric relation so the pair exists once.
func addLink(ctx context.Context, tx *sql.Tx, from, to, relation string) error {
	if (relation == "see_also" || relation == "conflicts_with") && from > to {
		from, to = to, from
	}
	_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO links (from_id, to_id, relation) VALUES (?, ?, ?)`, from, to, relation)
	return err
}
