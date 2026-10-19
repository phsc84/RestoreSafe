//go:build windows

package fsx

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestAppendFileCreatesThenAppends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run.log")
	if err := AppendFile(path, []byte("first\n")); err != nil {
		t.Fatal(err)
	}
	if err := AppendFile(path, []byte("second\n")); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "first\nsecond\n" {
		t.Fatalf("expected both lines in order, got %q (%v)", data, err)
	}
}

func TestAppendFileFailsWhenTheDirectoryIsMissing(t *testing.T) {
	if err := AppendFile(filepath.Join(t.TempDir(), "gone", "run.log"), []byte("x")); err == nil {
		t.Fatal("expected an error for a missing directory")
	}
}

// Appenders at once each keep all of their data: the lock makes seeking to
// the end and writing one step.
func TestAppendFileAtOnceKeepsEveryAppend(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run.log")
	const appenders = 20
	var wg sync.WaitGroup
	for i := range appenders {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := AppendFile(path, []byte(strings.Repeat(fmt.Sprintf("line %02d\n", i), 100))); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := range appenders {
		if got := strings.Count(string(data), fmt.Sprintf("line %02d\n", i)); got != 100 {
			t.Fatalf("expected line %02d 100 times, got %d", i, got)
		}
	}
}

// holdAppendLock takes the append lock of the file at path, as another
// appender would, until the test ends.
func holdAppendLock(t *testing.T, path string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	ol := &windows.Overlapped{OffsetHigh: appendLockOffset >> 32}
	if err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, ol); err != nil {
		f.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, ol) //nolint:errcheck
		f.Close()
	})
}

// The lock lies beyond the file's end, so the file stays readable while an
// appender holds it.
func TestAppendFileLockLeavesTheContentsReadable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run.log")
	if err := AppendFile(path, []byte("readable\n")); err != nil {
		t.Fatal(err)
	}
	holdAppendLock(t, path)
	if data, err := os.ReadFile(path); err != nil || string(data) != "readable\n" {
		t.Fatalf("expected the contents while locked, got %q (%v)", data, err)
	}
}

// An appender that holds the lock too long doesn't cost the data: after
// appendLockWait, AppendFile appends without the lock.
func TestAppendFileAppendsWithoutTheLockAfterWaiting(t *testing.T) {
	old := appendLockWait
	appendLockWait = 100 * time.Millisecond
	t.Cleanup(func() { appendLockWait = old })

	path := filepath.Join(t.TempDir(), "run.log")
	if err := AppendFile(path, []byte("first\n")); err != nil {
		t.Fatal(err)
	}
	holdAppendLock(t, path)
	start := time.Now()
	if err := AppendFile(path, []byte("second\n")); err != nil {
		t.Fatal(err)
	}
	if waited := time.Since(start); waited < appendLockWait {
		t.Fatalf("expected AppendFile to wait for the lock first, it waited %v", waited)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "first\nsecond\n" {
		t.Fatalf("expected both lines, got %q (%v)", data, err)
	}
}
