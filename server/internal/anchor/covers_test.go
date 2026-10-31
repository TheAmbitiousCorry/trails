package anchor_test

import (
	"strings"
	"testing"

	"github.com/TheAmbitiousCorry/trails/internal/anchor"
)

type counts map[string]int

func (c counts) Count(dir string) int { return c[dir] }

func TestCoversPredicate(t *testing.T) {
	tracked := counts{"src": 50, "big": 51, "src2": 1}
	scope := anchor.Scope{Tracked: tracked, DirMax: 50}
	cases := []struct {
		name   string
		anchor anchor.Live
		rel    string
		want   bool
	}{
		{"file on the file", anchor.Live{Type: "file", Path: "src/a.go"}, "src/a.go", true},
		{"file on a sibling", anchor.Live{Type: "file", Path: "src/a.go"}, "src/b.go", false},
		{"range on the file", anchor.Live{Type: "range", Path: "src/a.go"}, "src/a.go", true},
		{"file anchor is not a directory", anchor.Live{Type: "file", Path: "src"}, "src/a.go", false},
		{"dir at the ceiling covers", anchor.Live{Type: "dir", Path: "src"}, "src/deep/a.go", true},
		{"dir over the ceiling does not", anchor.Live{Type: "dir", Path: "big"}, "big/a.go", false},
		{"dir does not cover a name sharing its prefix", anchor.Live{Type: "dir", Path: "src"}, "src2/a.go", false},
		{"dir with a trailing slash", anchor.Live{Type: "dir", Path: "src/"}, "src/a.go", true},
		{"other anchor types never cover", anchor.Live{Type: "commit", Path: "src/a.go"}, "src/a.go", false},
	}
	for _, c := range cases {
		if got := scope.Covers(c.anchor, c.rel); got != c.want {
			t.Errorf("%s: Covers = %v, want %v", c.name, got, c.want)
		}
	}
	// Without a tracked-file source a dir anchor cannot be measured, so it keeps covering as the hook always did.
	if !(anchor.Scope{DirMax: 50}).Covers(anchor.Live{Type: "dir", Path: "big"}, "big/a.go") {
		t.Error("a dir anchor with no tracked-file count stopped covering")
	}
}

func TestTrackedCountsASubtree(t *testing.T) {
	tr := anchor.NewTracked([]string{"a/x.go", "a/y.go", "a/b/z.go", "ab/w.go", "top.go", "top.go"})
	for dir, want := range map[string]int{"a": 3, "a/b": 1, "ab": 1, "": 5, ".": 5, "none": 0} {
		if got := tr.Count(dir); got != want {
			t.Errorf("Count(%q) = %d, want %d", dir, got, want)
		}
	}
	if strings.Join(tr.Paths(), ",") != "a/b/z.go,a/x.go,a/y.go,ab/w.go,top.go" {
		t.Errorf("Paths = %v", tr.Paths())
	}
}
