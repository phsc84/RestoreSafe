// Package filelock makes a file unreadable for a test, like a PST file that
// Outlook holds open.
package filelock

import (
	"sync"
	"testing"

	"golang.org/x/sys/windows"
)

// Hold opens path without sharing until release is called or the test ends,
// so other opens fail with a sharing violation. RestoreSafe opens source
// files without backup semantics, so this holds also when the test runs
// elevated, as on the CI runner (refactoring 2.0 RF-59).
func Hold(t *testing.T, path string) (release func()) {
	t.Helper()
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ, 0, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatalf("lock %s: %v", path, err)
	}
	release = sync.OnceFunc(func() { windows.CloseHandle(h) })
	t.Cleanup(release)
	return release
}
