package web

import (
	"bufio"
	"database/sql"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/TheAmbitiousCorry/trails/internal/anchor"
	"github.com/TheAmbitiousCorry/trails/internal/entries"
)

type versionRow struct {
	ID        int64  `json:"id"`
	Actor     string `json:"actor"`
	Title     string `json:"title"`
	ReadWhen  string `json:"read_when"`
	CreatedAt string `json:"created_at"`
}

type linkRow struct {
	Relation string `json:"relation"`
	ID       string `json:"id"`
	Title    string `json:"title"`
	Tier     string `json:"tier"`
}

func (a *api) entry(r *http.Request) (any, error) {
	ctx, db := r.Context(), a.st.DB
	id := r.PathValue("id")

	var e signEntry
	var tags, body string
	var signID string
	var supersededBy, promotedBy, decidedBy, decidedAt, createdAt sql.NullString
	err := db.QueryRowContext(ctx, `SELECT e.id, e.sign_id, e.title, e.kind, `+effTier+`, e.read_when, e.tags_json, e.body_json, `+readsSince+`,
			e.updated_at, e.created_at, e.superseded_by, e.promoted_by, e.decided_by, e.decided_at
		FROM entries e WHERE e.id = ?`, since30(), id).
		Scan(&e.ID, &signID, &e.Title, &e.Kind, &e.Tier, &e.ReadWhen, &tags, &body, &e.Reads30d, &e.UpdatedAt, &createdAt, &supersededBy, &promotedBy, &decidedBy, &decidedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFound("unknown entry")
	}
	if err != nil {
		return nil, err
	}
	e.Tags = decodeTags(tags)
	e.Body, e.BodyProblem = bodyMarkdown(body)
	chain, err := entries.Chain(ctx, db, id)
	if err != nil {
		return nil, err
	}
	e.ChainLength = len(chain)
	if e.Anchors, err = anchor.Load(ctx, db, id); err != nil {
		return nil, err
	}
	if e.Anchors == nil {
		e.Anchors = []anchor.Anchor{}
	}

	path, err := signPath(ctx, db, signID)
	if err != nil {
		return nil, err
	}
	versions, err := collect(ctx, db, `SELECT id, actor, title, read_when, created_at FROM versions WHERE entry_id = ? ORDER BY id DESC`, []any{id},
		func(rs *sql.Rows) (versionRow, error) {
			var v versionRow
			return v, rs.Scan(&v.ID, &v.Actor, &v.Title, &v.ReadWhen, &v.CreatedAt)
		})
	if err != nil {
		return nil, err
	}
	scanLink := func(rs *sql.Rows) (linkRow, error) {
		var l linkRow
		return l, rs.Scan(&l.Relation, &l.ID, &l.Title, &l.Tier)
	}
	// Directed relations read forward from this entry; the two symmetric ones read from either end.
	links, err := collect(ctx, db, `SELECT l.relation, o.id, o.title, o.tier FROM links l
		JOIN entries o ON o.id = CASE WHEN l.from_id = ?1 THEN l.to_id ELSE l.from_id END
		WHERE l.from_id = ?1 OR (l.to_id = ?1 AND l.relation IN ('see_also', 'conflicts_with')) ORDER BY l.rowid`, []any{id}, scanLink)
	if err != nil {
		return nil, err
	}
	backlinks, err := collect(ctx, db, `SELECT l.relation, o.id, o.title, o.tier FROM links l JOIN entries o ON o.id = l.from_id
		WHERE l.to_id = ? AND l.relation IN ('requires', 'supersedes') ORDER BY l.rowid`, []any{id}, scanLink)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"entry":         e,
		"sign_id":       signID,
		"path":          path,
		"created_at":    createdAt.String,
		"superseded_by": supersededBy.String,
		"promoted_by":   promotedBy.String,
		"decided_by":    decidedBy.String,
		"decided_at":    decidedAt.String,
		"chain":         chain,
		"versions":      versions,
		"links":         links,
		"backlinks":     backlinks,
	}, nil
}

const (
	snippetMaxLines = 80
	snippetMaxBytes = 16 << 10
)

// snippet returns the text a file or range anchor points at, read from the working tree now.
func (a *api) snippet(r *http.Request) (any, error) {
	ctx, db := r.Context(), a.st.DB
	id := r.PathValue("id")
	anchors, err := anchor.Load(ctx, db, id)
	if err != nil {
		return nil, err
	}
	want, _ := strconv.ParseInt(r.URL.Query().Get("anchor"), 10, 64)
	var pick *anchor.Anchor
	for i := range anchors {
		x := &anchors[i]
		if x.Type != "range" && x.Type != "file" {
			continue
		}
		if (want != 0 && x.ID == want) || (want == 0 && pick == nil) {
			pick = x
		}
	}
	if pick == nil {
		return nil, notFound("no file or range anchor")
	}
	full, err := repoFile(a.st.Repo, pick.PathOrURL)
	if err != nil {
		return nil, notFound("anchored path is not readable")
	}
	f, err := os.Open(full)
	if err != nil {
		return nil, notFound("anchored path is not readable")
	}
	defer f.Close()

	start, end := 1, snippetMaxLines
	if pick.Type == "range" {
		start, end = pick.RangeStart, pick.RangeEnd
	}
	var lines []string
	truncated := false
	size := 0
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for n := 1; sc.Scan(); n++ {
		if n < start {
			continue
		}
		if n > end {
			if pick.Type == "file" {
				truncated = true
			}
			break
		}
		size += len(sc.Text()) + 1
		if len(lines) >= snippetMaxLines || size > snippetMaxBytes {
			truncated = true
			break
		}
		lines = append(lines, sc.Text())
	}
	return map[string]any{
		"anchor_id": pick.ID,
		"type":      pick.Type,
		"path":      pick.PathOrURL,
		"start":     start,
		"end":       start + len(lines) - 1,
		"language":  strings.TrimPrefix(filepath.Ext(pick.PathOrURL), "."),
		"text":      strings.Join(lines, "\n"),
		"truncated": truncated,
		"ok":        pick.OK,
	}, nil
}

// repoFile resolves a repo-relative path and refuses one that leaves the repo, by name or through a symlink.
func repoFile(repo, rel string) (string, error) {
	if !filepath.IsLocal(rel) {
		return "", errors.New("outside repo")
	}
	root, err := filepath.EvalSymlinks(repo)
	if err != nil {
		return "", err
	}
	full, err := filepath.EvalSymlinks(filepath.Join(repo, rel))
	if err != nil {
		return "", err
	}
	if back, err := filepath.Rel(root, full); err != nil || !filepath.IsLocal(back) {
		return "", errors.New("outside repo")
	}
	return full, nil
}
