package signs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/TheAmbitiousCorry/trails/internal/view"

	"github.com/TheAmbitiousCorry/trails/internal/content"
)

// PathSign is one sign on the way from the root to an entry.
type PathSign struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Summary string `json:"summary"`
}

// PathTo returns the signs from the root to the home sign of the entry, each with the summary line look shows for it.
func PathTo(ctx context.Context, db querier, entryID string) ([]PathSign, error) {
	var cur string
	if err := db.QueryRowContext(ctx, `SELECT sign_id FROM entries WHERE id = ?`, entryID).Scan(&cur); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return []PathSign{}, nil
		}
		return nil, err
	}
	path := []PathSign{}
	seen := map[string]bool{}
	for cur != "" && !seen[cur] {
		seen[cur] = true
		var title string
		var parent sql.NullString
		if err := db.QueryRowContext(ctx, `SELECT title, parent_id FROM signs WHERE id = ?`, cur).Scan(&title, &parent); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				break
			}
			return nil, err
		}
		text, err := signSummary(ctx, db, cur)
		if err != nil {
			return nil, err
		}
		path = append([]PathSign{{ID: cur, Title: title, Summary: firstParagraph(text, title)}}, path...)
		cur = parent.String
	}
	return path, nil
}

// signSummary is the plain text of the oldest summary under the sign that the call's Viewer sees.
func signSummary(ctx context.Context, db querier, signID string) (string, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, body_json FROM entries WHERE sign_id = ? AND kind = 'summary' AND tier != 'dormant' ORDER BY created_at, id`, signID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	viewer := view.From(ctx)
	for rows.Next() {
		var id, body string
		if err := rows.Scan(&id, &body); err != nil {
			return "", err
		}
		if viewer.Sees(id) {
			return content.PlainText(json.RawMessage(body)), nil
		}
	}
	return "", rows.Err()
}
