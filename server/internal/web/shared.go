package web

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/TheAmbitiousCorry/trails/internal/content/markdown"
)

// effTier is the tier a reader sees: stale when the stored tier or any anchor says so, matching anchor.StaleCount.
const effTier = `CASE WHEN e.tier = 'stale' OR EXISTS(SELECT 1 FROM anchors a WHERE a.entry_id = e.id AND a.ok = 0) THEN 'stale' ELSE e.tier END`

// readsSince counts successful reads of the entry from the given time on; an empty time counts all of them.
const readsSince = `(SELECT COUNT(*) FROM events ev WHERE ev.tool = 'read' AND ev.result = 'ok' AND ev.target = e.id AND ev.at >= ?)`

func since30() string { return time.Now().UTC().AddDate(0, 0, -30).Format(time.RFC3339Nano) }

type pathStep struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// signPath returns the signs from the root down to and including id.
func signPath(ctx context.Context, db *sql.DB, id string) ([]pathStep, error) {
	rows, err := db.QueryContext(ctx, `WITH RECURSIVE up(id, parent_id, title, depth) AS (
			SELECT id, parent_id, title, 0 FROM signs WHERE id = ?
			UNION ALL
			SELECT s.id, s.parent_id, s.title, up.depth + 1 FROM signs s JOIN up ON s.id = up.parent_id)
		SELECT id, title FROM up ORDER BY depth DESC`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []pathStep{}
	for rows.Next() {
		var p pathStep
		if err := rows.Scan(&p.ID, &p.Title); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func decodeTags(raw string) []string {
	tags := []string{}
	json.Unmarshal([]byte(raw), &tags)
	if tags == nil {
		return []string{}
	}
	return tags
}

// bodyMarkdown renders a stored body; a body that fails validation comes back as an error string instead of failing the page.
func bodyMarkdown(raw string) (md, problem string) {
	md, err := markdown.ToMarkdown(json.RawMessage(raw))
	if err != nil {
		return "", err.Error()
	}
	return md, ""
}

type rowScanner = *sql.Rows

type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// collect runs a query and scans every row with scan.
func collect[T any](ctx context.Context, db queryer, query string, args []any, scan func(*sql.Rows) (T, error)) ([]T, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []T{}
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
