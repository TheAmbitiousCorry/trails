package store

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeMapFile(t *testing.T, repo, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(repo, ".trail"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, mapPath), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

type signRow struct {
	Parent   string
	Title    string
	Position int
}

func signRows(t *testing.T, st *Store) map[string]signRow {
	t.Helper()
	rows, err := st.DB.Query(`SELECT id, COALESCE(parent_id, ''), title, position FROM signs`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]signRow{}
	for rows.Next() {
		var id string
		var r signRow
		if err := rows.Scan(&id, &r.Parent, &r.Title, &r.Position); err != nil {
			t.Fatal(err)
		}
		out[id] = r
	}
	return out
}

func tagRow(t *testing.T, st *Store, id string) (desc string, defined int, ok bool) {
	t.Helper()
	err := st.DB.QueryRow(`SELECT description, defined FROM tags WHERE id = ?`, id).Scan(&desc, &defined)
	if err != nil {
		return "", 0, false
	}
	return desc, defined, true
}

func TestParseMapNested(t *testing.T) {
	src := "# Signs\n\n- Building\n  - Frontend\n    - Forms\n  - Backend\n- Operations\n\nnotes that are ignored\n\n# Tags\n- billing: Money in and out\n- auth: Sign-in and sessions: cookies\n"
	m, err := ParseMap(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Signs) != 2 || m.Signs[0].Title != "Building" || m.Signs[1].Title != "Operations" {
		t.Fatalf("top-level signs = %+v", m.Signs)
	}
	building := m.Signs[0].Children
	if len(building) != 2 || building[0].Title != "Frontend" || building[1].Title != "Backend" {
		t.Fatalf("children of Building = %+v", building)
	}
	forms := building[0].Children
	if len(forms) != 1 || forms[0].Title != "Forms" || forms[0].Line != 5 {
		t.Fatalf("children of Frontend = %+v", forms)
	}
	want := []MapTag{
		{Name: "billing", Description: "Money in and out", Line: 12},
		{Name: "auth", Description: "Sign-in and sessions: cookies", Line: 13},
	}
	if !reflect.DeepEqual(m.Tags, want) {
		t.Fatalf("tags = %+v, want %+v", m.Tags, want)
	}
}

func TestMapCreatesSignsAndTags(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	writeMapFile(t, repo, "# Signs\n- Building\n  - Frontend\n  - Backend\n- Operations\n  - Deploys\n\n# Tags\n- billing: Money in and out\n- auth: Sign-in\n")
	st, err := Open(home, repo)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	signs := signRows(t, st)
	want := map[string]signRow{
		"building":   {Parent: "root", Title: "Building", Position: 0},
		"frontend":   {Parent: "building", Title: "Frontend", Position: 0},
		"backend":    {Parent: "building", Title: "Backend", Position: 1},
		"operations": {Parent: "root", Title: "Operations", Position: 1},
		"deploys":    {Parent: "operations", Title: "Deploys", Position: 0},
	}
	for id, w := range want {
		if got := signs[id]; got != w {
			t.Errorf("sign %s = %+v, want %+v", id, got, w)
		}
	}
	if desc, defined, ok := tagRow(t, st, "billing"); !ok || defined != 1 || desc != "Money in and out" {
		t.Errorf("billing = %q, %d, %v", desc, defined, ok)
	}
	if _, defined, ok := tagRow(t, st, "auth"); !ok || defined != 1 {
		t.Errorf("auth defined = %d, present %v", defined, ok)
	}
}

func TestMapSecondOpenChangesNothing(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	writeMapFile(t, repo, "# Signs\n- Building\n  - Frontend\n\n# Tags\n- billing: Money\n")
	st, err := Open(home, repo)
	if err != nil {
		t.Fatal(err)
	}
	before := signRows(t, st)
	st.Close()
	st, err = Open(home, repo)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if after := signRows(t, st); !reflect.DeepEqual(before, after) {
		t.Errorf("signs changed on second open:\nbefore %+v\nafter  %+v", before, after)
	}
}

func TestMapRemovedSignDeletedWhenEmptyKeptWhenHeld(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	writeMapFile(t, repo, "# Signs\n- Building\n  - Frontend\n  - Backend\n")
	st, err := Open(home, repo)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.Exec(`INSERT INTO entries (id, sign_id, kind, title, read_when, body_json, body_schema, tier, decided_by, decided_at, created_at, updated_at)
		VALUES ('frontend-note', 'frontend', 'summary', 'Frontend', 'x', '{"type":"doc","content":[]}', 1, 'verified', 'seed', 'now', 'now', 'now')`); err != nil {
		t.Fatal(err)
	}
	st.Close()

	writeMapFile(t, repo, "# Signs\n- Building\n")
	st, err = Open(home, repo)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	signs := signRows(t, st)
	if _, ok := signs["backend"]; ok {
		t.Error("empty sign backend was not deleted")
	}
	if _, ok := signs["frontend"]; !ok {
		t.Error("sign frontend holding an entry was deleted")
	}
}

func TestMapRemovedTagBecomesUndefined(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	writeMapFile(t, repo, "# Tags\n- billing: Money\n- auth: Sign-in\n")
	st, err := Open(home, repo)
	if err != nil {
		t.Fatal(err)
	}
	st.Close()

	writeMapFile(t, repo, "# Tags\n- auth: Sign-in\n")
	st, err = Open(home, repo)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	desc, defined, ok := tagRow(t, st, "billing")
	if !ok {
		t.Fatal("billing was deleted, want it kept undefined")
	}
	if defined != 0 || desc != "Money" {
		t.Errorf("billing = %q, defined %d; want \"Money\", 0", desc, defined)
	}
}

func TestMapTagWithoutColonFailsOpen(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	writeMapFile(t, repo, "# Signs\n- Building\n\n# Tags\n- billing\n")
	_, err := Open(home, repo)
	if err == nil {
		t.Fatal("Open accepted a tag bullet without a colon")
	}
	if !strings.Contains(err.Error(), "line 5") {
		t.Errorf("error %q does not name line 5", err)
	}
}
