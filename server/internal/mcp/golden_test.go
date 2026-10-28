package mcp_test

import (
	"context"
	"encoding/json"
	"flag"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/TheAmbitiousCorry/trails/internal/content"
	"github.com/TheAmbitiousCorry/trails/internal/content/markdown"
	"github.com/TheAmbitiousCorry/trails/internal/entries"
	"github.com/TheAmbitiousCorry/trails/internal/mcp"
	"github.com/TheAmbitiousCorry/trails/internal/signs"
	"github.com/TheAmbitiousCorry/trails/internal/trail"
)

var update = flag.Bool("update", false, "rewrite testdata/contract.golden")

const goldenPath = "testdata/contract.golden"

// refusalRules is every rule name any package refuses with, sorted and de-duplicated.
func refusalRules() []string {
	seen := map[string]bool{}
	var out []string
	for _, list := range [][]string{trail.RefusalRules, entries.RefusalRules, signs.RefusalRules, content.RefusalRules, markdown.RefusalRules, mcp.RefusalRules} {
		for _, r := range list {
			if !seen[r] {
				seen[r] = true
				out = append(out, r)
			}
		}
	}
	sort.Strings(out)
	return out
}

func listTools(t *testing.T) []*sdk.Tool {
	t.Helper()
	_, addr := startServer(t)
	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "0"}, nil)
	sess, err := client.Connect(context.Background(), &sdk.StreamableClientTransport{Endpoint: "http://" + addr + "/mcp", HTTPClient: http.DefaultClient}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	res, err := sess.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return res.Tools
}

// renderContract is the text pinned in the golden file: each tool's name, description and input schema, then the refusal rule names.
func renderContract(t *testing.T, tools []*sdk.Tool) string {
	t.Helper()
	sorted := append([]*sdk.Tool{}, tools...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	var b strings.Builder
	b.WriteString("contract " + strconv.Itoa(mcp.Contract) + "\n")
	for _, tool := range sorted {
		schema, err := json.MarshalIndent(tool.InputSchema, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		b.WriteString("\ntool " + tool.Name + "\ndescription: " + tool.Description + "\ninput_schema: " + string(schema) + "\n")
	}
	b.WriteString("\nrefusal rules\n")
	for _, r := range refusalRules() {
		b.WriteString("- " + r + "\n")
	}
	return b.String()
}

func TestContractGolden(t *testing.T) {
	got := renderContract(t, listTools(t))
	if *update {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("%v; run go test ./internal/mcp -update", err)
	}
	if got != string(want) {
		t.Errorf("the tool contract changed; if intended, bump the contract and run go test ./internal/mcp -update\n%s", firstDiff(string(want), got))
	}
}

func TestContractGoldenFailsOnMutatedDescription(t *testing.T) {
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Skip("golden file not written yet")
	}
	tools := listTools(t)
	tools[0].Description += " (edited)"
	if renderContract(t, tools) == string(want) {
		t.Error("a changed description left the rendered contract equal to the golden file")
	}
}

func TestContractCoversAllTools(t *testing.T) {
	var names []string
	total := 0
	for _, tool := range listTools(t) {
		names = append(names, tool.Name)
		total += len(tool.Description)
	}
	sort.Strings(names)
	if got := strings.Join(names, ","); got != "confirm,look,read,resolve_gap,search,session_end,session_start,write,write_sign" {
		t.Errorf("tools = %s", got)
	}
	// The spec caps descriptions at 1,500 tokens; four characters a token is the usual estimate.
	if total > 1500*4 {
		t.Errorf("descriptions total %d characters", total)
	}
}

func firstDiff(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	for i := 0; i < len(w) || i < len(g); i++ {
		var a, b string
		if i < len(w) {
			a = w[i]
		}
		if i < len(g) {
			b = g[i]
		}
		if a != b {
			return "line " + strconv.Itoa(i+1) + " differs:\n  golden: " + a + "\n  actual: " + b
		}
	}
	return ""
}

// Every rule name written in the source of the packages that refuse must be listed in a RefusalRules slice, so the golden file cannot miss one.
func TestRefusalRulesMatchSource(t *testing.T) {
	re := regexp.MustCompile(`(?:Rule: *|refusal\(|refuse\(|Rule\w* += *)"([^"]+)"`)
	found := map[string]bool{}
	for _, dir := range []string{"trail", "entries", "signs", "content", "mcp", "project"} {
		files, _ := filepath.Glob(filepath.Join("..", dir, "*.go"))
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			b, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			for _, m := range re.FindAllStringSubmatch(string(b), -1) {
				found[m[1]] = true
			}
		}
	}
	listed := map[string]bool{}
	for _, r := range refusalRules() {
		listed[r] = true
	}
	for r := range found {
		if !listed[r] {
			t.Errorf("rule %q is refused in source but not listed in a RefusalRules slice", r)
		}
	}
	for r := range listed {
		if !found[r] && r != "unsupported: <construct>" {
			t.Errorf("rule %q is listed but no source refuses with it", r)
		}
	}
}
