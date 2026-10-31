package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/TheAmbitiousCorry/trails/internal/anchor"
	"github.com/TheAmbitiousCorry/trails/internal/entries"
	"github.com/TheAmbitiousCorry/trails/internal/store"
	"github.com/TheAmbitiousCorry/trails/internal/trail"
)

// logUserEvent records a browser write under session id 'user', the id trail.ConfirmHuman logs under and the rejection log reads as actor kind user.
func (a *api) logUserEvent(ctx context.Context, tool, target, result string) error {
	_, err := a.st.DB.ExecContext(ctx, `INSERT INTO events (session_id, at, tool, target, result) VALUES ('user', ?, ?, ?, ?)`, store.Now(), tool, target, result)
	return err
}

// anchorForm is an anchor as the edit form shows it and the write tool takes it: the same fields as the MCP's anchors.
type anchorForm struct {
	Type    string `json:"type"`
	Path    string `json:"path,omitempty"`
	Start   int    `json:"start,omitempty"`
	End     int    `json:"end,omitempty"`
	URL     string `json:"url,omitempty"`
	Command string `json:"command,omitempty"`
	SHA     string `json:"sha,omitempty"`
}

// toAnchor is the inverse of formOf and matches the MCP's mapping onto the stored columns.
func (f anchorForm) toAnchor() anchor.Anchor {
	out := anchor.Anchor{Type: f.Type, PathOrURL: f.Path, RangeStart: f.Start, RangeEnd: f.End, Hash: f.SHA}
	switch f.Type {
	case "url":
		out.PathOrURL = f.URL
	case "produced_by":
		out.PathOrURL = f.Command
	case "transcript":
		out.PathOrURL, out.Extra = "", f.Path
	}
	return out
}

func formOf(a anchor.Anchor) anchorForm {
	f := anchorForm{Type: a.Type, Path: a.PathOrURL, Start: a.RangeStart, End: a.RangeEnd}
	switch a.Type {
	case "url":
		f.Path, f.URL = "", a.PathOrURL
	case "produced_by":
		f.Path, f.Command, f.SHA = "", a.PathOrURL, a.Hash
	case "commit":
		f.SHA = a.Hash
	case "transcript":
		f.Path = a.Extra
	}
	return f
}

type editIn struct {
	ETag           string       `json:"etag"`
	Title          string       `json:"title"`
	ReadWhen       string       `json:"read_when"`
	Body           string       `json:"body_markdown"`
	Tags           []string     `json:"tags"`
	Anchors        []anchorForm `json:"anchors"`
	Requires       []string     `json:"requires"`
	SeeAlso        []string     `json:"see_also"`
	DecidedBy      string       `json:"decided_by"`
	DecidedAt      string       `json:"decided_at"`
	Resolution     string       `json:"resolution"`
	DistinctReason string       `json:"distinct_reason"`
}

// edit is the browser's save of an entry: entries.Write as actor user with Edit, so the entry keeps its tier and a stale one stays stale until Confirm.
// A refusal is 400 "rule: fix", the shape SignPage.vue parses; stale_edit is 409 with the entry as it is now.
func (a *api) edit(r *http.Request) (any, error) {
	var in editIn
	if err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20)).Decode(&in); err != nil || in.ETag == "" {
		return nil, badRequest("body must be JSON with etag and the entry's fields")
	}
	id := r.PathValue("id")
	req := entries.WriteRequest{Actor: "user", Edit: true, IfMatch: in.ETag, EntryID: id,
		Title: in.Title, ReadWhen: in.ReadWhen, BodyMarkdown: in.Body, Tags: in.Tags, Requires: in.Requires, SeeAlso: in.SeeAlso,
		DecidedBy: in.DecidedBy, DecidedAt: in.DecidedAt, Resolution: in.Resolution, DistinctReason: in.DistinctReason,
		Anchors: []anchor.Anchor{}}
	if req.Tags == nil {
		req.Tags = []string{}
	}
	for _, x := range in.Anchors {
		req.Anchors = append(req.Anchors, x.toAnchor())
	}
	res, err := entries.Write(r.Context(), a.st.DB, a.proj.Checker(), req)
	result := "ok"
	switch {
	case err != nil:
		result = "error"
	case res.Refusal != nil:
		result = "refused:" + res.Refusal.Rule
	}
	if lerr := a.logUserEvent(r.Context(), "edit", id, result); lerr != nil && err == nil {
		return nil, lerr
	}
	if err != nil {
		return nil, err
	}
	if ref := res.Refusal; ref != nil {
		switch ref.Rule {
		case "unknown_entry":
			return nil, notFound(ref.Error())
		case "stale_edit":
			cur, cerr := a.entry(r)
			if cerr != nil {
				return nil, cerr
			}
			body, _ := cur.(map[string]any)
			body["refusal"] = ref.Error()
			return nil, &statusError{code: http.StatusConflict, msg: ref.Error(), json: body}
		}
		return nil, badRequest(ref.Error())
	}
	return a.entry(r)
}

// confirm is the browser's Confirm: trail.ConfirmHuman re-checks the anchors and makes the entry verified. A stale anchor refuses it, naming the anchor.
func (a *api) confirm(r *http.Request) (any, error) {
	id := r.PathValue("id")
	_, err := trail.ConfirmHuman(r.Context(), a.st.DB, a.proj.Checker(), id)
	var ref *trail.Refusal
	// A success is logged by ConfirmHuman itself.
	if errors.As(err, &ref) {
		if lerr := a.logUserEvent(r.Context(), "confirm", id, "refused:"+ref.Rule); lerr != nil {
			return nil, lerr
		}
		if ref.Rule == "unknown_entry" {
			return nil, notFound(ref.Error())
		}
		return nil, badRequest(ref.Error())
	}
	if err != nil {
		return nil, err
	}
	return a.entry(r)
}
