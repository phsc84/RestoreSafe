// Package archive produces and consumes the TAR stream inside a backup set's
// data section, together with the manifest entries that describe it.
package archive

import (
	"RestoreSafe/internal/format/manifest"
	"RestoreSafe/internal/problem"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// fileBasicInfo mirrors the Windows FILE_BASIC_INFO structure.
type fileBasicInfo struct {
	CreationTime   int64
	LastAccessTime int64
	LastWriteTime  int64
	ChangeTime     int64
	FileAttributes uint32
	_              uint32 // padding to 8-byte alignment
}

// basicInfo is the metadata RestoreSafe records per entry. Times are UTC
// nanoseconds since the Unix epoch.
type basicInfo struct {
	CreationTime int64
	ModTime      int64
	ChangeTime   int64
	Attributes   uint32 // restorable subset (manifest.AttrMask)
}

// filetimeEpochDelta is the number of 100 ns intervals between 1601-01-01
// (FILETIME epoch) and 1970-01-01 (Unix epoch).
const filetimeEpochDelta = 116444736000000000

func filetimeToUnixNano(ft int64) int64 {
	if ft == 0 {
		return 0
	}
	return (ft - filetimeEpochDelta) * 100
}

func unixNanoToFiletime(ns int64) windows.Filetime {
	if ns == 0 {
		return windows.Filetime{}
	}
	ft := ns/100 + filetimeEpochDelta
	return windows.Filetime{LowDateTime: uint32(ft), HighDateTime: uint32(ft >> 32)}
}

func basicInfoFromHandle(h windows.Handle) (basicInfo, error) {
	var fbi fileBasicInfo
	if err := windows.GetFileInformationByHandleEx(h, windows.FileBasicInfo, (*byte)(unsafe.Pointer(&fbi)), uint32(unsafe.Sizeof(fbi))); err != nil {
		return basicInfo{}, err
	}
	return basicInfo{
		CreationTime: filetimeToUnixNano(fbi.CreationTime),
		ModTime:      filetimeToUnixNano(fbi.LastWriteTime),
		ChangeTime:   filetimeToUnixNano(fbi.ChangeTime),
		Attributes:   fbi.FileAttributes & manifest.AttrMask,
	}, nil
}

// statBasic reads the metadata of a file or directory without opening it for
// reading, so it works for files locked by other processes.
func statBasic(path string) (basicInfo, error) {
	h, err := openForAttributes(path, windows.FILE_READ_ATTRIBUTES)
	if err != nil {
		return basicInfo{}, err
	}
	defer windows.CloseHandle(h) //nolint:errcheck
	return basicInfoFromHandle(h)
}

func openForAttributes(path string, access uint32) (windows.Handle, error) {
	p, err := windows.UTF16PtrFromString(extendedPath(path))
	if err != nil {
		return 0, err
	}
	return windows.CreateFile(
		p,
		access,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
}

// setTimes sets creation and last-write time of a file or directory. The last
// access time is left to the file system.
func setTimes(path string, creation, modTime int64) error {
	h, err := openForAttributes(path, windows.FILE_WRITE_ATTRIBUTES)
	if err != nil {
		return fmt.Errorf("Failed to open %q to set timestamps: %w", path, err)
	}
	defer windows.CloseHandle(h) //nolint:errcheck
	var ctime, wtime *windows.Filetime
	if creation != 0 {
		ft := unixNanoToFiletime(creation)
		ctime = &ft
	}
	if modTime != 0 {
		ft := unixNanoToFiletime(modTime)
		wtime = &ft
	}
	if err := windows.SetFileTime(h, ctime, nil, wtime); err != nil {
		return fmt.Errorf("Failed to set timestamps of %q: %w", path, err)
	}
	return nil
}

// setAttributes applies the restorable attribute bits. Attributes outside
// manifest.AttrMask are never set.
func setAttributes(path string, attrs uint32) error {
	attrs &= manifest.AttrMask
	if attrs == 0 {
		return nil
	}
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	current, err := windows.GetFileAttributes(p)
	if err != nil {
		return fmt.Errorf("Failed to read attributes of %q: %w", path, err)
	}
	if err := windows.SetFileAttributes(p, current|attrs); err != nil {
		return fmt.Errorf("Failed to set attributes of %q: %w", path, err)
	}
	return nil
}

// isRegularOrDir reports whether a walked entry is a plain directory or a
// regular file. Symlinks, junctions, and other reparse points are excluded.
func isRegularOrDir(fi os.FileInfo) bool {
	return fi.Mode().IsDir() || fi.Mode().IsRegular()
}

// openSource opens a file to back up for reading. os.Open asks for backup
// semantics, and with them a process that holds the backup privilege (an
// elevated administrator) passes the share-mode check: it would read files
// that other programs hold locked, possibly while they are being written.
// openSource asks for none, so a locked file is unreadable whether
// RestoreSafe runs elevated or not, and on_unreadable_file decides
// (refactoring 2.0 RF-59). The share mode is that of os.Open.
func openSource(path string) (*os.File, error) {
	p, err := windows.UTF16PtrFromString(extendedPath(path))
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(h), path), nil
}

// extendedPath adds the \\?\ prefix to a long absolute path, as os.Open
// does, so CreateFile opens it even where Windows' long-path support is off.
// Paths shorter than 248 characters are left as they are.
func extendedPath(path string) string {
	if len(path) < 248 || !filepath.IsAbs(path) || strings.HasPrefix(path, `\\?\`) {
		return path
	}
	path = filepath.Clean(path)
	if strings.HasPrefix(path, `\\`) {
		return `\\?\UNC\` + path[2:]
	}
	return `\\?\` + path
}

// checkNotReparsePoint refuses a folder of the restore destination that is a
// reparse point (a junction or a symbolic link): another process could swap
// a folder RestoreSafe created for one, to send the restored files
// elsewhere. GetFileAttributes does not follow the last path element.
func checkNotReparsePoint(dir string) error {
	p, err := windows.UTF16PtrFromString(extendedPath(dir))
	if err != nil {
		return err
	}
	attrs, err := windows.GetFileAttributes(p)
	if err != nil {
		return problem.Errorf("Failed to check the restore folder %q: %w.", dir, err).WithRemedy("Check read permissions in the restore destination.")
	}
	if attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return problem.Errorf("The restore folder %q is a link to another place (a junction or symbolic link).", dir).WithRemedy("Restore into a normal folder, and check which program changed this one.")
	}
	return nil
}
