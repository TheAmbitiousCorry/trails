package store

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// mapPath is the committed file that declares the sign tree and the tag vocabulary, relative to the repo.
var mapPath = filepath.Join(".trail", "map.md")

// MapSign is one bullet under "# Signs"; Children are the bullets indented one level under it.
type MapSign struct {
	Title    string
	Line     int
	Children []*MapSign
}

// MapTag is one bullet under "# Tags".
type MapTag struct {
	Name        string
	Description string
	Line        int
}

// Map is the parsed content of .trail/map.md.
type Map struct {
	Signs []*MapSign
	Tags  []MapTag
}

type mapSection int

const (
	sectionNone mapSection = iota
	sectionSigns
	sectionTags
)

// ParseMap reads the map format: "# Signs" with nested bullets (two spaces per level) and "# Tags" with "- name: description" bullets.
// Blank lines and every other line are ignored. A problem is reported with its line number.
func ParseMap(r io.Reader) (Map, error) {
	var m Map
	var stack []*MapSign
	section := sectionNone
	signLines := map[string]int{}
	tagLines := map[string]int{}
	sc := bufio.NewScanner(r)
	line := 0
	for sc.Scan() {
		line++
		text := strings.TrimRight(sc.Text(), " \t\r")
		if strings.HasPrefix(text, "# ") {
			switch strings.TrimSpace(text[2:]) {
			case "Signs":
				section = sectionSigns
				stack = nil
			case "Tags":
				section = sectionTags
			default:
				section = sectionNone
			}
			continue
		}
		rest := strings.TrimLeft(text, " ")
		if section == sectionNone || !(rest == "-" || strings.HasPrefix(rest, "- ")) {
			continue
		}
		content := strings.TrimSpace(strings.TrimPrefix(rest, "-"))
		if section == sectionSigns {
			if content == "" {
				return Map{}, fmt.Errorf("%s line %d: sign bullet has no title", mapPath, line)
			}
			level := (len(text) - len(rest)) / 2
			if level > len(stack) {
				return Map{}, fmt.Errorf("%s line %d: %q is indented under no sign", mapPath, line, content)
			}
			id := Slug(content, "sign")
			if prev, ok := signLines[id]; ok {
				return Map{}, fmt.Errorf("%s line %d: sign %q has the same id as line %d", mapPath, line, content, prev)
			}
			signLines[id] = line
			sg := &MapSign{Title: content, Line: line}
			stack = stack[:level]
			if level == 0 {
				m.Signs = append(m.Signs, sg)
			} else {
				stack[level-1].Children = append(stack[level-1].Children, sg)
			}
			stack = append(stack, sg)
			continue
		}
		name, desc, ok := strings.Cut(content, ":")
		if !ok {
			return Map{}, fmt.Errorf("%s line %d: tag bullet has no colon; write it as - name: description", mapPath, line)
		}
		name = strings.TrimSpace(name)
		if name == "" {
			return Map{}, fmt.Errorf("%s line %d: tag bullet has no name", mapPath, line)
		}
		if prev, ok := tagLines[name]; ok {
			return Map{}, fmt.Errorf("%s line %d: tag %q is already listed at line %d", mapPath, line, name, prev)
		}
		tagLines[name] = line
		m.Tags = append(m.Tags, MapTag{Name: name, Description: strings.TrimSpace(desc), Line: line})
	}
	if err := sc.Err(); err != nil {
		return Map{}, err
	}
	return m, nil
}

// ApplyMap reconciles the sign tree and the tag vocabulary with <repo>/.trail/map.md in one transaction.
// A missing file changes nothing. Listed signs that do not exist are created; existing ones keep all they have.
// An unlisted sign is deleted only when it has no entries and no child signs, otherwise it is kept and logged.
// Listed tags are upserted as defined; unlisted defined tags are set undefined, never deleted.
func (s *Store) ApplyMap(repo string) error {
	b, err := os.ReadFile(filepath.Join(repo, mapPath))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	m, err := ParseMap(bytes.NewReader(b))
	if err != nil {
		return err
	}
	ctx := context.Background()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	listed := map[string]bool{}
	if err := applyMapSigns(ctx, tx, "root", m.Signs, listed); err != nil {
		return err
	}
	if err := pruneMapSigns(ctx, tx, listed); err != nil {
		return err
	}
	if err := applyMapTags(ctx, tx, m.Tags); err != nil {
		return err
	}
	return tx.Commit()
}

func applyMapSigns(ctx context.Context, tx *sql.Tx, parent string, signs []*MapSign, listed map[string]bool) error {
	for i, sg := range signs {
		id := Slug(sg.Title, "sign")
		listed[id] = true
		var signExists, entryExists int
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM signs WHERE id = ?), EXISTS(SELECT 1 FROM entries WHERE id = ?)`, id, id).Scan(&signExists, &entryExists); err != nil {
			return err
		}
		if signExists == 0 {
			if entryExists == 1 {
				return fmt.Errorf("%s line %d: sign id %q is already held by an entry", mapPath, sg.Line, id)
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO signs (id, parent_id, title, position) VALUES (?, ?, ?, ?)`, id, parent, sg.Title, i); err != nil {
				return err
			}
		}
		if err := applyMapSigns(ctx, tx, id, sg.Children, listed); err != nil {
			return err
		}
	}
	return nil
}

func pruneMapSigns(ctx context.Context, tx *sql.Tx, listed map[string]bool) error {
	kept := map[string][2]int{}
	for {
		ids, err := queryIDs(ctx, tx, `SELECT id FROM signs WHERE id <> 'root'`)
		if err != nil {
			return err
		}
		deleted := false
		for _, id := range ids {
			if listed[id] {
				continue
			}
			var entries, children int
			if err := tx.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM entries WHERE sign_id = ?), (SELECT COUNT(*) FROM signs WHERE parent_id = ?)`, id, id).Scan(&entries, &children); err != nil {
				return err
			}
			if entries == 0 && children == 0 {
				if _, err := tx.ExecContext(ctx, `DELETE FROM signs WHERE id = ?`, id); err != nil {
					return err
				}
				delete(kept, id)
				deleted = true
				continue
			}
			kept[id] = [2]int{entries, children}
		}
		if !deleted {
			break
		}
	}
	keys := make([]string, 0, len(kept))
	for id := range kept {
		keys = append(keys, id)
	}
	sort.Strings(keys)
	for _, id := range keys {
		log.Printf("trails: map: kept sign %s, not in %s: %d entries, %d child signs", id, mapPath, kept[id][0], kept[id][1])
	}
	return nil
}

func applyMapTags(ctx context.Context, tx *sql.Tx, tags []MapTag) error {
	listed := map[string]bool{}
	for _, t := range tags {
		listed[t.Name] = true
		if _, err := tx.ExecContext(ctx, `INSERT INTO tags (id, description, defined) VALUES (?, ?, 1)
			ON CONFLICT(id) DO UPDATE SET description = excluded.description, defined = 1`, t.Name, t.Description); err != nil {
			return err
		}
	}
	defined, err := queryIDs(ctx, tx, `SELECT id FROM tags WHERE defined = 1`)
	if err != nil {
		return err
	}
	for _, id := range defined {
		if listed[id] {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE tags SET defined = 0 WHERE id = ?`, id); err != nil {
			return err
		}
	}
	return nil
}

func queryIDs(ctx context.Context, tx *sql.Tx, query string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
