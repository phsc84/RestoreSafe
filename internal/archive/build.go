package archive

import (
	"RestoreSafe/internal/manifest"
	"RestoreSafe/internal/util"
	"archive/tar"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// BuildOptions configures BuildTar.
type BuildOptions struct {
	// SourceDir is the directory to back up.
	SourceDir string
	// ExcludeDirs are absolute directories to skip (e.g. the backup directory
	// when it lies inside the source).
	ExcludeDirs []string
	// Exclude holds the configured exclude patterns; nil excludes nothing.
	Exclude *util.ExcludeMatcher
	// SkipUnreadable records files that cannot be read as skipped instead of
	// aborting (on_unreadable_file: skip).
	SkipUnreadable bool
	// OnSkip is called for every skipped file or directory. stale is true
	// when a differential keeps the older version from the full backup.
	OnSkip func(rel, reason string, stale bool)
	// Stats receives counts; may be nil.
	Stats *BuildStats
	// Base is the manifest of the chain's full backup. When set, BuildTar
	// writes a differential: files unchanged since the base (same size,
	// last-write time, and change time) are recorded without being read,
	// and only new and changed files go into the TAR.
	Base *manifest.Manifest
}

// BuildStats counts entries by how they were handled.
type BuildStats struct {
	Excluded  int // matched an exclude pattern
	Skipped   int // unreadable (on_unreadable_file: skip)
	Stale     int // unreadable in a differential; older version kept
	Vanished  int // deleted while the backup was running
	Unchanged int // differential: unchanged since the full backup
	Stored    int // files whose content is in this set's TAR
}

type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// unreadableError marks a problem reading a source file or directory (as
// opposed to writing the backup). headerWritten tells whether a TAR entry for
// the file was already started.
type unreadableError struct {
	path          string
	err           error
	headerWritten bool
}

func (e *unreadableError) Error() string {
	return fmt.Sprintf("Cannot read %q: %v. Remedy: Close programs that lock or modify the file and start the backup again, or set 'on_unreadable_file: skip' in config.yaml to back up everything else and list such files as warnings.", e.path, e.err)
}

func (e *unreadableError) Unwrap() error { return e.err }

// BuildTar walks opts.SourceDir, writes the content of every regular file as a
// TAR stream to w, and adds one manifest entry per directory and file to mb.
// The TAR contains only regular files; directories and all metadata live in
// the manifest. Symlinks, junctions, and other special entries are skipped.
func BuildTar(w io.Writer, opts BuildOptions, mb *manifest.Builder) error {
	cw := &countingWriter{w: w}
	tw := tar.NewWriter(cw)
	stats := opts.Stats
	if stats == nil {
		stats = &BuildStats{}
	}

	srcDir := filepath.Clean(opts.SourceDir)
	excludes := normalizeExcludes(srcDir, opts.ExcludeDirs)

	origin := manifest.OriginFull
	baseFiles := make(map[string]*manifest.Entry)
	if opts.Base != nil {
		origin = manifest.OriginDiff
		for i := range opts.Base.Entries {
			if e := &opts.Base.Entries[i]; e.Type == manifest.TypeFile {
				baseFiles[e.Path] = e
			}
		}
	}

	skip := func(rel string, err error) {
		stats.Skipped++
		if opts.OnSkip != nil {
			opts.OnSkip(rel, err.Error(), false)
		}
	}
	// skipFile records an unreadable file: in a differential, a file that
	// exists in the full backup keeps that older version (stale).
	skipFile := func(rel string, err error, headerWritten bool) {
		if base := baseFiles[rel]; base != nil {
			mb.Add(fromBase(base, true, headerWritten))
			stats.Stale++
			if opts.OnSkip != nil {
				opts.OnSkip(rel, err.Error(), true)
			}
			return
		}
		mb.Add(manifest.Entry{Path: rel, Type: manifest.TypeSkipped, Reason: err.Error(), Void: headerWritten})
		skip(rel, err)
	}

	walkErr := filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if path == srcDir {
			if err != nil {
				return fmt.Errorf("Failed to scan source directory %q: %w", path, err)
			}
			return nil
		}
		rel, relErr := filepath.Rel(srcDir, path)
		if relErr != nil {
			return fmt.Errorf("Failed to compute relative path: %w", relErr)
		}
		rel = filepath.ToSlash(rel)

		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				stats.Vanished++
				return skipDirOrNil(d)
			}
			if !opts.SkipUnreadable {
				return &unreadableError{path: path, err: err}
			}
			// WalkDir reports a directory it cannot list after it was
			// recorded; turn that record into a skipped entry.
			if !mb.SkipLastDirectory(rel, err.Error()) {
				mb.Add(manifest.Entry{Path: rel, Type: manifest.TypeSkipped, Reason: err.Error()})
			}
			skip(rel, err)
			return skipDirOrNil(d)
		}

		if isExcluded(path, excludes) {
			return skipDirOrNil(d)
		}
		if opts.Exclude.Match(rel, d.IsDir()) {
			stats.Excluded++
			return skipDirOrNil(d)
		}

		info, err := d.Info()
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				stats.Vanished++
				return skipDirOrNil(d)
			}
			if !opts.SkipUnreadable {
				return &unreadableError{path: path, err: err}
			}
			if d.IsDir() {
				mb.Add(manifest.Entry{Path: rel, Type: manifest.TypeSkipped, Reason: err.Error()})
				skip(rel, err)
				return filepath.SkipDir
			}
			skipFile(rel, err, false)
			return nil
		}
		if !isRegularOrDir(info) {
			return nil
		}
		if err := manifest.ValidatePath(rel); err != nil {
			return err
		}

		if info.IsDir() {
			meta, err := statBasic(path)
			if err != nil {
				if !opts.SkipUnreadable {
					return &unreadableError{path: path, err: err}
				}
				mb.Add(manifest.Entry{Path: rel, Type: manifest.TypeSkipped, Reason: err.Error()})
				skip(rel, err)
				return filepath.SkipDir
			}
			mb.Add(manifest.Entry{Path: rel, Type: manifest.TypeDir, ModTime: meta.ModTime, CreationTime: meta.CreationTime, Attributes: meta.Attributes})
			return nil
		}

		if base := baseFiles[rel]; base != nil {
			if meta, err := statBasic(path); err == nil && unchanged(base, info.Size(), meta) {
				mb.Add(fromBase(base, false, false))
				stats.Unchanged++
				return nil
			}
		}

		entry, err := writeFile(tw, cw, path, rel, origin)
		var unreadable *unreadableError
		if errors.As(err, &unreadable) {
			if errors.Is(unreadable.err, fs.ErrNotExist) && !unreadable.headerWritten {
				stats.Vanished++
				return nil
			}
			if !opts.SkipUnreadable {
				return err
			}
			skipFile(rel, unreadable.err, unreadable.headerWritten)
			return nil
		}
		if err != nil {
			return err
		}
		mb.Add(entry)
		stats.Stored++
		return nil
	})
	if walkErr != nil {
		return walkErr
	}
	if err := tw.Close(); err != nil {
		return fmt.Errorf("Failed to finish TAR stream: %w", err)
	}
	return nil
}

