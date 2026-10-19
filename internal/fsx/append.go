//go:build windows

package fsx

import (
	"errors"
	"io"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

// appendLockOffset is the byte AppendFile locks: far beyond the end of any
// file, so the lock serializes appenders without keeping readers from the
// file's contents.
const appendLockOffset = 1 << 62

// appendLockWait is how long AppendFile waits for another appender's lock
// before it appends without it.
var appendLockWait = 5 * time.Second

// AppendFile appends data to the file at path, which it creates if needed.
// It opens the file for writing and seeks to its end instead of opening it
// with os.O_APPEND: Go opens such a file with append access only, and some
// SMB servers refuse every write through that handle ("Access is denied").
// A lock held while seeking and writing keeps two appenders, also in two
// processes, from writing over each other's data. Appending matters more
// than that protection: when the file system can't lock, or another
// appender holds the lock for appendLockWait, it appends without the lock.
func AppendFile(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	h := windows.Handle(f.Fd())
	ol := &windows.Overlapped{OffsetHigh: appendLockOffset >> 32}
	locked := lockForAppend(h, ol)
	_, err = f.Seek(0, io.SeekEnd)
	if err == nil {
		_, err = f.Write(data)
	}
	if locked {
		windows.UnlockFileEx(h, 0, 1, 0, ol) //nolint:errcheck // closing the file releases it too
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// lockForAppend takes the append lock of h, waiting up to appendLockWait
// while another appender holds it. It reports whether it holds the lock.
func lockForAppend(h windows.Handle, ol *windows.Overlapped) bool {
	deadline := time.Now().Add(appendLockWait)
	for {
		err := windows.LockFileEx(h, windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, ol)
		if err == nil {
			return true
		}
		if !errors.Is(err, windows.ERROR_LOCK_VIOLATION) || time.Now().After(deadline) {
			return false
		}
		time.Sleep(20 * time.Millisecond)
	}
}
