package entries_test

import (
	"context"
	"testing"

	"github.com/TheAmbitiousCorry/trails/internal/entries"
	"github.com/TheAmbitiousCorry/trails/internal/store"
)

const longAgo = "2000-01-01T00:00:00Z"

func age(t *testing.T, st *store.Store, ids ...string) {
	t.Helper()
	for _, id := range ids {
		if _, err := st.DB.Exec(`UPDATE entries SET updated_at = ? WHERE id = ?`, longAgo, id); err != nil {
			t.Fatal(err)
		}
	}
}

func moved(t *testing.T, st *store.Store, what, id string) {
	t.Helper()
	if column(t, st, `SELECT updated_at FROM entries WHERE id = ?`, id) == longAgo {
		t.Errorf("%s left updated_at of %s where it was; the edit token would not move", what, id)
	}
}

func TestWritersOfAnEntryMoveUpdatedAt(t *testing.T) {
	st, c := open(t)
	fillers(t, st, c)
	const body = "Round each invoice line to the nearest cent before summing totals."
	x := mustWrite(t, st, c, decision("Quarterly invoice rounding", body)).EntryID

	age(t, st, x)
	merge := decision("Quarterly invoice rounding", body)
	merge.ReadWhen, merge.Resolution = "pennies disagree with the ledger", "merge"
	mustWrite(t, st, c, merge)
	moved(t, st, "merge alias", x)

	age(t, st, x)
	replace := decision("Quarterly invoice rounding", body)
	replace.Resolution = "replace"
	y := mustWrite(t, st, c, replace).EntryID
	moved(t, st, "replace (retire)", x)

	age(t, st, y)
	z := mustWrite(t, st, c, decision("Harbour dredging schedule", "Dredge the harbour channel every spring after the thaw.")).EntryID
	if _, err := st.DB.Exec(`INSERT INTO links (from_id, to_id, relation) VALUES (?, ?, 'conflicts_with')`, min(y, z), max(y, z)); err != nil {
		t.Fatal(err)
	}
	age(t, st, y, z)
	if err := entries.Supersede(context.Background(), st.DB, y, z); err != nil {
		t.Fatal(err)
	}
	moved(t, st, "Supersede", y)

	age(t, st, z)
	up := decision("Harbour dredging schedule", "Dredge the harbour channel every autumn.")
	up.EntryID = z
	mustWrite(t, st, c, up)
	moved(t, st, "agent update", z)
}
