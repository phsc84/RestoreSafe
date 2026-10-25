//go:build windows

package fsx

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// computerName is this PC's name, as the holder line records it.
func computerName() string {
	name, _ := os.Hostname()
	return name
}

// processStart is when this process started; zero when Windows doesn't say.
func processStart() time.Time {
	t, _ := creationTime(windows.CurrentProcess())
	return t
}

// creationTime is when the process of h started.
func creationTime(h windows.Handle) (time.Time, error) {
	var created, exited, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(h, &created, &exited, &kernel, &user); err != nil {
		return time.Time{}, err
	}
	return time.Unix(0, created.Nanoseconds()), nil
}

// ownImage is the file name of this program, e.g. "RestoreSafe.exe".
func ownImage() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Base(exe)
}

// runningRestoreSafe reports whether the process pid of a holder line is a
// running RestoreSafe, by the three-part test of MountSafe core spec 6.2: the
// process exists, its image is this program's, and it started no later than
// since, the time in the line. Windows reuses process IDs, so the ID alone
// could name an unrelated program or a RestoreSafe started later. A process
// whose details can't be read (another user's) counts as running: the safe
// side, which doesn't wait for the lock.
func runningRestoreSafe(pid uint32, since time.Time) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return errors.Is(err, windows.ERROR_ACCESS_DENIED)
	}
	defer windows.CloseHandle(h) //nolint:errcheck
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &n); err != nil {
		return true
	}
	if !strings.EqualFold(filepath.Base(windows.UTF16ToString(buf[:n])), ownImage()) {
		return false
	}
	started, err := creationTime(h)
	if err != nil {
		return true
	}
	return !started.After(since)
}

// otherRestoreSafeRunning reports whether another process of this program
// runs on this PC: without a holder line, it may hold the lock.
func otherRestoreSafeRunning() bool {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return true // the safe side, as in runningRestoreSafe
	}
	defer windows.CloseHandle(snap) //nolint:errcheck
	own, image := uint32(os.Getpid()), ownImage()
	e := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		if e.ProcessID != own && strings.EqualFold(windows.UTF16ToString(e.ExeFile[:]), image) {
			return true
		}
	}
	return false
}