// unchanged reports whether a file still matches its base entry. Equality
// (not "newer than") makes the check immune to clock changes; the NTFS change
// time is updated on every write and attribute change and cannot be reset by
// ordinary programs, so a tool that restores the last-write time is caught.
func unchanged(base *manifest.Entry, size int64, meta basicInfo) bool {
	return base.Size == size && base.ModTime == meta.ModTime && base.ChangeTime == meta.ChangeTime
}

// fromBase returns a differential entry that refers to the full backup's
// content of base. stale marks a file that could not be read in this run;
// void marks a started TAR entry in this set that restore must skip.
func fromBase(base *manifest.Entry, stale, void bool) manifest.Entry {
	e := *base
	e.Origin = manifest.OriginFull
	e.Offset = nil
	e.Stale = stale
	e.Void = void
	return e
}

func skipDirOrNil(d fs.DirEntry) error {
	if d != nil && d.IsDir() {
		return filepath.SkipDir
	}
	return nil
}

// sourceReader records read errors so they can be told apart from errors
// writing the backup.
type sourceReader struct {
	r   io.Reader
	err error
}

func (s *sourceReader) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	if err != nil && err != io.EOF {
		s.err = err
	}
	return n, err
}

// writeFile copies one file into the TAR stream and returns its manifest
// entry. The file must keep its size while it is copied. Problems reading the
// file are returned as *unreadableError; when the TAR header was already
// written, the entry is completed with zeros so the stream stays valid, and
// the caller marks it void.
func writeFile(tw *tar.Writer, cw *countingWriter, path, rel, origin string) (manifest.Entry, error) {
	f, err := os.Open(path)
	if err != nil {
		return manifest.Entry{}, &unreadableError{path: path, err: err}
	}
	defer f.Close()

	meta, err := basicInfoFromHandle(windows.Handle(f.Fd()))
	if err != nil {
		return manifest.Entry{}, &unreadableError{path: path, err: err}
	}
	fi, err := f.Stat()
	if err != nil {
		return manifest.Entry{}, &unreadableError{path: path, err: err}
	}
	size := fi.Size()

	// Flush the padding of the previous entry so the counter points exactly
	// at this entry's header.
	if err := tw.Flush(); err != nil {
		return manifest.Entry{}, fmt.Errorf("Failed to write TAR stream: %w", err)
	}
	offset := cw.n
	hdr := &tar.Header{Name: rel, Size: size, Mode: 0o600, Typeflag: tar.TypeReg, Format: tar.FormatPAX}
	if err := tw.WriteHeader(hdr); err != nil {
		return manifest.Entry{}, fmt.Errorf("Failed to write TAR header for %q: %w", path, err)
	}

	hasher := sha256.New()
	src := &sourceReader{r: f}
	copied, err := io.CopyN(io.MultiWriter(tw, hasher), src, size)
	if err != nil {
		readFailed := src.err != nil || errors.Is(err, io.EOF)
		if !readFailed {
			return manifest.Entry{}, fmt.Errorf("Failed to write %q to the backup: %w", path, err)
		}
		if _, padErr := io.CopyN(tw, zeroReader{}, size-copied); padErr != nil {
			return manifest.Entry{}, fmt.Errorf("Failed to write TAR stream: %w", padErr)
		}
		cause := src.err
		if cause == nil {
			cause = fmt.Errorf("the file became smaller during backup (%d of %d bytes)", copied, size)
		}
		return manifest.Entry{}, &unreadableError{path: path, err: cause, headerWritten: true}
	}
	var probe [1]byte
	if n, _ := f.Read(probe[:]); n > 0 {
		return manifest.Entry{}, &unreadableError{path: path, err: errors.New("the file grew during backup"), headerWritten: true}
	}

	return manifest.Entry{
		Path:         rel,
		Type:         manifest.TypeFile,
		Size:         size,
		ModTime:      meta.ModTime,
		ChangeTime:   meta.ChangeTime,
		CreationTime: meta.CreationTime,
		Attributes:   meta.Attributes,
		Hash:         hex.EncodeToString(hasher.Sum(nil)),
		Origin:       origin,
		Offset:       &offset,
	}, nil
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

func normalizeExcludes(srcDir string, excludeDirs []string) []string {
	out := make([]string, 0, len(excludeDirs))
	for _, e := range excludeDirs {
		if e == "" {
			continue
		}
		ce := filepath.Clean(e)
		rel, err := filepath.Rel(srcDir, ce)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		out = append(out, ce)
	}
	return out
}

func isExcluded(path string, excludes []string) bool {
	for _, ex := range excludes {
		if strings.EqualFold(path, ex) {
			return true
		}
		if rel, err := filepath.Rel(ex, path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "." {
			return true
		}
	}
	return false
}
