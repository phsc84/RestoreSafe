package archive

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// What the walk records of each entry (core spec 6.4.1): it classifies an
// entry by its attributes and, for a reparse point, its tag, not by what os
// reports as regular. Go reports every reparse tag it doesn't know as
// irregular, also the cloud files of OneDrive and iCloud, which hold the
// user's data; Windows disguises them as plain files for RestoreSafe today,
// but the walk doesn't depend on it.

// entryKind says how the walk handles an entry.
type entryKind int

const (
	// kindFile and kindDir hold data: they are recorded, a directory is
	// walked.
	kindFile entryKind = iota
	kindDir
	// kindLink is a name surrogate (symbolic link, junction, mount point):
	// left out, never followed, counted and logged.
	kindLink
	// kindOther is a device, pipe or other non-file: left out, counted and
	// logged.
	kindOther
)

// leftOutWords name the kinds the walk leaves out, for the log.
var leftOutWords = map[entryKind]string{kindLink: "link", kindOther: "not a file or folder"}

const (
	// reparseTagNameSurrogate is the bit IsReparseTagNameSurrogate tests:
	// the reparse point names another file or folder.
	reparseTagNameSurrogate = 0x20000000
	// attrRecallOnDataAccess marks a cloud file whose data is not on the
	// device: reading it downloads it.
	attrRecallOnDataAccess = 0x00400000
	// The flags of GetFinalPathNameByHandle for a normalized path with a
	// drive letter (FILE_NAME_NORMALIZED, VOLUME_NAME_DOS; x/sys has none).
	fileNameNormalized = 0x0
	volumeNameDOS      = 0x0
)

// kindOf classifies an entry from its attributes and its reparse tag (0
// without reparse point); regular is whether os reports a regular file.
func kindOf(attrs, tag uint32, regular bool) entryKind {
	reparse := attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0
	switch {
	case reparse && tag&reparseTagNameSurrogate != 0:
		return kindLink
	case attrs&windows.FILE_ATTRIBUTE_DIRECTORY != 0:
		return kindDir
	case reparse || regular:
		return kindFile
	}
	return kindOther
}

// fileAttributeTagInfo mirrors FILE_ATTRIBUTE_TAG_INFO.
type fileAttributeTagInfo struct {
	FileAttributes uint32
	ReparseTag     uint32
}

// reparseTag reads the reparse tag of path without following it and
// without reading its data, so a cloud file is not downloaded.
func reparseTag(path string) (uint32, error) {
	h, err := openForAttributes(path, windows.FILE_READ_ATTRIBUTES)
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(h) //nolint:errcheck
	var info fileAttributeTagInfo
	if err := windows.GetFileInformationByHandleEx(h, windows.FileAttributeTagInfo, (*byte)(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		return 0, err
	}
	return info.ReparseTag, nil
}

// attributesOf returns the Windows attributes of an entry the walk listed.
func attributesOf(info fs.FileInfo) uint32 {
	if a, ok := info.Sys().(*syscall.Win32FileAttributeData); ok {
		return a.FileAttributes
	}
	if info.IsDir() {
		return windows.FILE_ATTRIBUTE_DIRECTORY
	}
	return 0
}

// classify classifies the entry at path the walk listed with info. The tag
// is read only for a reparse point; an error reading it makes the entry
// unreadable.
func classify(path string, info fs.FileInfo) (entryKind, error) {
	attrs := attributesOf(info)
	var tag uint32
	if attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		var err error
		if tag, err = reparseTag(path); err != nil {
			return kindOther, err
		}
	}
	return kindOf(attrs, tag, info.Mode().IsRegular()), nil
}

// isWalkedDir reports whether the walk descends into d: a directory, also
// one whose reparse point holds data, which fs reports as irregular.
func isWalkedDir(path string, d fs.DirEntry) bool {
	if d == nil {
		return false
	}
	if d.IsDir() {
		return true
	}
	if d.Type()&fs.ModeIrregular == 0 {
		return false
	}
	info, err := d.Info()
	if err != nil {
		return false
	}
	kind, err := classify(path, info)
	return err == nil && kind == kindDir
}

// walk is filepath.WalkDir, except that it also descends into a directory
// whose reparse point holds data (isWalkedDir); like WalkDir, it follows no
// link. The code follows filepath.WalkDir, with isWalkedDir for d.IsDir.
func walk(root string, fn fs.WalkDirFunc) error {
	info, err := os.Lstat(root)
	if err != nil {
		err = fn(root, nil, err)
	} else {
		err = walkDir(root, fs.FileInfoToDirEntry(info), fn)
	}
	if err == filepath.SkipDir || err == filepath.SkipAll {
		return nil
	}
	return err
}

func walkDir(path string, d fs.DirEntry, fn fs.WalkDirFunc) error {
	dir := isWalkedDir(path, d)
	if err := fn(path, d, nil); err != nil || !dir {
		if err == filepath.SkipDir && dir {
			err = nil // the directory is skipped
		}
		return err
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		// A second call reports the error of the listing.
		if err = fn(path, d, err); err != nil {
			if err == filepath.SkipDir {
				err = nil
			}
			return err
		}
	}
	for _, e := range entries {
		if err := walkDir(filepath.Join(path, e.Name()), e, fn); err != nil {
			if err == filepath.SkipDir {
				break
			}
			return err
		}
	}
	return nil
}

// skipDirOrNil returns filepath.SkipDir for an entry the walk would descend
// into, so that it doesn't; nil for any other entry, for which SkipDir
// would skip the rest of its directory.
func skipDirOrNil(path string, d fs.DirEntry) error {
	if isWalkedDir(path, d) {
		return filepath.SkipDir
	}
	return nil
}

// resolveRoot returns the folder a source directory stands for: itself,
// or, when it is a junction, symbolic link or mount point (a redirected
// Documents folder), its target, resolved once (core spec 6.4.1). The walk
// follows no link, so without this a linked source would be backed up
// empty. A path that can't be resolved is returned as it is.
func resolveRoot(dir string) string {
	if fi, err := os.Lstat(dir); err != nil || fi.IsDir() {
		return dir
	}
	f, err := os.Open(dir)
	if err != nil {
		return dir
	}
	defer f.Close()
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n, err := windows.GetFinalPathNameByHandle(windows.Handle(f.Fd()), &buf[0], uint32(len(buf)), fileNameNormalized|volumeNameDOS)
	if err != nil || n == 0 || int(n) >= len(buf) {
		return dir
	}
	p := windows.UTF16ToString(buf[:n])
	switch {
	case strings.HasPrefix(p, `\\?\UNC\`):
		p = `\\` + p[len(`\\?\UNC\`):]
	case strings.HasPrefix(p, `\\?\`):
		p = p[len(`\\?\`):]
	}
	return p
}
