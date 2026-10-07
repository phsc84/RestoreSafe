// Package filelock makes a file unreadable for a test, like a PST file that
// Outlook holds open.
package filelock

import (
	"os"
	"sync"
	"testing"

	"golang.org/x/sys/windows"
)

var dropBackupPrivilege = sync.OnceFunc(func() {
	var token windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_ADJUST_PRIVILEGES|windows.TOKEN_QUERY, &token); err != nil {
		return
	}
	defer token.Close()
	var luid windows.LUID
	name, _ := windows.UTF16PtrFromString("SeBackupPrivilege")
	if err := windows.LookupPrivilegeValue(nil, name, &luid); err != nil {
		return
	}
	tp := windows.Tokenprivileges{PrivilegeCount: 1}
	tp.Privileges[0] = windows.LUIDAndAttributes{Luid: luid, Attributes: windows.SE_PRIVILEGE_REMOVED}
	windows.AdjustTokenPrivileges(token, false, &tp, 0, nil, nil) //nolint:errcheck // checked by the open below
})

// Hold opens path without sharing until release is called or the test ends,
// so other opens fail with a sharing violation.
//
// An elevated process with the backup privilege, such as a CI runner, opens
// such a file anyway when it asks for backup semantics, as os.Open does. Hold
// removes that privilege from the test process, as a normal user doesn't
// have it, and skips the test if the file still opens.
func Hold(t *testing.T, path string) (release func()) {
	t.Helper()
	dropBackupPrivilege()
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
	if f, err := os.Open(path); err == nil {
		f.Close()
		release()
		t.Skipf("%s opens despite the lock: this process reads files that others hold without sharing", path)
	}
	return release
}
