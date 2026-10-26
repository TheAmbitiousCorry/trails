package markdown

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/TheAmbitiousCorry/trails/internal/content"
)

var (
	// entityRef is anchored at the start of its input and matches the references goldmark resolves.
	entityRef = regexp.MustCompile(`^&(#[0-9]{1,7}|#[xX][0-9a-fA-F]{1,6}|[A-Za-z][A-Za-z0-9]*);`)

	headingStart = regexp.MustCompile(`^#{1,6}( |$)`)
	bulletStart  = regexp.MustCompile(`^[-+]( |$|[-|:])`)
	orderedStart = regexp.MustCompile(`^[0-9]{1,9}[.)]( |$)`)
	setextStart  = regexp.MustCompile(`^=+$`)
)

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

// ToMarkdown renders a valid document as canonical Markdown: emitting it twice gives the same bytes. An invalid document returns the validator's *content.Refusal.
func ToMarkdown(doc json.RawMessage) (string, error) {
	if err := content.Validate(doc); err != nil {
		return "", err
	}
	var root node
	if err := json.Unmarshal(doc, &root); err != nil {
		return "", err
	}
	out := blocks(root.Content)
	if out == "" {
		return "", nil
	}
	return out + "\n", nil
}

func blocks(nodes []node) string {
	var parts []string
	for i := range nodes {
		if s := blockString(&nodes[i]); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, "\n\n")
}

func blockString(n *node) string {
	switch n.Type {
	case "paragraph":
		return inline(n.Content, ctxParagraph)
	case "heading":
		prefix := strings.Repeat("#", attrInt(n.Attrs, "level"))
		if text := inline(n.Content, ctxHeading); text != "" {
			return prefix + " " + text
		}
		return prefix
	case "bulletList":
		return list(n.Content, "- ")
	case "orderedList":
		return list(n.Content, "1. ")
	case "blockquote":
		return prefixLines(blocks(n.Content), "> ", ">")
	case "codeBlock":
		return codeBlock(n)
	case "image":
		return "![" + escapeText(attrString(n.Attrs, "alt"), ctxParagraph) + "](" + linkDest(attrString(n.Attrs, "src")) + ")"
	case "table":
		return table(n.Content)
	}
	return ""
}

// list separates items with a single newline; an item holding several blocks keeps its own blank lines, indented under the marker.
func list(items []node, marker string) string {
	indent := strings.Repeat(" ", len(marker))
	lines := make([]string, 0, len(items))
	for i := range items {
		body := blocks(items[i].Content)
		if body == "" {
			lines = append(lines, strings.TrimRight(marker, " "))
			continue
		}
		var b strings.Builder
		for j, l := range strings.Split(body, "\n") {
			switch {
			case j == 0:
				b.WriteString(marker + l)
			case l == "":
				b.WriteString("\n")
			default:
				b.WriteString("\n" + indent + l)
			}
		}
		lines = append(lines, b.String())
	}
	return strings.Join(lines, "\n")
}

func prefixLines(s, prefix, bare string) string {
	if s == "" {
		return bare
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if l == "" {
			lines[i] = bare
		} else {
			lines[i] = prefix + l
		}
	}
	return strings.Join(lines, "\n")
}

func codeBlock(n *node) string {
	var text strings.Builder
	for _, c := range n.Content {
		text.WriteString(c.Text)
	}
	fence := strings.Repeat("`", max(3, longestRun(text.String(), '`')+1))
	head := fence + attrString(n.Attrs, "language")
	if text.Len() == 0 {
		return head + "\n" + fence
	}
	return head + "\n" + text.String() + "\n" + fence
}

func longestRun(s string, c byte) int {
	longest, run := 0, 0
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	return longest
}

// table writes the first row as the header, since pipe tables require one.
func table(rows []node) string {
	if len(rows) == 0 {
		return ""
	}
	lines := make([]string, 0, len(rows)+1)
	for i := range rows {
		cells := make([]string, len(rows[i].Content))
		for j, cell := range rows[i].Content {
			if len(cell.Content) > 0 {
				cells[j] = inline(cell.Content[0].Content, ctxCell)
			}
			if cells[j] == "" {
				cells[j] = " "
			}
		}
		lines = append(lines, "| "+strings.Join(cells, " | ")+" |")
		if i == 0 {
			sep := make([]string, len(cells))
			for j := range sep {
				sep[j] = "---"
			}
			lines = append(lines, "| "+strings.Join(sep, " | ")+" |")
		}
	}
	return strings.Join(lines, "\n")
}

type context int

const (
	ctxParagraph context = iota
	ctxHeading
	ctxCell
)

// run is a maximal sequence of text nodes under one link, or a single hard break.
type run struct {
	href  string
	nodes []node
	brk   bool
}

func inline(nodes []node, ctx context) string {
	var runs []run
	for _, n := range nodes {
		switch {
		case n.Type == "hardBreak":
			runs = append(runs, run{brk: true})
		case n.Type == "text" && n.Text != "":
			href := linkHref(n)
			if last := len(runs) - 1; last >= 0 && !runs[last].brk && runs[last].href == href {
				runs[last].nodes = append(runs[last].nodes, n)
			} else {
				runs = append(runs, run{href: href, nodes: []node{n}})
			}
		}
	}

	var b strings.Builder
	for i, r := range runs {
		switch {
		case r.brk && ctx == ctxCell:
			b.WriteByte(' ')
		case r.brk:
			b.WriteString("\\\n")
		case r.href != "":
			b.WriteString("[" + textNodes(r.nodes, ctx, neighbourEmphasis(runs, i)) + "](" + linkDest(r.href) + ")")
		default:
			b.WriteString(textNodes(r.nodes, ctx, neighbourEmphasis(runs, i)))
		}
	}
	return fixLines(b.String(), ctx)
}

