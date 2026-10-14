package archive

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/phsc84/restoresafe/internal/config"
	"github.com/phsc84/restoresafe/internal/format/manifest"
	"github.com/phsc84/restoresafe/internal/fsx"
	"github.com/phsc84/restoresafe/internal/problem"

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
	Exclude *config.ExcludeMatcher
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
	// Progress receives the bytes of source files handled so far: read into
	// the TAR, or unchanged since the full backup; may be nil.
	Progress *atomic.Int64
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

func (e *unreadableError) Error() string { return fmt.Sprintf("Cannot read %q: %v", e.path, e.err) }

func (e *unreadableError) Unwrap() error { return e.err }

// newUnreadable returns the error of a source file or directory that can't
// be read, with what the user does about it; errors.As finds the
// *unreadableError in it.
func newUnreadable(path string, err error, headerWritten bool) error {
	return problem.Errorf("%w.", &unreadableError{path: path, err: err, headerWritten: headerWritten}).
		WithRemedy("Close programs that lock or modify the file and start the backup again, or set 'on_unreadable_file: skip' in config.yaml to back up everything else and list such files as warnings.")
}

// BuildTar walks opts.SourceDir, writes the content of every regular file as a
// TAR stream to w, and adds one manifest entry per directory and file to mb.
// The TAR contains only regular files; directories and all metadata live in
// the manifest. Symlinks, junctions, and other special entries are skipped.
// It stops with the error of ctx when ctx is cancelled.
func BuildTar(ctx context.Context, w io.Writer, opts BuildOptions, mb *manifest.Builder) error {
	b := newTarBuilder(ctx, w, opts, mb)
	if err := filepath.WalkDir(b.srcDir, b.visit); err != nil {
		return err
	}
	if err := b.tw.Close(); err != nil {
		return fmt.Errorf("Failed to finish TAR stream: %w", err)
	}
	return nil
}

// tarBuilder is the state of one BuildTar run.
type tarBuilder struct {
	ctx   context.Context
	opts  BuildOptions
	mb    *manifest.Builder
	cw    *countingWriter
	tw    *tar.Writer
	buf   []byte // the copy buffer
	stats *BuildStats
	// srcDir is the cleaned source directory; excludes are the excluded
	// directories inside it.
	srcDir   string
	excludes []string
	// origin is the origin of the files this run stores; baseFiles are the
	// files of the full backup a differential is based on, by path.
	origin    string
	baseFiles map[string]*manifest.Entry
}

func newTarBuilder(ctx context.Context, w io.Writer, opts BuildOptions, mb *manifest.Builder) *tarBuilder {
	cw := &countingWriter{w: &fsx.ContextWriter{Ctx: ctx, W: w}}
	b := &tarBuilder{
		ctx: ctx, opts: opts, mb: mb, cw: cw, tw: tar.NewWriter(cw),
		buf:       make([]byte, copyBufferSize),
		stats:     opts.Stats,
		srcDir:    filepath.Clean(opts.SourceDir),
		origin:    manifest.OriginFull,
		baseFiles: make(map[string]*manifest.Entry),
	}
	if b.stats == nil {
		b.stats = &BuildStats{}
	}
	b.excludes = normalizeExcludes(b.srcDir, opts.ExcludeDirs)
	if opts.Base != nil {
		b.origin = manifest.OriginDiff
		for i := range opts.Base.Entries {
			if e := &opts.Base.Entries[i]; e.Type == manifest.TypeFile {
				b.baseFiles[e.Path] = e
			}
		}
	}
	return b
}

// visit handles one entry of the walk (fs.WalkDirFunc).
func (b *tarBuilder) visit(path string, d fs.DirEntry, err error) error {
	if err := b.ctx.Err(); err != nil {
		return err
	}
	if path == b.srcDir {
		if err != nil {
			return fmt.Errorf("Failed to scan source directory %q: %w", path, err)
		}
		return nil
	}
	rel, relErr := filepath.Rel(b.srcDir, path)
	if relErr != nil {
		return fmt.Errorf("Failed to compute relative path: %w", relErr)
	}
	rel = filepath.ToSlash(rel)

	if err != nil {
		return b.unlisted(path, rel, d, err)
	}
	if isExcluded(path, b.excludes) {
		return skipDirOrNil(d)
	}
	if b.opts.Exclude.Match(rel, d.IsDir()) {
		b.stats.Excluded++
		return skipDirOrNil(d)
	}

	info, err := d.Info()
	if err != nil {
		return b.unstatable(path, rel, d, err)
	}
	if !isRegularOrDir(info) {
		return nil
	}
	if err := manifest.ValidatePath(rel); err != nil {
		return err
	}
	if info.IsDir() {
		return b.addDir(path, rel)
	}
	return b.addFile(path, rel, info.Size())
}

