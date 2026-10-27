package signs

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/TheAmbitiousCorry/trails/internal/store"
	"github.com/TheAmbitiousCorry/trails/internal/trail"
)

const maxSignTitle = 80

// CreateRequest is one new sign under ParentID, taking the entries and child signs in Move with it.
type CreateRequest struct {
	ParentID string
	Title    string
	Move     []string
}

// Created is what CreateSign made.
type Created struct {
	SignID string
	Title  string
	Moved  int
}

// CreateSign adds a sign and moves the listed entries and child signs under it in one transaction. The parent's cap is checked as the parent will stand afterwards, and the new sign's own cap over the moved entries.
func CreateSign(ctx context.Context, db *sql.DB, req CreateRequest) (Created, error) {
	var out Created
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()

	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM signs WHERE id = ?`, req.ParentID).Scan(&exists); err != nil {
		return out, err
	}
	if exists == 0 {
		return out, &trail.Refusal{Rule: "unknown_sign", Fix: "sign ids come from look; check parent_sign_id"}
	}
	title := strings.TrimSpace(req.Title)
	if title == "" || utf8.RuneCountInString(title) > maxSignTitle || strings.ContainsAny(title, "\r\n") {
		return out, &trail.Refusal{Rule: "schema:title", Fix: "title is one line of 1 to 80 characters"}
	}

	var moveEntries, moveSigns []string
	moving := map[string]bool{}
	for _, id := range req.Move {
		id = strings.TrimSpace(id)
		if id == "" || moving[id] {
			continue
		}
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM entries WHERE id = ? AND sign_id = ?`, id, req.ParentID).Scan(&n); err != nil {
			return out, err
		}
		if n > 0 {
			moveEntries = append(moveEntries, id)
		} else {
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM signs WHERE id = ? AND parent_id = ?`, id, req.ParentID).Scan(&n); err != nil {
				return out, err
			}
			if n == 0 {
				return out, &trail.Refusal{Rule: "move_scope", Fix: "entries and signs to move must hang directly under parent_sign_id"}
			}
			moveSigns = append(moveSigns, id)
		}
		moving[id] = true
	}

	limit, err := fanOutCap(ctx, tx)
	if err != nil {
		return out, err
	}
	p, err := load(ctx, tx)
	if err != nil {
		return out, err
	}
	var movedEntries, leftEntries []*entryRow
	for _, e := range p.bySign[req.ParentID] {
		if moving[e.ID] {
			movedEntries = append(movedEntries, e)
		} else {
			leftEntries = append(leftEntries, e)
		}
	}
	var leftKids []string
	for _, k := range p.kids[req.ParentID] {
		if !moving[k] {
			leftKids = append(leftKids, k)
		}
	}
	if after := len(p.unitsOf(leftKids, leftEntries)) + 1; after > limit {
		return out, &trail.Refusal{Rule: "split this fork", Fix: p.proposeSplit(p.units(req.ParentID), limit, "this sign already has")}
	}
	own := p.unitsOf(moveSigns, movedEntries)
	if len(own) > limit {
		return out, &trail.Refusal{Rule: "split this fork", Fix: p.proposeSplit(own, limit, "the new sign would hold")}
	}

	id, err := store.FreeID(ctx, tx, store.Slug(title, "sign"))
	if err != nil {
		return out, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO signs (id, parent_id, title, position)
		VALUES (?, ?, ?, (SELECT COALESCE(MAX(position), -1) + 1 FROM signs WHERE parent_id = ?))`, id, req.ParentID, title, req.ParentID); err != nil {
		return out, err
	}
	for _, mid := range moveEntries {
		res, err := tx.ExecContext(ctx, `UPDATE entries SET sign_id = ? WHERE id = ? AND sign_id = ?`, id, mid, req.ParentID)
		if err != nil {
			return out, err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return out, errors.New("entry " + mid + " moved while the sign was being created")
		}
	}
	for _, sid := range moveSigns {
		res, err := tx.ExecContext(ctx, `UPDATE signs SET parent_id = ?, position = (SELECT COALESCE(MAX(position), -1) + 1 FROM signs WHERE parent_id = ?)
			WHERE id = ? AND parent_id = ?`, id, id, sid, req.ParentID)
		if err != nil {
			return out, err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return out, errors.New("sign " + sid + " moved while the sign was being created")
		}
	}
	if err := tx.Commit(); err != nil {
		return out, err
	}
	return Created{SignID: id, Title: title, Moved: len(moving)}, nil
}
