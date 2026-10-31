package store_test

import (
	"database/sql"
	"os"
	"testing"

	"github.com/TheAmbitiousCorry/trails/internal/store"

	_ "modernc.org/sqlite"
)

func TestMigration8WritesACopyOfAnExistingDatabaseFirst(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	st, err := store.Open(home, repo)
	if err != nil {
		t.Fatal(err)
	}
	copyPath := st.Path + ".pre-0008"
	if _, err := os.Stat(copyPath); err == nil {
		t.Fatal("a fresh database wrote a pre-0008 copy")
	}
	// Put the database back at schema 7 with a row to lose.
	for _, q := range []string{
		`DROP INDEX entries_staged`,
		`ALTER TABLE entries DROP COLUMN staged_branch`, `ALTER TABLE entries DROP COLUMN staged_base_name`, `ALTER TABLE entries DROP COLUMN staged_base`,
		`ALTER TABLE entries DROP COLUMN staged_landing`, `ALTER TABLE entries DROP COLUMN stages_over`, `ALTER TABLE entries DROP COLUMN stages_over_version`,
		`DELETE FROM settings WHERE key LIKE 'coverage_%'`, `DELETE FROM schema_version WHERE version >= 8`,
	} {
		if _, err := st.DB.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	st.Close()

	st, err = store.Open(home, repo)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	db, err := sql.Open("sqlite", "file:"+copyPath+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version int
	if err := db.QueryRow(`SELECT MAX(version) FROM schema_version`).Scan(&version); err != nil || version != 7 {
		t.Fatalf("copy is at schema %d (%v), want 7", version, err)
	}
	if _, err := db.Exec(`SELECT staged_branch FROM entries LIMIT 1`); err == nil {
		t.Error("the copy already has the new columns")
	}
	if _, err := st.DB.Exec(`SELECT staged_branch, staged_base_name, staged_base, staged_landing, stages_over, stages_over_version FROM entries LIMIT 1`); err != nil {
		t.Errorf("the migrated database lacks a column: %v", err)
	}
}