// unlisted handles an entry the walk reports with err: one that vanished, or
// a directory it cannot list.
func (b *tarBuilder) unlisted(path, rel string, d fs.DirEntry, err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		b.stats.Vanished++
		return skipDirOrNil(d)
	}
	if !b.opts.SkipUnreadable {
		return newUnreadable(path, err, false)
	}
	// WalkDir reports a directory it cannot list after it was recorded;
	// turn that record into a skipped entry.
	if !b.mb.SkipLastDirectory(rel, err.Error()) {
		b.mb.Add(manifest.Entry{Path: rel, Type: manifest.TypeSkipped, Reason: err.Error()})
	}
	b.skip(rel, err)
	return skipDirOrNil(d)
}

// unstatable handles an entry whose information cannot be read.
func (b *tarBuilder) unstatable(path, rel string, d fs.DirEntry, err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		b.stats.Vanished++
		return skipDirOrNil(d)
	}
	if !b.opts.SkipUnreadable {
		return newUnreadable(path, err, false)
	}
	if d.IsDir() {
		b.mb.Add(manifest.Entry{Path: rel, Type: manifest.TypeSkipped, Reason: err.Error()})
		b.skip(rel, err)
		return filepath.SkipDir
	}
	b.skipFile(rel, err, false)
	return nil
}

// addDir records a directory with its times and attributes.
func (b *tarBuilder) addDir(path, rel string) error {
	meta, err := statBasic(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// Deleted since the walk listed it, like a file that vanishes.
			b.stats.Vanished++
			return filepath.SkipDir
		}
		if !b.opts.SkipUnreadable {
			return newUnreadable(path, err, false)
		}
		b.mb.Add(manifest.Entry{Path: rel, Type: manifest.TypeSkipped, Reason: err.Error()})
		b.skip(rel, err)
		return filepath.SkipDir
	}
	b.mb.Add(manifest.Entry{Path: rel, Type: manifest.TypeDir, ModTime: meta.ModTime, CreationTime: meta.CreationTime, Attributes: meta.Attributes})
	return nil
}

// addFile records a file of size bytes: unchanged since the full backup, or
// stored in the TAR, or skipped when it cannot be read.
func (b *tarBuilder) addFile(path, rel string, size int64) error {
	if base := b.baseFiles[rel]; base != nil {
		if meta, err := statBasic(path); err == nil && unchanged(base, size, meta) {
			b.mb.Add(fromBase(base, false, false))
			b.stats.Unchanged++
			addProgress(b.opts.Progress, size)
			return nil
		}
	}

	entry, err := b.writeFile(path, rel)
	var unreadable *unreadableError
	if errors.As(err, &unreadable) {
		if errors.Is(unreadable.err, fs.ErrNotExist) && !unreadable.headerWritten {
			b.stats.Vanished++
			return nil
		}
		if !b.opts.SkipUnreadable {
			return err
		}
		b.skipFile(rel, unreadable.err, unreadable.headerWritten)
		return nil
	}
	if err != nil {
		return err
	}
	b.mb.Add(entry)
	b.stats.Stored++
	return nil
}

func (b *tarBuilder) skip(rel string, err error) {
	b.stats.Skipped++
	if b.opts.OnSkip != nil {
		b.opts.OnSkip(rel, err.Error(), false)
	}
}

