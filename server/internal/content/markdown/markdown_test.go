package markdown

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/TheAmbitiousCorry/trails/internal/content"
)

// normalise decodes and re-encodes so key order and whitespace do not count as differences.
func normalise(t *testing.T, raw []byte) string {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func checkCanonical(t *testing.T, md string) {
	t.Helper()
	if md == "" {
		return
	}
	if !strings.HasSuffix(md, "\n") || strings.HasSuffix(md, "\n\n") {
		t.Errorf("want exactly one trailing newline, got %q", md)
	}
	for _, l := range strings.Split(strings.TrimSuffix(md, "\n"), "\n") {
		if l != strings.TrimRight(l, " \t") {
			t.Errorf("trailing whitespace on line %q", l)
		}
	}
}

func roundTrip(t *testing.T, raw []byte) {
	t.Helper()
	md, err := ToMarkdown(raw)
	if err != nil {
		t.Fatal(err)
	}
	checkCanonical(t, md)
	back, err := FromMarkdown(md)
	if err != nil {
		t.Fatalf("FromMarkdown(%q): %v", md, err)
	}
	if err := content.Validate(back); err != nil {
		t.Fatal(err)
	}
	if got, want := normalise(t, back), normalise(t, raw); got != want {
		t.Errorf("markdown %q\n got %s\nwant %s", md, got, want)
	}
	again, err := ToMarkdown(back)
	if err != nil {
		t.Fatal(err)
	}
	if again != md {
		t.Errorf("not canonical:\nfirst  %q\nsecond %q", md, again)
	}
}

func TestRoundTripAcceptFixtures(t *testing.T) {
	paths, err := filepath.Glob("../testdata/accept-*.json")
	if err != nil || len(paths) == 0 {
		t.Fatalf("no accept fixtures: %v", err)
	}
	for _, p := range paths {
		t.Run(filepath.Base(p), func(t *testing.T) {
			raw, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			roundTrip(t, raw)
		})
	}
}

// TestRoundTripAwkwardText covers text that would read back as syntax if it were not escaped.
func TestRoundTripAwkwardText(t *testing.T) {
	texts := []string{
		`*star* and _under_ and snake_case`, "a `tick` [brackets] <tag> &amp; \\ back",
		"# not a heading", "> not a quote", "- not a bullet", "1. not a list", "=== not setext",
		"R&D and a < b and wow!", "![not an image](x)", "pipe | here", "~~not struck~~",
		" leading space", "trailing space ",
	}
	for i, text := range texts {
		doc := map[string]any{"type": "doc", "content": []any{
			map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": text}}},
		}}
		raw, _ := json.Marshal(doc)
		t.Run(string(rune('a'+i)), func(t *testing.T) { roundTrip(t, raw) })
	}
}

func TestRoundTripMarksAndStructure(t *testing.T) {
	const doc = `{"type":"doc","content":[
	{"type":"heading","attrs":{"level":2},"content":[{"type":"text","text":"C# notes"}]},
	{"type":"paragraph","content":[
		{"type":"text","text":"plain "},
		{"type":"text","text":"both","marks":[{"type":"bold"},{"type":"italic"}]},
		{"type":"text","text":" "},
		{"type":"text","text":"b","marks":[{"type":"bold"}]},
		{"type":"text","text":"i","marks":[{"type":"italic"}]},
		{"type":"text","text":" see "},
		{"type":"text","text":"a \u0060 tick","marks":[{"type":"code"}]},
		{"type":"text","text":" in "},
		{"type":"text","text":"link","marks":[{"type":"link","attrs":{"href":"https://example.com/a(b)"}},{"type":"bold"}]},
		{"type":"text","text":" text","marks":[{"type":"link","attrs":{"href":"https://example.com/a(b)"}}]},
		{"type":"hardBreak"},
		{"type":"text","text":"next line"}]},
	{"type":"bulletList","content":[
		{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"one"}]},
			{"type":"codeBlock","attrs":{"language":"go"},"content":[{"type":"text","text":"a := \u0060\u0060\u0060\n\nb"}]}]},
		{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"two"}]},
			{"type":"orderedList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"nested"}]}]}]}]}]},
	{"type":"blockquote","content":[{"type":"paragraph","content":[{"type":"text","text":"q1"}]},{"type":"paragraph","content":[{"type":"text","text":"q2"}]}]},
	{"type":"image","attrs":{"src":"https://example.com/a b.png","alt":"two words"}},
	{"type":"table","content":[
		{"type":"tableRow","content":[{"type":"tableHeader","content":[{"type":"paragraph","content":[{"type":"text","text":"A"}]}]},{"type":"tableHeader","content":[{"type":"paragraph","content":[{"type":"text","text":"B | C"}]}]}]},
		{"type":"tableRow","content":[{"type":"tableCell","content":[{"type":"paragraph","content":[{"type":"text","text":"x","marks":[{"type":"code"}]}]}]},{"type":"tableCell","content":[{"type":"paragraph","content":[{"type":"text","text":"y"}]}]}]}]}
	]}`
	roundTrip(t, []byte(doc))
}

func TestToMarkdownCanonicalShape(t *testing.T) {
	const doc = `{"type":"doc","content":[
	{"type":"heading","attrs":{"level":3},"content":[{"type":"text","text":"Title"}]},
	{"type":"orderedList","content":[
		{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"a"}]}]},
		{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"b"}]}]}]},
	{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"c"}]}]}]}
	]}`
	got, err := ToMarkdown([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	if want := "### Title\n\n1. a\n1. b\n\n- c\n"; got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestToMarkdownRefusesInvalidDocument(t *testing.T) {
	_, err := ToMarkdown([]byte(`{"type":"doc","content":[{"type":"horizontalRule"}]}`))
	var ref *content.Refusal
	if !errors.As(err, &ref) || ref.Rule != content.RuleUnknownNode {
		t.Fatalf("want %s, got %v", content.RuleUnknownNode, err)
	}
}

func TestFromMarkdownRefusals(t *testing.T) {
	cases := []struct{ name, src, rule string }{
		{"html block", "<div>hi</div>\n", "unsupported: html block"},
		{"inline html", "a <span>b</span> c\n", "unsupported: inline html"},
		{"task list", "- [ ] todo\n- [x] done\n", "unsupported: task list"},
		{"strikethrough", "a ~~b~~ c\n", "unsupported: strikethrough"},
		{"heading 1", "# Title\n", "unsupported: heading level"},
		{"heading 5", "##### Deep\n", "unsupported: heading level"},
		{"thematic break", "a\n\n---\n\nb\n", "unsupported: thematic break"},
		{"footnote", "a[^1]\n\n[^1]: note\n", "unsupported: footnote"},
		{"inline image", "see ![x](https://example.com/a.png) here\n", "unsupported: inline image"},
		{"link title", "[a](https://example.com \"t\")\n", "unsupported: link title"},
		{"ordered start", "3. a\n4. b\n", "unsupported: ordered list start"},
		{"table alignment", "| a |\n| :-: |\n| b |\n", "unsupported: table alignment"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := FromMarkdown(c.src)
			var ref *content.Refusal
			if !errors.As(err, &ref) {
				t.Fatalf("want *content.Refusal, got %v", err)
			}
			if !strings.HasPrefix(ref.Rule, "unsupported: ") || ref.Rule != c.rule || ref.Fix == "" {
				t.Errorf("got %+v, want rule %q with a fix", ref, c.rule)
			}
		})
	}
}

func TestFromMarkdownSchemaRefusalsPassThrough(t *testing.T) {
	_, err := FromMarkdown("[x](javascript:alert)\n")
	var ref *content.Refusal
	if !errors.As(err, &ref) || ref.Rule != content.RuleURLScheme {
		t.Fatalf("want %s, got %v", content.RuleURLScheme, err)
	}
}

func TestFromMarkdownAcceptsAgentMarkdown(t *testing.T) {
	const src = "## Setup\n\nRun `make run`, then open [the page](https://example.com).\nSecond line joins the first.\n\n```go\nfmt.Println(1)\n```\n\n1. one\n2. two\n\n![shot](/media/a.png)\n"
	raw, err := FromMarkdown(src)
	if err != nil {
		t.Fatal(err)
	}
	var v struct{ Content []struct{ Type string } }
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	var types []string
	for _, c := range v.Content {
		types = append(types, c.Type)
	}
	if want := []string{"heading", "paragraph", "codeBlock", "orderedList", "image"}; !reflect.DeepEqual(types, want) {
		t.Errorf("got %v want %v", types, want)
	}
	if md, err := ToMarkdown(raw); err != nil || !strings.Contains(md, "1. one\n1. two\n") {
		t.Errorf("numbering not canonical: %q %v", md, err)
	}
}
