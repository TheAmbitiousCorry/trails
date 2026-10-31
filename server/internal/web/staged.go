package web

import (
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/TheAmbitiousCorry/trails/internal/gitx"
	"github.com/TheAmbitiousCorry/trails/internal/trail"
)

type stagedOver struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Tier  string `json:"tier"`
}

// stagedRow's state is abandoned (its branch is gone, the entry dormant), committed (the landing commit is pinned) or branch_only (the work is uncommitted or in files the entry does not anchor).
type stagedRow struct {
	ID         string      `json:"id"`
	Title      string      `json:"title"`
	Kind       string      `json:"kind"`
	Tier       string      `json:"tier"`
	State      string      `json:"state"`
	Landing    string      `json:"landing"`
	LastCheck  string      `json:"last_check"`
	StagesOver *stagedOver `json:"stages_over"`
}

type stagedBranch struct {
	Branch  string      `json:"branch"`
	Base    string      `json:"base"`
	Alive   bool        `json:"alive"`
	Entries []stagedRow `json:"entries"`
}

// staged lists every entry held by a branch, grouped by branch. The browser is not a session on any branch, so it sees them all.
func (a *api) staged(r *http.Request) (any, error) {
	ctx := r.Context()
	type held struct {
		stagedRow
		branch, base string
	}
	rows, err := collect(ctx, a.st.DB, `SELECT e.id, e.title, e.kind, `+effTier+`, e.tier = 'dormant', e.staged_branch, COALESCE(e.staged_base_name, ''), COALESCE(e.staged_landing, ''),
			COALESCE((SELECT MAX(last_checked_at) FROM anchors WHERE entry_id = e.id), ''),
			COALESCE(o.id, ''), COALESCE(o.title, ''), COALESCE(o.tier, '')
		FROM entries e LEFT JOIN entries o ON o.id = e.stages_over
		WHERE e.staged_branch IS NOT NULL ORDER BY e.staged_branch, e.created_at, e.id`, nil,
		func(rs *sql.Rows) (held, error) {
			var h held
			var dormant bool
			var o stagedOver
			err := rs.Scan(&h.ID, &h.Title, &h.Kind, &h.Tier, &dormant, &h.branch, &h.base, &h.Landing, &h.LastCheck, &o.ID, &o.Title, &o.Tier)
			switch {
			case dormant:
				h.State = "abandoned"
			case h.Landing != "":
				h.State = "committed"
			default:
				h.State = "branch_only"
			}
			if o.ID != "" {
				h.StagesOver = &o
			}
			return h, err
		})
	if err != nil {
		return nil, err
	}
	top := a.proj.Checker().Repo
	branches := []stagedBranch{}
	for _, h := range rows {
		if n := len(branches); n == 0 || branches[n-1].Branch != h.branch {
			_, alive := gitx.BranchRef(ctx, top, h.branch)
			branches = append(branches, stagedBranch{Branch: h.branch, Base: h.base, Alive: alive, Entries: []stagedRow{}})
		}
		b := &branches[len(branches)-1]
		b.Entries = append(b.Entries, h.stagedRow)
	}
	return map[string]any{"branches": branches}, nil
}

// promote is the human's by-hand promotion of a staged entry, logged under session id 'user' like supersede. It runs the code the sweep runs.
func (a *api) promote(r *http.Request) (any, error) {
	id := r.PathValue("id")
	err := trail.PromoteByHand(r.Context(), a.st.DB, a.proj.Checker(), id, time.Now)
	var result string
	switch {
	case err == nil:
		result = "ok"
	case errors.Is(err, trail.ErrNotStaged):
		result = "refused:not_staged"
	case errors.Is(err, trail.ErrUnknownStaged):
		result = "refused:unknown_entry"
	default:
		result = "error"
	}
	// A success is logged inside the promotion's own transaction.
	if result != "ok" {
		if lerr := a.logUserEvent(r.Context(), "promote", id, result); lerr != nil && err == nil {
			return nil, lerr
		}
	}
	switch {
	case errors.Is(err, trail.ErrNotStaged):
		return nil, badRequest(err.Error())
	case errors.Is(err, trail.ErrUnknownStaged):
		return nil, notFound(err.Error())
	case err != nil:
		return nil, err
	}
	var superseded sql.NullString
	if err := a.st.DB.QueryRowContext(r.Context(), `SELECT o.id FROM entries c JOIN entries o ON o.id = c.stages_over AND o.superseded_by = c.id WHERE c.id = ?`, id).Scan(&superseded); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	return map[string]any{"entry_id": id, "replaced": superseded.String}, nil
}
