package anchor_test

import (
	"context"
	"testing"

	"github.com/TheAmbitiousCorry/trails/internal/anchor"
)

const longAgo = "2000-01-01T00:00:00Z"

func TestAnchorChecksMoveUpdatedAtOnlyWhenSomethingChanged(t *testing.T) {
	repo := newRepo(t)
	st, c := open(t, repo)
	ctx := context.Background()
	addEntry(t, st.DB, "e1", "root", "reference", "verified")
	anchorTo(t, c, st.DB, anchor.Anchor{EntryID: "e1", Type: "file", PathOrURL: "src/a.txt"})
	age := func() {
		if _, err := st.DB.Exec(`UPDATE entries SET updated_at = ? WHERE id = 'e1'`, longAgo); err != nil {
			t.Fatal(err)
		}
	}
	stamp := func() string {
		var s string
		if err := st.DB.QueryRow(`SELECT updated_at FROM entries WHERE id = 'e1'`).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}

	age()
	if _, err := anchor.RefreshEntry(ctx, st.DB, c, "e1"); err != nil || stamp() != longAgo {
		t.Fatalf("a check that found nothing new moved updated_at to %s (%v)", stamp(), err)
	}
	write(t, repo, "src/a.txt", "one\nchanged\nthree\n")
	if _, err := anchor.RefreshEntry(ctx, st.DB, c, "e1"); err != nil || stamp() == longAgo {
		t.Fatalf("an entry going stale did not move updated_at (%v)", err)
	}
	age()
	if _, err := anchor.RefreshEntry(ctx, st.DB, c, "e1"); err != nil || stamp() != longAgo {
		t.Fatalf("a second failing check moved updated_at to %s (%v)", stamp(), err)
	}
	write(t, repo, "src/a.txt", "one\ntwo\nthree\n")
	if _, err := anchor.RefreshEntry(ctx, st.DB, c, "e1"); err != nil || stamp() == longAgo {
		t.Fatalf("an entry restored from stale did not move updated_at (%v)", err)
	}

	// Re-hashing an anchor under a tier that does not change still changes what the entry stores.
	write(t, repo, "src/a.txt", "one\ntwo\nthree\nfour\n")
	age()
	if failed, err := anchor.Reattach(ctx, st.DB, c, "e1"); err != nil || len(failed) != 0 || stamp() == longAgo {
		t.Fatalf("a new stored hash did not move updated_at (failed %v, %v)", failed, err)
	}
}
