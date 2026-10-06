package archive

import (
	"RestoreSafe/internal/format/manifest"
	"archive/tar"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Action tells the extractor what to do with one TAR entry.
type Action int

const (
	// ActionExtract restores (or, in verify mode, checks) the entry.
	ActionExtract Action = iota
	// ActionSkip reads past the entry without restoring it (superseded
	// content or a void entry).
	ActionSkip
)

// Decide maps a TAR entry name to the manifest entry that describes the
// expected content and the action to take. Returning an error aborts the
// section as corrupt.
type Decide func(name string) (*manifest.Entry, Action, error)

// Restorer restores (or verifies) one restore point: the target manifest plus
// one or more data sections.
type Restorer struct {
	destDir    string
	verifyOnly bool
	ownOnly    bool
	target     *manifest.Manifest
	done       map[string]bool
	failures   []string
	buf        []byte
}

// ExpectOwnContentOnly makes Finish require only the files whose content is
// in this set, e.g. to verify a differential's own data without its base.
func (r *Restorer) ExpectOwnContentOnly() { r.ownOnly = true }

// NewRestorer prepares a restore of target into destDir. In verify mode
// (verifyOnly) nothing is written; every file's content is hashed and checked.
func NewRestorer(target *manifest.Manifest, destDir string, verifyOnly bool) *Restorer {
	return &Restorer{destDir: destDir, verifyOnly: verifyOnly, target: target, done: make(map[string]bool), buf: make([]byte, copyBufferSize)}
}

func (r *Restorer) targetPath(rel string) (string, error) {
	if err := manifest.ValidatePath(rel); err != nil {
		return "", err
	}
	p := filepath.Join(r.destDir, filepath.FromSlash(rel))
	root := filepath.Clean(r.destDir) + string(os.PathSeparator)
	if !strings.HasPrefix(filepath.Clean(p)+string(os.PathSeparator), root) {
		return "", fmt.Errorf("Invalid path in backup (path traversal): %q. Remedy: Do not use this backup; use only unmodified, trusted backup files.", rel)
	}
	return p, nil
}

// CreateDirectories creates every directory of the target manifest.
func (r *Restorer) CreateDirectories() error {
	if r.verifyOnly {
		return nil
	}
	for _, e := range r.target.Entries {
		if e.Type != manifest.TypeDir {
			continue
		}
		p, err := r.targetPath(e.Path)
		if err != nil {
			return err
		}
		if err := os.Mkdir(p, 0o750); err != nil {
			return fmt.Errorf("Failed to create directory %q: %w. Remedy: Check write permissions in the restore destination.", p, err)
		}
	}
	return nil
}

// ExtractSection consumes one TAR stream. Every entry is looked up with
// decide; extracted files are written (or hashed) and checked against the
// manifest hash and size.
func (r *Restorer) ExtractSection(tarStream io.Reader, decide Decide) error {
	tr := tar.NewReader(tarStream)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("Failed to read TAR entry: %w. Remedy: Use an unmodified backup created by RestoreSafe.", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			return fmt.Errorf("Unexpected TAR entry type %q for %q. Remedy: Use an unmodified backup created by RestoreSafe.", hdr.Typeflag, hdr.Name)
		}
		entry, action, err := decide(hdr.Name)
		if err != nil {
			return err
		}
		if action == ActionSkip {
			if _, err := io.Copy(io.Discard, tr); err != nil {
				return fmt.Errorf("Failed to read TAR entry %q: %w", hdr.Name, err)
			}
			continue
		}
		if entry == nil || entry.Type != manifest.TypeFile || entry.Path != hdr.Name {
			return fmt.Errorf("Internal error: no manifest entry for %q.", hdr.Name)
		}
		if r.done[entry.Path] {
			return fmt.Errorf("File %q appears twice in the backup. Remedy: Use an unmodified backup created by RestoreSafe.", entry.Path)
		}
		if hdr.Size != entry.Size {
			return fmt.Errorf("Size of %q in the backup (%d bytes) does not match the manifest (%d bytes). Remedy: Use an unmodified backup created by RestoreSafe.", entry.Path, hdr.Size, entry.Size)
		}
		if err := r.extractFile(tr, entry); err != nil {
			return err
		}
		r.done[entry.Path] = true
	}
}

