// Package store opens a project's SQLite database, migrates it and seeds a first thread.
package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"embed"
	"encoding/base32"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/TheAmbitiousCorry/trails/internal/search"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

type Store struct {
	DB        *sql.DB
	ProjectID string
	Path      string
	// Repo is the working tree anchors resolve against.
	Repo string
}

// Open resolves the project id from <repo>/.trail/id (creating it when missing) and opens <home>/<id>/trail.db.
func Open(home, repo string) (*Store, error) {
	id, err := projectID(repo)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(home, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "trail.db")
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	s := &Store{DB: db, ProjectID: id, Path: path, Repo: repo}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	if err := s.seed(filepath.Base(repo)); err != nil {
		db.Close()
		return nil, err
	}
	if err := search.Reindex(context.Background(), db); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.DB.Close() }

func projectID(repo string) (string, error) {
	idPath := filepath.Join(repo, ".trail", "id")
	if b, err := os.ReadFile(idPath); err == nil {
		id := strings.TrimSpace(string(b))
		if id == "" || strings.ContainsAny(id, `/\.`) {
			return "", fmt.Errorf("invalid project id in %s", idPath)
		}
		return id, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	id := RandomSlug(10)
	if err := os.MkdirAll(filepath.Dir(idPath), 0o755); err != nil {
		return "", err
	}
	return id, os.WriteFile(idPath, []byte(id+"\n"), 0o644)
}

// RandomSlug returns n lowercase base32 characters.
func RandomSlug(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b))[:n]
}

type migration struct {
	version int
	sql     string
}

func loadMigrations() ([]migration, error) {
	files, err := fs.Glob(migrationFS, "migrations/*.sql")
	if err != nil {
		return nil, err
	}
	var out []migration
	for _, f := range files {
		name := filepath.Base(f)
		num, _, _ := strings.Cut(name, "_")
		v, err := strconv.Atoi(num)
		if err != nil {
			return nil, fmt.Errorf("migration %s: name must start with a number", name)
		}
		b, err := migrationFS.ReadFile(f)
		if err != nil {
			return nil, err
		}
		out = append(out, migration{v, string(b)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}

// migrate is forward-only: a database newer than this binary is refused, never downgraded.
func (s *Store) migrate() error {
	if _, err := s.DB.Exec(`CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL, applied_at TEXT NOT NULL)`); err != nil {
		return err
	}
	var current int
	if err := s.DB.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&current); err != nil {
		return err
	}
	ms, err := loadMigrations()
	if err != nil {
		return err
	}
	if len(ms) > 0 && current > ms[len(ms)-1].version {
		return fmt.Errorf("database is at schema %d, newer than this binary (%d)", current, ms[len(ms)-1].version)
	}
	for _, m := range ms {
		if m.version <= current {
			continue
		}
		tx, err := s.DB.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(m.sql); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", m.version, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_version (version, applied_at) VALUES (?, ?)`, m.version, Now()); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func Now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

const seedBody = `{"type":"doc","content":[` +
	`{"type":"paragraph","content":[{"type":"text","text":"Trail is this project's wiki. Agents walk it one sign at a time and read only the entries that answer the task."}]},` +
	`{"type":"paragraph","content":[{"type":"text","text":"Every call an agent makes is recorded as a trail event, so a human can follow the route afterwards."}]}` +
	`]}`

// seed gives a fresh database one root sign and one entry; it does nothing once any sign exists.
func (s *Store) seed(projectTitle string) error {
	var n int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM signs`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := Now()
	if _, err := tx.Exec(`INSERT INTO signs (id, parent_id, title, position) VALUES ('root', NULL, ?, 0)`, projectTitle); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO entries (id, sign_id, kind, title, read_when, body_json, body_schema, tier, decided_by, decided_at, created_at, updated_at)
		VALUES ('trail', 'root', 'summary', 'Trail', 'you are new to this project', ?, 1, 'verified', 'seed', ?, ?, ?)`,
		seedBody, now, now, now); err != nil {
		return err
	}
	return tx.Commit()
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

const maxSlug = 60

// Slug lowercases title into hyphen-joined [a-z0-9] words of at most 60 characters; fallback stands in when nothing is left.
func Slug(title, fallback string) string {
	s := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(title), "-"), "-")
	if len(s) > maxSlug {
		s = strings.Trim(s[:maxSlug], "-")
	}
	if s == "" {
		return fallback
	}
	return s
}

// FreeID returns base, or base-2, base-3 and so on, the first that no entry or sign holds and no expired entry held. Entries and signs share one id space.
func FreeID(ctx context.Context, tx *sql.Tx, base string) (string, error) {
	for n := 1; ; n++ {
		id := base
		if n > 1 {
			id = base + "-" + strconv.Itoa(n)
		}
		var taken int
		err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM entries WHERE id = ?) OR EXISTS(SELECT 1 FROM signs WHERE id = ?) OR EXISTS(SELECT 1 FROM ids WHERE id = ?)`, id, id, id).Scan(&taken)
		if err != nil || taken == 0 {
			return id, err
		}
	}
}
