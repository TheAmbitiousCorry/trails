package mcp_test

import "testing"

func orientOf(t *testing.T, a *agent) map[string]any {
	t.Helper()
	return a.ok("session_start", map[string]any{"task": "again", "task_type": "report"})["orient"].(map[string]any)
}

func TestOrientCountsStaleEntriesAndOpenGaps(t *testing.T) {
	st, addr := startServer(t)
	a := newAgent(t, addr, "")
	id := a.writeDecision("Soon stale", "Written here.")
	if _, err := st.DB.Exec(`INSERT INTO anchors (entry_id, type, path_or_url, ok) VALUES (?, 'file', 'gone.go', 0)`, id); err != nil {
		t.Fatal(err)
	}
	a.ok("search", map[string]any{"query": "qaaa"})

	o := orientOf(t, a)
	if o["stale_count"] != float64(1) || o["open_gaps"] != float64(1) {
		t.Errorf("stale_count %v, open_gaps %v, want 1 and 1", o["stale_count"], o["open_gaps"])
	}
	if n := o["notice"]; n != "1 stale, 1 open gaps: the trails:navigate skill works through them" {
		t.Errorf("notice = %q", n)
	}
}

func TestOrientNoticeAtGapCapLeadsWithTheCapMessage(t *testing.T) {
	st, addr := startServer(t)
	st.DB.Exec(`UPDATE settings SET value = '1' WHERE key = 'open_gap_cap'`)
	a := newAgent(t, addr, "")
	a.ok("search", map[string]any{"query": "qaaa"})

	want := "Open gaps are at the cap. A search miss is still logged but opens no gap until some are resolved.; 0 stale, 1 open gaps: the trails:navigate skill works through them"
	if n := orientOf(t, a)["notice"]; n != want {
		t.Errorf("notice = %q, want %q", n, want)
	}
}

func TestOrientOfACleanProjectHasZerosAndNoNotice(t *testing.T) {
	_, addr := startServer(t)
	a := newAgent(t, addr, "")
	o := orientOf(t, a)
	if o["stale_count"] != float64(0) || o["open_gaps"] != float64(0) {
		t.Errorf("stale_count %v, open_gaps %v, want zeros", o["stale_count"], o["open_gaps"])
	}
	if n, present := o["notice"]; present {
		t.Errorf("notice = %q on a clean project", n)
	}
}