func (r *Restorer) extractFile(content io.Reader, e *manifest.Entry) error {
	hasher := sha256.New()
	if r.verifyOnly {
		if _, err := io.CopyBuffer(hasher, content, r.buf); err != nil {
			return fmt.Errorf("Failed to read %q from the backup: %w", e.Path, err)
		}
		return r.checkHash(e, hasher.Sum(nil))
	}

	p, err := r.targetPath(e.Path)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
	if err != nil {
		return fmt.Errorf("Failed to create file %q: %w. Remedy: Check write permissions in the restore destination.", p, err)
	}
	_, copyErr := io.CopyBuffer(io.MultiWriter(f, hasher), content, r.buf)
	closeErr := f.Close()
	if copyErr != nil {
		return fmt.Errorf("Failed to write file %q: %w", p, copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("Failed to close file %q: %w", p, closeErr)
	}
	if err := r.checkHash(e, hasher.Sum(nil)); err != nil {
		return err
	}
	if err := setTimes(p, e.CreationTime, e.ModTime); err != nil {
		return err
	}
	return setAttributes(p, e.Attributes)
}

func (r *Restorer) checkHash(e *manifest.Entry, sum []byte) error {
	if hex.EncodeToString(sum) != e.Hash {
		return fmt.Errorf("Content of %q does not match its checksum. Remedy: The backup is damaged; use another backup.", e.Path)
	}
	return nil
}

// Finish checks that every file of the target manifest was restored exactly
// once, then applies directory timestamps and attributes (deepest first,
// because writing files changes the parent directory's times). It returns an
// error listing the missing files if the restore is incomplete.
func (r *Restorer) Finish() error {
	var missing []string
	for _, e := range r.target.Entries {
		if e.Type != manifest.TypeFile || (r.ownOnly && !e.HasContentInSet()) {
			continue
		}
		if !r.done[e.Path] {
			missing = append(missing, e.Path)
		}
	}
	if len(missing) > 0 {
		r.failures = missing
		return fmt.Errorf("%d file(s) listed in the manifest were not found in the backup data (first: %q). Remedy: The backup is damaged or incomplete; use another backup.", len(missing), missing[0])
	}
	if r.verifyOnly {
		return nil
	}

	dirs := make([]manifest.Entry, 0)
	for _, e := range r.target.Entries {
		if e.Type == manifest.TypeDir {
			dirs = append(dirs, e)
		}
	}
	sort.SliceStable(dirs, func(i, j int) bool {
		return strings.Count(dirs[i].Path, "/") > strings.Count(dirs[j].Path, "/")
	})
	for _, d := range dirs {
		p, err := r.targetPath(d.Path)
		if err != nil {
			return err
		}
		if err := setTimes(p, d.CreationTime, d.ModTime); err != nil {
			return err
		}
		if err := setAttributes(p, d.Attributes); err != nil {
			return err
		}
	}
	return nil
}

// MissingFiles returns the files Finish reported as missing.
func (r *Restorer) MissingFiles() []string { return r.failures }

// SkippedFiles returns the paths of files that could not be read during the
// backup and are therefore not part of the restore point.
func SkippedFiles(m *manifest.Manifest) []string {
	var out []string
	for _, e := range m.Entries {
		if e.Type == manifest.TypeSkipped {
			out = append(out, e.Path)
		}
	}
	return out
}

// StaleFiles returns the paths of files a differential could not read; the
// restore point holds their older version from the full backup.
func StaleFiles(m *manifest.Manifest) []string {
	var out []string
	for _, e := range m.Entries {
		if e.Stale {
			out = append(out, e.Path)
		}
	}
	return out
}

// DecideFromBase returns the Decide function for the full backup's data
// section when restoring a differential: only files the target takes from the
// full backup (origin "F", including stale files) are extracted and checked
// against the target's hash; superseded, deleted, and void entries are read
// past without being written.
func DecideFromBase(target *manifest.Manifest) Decide {
	byPath := make(map[string]*manifest.Entry, len(target.Entries))
	for i := range target.Entries {
		byPath[target.Entries[i].Path] = &target.Entries[i]
	}
	return func(name string) (*manifest.Entry, Action, error) {
		if e, ok := byPath[name]; ok && e.Type == manifest.TypeFile && e.Origin == manifest.OriginFull {
			return e, ActionExtract, nil
		}
		return nil, ActionSkip, nil
	}
}

// DecideOwn returns the Decide function for a set's own data section: every
// file whose content is in the set (all files of a full backup, the new and
// changed files of a differential) is extracted, void entries are skipped,
// and any other TAR entry is an error.
func DecideOwn(m *manifest.Manifest) Decide {
	byPath := make(map[string]*manifest.Entry, len(m.Entries))
	for i := range m.Entries {
		byPath[m.Entries[i].Path] = &m.Entries[i]
	}
	return func(name string) (*manifest.Entry, Action, error) {
		e, ok := byPath[name]
		switch {
		case ok && e.HasContentInSet():
			return e, ActionExtract, nil
		case ok && e.Void:
			return nil, ActionSkip, nil
		default:
			return nil, ActionSkip, fmt.Errorf("Backup data contains %q, which is not in the manifest. Remedy: Use an unmodified backup created by RestoreSafe.", name)
		}
	}
}
