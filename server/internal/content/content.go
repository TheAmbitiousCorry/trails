// Package content validates Trails entry bodies, which are ProseMirror JSON documents.
// It is an allow-list tree walk, not a schema: testdata fixtures pin its lists to the front end's editor schema.
package content

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"unicode"
)

// Rule names are part of the agent-facing refusal text, so renaming one is a protocol change.
const (
	RuleUnknownNode  = "schema:unknown_node"
	RuleUnknownMark  = "schema:unknown_mark"
	RuleHeadingLevel = "schema:heading_level"
	RuleURLScheme    = "schema:url_scheme"
	RuleDepth        = "schema:depth"
	RuleBytes        = "schema:bytes"
	RuleMalformed    = "schema:malformed"
)

const (
	// MaxDepth is the deepest nesting allowed; the doc root is depth 0.
	MaxDepth = 32

	// MaxBytes is the largest accepted body, measured on the raw JSON.
	MaxBytes = 64 * 1024

	// localServePrefix is where the server serves stored images from.
	localServePrefix = "/media/"
)

var allowedNodes = map[string]struct{}{
	"doc":         {},
	"paragraph":   {},
	"text":        {},
	"heading":     {},
	"bulletList":  {},
	"orderedList": {},
	"listItem":    {},
	"blockquote":  {},
	"codeBlock":   {},
	"hardBreak":   {},
	"image":       {},
	"table":       {},
	"tableRow":    {},
	"tableCell":   {},
	"tableHeader": {},
}

var allowedMarks = map[string]struct{}{
	"bold":   {},
	"italic": {},
	"code":   {},
	"link":   {},
}

// linkSchemes governs link.href. Schemeless values (relative paths, anchors) pass, as in the client sanitizer.
var linkSchemes = map[string]struct{}{
	"http":   {},
	"https":  {},
	"mailto": {},
}

// imageSchemes governs image.src. data: and blob: are excluded because they carry bytes past the upload boundary.
var imageSchemes = map[string]struct{}{
	"http":  {},
	"https": {},
}

// Refusal is the error Validate returns. Rule names the failed check and Fix tells the writer what to change.
type Refusal struct {
	Rule string
	Fix  string
}

func (r *Refusal) Error() string { return r.Rule + ": " + r.Fix }

func refuse(rule, format string, args ...any) *Refusal {
	return &Refusal{Rule: rule, Fix: fmt.Sprintf(format, args...)}
}

type node struct {
	Type    string          `json:"type"`
	Text    string          `json:"text"`
	Content []node          `json:"content"`
	Marks   []mark          `json:"marks"`
	Attrs   json.RawMessage `json:"attrs"`
}

type mark struct {
	Type  string          `json:"type"`
	Attrs json.RawMessage `json:"attrs"`
}

// Validate reports whether raw is an acceptable body. A nil error means accepted; any other error is a *Refusal.
func Validate(raw json.RawMessage) error {
	if _, err := parse(raw); err != nil {
		return err
	}
	return nil
}

func parse(raw json.RawMessage) (*node, *Refusal) {
	if len(raw) > MaxBytes {
		return nil, refuse(RuleBytes, "body is %d bytes, the cap is %d; split it into several entries", len(raw), MaxBytes)
	}
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, refuse(RuleMalformed, "body is empty; send a document of type doc")
	}
	var root node
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, refuse(RuleMalformed, "body is not a valid document: %v", err)
	}
	if root.Type != "doc" {
		return nil, refuse(RuleMalformed, "the root must be type doc, got %q", root.Type)
	}
	if err := walk(&root, 0); err != nil {
		return nil, err
	}
	return &root, nil
}

func walk(n *node, depth int) *Refusal {
	if depth > MaxDepth {
		return refuse(RuleDepth, "nesting is deeper than %d levels; flatten the structure", MaxDepth)
	}
	if n.Type == "" {
		return refuse(RuleMalformed, "a node has no type")
	}
	if _, ok := allowedNodes[n.Type]; !ok {
		return refuse(RuleUnknownNode, "node type %q is not allowed; use paragraph, heading, lists, codeBlock, blockquote, image, table or hardBreak", n.Type)
	}

	for i := range n.Marks {
		m := &n.Marks[i]
		if m.Type == "" {
			return refuse(RuleMalformed, "a mark has no type")
		}
		if _, ok := allowedMarks[m.Type]; !ok {
			return refuse(RuleUnknownMark, "mark type %q is not allowed; use bold, italic, code or link", m.Type)
		}
		if m.Type == "link" {
			if err := checkURL(m.Attrs, "href", linkSchemes, false); err != nil {
				return err
			}
		}
	}

	switch n.Type {
	case "heading":
		if err := checkHeadingLevel(n.Attrs); err != nil {
			return err
		}
	case "image":
		if err := checkURL(n.Attrs, "src", imageSchemes, true); err != nil {
			return err
		}
	}

	for i := range n.Content {
		if err := walk(&n.Content[i], depth+1); err != nil {
			return err
		}
	}
	return nil
}

