package mcp

import (
	"encoding/json"
	"strings"
)

type pmNode struct {
	Type    string          `json:"type"`
	Text    string          `json:"text"`
	Content []pmNode        `json:"content"`
	Attrs   json.RawMessage `json:"attrs"`
}

// toMarkdown is the step 1 serializer: paragraphs and headings only, other nodes degrade to their text. The full adapter replaces it in step 4.
func toMarkdown(bodyJSON string) (string, error) {
	var doc pmNode
	if err := json.Unmarshal([]byte(bodyJSON), &doc); err != nil {
		return "", err
	}
	var blocks []string
	for _, n := range doc.Content {
		text := inlineText(n)
		if n.Type == "heading" {
			var a struct {
				Level int `json:"level"`
			}
			json.Unmarshal(n.Attrs, &a)
			if a.Level < 2 || a.Level > 4 {
				a.Level = 2
			}
			text = strings.Repeat("#", a.Level) + " " + text
		}
		blocks = append(blocks, text)
	}
	return strings.Join(blocks, "\n\n"), nil
}

func inlineText(n pmNode) string {
	if n.Type == "hardBreak" {
		return "\n"
	}
	var b strings.Builder
	b.WriteString(n.Text)
	for _, c := range n.Content {
		b.WriteString(inlineText(c))
	}
	return b.String()
}
