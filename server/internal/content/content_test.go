package content

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixtures(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	paths, err := filepath.Glob("testdata/*.json")
	if err != nil || len(paths) == 0 {
		t.Fatalf("no fixtures found: %v", err)
	}
	out := make(map[string]json.RawMessage, len(paths))
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		out[filepath.Base(p)] = raw
	}
	return out
}

func TestFixtures(t *testing.T) {
	for name, raw := range fixtures(t) {
		t.Run(name, func(t *testing.T) {
			err := Validate(raw)
			switch {
			case strings.HasPrefix(name, "accept-"):
				if err != nil {
					t.Fatalf("want accepted, got %v", err)
				}
			case strings.HasPrefix(name, "refuse-"):
				rule := "schema:" + strings.SplitN(strings.TrimPrefix(name, "refuse-"), "-", 2)[0]
				var ref *Refusal
				if !errors.As(err, &ref) {
					t.Fatalf("want *Refusal %s, got %v", rule, err)
				}
				if ref.Rule != rule || ref.Fix == "" {
					t.Fatalf("want rule %s with a fix, got %+v", rule, ref)
				}
			default:
				t.Fatalf("fixture name must start with accept- or refuse-")
			}
		})
	}
}

// Every allowed node and mark must appear in an accepted fixture, and every refusal rule in a refused one.
func TestFixturesCoverTheSchema(t *testing.T) {
	seenNodes := map[string]bool{}
	seenMarks := map[string]bool{}
	seenRules := map[string]bool{}
	for name, raw := range fixtures(t) {
		if rest, ok := strings.CutPrefix(name, "refuse-"); ok {
			seenRules["schema:"+strings.SplitN(rest, "-", 2)[0]] = true
			continue
		}
		var root node
		if err := json.Unmarshal(raw, &root); err != nil {
			t.Fatal(err)
		}
		var visit func(n *node)
		visit = func(n *node) {
			seenNodes[n.Type] = true
			for _, m := range n.Marks {
				seenMarks[m.Type] = true
			}
			for i := range n.Content {
				visit(&n.Content[i])
			}
		}
		visit(&root)
	}
	for n := range allowedNodes {
		if !seenNodes[n] {
			t.Errorf("no accepted fixture uses node %s", n)
		}
	}
	for m := range allowedMarks {
		if !seenMarks[m] {
			t.Errorf("no accepted fixture uses mark %s", m)
		}
	}
	for _, r := range []string{RuleUnknownNode, RuleUnknownMark, RuleHeadingLevel, RuleURLScheme, RuleDepth, RuleBytes, RuleMalformed} {
		if !seenRules[r] {
			t.Errorf("no refused fixture for %s", r)
		}
	}
}

func TestValidateEmptyInput(t *testing.T) {
	for _, in := range []string{"", "null", "  "} {
		var ref *Refusal
		if err := Validate(json.RawMessage(in)); !errors.As(err, &ref) || ref.Rule != RuleMalformed {
			t.Errorf("Validate(%q) = %v, want %s", in, err, RuleMalformed)
		}
	}
}

func TestControlCharacterInSchemeIsRefused(t *testing.T) {
	doc := `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"x","marks":[{"type":"link","attrs":{"href":"java\tscript:alert(1)"}}]}]}]}`
	var ref *Refusal
	if err := Validate(json.RawMessage(doc)); !errors.As(err, &ref) || ref.Rule != RuleURLScheme {
		t.Fatalf("got %v, want %s", err, RuleURLScheme)
	}
}

func TestPlainTextAndWordCount(t *testing.T) {
	raw := json.RawMessage(`{"type":"doc","content":[
		{"type":"heading","attrs":{"level":2},"content":[{"type":"text","text":"Cache rules"}]},
		{"type":"paragraph","content":[{"type":"text","text":"one"},{"type":"hardBreak"},{"type":"text","text":"two three"}]},
		{"type":"table","content":[{"type":"tableRow","content":[
			{"type":"tableCell","content":[{"type":"paragraph","content":[{"type":"text","text":"a"}]}]},
			{"type":"tableCell","content":[{"type":"paragraph","content":[{"type":"text","text":"b"}]}]}]}]}]}`)
	got := PlainText(raw)
	for _, want := range []string{"Cache rules\n", "one\ntwo three\n", "a"} {
		if !strings.Contains(got, want) {
			t.Errorf("PlainText = %q, missing %q", got, want)
		}
	}
	if n := WordCount(raw); n != 7 {
		t.Errorf("WordCount = %d, want 7", n)
	}
	if PlainText(json.RawMessage("{")) != "" || WordCount(nil) != 0 {
		t.Error("unparseable input must yield empty text and zero words")
	}
}