func linkHref(n node) string {
	for _, m := range n.Marks {
		if m.Type == "link" {
			return attrString(m.Attrs, "href")
		}
	}
	return ""
}

func hasMark(n node, typ string) bool {
	for _, m := range n.Marks {
		if m.Type == typ {
			return true
		}
	}
	return false
}

func emphasised(n node) bool { return hasMark(n, "bold") || hasMark(n, "italic") }

// neighbourEmphasis reports whether a run beside runs[i] carries emphasis; italic then uses underscores so two star delimiters never touch.
func neighbourEmphasis(runs []run, i int) bool {
	for _, j := range []int{i - 1, i + 1} {
		if j < 0 || j >= len(runs) {
			continue
		}
		for _, n := range runs[j].nodes {
			if emphasised(n) {
				return true
			}
		}
	}
	return false
}

func textNodes(nodes []node, ctx context, underscore bool) string {
	var b strings.Builder
	for _, n := range nodes {
		b.WriteString(renderText(n, ctx, underscore))
	}
	return b.String()
}

// renderText wraps marks canonically: bold outside italic outside code. Whitespace at the edges stays outside the delimiters.
func renderText(n node, ctx context, underscore bool) string {
	text := strings.ReplaceAll(n.Text, "\n", " ")
	if hasMark(n, "code") {
		return wrap(n, codeSpan(text), underscore)
	}
	core := strings.TrimSpace(text)
	if core == "" || !emphasised(n) {
		return escapeText(text, ctx)
	}
	lead := text[:strings.Index(text, core)]
	trail := text[len(lead)+len(core):]
	return escapeText(lead, ctx) + wrap(n, escapeText(core, ctx), underscore) + escapeText(trail, ctx)
}

func wrap(n node, s string, underscore bool) string {
	bold, italic := hasMark(n, "bold"), hasMark(n, "italic")
	delim := "*"
	if underscore || bold {
		delim = "_"
	}
	if italic {
		s = delim + s + delim
	}
	if bold {
		s = "**" + s + "**"
	}
	return s
}

func codeSpan(s string) string {
	fence := strings.Repeat("`", longestRun(s, '`')+1)
	if strings.HasPrefix(s, "`") || strings.HasSuffix(s, "`") || strings.HasPrefix(s, " ") || strings.HasSuffix(s, " ") {
		s = " " + s + " "
	}
	return fence + s + fence
}

// escapeText backslash-escapes only the characters that would read back as syntax in ctx.
func escapeText(s string, ctx context) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		escape := false
		switch c {
		case '\\', '*', '`', '[', ']', '~':
			escape = true
		case '_':
			escape = !(i > 0 && i+1 < len(s) && isAlnum(s[i-1]) && isAlnum(s[i+1]))
		case '<':
			escape = i+1 < len(s) && (isAlpha(s[i+1]) || strings.IndexByte("/!?", s[i+1]) >= 0)
		case '&':
			escape = entityRef.MatchString(s[i:])
		case '!':
			escape = i+1 == len(s) || s[i+1] == '['
		case '|':
			escape = ctx == ctxCell
		case '#':
			escape = ctx == ctxHeading
		}
		if escape {
			b.WriteByte('\\')
		}
		b.WriteByte(c)
	}
	return b.String()
}

func isAlpha(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
func isAlnum(c byte) bool { return isAlpha(c) || c >= '0' && c <= '9' }

// fixLines protects what only shows up at a line edge: leading and trailing spaces, which the parser strips, and block openers in paragraph text.
func fixLines(s string, ctx context) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		switch {
		case strings.HasPrefix(l, " "):
			l = "&#32;" + l[1:]
		case strings.HasPrefix(l, "\t"):
			l = "&#9;" + l[1:]
		}
		if ctx == ctxParagraph {
			switch {
			case headingStart.MatchString(l) || strings.HasPrefix(l, ">") || bulletStart.MatchString(l) || setextStart.MatchString(l):
				l = "\\" + l
			case orderedStart.MatchString(l):
				at := strings.IndexAny(l, ".)")
				l = l[:at] + "\\" + l[at:]
			}
		}
		lines[i] = l
	}
	out := strings.Join(lines, "\n")
	if strings.HasSuffix(out, " ") {
		out = out[:len(out)-1] + "&#32;"
	} else if strings.HasSuffix(out, "\t") {
		out = out[:len(out)-1] + "&#9;"
	}
	return out
}

// linkDest writes a link or image target bare when it can be and in angle brackets when it holds a space.
func linkDest(s string) string {
	if s == "" || strings.ContainsAny(s, " \t") {
		return "<" + strings.NewReplacer("\\", "\\\\", "<", "\\<", ">", "\\>").Replace(s) + ">"
	}
	return strings.NewReplacer("\\", "\\\\", "(", "\\(", ")", "\\)", "<", "\\<").Replace(s)
}

func attrString(attrs json.RawMessage, name string) string {
	var m map[string]json.RawMessage
	if len(attrs) == 0 || bytes.Equal(attrs, []byte("null")) || json.Unmarshal(attrs, &m) != nil {
		return ""
	}
	var v string
	_ = json.Unmarshal(m[name], &v)
	return v
}

func attrInt(attrs json.RawMessage, name string) int {
	var m map[string]json.RawMessage
	if len(attrs) == 0 || json.Unmarshal(attrs, &m) != nil {
		return 0
	}
	var v int
	_ = json.Unmarshal(m[name], &v)
	return v
}
