package archive

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/phsc84/restoresafe/internal/format/manifest"
	"github.com/phsc84/restoresafe/internal/testutil/filelock"

	"golang.org/x/sys/windows"
)

// tarEntry is one entry of a TAR stream a test crafts.
type tarEntry struct {
	name string
	data string
	typ  byte
	size int64 // the header's size; -1 means len(data)
}

func craftTar(t *testing.T, entries ...tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, e := range entries {
		typ := e.typ
		if typ == 0 {
			typ = tar.TypeReg
		}
		size := e.size
		if size < 0 {
			size = int64(len(e.data))
		}
		if err := tw.WriteHeader(&tar.Header{Name: e.name, Typeflag: typ, Size: size, Mode: 0o600}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(e.data)); err != nil && !errors.Is(err, tar.ErrWriteTooLong) {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil && !strings.Contains(err.Error(), "missed writing") {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// alphaManifest backs up a source with one file a.txt = "alpha".
func alphaManifest(t *testing.T) *manifest.Manifest {
	t.Helper()
	src := t.TempDir()
	mustWrite(t, filepath.Join(src, "a.txt"), []byte("alpha"))
	m, _ := build(t, src)
	return m
}

func TestExtractSectionRefusesWhatRestoreSafeNeverWrites(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		tar  []tarEntry
		want string
	}{
		{"a file twice", []tarEntry{{name: "a.txt", data: "alpha", size: -1}, {name: "a.txt", data: "alpha", size: -1}}, "appears twice"},
		{"a size the manifest does not have", []tarEntry{{name: "a.txt", data: "alphabet", size: -1}}, "does not match the manifest"},
		{"a directory entry", []tarEntry{{name: "a.txt", typ: tar.TypeDir, size: 0}}, "Unexpected TAR entry type"},
		{"a file not in the manifest", []tarEntry{{name: "b.txt", data: "beta", size: -1}}, "not in the manifest"},
	} {
		m := alphaManifest(t)
		err := restore(t, m, craftTar(t, tc.tar...), t.TempDir(), true)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: expected an error with %q, got %v", tc.name, tc.want, err)
		}
	}
}

func TestExtractSectionRefusesATruncatedStream(t *testing.T) {
	t.Parallel()

	m := alphaManifest(t)
	data := craftTar(t, tarEntry{name: "a.txt", data: "alpha", size: -1})
	r := NewRestorer(m, t.TempDir(), true)
	err := r.ExtractSection(bytes.NewReader(data[:100]), DecideOwn(m)) // inside the first header
	if err == nil || !strings.Contains(err.Error(), "Failed to read TAR entry") {
		t.Fatalf("expected a TAR read error, got %v", err)
	}
}

func TestExtractRefusesToOverwriteAFileInTheDestination(t *testing.T) {
	t.Parallel()

	src := t.TempDir()
	mustWrite(t, filepath.Join(src, "a.txt"), []byte("alpha"))
	m, data := build(t, src)
	dest := t.TempDir()
	mustWrite(t, filepath.Join(dest, "a.txt"), []byte("the user's file"))

	err := restore(t, m, data, dest, false)
	if err == nil || !strings.Contains(err.Error(), "Failed to create file") {
		t.Fatalf("expected the existing file to be refused, got %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(dest, "a.txt")); string(got) != "the user's file" {
		t.Fatalf("the existing file was changed to %q", got)
	}
}

func TestStaleFilesAndDecideFromBaseFollowTheManifest(t *testing.T) {
	t.Parallel()

	m := &manifest.Manifest{Entries: []manifest.Entry{
		{Path: "dir", Type: manifest.TypeDir},
		{Path: "dir/full.txt", Type: manifest.TypeFile, Origin: manifest.OriginFull},
		{Path: "dir/stale.txt", Type: manifest.TypeFile, Origin: manifest.OriginFull, Stale: true},
		{Path: "dir/new.txt", Type: manifest.TypeFile, Origin: manifest.OriginDiff},
	}}
	if got := StaleFiles(m); len(got) != 1 || got[0] != "dir/stale.txt" {
		t.Fatalf("StaleFiles = %v", got)
	}

	decide := DecideFromBase(m)
	for name, want := range map[string]Action{
		"dir/full.txt":  ActionExtract,
		"dir/stale.txt": ActionExtract, // the older version comes from the full backup
		"dir/new.txt":   ActionSkip,    // superseded by the differential
		"dir/gone.txt":  ActionSkip,    // deleted since the full backup
	} {
		e, action, err := decide(name)
		if err != nil || action != want || (want == ActionExtract && (e == nil || e.Path != name)) {
			t.Fatalf("decide(%q) = %v, %v, %v; want %v", name, e, action, err, want)
		}
	}
}

func TestExtractReportsContentCutOffInsideAFile(t *testing.T) {
	t.Parallel()

	m := alphaManifest(t)
	data := craftTar(t, tarEntry{name: "a.txt", data: "alpha", size: -1})
	cut := data[:512+3] // the header and three of the five bytes
	for _, verifyOnly := range []bool{true, false} {
		dest := t.TempDir()
		r := NewRestorer(m, dest, verifyOnly)
		err := r.ExtractSection(bytes.NewReader(cut), DecideOwn(m))
		if !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("verifyOnly=%v: expected the cut-off content to be reported, got %v", verifyOnly, err)
		}
	}
}

func TestCreateDirectoriesRefusesAFolderThatExists(t *testing.T) {
	t.Parallel()

	src := t.TempDir()
	mustWrite(t, filepath.Join(src, "nested", "a.txt"), []byte("alpha"))
	m, _ := build(t, src)
	dest := t.TempDir()
	if err := os.Mkdir(filepath.Join(dest, "nested"), 0o750); err != nil {
		t.Fatal(err)
	}
	err := NewRestorer(m, dest, false).CreateDirectories()
	if err == nil || !strings.Contains(err.Error(), "Failed to create directory") {
		t.Fatalf("expected the existing folder to be refused, got %v", err)
	}
}

func TestNormalizeExcludesKeepsOnlyFoldersInsideTheSource(t *testing.T) {
	t.Parallel()

	src := filepath.Join(t.TempDir(), "src")
	inside := filepath.Join(src, "backup")
	got := normalizeExcludes(src, []string{"", filepath.Join(src, "..", "other"), filepath.Dir(src), inside + string(filepath.Separator)})
	if len(got) != 1 || got[0] != inside {
		t.Fatalf("normalizeExcludes = %v, want only %s", got, inside)
	}
}

func TestUnreadableFileErrorKeepsItsCause(t *testing.T) {
	t.Parallel()

	src := t.TempDir()
	locked := filepath.Join(src, "mail.pst")
	mustWrite(t, locked, []byte("mail"))
	filelock.Hold(t, locked)

	_, _, err := buildWith(t, BuildOptions{SourceDir: src})
	var unreadable *unreadableError
	if !errors.As(err, &unreadable) || !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		t.Fatalf("expected the sharing violation inside the unreadable-file error, got %v", err)
	}
}

// failingWriter takes n bytes, then fails like a full disk.
type failingWriter struct{ n int }

var errDiskFull = errors.New("disk full")

func (w *failingWriter) Write(p []byte) (int, error) {
	if len(p) > w.n {
		k := w.n
		w.n = 0
		return k, errDiskFull
	}
	w.n -= len(p)
	return len(p), nil
}

func TestBuildTarStopsWhenTheOutputFails(t *testing.T) {
	t.Parallel()

	src := t.TempDir()
	mustWrite(t, filepath.Join(src, "a.bin"), bytes.Repeat([]byte{7}, 200_000))

	// In the header, in the content, and in the padding of the last block.
	for _, n := range []int{100, 100_000, 200_512 + 100} {
		mb := manifest.NewBuilder(manifest.Header{SetType: manifest.SetTypeFull, ChainID: "ABC123", DirectoryName: "src", SourcePath: src})
		err := BuildTar(context.Background(), &failingWriter{n: n}, BuildOptions{SourceDir: src}, mb)
		if !errors.Is(err, errDiskFull) || !strings.Contains(err.Error(), "Failed to") {
			t.Fatalf("output failing after %d bytes: expected the write error, got %v", n, err)
		}
	}
}

func TestBuildTarReportsAMissingSourceFolder(t *testing.T) {
	t.Parallel()

	src := filepath.Join(t.TempDir(), "gone")
	mb := manifest.NewBuilder(manifest.Header{SetType: manifest.SetTypeFull, ChainID: "ABC123", DirectoryName: "gone", SourcePath: src})
	err := BuildTar(context.Background(), &bytes.Buffer{}, BuildOptions{SourceDir: src}, mb)
	if err == nil || !strings.Contains(err.Error(), "Failed to scan source directory") {
		t.Fatalf("expected the missing folder to be reported, got %v", err)
	}
}

func TestBuildTarCountsProgressAndStopsWhenCancelled(t *testing.T) {
	t.Parallel()

	src := t.TempDir()
	mustWrite(t, filepath.Join(src, "a.txt"), []byte("alpha"))
	mustWrite(t, filepath.Join(src, "dir", "b.bin"), bytes.Repeat([]byte{1}, 3000))

	var progress atomic.Int64
	mb := manifest.NewBuilder(manifest.Header{SetType: manifest.SetTypeFull, ChainID: "ABC123", DirectoryName: "src", SourcePath: src})
	if err := BuildTar(context.Background(), &bytes.Buffer{}, BuildOptions{SourceDir: src, Progress: &progress}, mb); err != nil {
		t.Fatal(err)
	}
	if got := progress.Load(); got != 3005 {
		t.Fatalf("progress = %d, want the 3005 bytes of the files", got)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	mb = manifest.NewBuilder(manifest.Header{SetType: manifest.SetTypeFull, ChainID: "ABC123", DirectoryName: "src", SourcePath: src})
	if err := BuildTar(ctx, &bytes.Buffer{}, BuildOptions{SourceDir: src}, mb); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}
