package entries

import (
	"context"
	"database/sql"
)

// Edge is one line for a linked entry: enough to decide whether to read it.
type Edge struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	ReadWhen string `json:"read_when"`
	Tier     string `json:"tier"`
	Relation string `json:"relation"`
}

// Chain returns the ids id requires, transitively, depth first with the earliest prerequisite first. A visited set tolerates cycles.
func Chain(ctx context.Context, db *sql.DB, id string) ([]string, error) {
	visited := map[string]bool{id: true}
	var out []string
	var visit func(cur string) error
	visit = func(cur string) error {
		reqs, err := requires(ctx, db, cur)
		if err != nil {
			return err
		}
		for _, r := range reqs {
			if visited[r] {
				continue
			}
			visited[r] = true
			if err := visit(r); err != nil {
				return err
			}
			out = append(out, r)
		}
		return nil
	}
	return out, visit(id)
}

func requires(ctx context.Context, db *sql.DB, id string) ([]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT to_id FROM links WHERE from_id = ? AND relation = 'requires' ORDER BY rowid`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Edges returns the see_also links of id and every backlink: each entry linking to it by any relation, symmetric relations counted from both ends.
func Edges(ctx context.Context, db *sql.DB, id string) (related, backlinks []Edge, err error) {
	const base = `SELECT e.id, e.title, e.read_when, e.tier, l.relation FROM links l
		JOIN entries e ON e.id = CASE WHEN l.from_id = ?1 THEN l.to_id ELSE l.from_id END WHERE `
	related, err = edges(ctx, db, base+`l.relation = 'see_also' AND (l.from_id = ?1 OR l.to_id = ?1) ORDER BY l.rowid`, id)
	if err != nil {
		return nil, nil, err
	}
	backlinks, err = edges(ctx, db, base+`(l.to_id = ?1 OR (l.from_id = ?1 AND l.relation IN ('see_also', 'conflicts_with'))) ORDER BY l.rowid`, id)
	return related, backlinks, err
}

func edges(ctx context.Context, db *sql.DB, query, id string) ([]Edge, error) {
	rows, err := db.QueryContext(ctx, query, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Edge
	for rows.Next() {
		var e Edge
		if err := rows.Scan(&e.ID, &e.Title, &e.ReadWhen, &e.Tier, &e.Relation); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
