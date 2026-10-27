package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/TheAmbitiousCorry/trails/internal/entries"
	"github.com/TheAmbitiousCorry/trails/internal/signs"
	"github.com/TheAmbitiousCorry/trails/internal/store"
	"github.com/TheAmbitiousCorry/trails/internal/trail"
)

type createSignIn struct {
	ParentID string   `json:"parent_id"`
	Title    string   `json:"title"`
	Move     []string `json:"move"`
}

type createSignOut struct {
	SignID string `json:"sign_id"`
	Title  string `json:"title"`
	Moved  int    `json:"moved"`
}

// createSign is the browser's write_sign. The event carries session_id 'user', the id trail.ConfirmHuman logs under and the rejection log reads as actor kind user.
func (a *api) createSign(r *http.Request) (any, error) {
	var in createSignIn
	if err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20)).Decode(&in); err != nil {
		return nil, badRequest("body must be JSON with parent_id, title and move")
	}
	c, err := signs.CreateSign(r.Context(), a.st.DB, signs.CreateRequest{ParentID: in.ParentID, Title: in.Title, Move: in.Move})
	target, result := in.ParentID, "ok"
	var ref *trail.Refusal
	switch {
	case err == nil:
		target = c.SignID
	case errors.As(err, &ref):
		result = "refused:" + ref.Rule
	default:
		result = "error"
	}
	if _, lerr := a.st.DB.ExecContext(r.Context(), `INSERT INTO events (session_id, at, tool, target, result) VALUES ('user', ?, 'write_sign', ?, ?)`, store.Now(), target, result); lerr != nil && err == nil {
		return nil, lerr
	}
	switch {
	case ref != nil && ref.Rule == "unknown_sign":
		return nil, notFound(strings.TrimSpace(ref.Error()))
	case ref != nil:
		return nil, badRequest(ref.Error())
	case err != nil:
		return nil, err
	}
	return createSignOut{SignID: c.SignID, Title: c.Title, Moved: c.Moved}, nil
}

type supersedeIn struct {
	By string `json:"by"`
}

// supersede is the browser's resolution of a conflicting pair, logged under session id 'user' like createSign.
func (a *api) supersede(r *http.Request) (any, error) {
	var in supersedeIn
	if err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20)).Decode(&in); err != nil || in.By == "" {
		return nil, badRequest("body must be JSON with by")
	}
	id := r.PathValue("id")
	err := entries.Supersede(r.Context(), a.st.DB, id, in.By)
	result := "ok"
	switch {
	case errors.Is(err, entries.ErrUnknownEntry):
		result = "refused:unknown_entry"
	case errors.Is(err, entries.ErrNotConflicting):
		result = "refused:not_conflicting"
	case err != nil:
		result = "error"
	}
	if _, lerr := a.st.DB.ExecContext(r.Context(), `INSERT INTO events (session_id, at, tool, target, result) VALUES ('user', ?, 'supersede', ?, ?)`, store.Now(), id, result); lerr != nil && err == nil {
		return nil, lerr
	}
	switch {
	case errors.Is(err, entries.ErrUnknownEntry):
		return nil, notFound(err.Error())
	case errors.Is(err, entries.ErrNotConflicting):
		return nil, badRequest(err.Error())
	case err != nil:
		return nil, err
	}
	return map[string]string{"entry_id": id, "superseded_by": in.By}, nil
}
