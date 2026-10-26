package mcp

import (
	"os"
	"strings"
	"testing"
)

// Each how-to-walk line must lead with words the protocol file also carries, so editing the protocol surfaces here.
func TestHowToWalkFollowsProtocol(t *testing.T) {
	b, err := os.ReadFile("../../../protocol/PROTOCOL.md")
	if err != nil {
		t.Skipf("protocol file not beside the module: %v", err)
	}
	for _, phrase := range []string{
		"session_start(task, task_type)", "Walk with `look`", "Read in full only when the `read_when` line matches your task",
		"`search` before `grep`", "Write what you learned", "follow the fix, do not retry unchanged", "End with `session_end`",
	} {
		if !strings.Contains(string(b), phrase) {
			t.Errorf("protocol no longer says %q", phrase)
		}
	}
	joined := strings.ToLower(howToWalk)
	for _, word := range []string{"session_start", "look", "read_when", "search before grep", "write", "refusal", "session_end", "resolve_gap"} {
		if !strings.Contains(joined, word) {
			t.Errorf("how_to_walk lacks %q", word)
		}
	}
}
