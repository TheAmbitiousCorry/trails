package mcp

import (
	"encoding/json"
	"strings"
)

// countLines is the size of the response as the agent receives it, indented JSON, so `lines` states what the hop cost.
func countLines(v readOut) int {
	b, _ := json.MarshalIndent(v, "", "  ")
	return strings.Count(string(b), "\n") + 1
}