func attrMap(attrs json.RawMessage) (map[string]json.RawMessage, *Refusal) {
	if len(attrs) == 0 || bytes.Equal(attrs, []byte("null")) {
		return nil, nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(attrs, &m); err != nil {
		return nil, refuse(RuleMalformed, "attrs must be an object: %v", err)
	}
	return m, nil
}

func checkHeadingLevel(attrs json.RawMessage) *Refusal {
	const fix = "heading level must be 2, 3 or 4; level 1 is the entry title"
	m, err := attrMap(attrs)
	if err != nil {
		return err
	}
	var level int
	if raw, ok := m["level"]; !ok || json.Unmarshal(raw, &level) != nil || level < 2 || level > 4 {
		return refuse(RuleHeadingLevel, "%s", fix)
	}
	return nil
}

// checkURL applies the scheme allow-list to one attr. Schemeless values pass for links; images must be http, https or under the local serve prefix.
func checkURL(attrs json.RawMessage, name string, schemes map[string]struct{}, isImage bool) *Refusal {
	m, err := attrMap(attrs)
	if err != nil {
		return err
	}
	var val string
	if raw, ok := m[name]; ok {
		if json.Unmarshal(raw, &val) != nil {
			return refuse(RuleMalformed, "%s must be a string", name)
		}
	}
	val = strings.TrimSpace(val)
	if val == "" {
		if isImage {
			return refuse(RuleMalformed, "image needs a src")
		}
		return nil
	}
	// Browsers strip tabs and newlines inside a scheme, so "java\tscript:" would pass a naive prefix check.
	if strings.IndexFunc(val, unicode.IsControl) >= 0 {
		return refuse(RuleURLScheme, "%s contains a control character", name)
	}

	colon := strings.IndexByte(val, ':')
	slash := strings.IndexAny(val, "/?#")
	hasScheme := colon > 0 && (slash < 0 || colon < slash)
	if !hasScheme {
		if isImage && !strings.HasPrefix(val, localServePrefix) {
			return refuse(RuleURLScheme, "image src must be http, https or start with %s", localServePrefix)
		}
		return nil
	}

	scheme := strings.ToLower(val[:colon])
	if _, ok := schemes[scheme]; !ok {
		return refuse(RuleURLScheme, "scheme %q is not allowed in %s; use %s", scheme, name, schemeList(schemes))
	}
	if scheme == "http" || scheme == "https" {
		if u, err := url.Parse(val); err == nil && u.User != nil {
			return refuse(RuleURLScheme, "%s must not include a username or password", name)
		}
	}
	return nil
}

func schemeList(schemes map[string]struct{}) string {
	if _, ok := schemes["mailto"]; ok {
		return "http, https or mailto"
	}
	return "http or https"
}

// PlainText flattens raw into searchable text. Input that does not parse yields "".
func PlainText(raw json.RawMessage) string {
	var root node
	if json.Unmarshal(raw, &root) != nil {
		return ""
	}
	var b strings.Builder
	writeText(&b, &root, 0)
	return strings.TrimSpace(b.String())
}

// writeText stops at MaxDepth so unvalidated input cannot make it recurse without bound.
func writeText(b *strings.Builder, n *node, depth int) {
	if depth > MaxDepth {
		return
	}
	switch n.Type {
	case "text":
		b.WriteString(n.Text)
		return
	case "hardBreak":
		b.WriteByte('\n')
		return
	}
	for i := range n.Content {
		writeText(b, &n.Content[i], depth+1)
	}
	switch n.Type {
	case "tableCell", "tableHeader":
		b.WriteByte(' ')
	case "paragraph", "heading", "codeBlock", "listItem", "tableRow":
		b.WriteByte('\n')
	}
}

// WordCount counts whitespace-separated words in the plain text of raw.
func WordCount(raw json.RawMessage) int {
	return len(strings.Fields(PlainText(raw)))
}
