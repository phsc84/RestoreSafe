package testutil

import (
	"os"
	"testing"
	"time"
)

// RemoveAll removes dir and logs when it cannot. It retries for a few seconds
// because on a network share a folder whose files were just deleted can still
// refuse its own removal while the server finishes those deletes.
func RemoveAll(t testing.TB, dir string) {
	t.Helper()
	var err error
	for range 10 {
		if err = os.RemoveAll(dir); err == nil {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Logf("cleanup %s: %v", dir, err)
}
