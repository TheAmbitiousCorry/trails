package mcp_test

import (
	"strings"
	"testing"
)

func openGap(t *testing.T, a *agent, query string) int64 {
	t.Helper()
	a.ok("look", map[string]any{"sign_id": "root"})
	if miss := a.ok("search", map[string]any{"query": query}); miss["miss"] != true {
		t.Fatalf("search %q = %v", query, miss)
	}
	ids := gapIDs(t, a.refused("session_end", map[string]any{}, "gaps_open"))
	return ids[len(ids)-1]
}

func TestAnotherSessionsGapIsFilledUnderTheRelevanceCheck(t *testing.T) {
	st, addr := startServer(t)
	a, b := newAgent(t, addr, ""), newAgent(t, addr, "")
	gap := openGap(t, a, "zqxv ledger cadence rounding")

	related := b.writeDecision("Ledger cadence", "The zqxv ledger closes each month; rounding is per line.")
	unrelated := b.writeDecision("Release train", "Ship on Fridays.")

	b.refused("resolve_gap", map[string]any{"gap_id": gap, "action": "merge", "entry_id": unrelated}, "gap_mismatch")
	b.refused("resolve_gap", map[string]any{"gap_id": gap, "action": "fill", "entry_id": unrelated}, "gap_mismatch")
	b.refused("resolve_gap", map[string]any{"gap_id": gap, "action": "dismiss", "reason": "not mine"}, "unknown_gap")

	b.ok("resolve_gap", map[string]any{"gap_id": gap, "action": "fill", "entry_id": related})
	var by string
	st.DB.QueryRow(`SELECT resolved_by FROM gaps WHERE id = ?`, gap).Scan(&by)
	if by != b.sid {
		t.Errorf("resolved_by = %q, want %q", by, b.sid)
	}
	if end := a.ok("session_end", map[string]any{}); end["closed"] != true {
		t.Errorf("A's session_end = %v", end)
	}
}

func TestOwnerResolutionFillsResolvedBy(t *testing.T) {
	st, addr := startServer(t)
	a := newAgent(t, addr, "")
	gap := openGap(t, a, "qaaa")
	mine := a.writeDecision("My note", "qaaa lives here.")
	a.ok("resolve_gap", map[string]any{"gap_id": gap, "action": "merge", "entry_id": mine})
	var by string
	st.DB.QueryRow(`SELECT resolved_by FROM gaps WHERE id = ?`, gap).Scan(&by)
	if by != a.sid {
		t.Errorf("resolved_by = %q, want %q", by, a.sid)
	}
}

func TestMergeCoversHalfTheTermsNotLess(t *testing.T) {
	_, addr := startServer(t)
	a, b := newAgent(t, addr, ""), newAgent(t, addr, "")
	gap := openGap(t, a, "zqxv flange torque gasket")
	one := b.writeDecision("Flange notes", "Only the flange is covered here.")
	two := b.writeDecision("Torque notes", "The zqxv flange torque spec is in the manual.")
	b.refused("resolve_gap", map[string]any{"gap_id": gap, "action": "merge", "entry_id": one}, "gap_mismatch")
	b.ok("resolve_gap", map[string]any{"gap_id": gap, "action": "merge", "entry_id": two})
}

func TestGapCloseCapOnOtherSessionsGaps(t *testing.T) {
	st, addr := startServer(t)
	a, b := newAgent(t, addr, ""), newAgent(t, addr, "")
	words := []string{"zqaa", "zqab", "zqac", "zqad", "zqae", "zqaf", "zqag", "zqah", "zqai", "zqaj", "zqak"}
	var gaps []int64
	for _, w := range words {
		gaps = append(gaps, openGap(t, a, w))
	}
	target := b.writeDecision("Words", strings.Join(words, " "))
	for _, g := range gaps[:10] {
		b.ok("resolve_gap", map[string]any{"gap_id": g, "action": "merge", "entry_id": target})
	}
	b.refused("resolve_gap", map[string]any{"gap_id": gaps[10], "action": "merge", "entry_id": target}, "gap_close_cap")

	// Gaps B opened itself do not count against the cap.
	own := openGap(t, b, "zqzz")
	b.ok("resolve_gap", map[string]any{"gap_id": own, "action": "merge", "entry_id": target})

	st.DB.Exec(`UPDATE settings SET value = '11' WHERE key = 'gap_close_cap'`)
	b.ok("resolve_gap", map[string]any{"gap_id": gaps[10], "action": "merge", "entry_id": target})
}