// skipFile records an unreadable file: in a differential, a file that exists
// in the full backup keeps that older version (stale).
func (b *tarBuilder) skipFile(rel string, err error, headerWritten bool) {
	if base := b.baseFiles[rel]; base != nil {
		b.mb.Add(fromBase(base, true, headerWritten))
		b.stats.Stale++
		if b.opts.OnSkip != nil {
			b.opts.OnSkip(rel, err.Error(), true)
		}
		return
	}
	b.mb.Add(manifest.Entry{Path: rel, Type: manifest.TypeSkipped, Reason: err.Error(), Void: headerWritten})
	b.skip(rel, err)
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

// copyBufferSize is the size of the reads from source files and the writes of
// restored files. Restoring to an SMB share is more than twice as fast with
// 1 MB writes than with the 32 KB of io.Copy; reads are served by the Windows
// read-ahead either way.
const copyBufferSize = 1 << 20

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

// Test hooks, nil outside tests: testHookOpen runs before a source file is
// opened, testHookCopy after its size was read and before it is copied, so
// that a test can delete, shrink, or grow the file at that moment.
var testHookOpen, testHookCopy func(path string)

// writeFile copies one file into the TAR stream and returns its manifest
// entry. The file must keep its size while it is copied. Problems reading the
// file are returned as *unreadableError; when the TAR header was already
// written, the entry is completed with zeros so the stream stays valid, and
// the caller marks it void.
func (b *tarBuilder) writeFile(path, rel string) (manifest.Entry, error) {
	if testHookOpen != nil {
		testHookOpen(path)
	}
	f, err := openSource(path)
	if err != nil {
		return manifest.Entry{}, newUnreadable(path, err, false)
	}
	defer f.Close()

	meta, err := basicInfoFromHandle(windows.Handle(f.Fd()))
	if err != nil {
		return manifest.Entry{}, newUnreadable(path, err, false)
	}
	fi, err := f.Stat()
	if err != nil {
		return manifest.Entry{}, newUnreadable(path, err, false)
	}
	size := fi.Size()
	if testHookCopy != nil {
		testHookCopy(path)
	}

	// Flush the padding of the previous entry so the counter points exactly
	// at this entry's header.
	if err := b.tw.Flush(); err != nil {
		return manifest.Entry{}, fmt.Errorf("Failed to write TAR stream: %w", err)
	}
	offset := b.cw.n
	hdr := &tar.Header{Name: rel, Size: size, Mode: 0o600, Typeflag: tar.TypeReg, Format: tar.FormatPAX}
	if err := b.tw.WriteHeader(hdr); err != nil {
		return manifest.Entry{}, fmt.Errorf("Failed to write TAR header for %q: %w", path, err)
	}

	hasher := sha256.New()
	src := &sourceReader{r: &fsx.CountingReader{R: f, Total: b.opts.Progress}}
	copied, err := io.CopyBuffer(io.MultiWriter(b.tw, hasher), io.LimitReader(src, size), b.buf)
	if err == nil && copied < size {
		err = io.EOF
	}
	if err != nil {
		readFailed := src.err != nil || errors.Is(err, io.EOF)
		if !readFailed {
			return manifest.Entry{}, fmt.Errorf("Failed to write %q to the backup: %w", path, err)
		}
		if _, padErr := io.CopyN(b.tw, zeroReader{}, size-copied); padErr != nil {
			return manifest.Entry{}, fmt.Errorf("Failed to write TAR stream: %w", padErr)
		}
		cause := src.err
		if cause == nil {
			cause = fmt.Errorf("the file became smaller during backup (%d of %d bytes)", copied, size)
		}
		return manifest.Entry{}, newUnreadable(path, cause, true)
	}
	var probe [1]byte
	if n, _ := f.Read(probe[:]); n > 0 {
		return manifest.Entry{}, newUnreadable(path, errors.New("the file grew during backup"), true)
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
		Origin:       b.origin,
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

func addProgress(progress *atomic.Int64, n int64) {
	if progress != nil {
		progress.Add(n)
	}
}

// SourceMeasure is the size of the regular files BuildTar would back up
// from a source directory.
type SourceMeasure struct {
	// Total counts all of them: what a full backup stores.
	Total int64
	// Changed counts those whose last-write or creation time is at or after
	// the time given to MeasureSource: an estimate of what a differential
	// stores. It misses files that were moved or renamed (they keep their
	// times but have a new path) and files whose times a tool set back; the
	// differential itself also compares the path and the NTFS change time.
	Changed int64
}

// MeasureSource measures the files BuildTar would back up for opts,
// following the same exclude rules; entries that cannot be read are left
// out. With a zero since, Changed equals Total. It reads only directory
// listings, no file contents. The error is set when the source directory
// itself cannot be read.
func MeasureSource(opts BuildOptions, since time.Time) (SourceMeasure, error) {
	var m SourceMeasure
	srcDir := filepath.Clean(opts.SourceDir)
	if _, err := os.Stat(srcDir); err != nil {
		return m, err
	}
	excludes := normalizeExcludes(srcDir, opts.ExcludeDirs)
	sinceNano := since.UnixNano()
	_ = filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || path == srcDir {
			return nil
		}
		rel, relErr := filepath.Rel(srcDir, path)
		if relErr != nil {
			return nil
		}
		if isExcluded(path, excludes) || opts.Exclude.Match(filepath.ToSlash(rel), d.IsDir()) {
			return skipDirOrNil(d)
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil || !isRegularOrDir(info) {
			return nil
		}
		m.Total += info.Size()
		if since.IsZero() || newestTime(info) >= sinceNano {
			m.Changed += info.Size()
		}
		return nil
	})
	return m, nil
}

// newestTime returns the later of a file's last-write and creation time, in
// nanoseconds since 1970.
func newestTime(info fs.FileInfo) int64 {
	t := info.ModTime().UnixNano()
	if attr, ok := info.Sys().(*syscall.Win32FileAttributeData); ok {
		t = max(t, attr.CreationTime.Nanoseconds())
	}
	return t
}

// SourceSize is the total for Progress: the size of the regular files
// BuildTar would back up for opts (see MeasureSource).
func SourceSize(opts BuildOptions) int64 {
	m, _ := MeasureSource(opts, time.Time{})
	return m.Total
}
