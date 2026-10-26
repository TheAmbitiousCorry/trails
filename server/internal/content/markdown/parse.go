// Package markdown converts between Markdown and the Trails content schema.
// FromMarkdown walks goldmark's AST and refuses every construct the schema cannot hold; ToMarkdown is a canonical emitter.
package markdown

import (
	"encoding/json"
	"reflect"
	"strings"

	"github.com/TheAmbitiousCorry/trails/internal/content"
	"github.com/yuin/goldmark"
	gast "github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	gtext "github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// The Strikethrough, TaskList and Footnote extensions are enabled only so their syntax parses into nodes that can be refused by name.
var parser = goldmark.New(goldmark.WithExtensions(extension.Table, extension.Strikethrough, extension.TaskList, extension.Footnote))

var fixes = map[string]string{
	"html block":     "remove the HTML and write the content as Markdown",
	"inline html":    "remove the HTML and write the content as Markdown",
	"task list":      "use a plain bullet list; checkbox state is not stored",
	"strikethrough":  "remove the ~~ markers; strikethrough is not stored",
	"heading level":  "use ## to ####; level 1 is the entry title",
	"thematic break": "remove the horizontal rule and use a heading to separate sections",
	"footnote":       "put the footnote text inline",
}

const genericFix = "rewrite it with paragraphs, headings, lists, code blocks, blockquotes, images or tables"

// RefusalRules is the Markdown adapter's one rule family; the construct follows the colon.
var RefusalRules = []string{"unsupported: <construct>"}

func unsupported(construct string) *content.Refusal {
	fix, ok := fixes[construct]
	if !ok {
		fix = genericFix
	}
	return &content.Refusal{Rule: "unsupported: " + construct, Fix: fix}
}

// FromMarkdown converts src to a validated document. A construct outside the schema returns a *content.Refusal named unsupported: <construct>.
func FromMarkdown(src string) (json.RawMessage, error) {
	if len(src) > content.MaxBytes {
		return nil, &content.Refusal{Rule: content.RuleBytes, Fix: "body is over the byte cap; split it into several entries"}
	}
	source := []byte(src)
	root := parser.Parser().Parse(gtext.NewReader(source))

	p := &reader{source: source}
	blocks := p.blocks(root, 1)
	if p.err != nil {
		return nil, p.err
	}
	if blocks == nil {
		blocks = []any{}
	}
	raw, err := json.Marshal(map[string]any{"type": "doc", "content": blocks})
	if err != nil {
		return nil, err
	}
	if err := content.Validate(raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// reader latches the first refusal; later work is skipped so the refusal reported is the first construct in document order.
type reader struct {
	source []byte
	err    *content.Refusal
}

func (p *reader) refuse(r *content.Refusal) {
	if p.err == nil {
		p.err = r
	}
}

func block(typ string, attrs map[string]any, children []any) map[string]any {
	n := map[string]any{"type": typ}
	if len(children) > 0 {
		n["content"] = children
	}
	if len(attrs) > 0 {
		n["attrs"] = attrs
	}
	return n
}

func textNode(s string, marks []any) map[string]any {
	n := map[string]any{"type": "text", "text": s}
	if len(marks) > 0 {
		n["marks"] = marks
	}
	return n
}

// withMark copies marks so sibling branches never share a backing array.
func withMark(marks []any, m map[string]any) []any {
	out := make([]any, len(marks), len(marks)+1)
	copy(out, marks)
	return append(out, m)
}

func (p *reader) blocks(n gast.Node, depth int) []any {
	var out []any
	for c := n.FirstChild(); c != nil && p.err == nil; c = c.NextSibling() {
		if v := p.blockNode(c, depth); v != nil {
			out = append(out, v)
		}
	}
	return out
}

func (p *reader) blockNode(n gast.Node, depth int) map[string]any {
	if depth > content.MaxDepth {
		p.refuse(&content.Refusal{Rule: content.RuleDepth, Fix: "nesting is deeper than the cap; flatten the structure"})
		return nil
	}
	switch n.Kind() {
	case gast.KindHeading:
		level := n.(*gast.Heading).Level
		if level < 2 || level > 4 {
			p.refuse(unsupported("heading level"))
			return nil
		}
		return block("heading", map[string]any{"level": level}, p.inline(n, nil, 0))
	case gast.KindParagraph, gast.KindTextBlock:
		if img, ok := soleImage(n); ok {
			return p.image(img)
		}
		return block("paragraph", nil, p.inline(n, nil, 0))
	case gast.KindBlockquote:
		return block("blockquote", nil, p.blocks(n, depth+1))
	case gast.KindList:
		return p.list(n.(*gast.List), depth)
	case gast.KindCodeBlock, gast.KindFencedCodeBlock:
		return p.codeBlock(n)
	case east.KindTable:
		return p.table(n.(*east.Table), depth)
	case gast.KindHTMLBlock:
		p.refuse(unsupported("html block"))
	case gast.KindThematicBreak:
		p.refuse(unsupported("thematic break"))
	case east.KindFootnote, east.KindFootnoteList:
		p.refuse(unsupported("footnote"))
	default:
		p.refuse(unsupported(strings.ToLower(n.Kind().String())))
	}
	return nil
}

// list refuses a start other than 1 because the schema has no start attr and the emitter numbers every item 1.
func (p *reader) list(l *gast.List, depth int) map[string]any {
	if l.IsOrdered() && l.Start != 1 {
		p.refuse(unsupported("ordered list start"))
		return nil
	}
	var items []any
	for it := l.FirstChild(); it != nil && p.err == nil; it = it.NextSibling() {
		items = append(items, block("listItem", nil, p.blocks(it, depth+2)))
	}
	if l.IsOrdered() {
		return block("orderedList", nil, items)
	}
	return block("bulletList", nil, items)
}

func (p *reader) codeBlock(n gast.Node) map[string]any {
	var attrs map[string]any
	if f, ok := n.(*gast.FencedCodeBlock); ok && f.Info != nil {
		info := strings.TrimSpace(string(f.Info.Value(p.source)))
		lang := string(f.Language(p.source))
		if info != lang {
			p.refuse(unsupported("code block info string"))
			return nil
		}
		if lang != "" {
			attrs = map[string]any{"language": lang}
		}
	}
	text := strings.TrimSuffix(string(n.Lines().Value(p.source)), "\n")
	var children []any
	if text != "" {
		children = []any{textNode(text, nil)}
	}
	return block("codeBlock", attrs, children)
}

func (p *reader) table(t *east.Table, depth int) map[string]any {
	for _, a := range t.Alignments {
		if a != east.AlignNone {
			p.refuse(unsupported("table alignment"))
			return nil
		}
	}
	var rows []any
	for row := t.FirstChild(); row != nil && p.err == nil; row = row.NextSibling() {
		cellType := "tableCell"
		if row.Kind() == east.KindTableHeader {
			cellType = "tableHeader"
		}
		var cells []any
		for cell := row.FirstChild(); cell != nil && p.err == nil; cell = cell.NextSibling() {
			para := block("paragraph", nil, p.inline(cell, nil, 0))
			cells = append(cells, block(cellType, nil, []any{para}))
		}
		rows = append(rows, block("tableRow", nil, cells))
	}
	return block("table", nil, rows)
}

// soleImage reports a paragraph whose only content is one image; the schema's image is a block node.
func soleImage(n gast.Node) (*gast.Image, bool) {
	img, ok := n.FirstChild().(*gast.Image)
	return img, ok && n.ChildCount() == 1
}

func (p *reader) image(img *gast.Image) map[string]any {
	if len(img.Title) > 0 {
		p.refuse(unsupported("image title"))
		return nil
	}
	attrs := map[string]any{"src": resolveDest(img.Destination)}
	if alt := p.plain(img); alt != "" {
		attrs["alt"] = alt
	}
	return block("image", attrs, nil)
}

// plain flattens inline children to text; an image's alt cannot carry formatting.
func (p *reader) plain(n gast.Node) string {
	var b strings.Builder
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		switch t := c.(type) {
		case *gast.Text:
			b.WriteString(unescape(t.Value(p.source)))
			if t.SoftLineBreak() || t.HardLineBreak() {
				b.WriteByte(' ')
			}
		case *gast.String:
			b.Write(t.Value)
		default:
			b.WriteString(p.plain(c))
		}
	}
	return b.String()
}

// maxInline bounds goldmark's emphasis and link nesting; block depth is checked separately.
const maxInline = content.MaxDepth

func (p *reader) inline(n gast.Node, marks []any, depth int) []any {
	var out []any
	for c := n.FirstChild(); c != nil && p.err == nil; c = c.NextSibling() {
		out = append(out, p.inlineNode(c, marks, depth)...)
	}
	return mergeText(out)
}

func (p *reader) inlineNode(n gast.Node, marks []any, depth int) []any {
	if depth > maxInline {
		p.refuse(&content.Refusal{Rule: content.RuleDepth, Fix: "inline nesting is deeper than the cap; flatten the structure"})
		return nil
	}
	switch n.Kind() {
	case gast.KindText:
		t := n.(*gast.Text)
		var out []any
		if s := unescape(t.Value(p.source)); s != "" {
			out = append(out, textNode(s, marks))
		}
		switch {
		case t.HardLineBreak():
			out = append(out, map[string]any{"type": "hardBreak"})
		case t.SoftLineBreak():
			out = append(out, textNode(" ", marks))
		}
		return out
	case gast.KindString:
		if s := string(n.(*gast.String).Value); s != "" {
			return []any{textNode(s, marks)}
		}
	case gast.KindEmphasis:
		name := "italic"
		if n.(*gast.Emphasis).Level >= 2 {
			name = "bold"
		}
		return p.inline(n, withMark(marks, map[string]any{"type": name}), depth+1)
	case gast.KindCodeSpan:
		var b strings.Builder
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			if t, ok := c.(*gast.Text); ok {
				b.Write(t.Value(p.source))
			}
		}
		if b.Len() > 0 {
			return []any{textNode(b.String(), withMark(marks, map[string]any{"type": "code"}))}
		}
	case gast.KindLink:
		l := n.(*gast.Link)
		if len(l.Title) > 0 {
			p.refuse(unsupported("link title"))
			return nil
		}
		m := map[string]any{"type": "link", "attrs": map[string]any{"href": resolveDest(l.Destination)}}
		return p.inline(n, withMark(marks, m), depth+1)
	case gast.KindAutoLink:
		a := n.(*gast.AutoLink)
		m := map[string]any{"type": "link", "attrs": map[string]any{"href": string(a.URL(p.source))}}
		return []any{textNode(string(a.Label(p.source)), withMark(marks, m))}
	case gast.KindImage:
		p.refuse(unsupported("inline image"))
	case gast.KindRawHTML:
		p.refuse(unsupported("inline html"))
	case east.KindStrikethrough:
		p.refuse(unsupported("strikethrough"))
	case east.KindTaskCheckBox:
		p.refuse(unsupported("task list"))
	case east.KindFootnoteLink, east.KindFootnoteBacklink:
		p.refuse(unsupported("footnote"))
	default:
		p.refuse(unsupported(strings.ToLower(n.Kind().String())))
	}
	return nil
}

// mergeText joins adjacent text nodes with equal marks, which soft line breaks and escapes split apart.
func mergeText(nodes []any) []any {
	var out []any
	for _, n := range nodes {
		cur := n.(map[string]any)
		if len(out) > 0 && cur["type"] == "text" {
			prev := out[len(out)-1].(map[string]any)
			if prev["type"] == "text" && reflect.DeepEqual(prev["marks"], cur["marks"]) {
				prev["text"] = prev["text"].(string) + cur["text"].(string)
				continue
			}
		}
		out = append(out, n)
	}
	return out
}

// resolveDest resolves a link or image destination the way goldmark's own renderer does.
func resolveDest(b []byte) string {
	b = util.UnescapePunctuations(b)
	b = util.ResolveNumericReferences(b)
	return string(util.ResolveEntityNames(b))
}

// unescape resolves backslash escapes and character references in one pass, so an escaped ampersand is not resolved a second time.
func unescape(b []byte) string {
	var out strings.Builder
	for i := 0; i < len(b); i++ {
		switch {
		case b[i] == '\\' && i+1 < len(b) && util.IsPunct(b[i+1]):
			out.WriteByte(b[i+1])
			i++
		case b[i] == '&':
			if loc := entityRef.FindIndex(b[i:]); loc != nil {
				ref := b[i : i+loc[1]]
				out.Write(util.ResolveEntityNames(util.ResolveNumericReferences(ref)))
				i += loc[1] - 1
				continue
			}
			out.WriteByte('&')
		default:
			out.WriteByte(b[i])
		}
	}
	return out.String()
}
