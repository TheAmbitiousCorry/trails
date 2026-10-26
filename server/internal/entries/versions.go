package entries

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"

	"github.com/TheAmbitiousCorry/trails/internal/store"
)

type versionAnchor struct {
	Type  string `json:"type"`
	Path  string `json:"path,omitempty"`
	Start int    `json:"start,omitempty"`
	End   int    `json:"end,omitempty"`
	Hash  string `json:"hash,omitempty"`
	Extra string `json:"extra,omitempty"`
}

// anchorsJSON leaves out check times and row ids so a rewrite that changes neither the claim nor its hash compares equal.
func (w *writer) anchorsJSON() string {
	out := make([]versionAnchor, 0, len(w.anchors))
	for _, a := range w.anchors {
		out = append(out, versionAnchor{a.Type, a.PathOrURL, a.RangeStart, a.RangeEnd, a.Hash, a.Extra})
	}
	b, _ := json.Marshal(out)
	return string(b)
}

// canonical re-marshals through a generic value so key order never decides equality.
func canonical(raw string) string {
	var v any
	if json.Unmarshal([]byte(raw), &v) != nil {
		return raw
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// appendVersion records the write, except an update that left body, title, read_when and anchors as the last version had them.
func appendVersion(ctx context.Context, tx *sql.Tx, id string, w *writer, update bool) error {
	anchorsJSON := w.anchorsJSON()
	if update {
		var body, title, readWhen, prevAnchors string
		err := tx.QueryRowContext(ctx, `SELECT body_json, title, read_when, anchors_json FROM versions WHERE entry_id = ? ORDER BY id DESC LIMIT 1`, id).
			Scan(&body, &title, &readWhen, &prevAnchors)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil && canonical(body) == canonical(string(w.body)) && title == w.req.Title && readWhen == w.req.ReadWhen && prevAnchors == anchorsJSON {
			return nil
		}
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO versions (entry_id, body_json, title, read_when, anchors_json, actor, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, string(w.body), w.req.Title, w.req.ReadWhen, anchorsJSON, w.req.Actor, store.Now())
	return err
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

const maxSlug = 60

func slug(title string) string {
	s := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(title), "-"), "-")
	if len(s) > maxSlug {
		s = strings.Trim(s[:maxSlug], "-")
	}
	if s == "" {
		return "entry"
	}
	return s
}

// freeID returns base, or base-2, base-3 and so on, the first that no entry or sign already holds.
func freeID(ctx context.Context, tx *sql.Tx, base string) (string, error) {
	for n := 1; ; n++ {
		id := base
		if n > 1 {
			id = base + "-" + strconv.Itoa(n)
		}
		var taken int
		err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM entries WHERE id = ?) OR EXISTS(SELECT 1 FROM signs WHERE id = ?)`, id, id).Scan(&taken)
		if err != nil || taken == 0 {
			return id, err
		}
	}
}
