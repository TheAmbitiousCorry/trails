package store

import (
	"context"
	"sync"
	"testing"
)

// Writers that read before they write must queue on the lock instead of failing with SQLITE_BUSY.
func TestConcurrentWritersWait(t *testing.T) {
	st, err := Open(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				tx, err := st.DB.BeginTx(ctx, nil)
				if err != nil {
					errs <- err
					return
				}
				var n int
				if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM events`).Scan(&n); err == nil {
					_, err = tx.ExecContext(ctx, `INSERT INTO events (session_id, at, tool, target, depth, result) VALUES ('s', 'now', 'look', '', '', 'ok')`)
				}
				if err != nil {
					tx.Rollback()
					errs <- err
					return
				}
				if err := tx.Commit(); err != nil {
					errs <- err
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
