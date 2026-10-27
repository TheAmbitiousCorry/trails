package signs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

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
		var body string
		err := db.QueryRowContext(ctx, `SELECT body_json FROM entries WHERE sign_id = ? AND kind = 'summary' AND tier != 'dormant' ORDER BY created_at, id LIMIT 1`, cur).Scan(&body)
		text := ""
		if err == nil {
			text = content.PlainText(json.RawMessage(body))
		} else if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		path = append([]PathSign{{ID: cur, Title: title, Summary: firstParagraph(text, title)}}, path...)
		cur = parent.String
	}
	return path, nil
}
