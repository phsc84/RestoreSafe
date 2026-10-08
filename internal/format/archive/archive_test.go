package archive

import (
	"RestoreSafe/internal/format/manifest"
	"archive/tar"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func mustWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func build(t *testing.T, src string, excludes ...string) (*manifest.Manifest, []byte) {
	t.Helper()
	mb := manifest.NewBuilder(manifest.Header{SetType: manifest.SetTypeFull, ChainID: "ABC123", DirectoryName: "src", SourcePath: src})
	var tarBuf bytes.Buffer
	if err := BuildTar(context.Background(), &tarBuf, BuildOptions{SourceDir: src, ExcludeDirs: excludes}, mb); err != nil {
		t.Fatalf("BuildTar: %v", err)
	}
	data, err := mb.Bytes()
	if err != nil {
		t.Fatalf("manifest Bytes: %v", err)
	}
	m, err := manifest.Parse(data)
	if err != nil {
		t.Fatalf("manifest Parse: %v", err)
	}
	return m, tarBuf.Bytes()
}

func restore(t *testing.T, m *manifest.Manifest, tarData []byte, dest string, verifyOnly bool) error {
	t.Helper()
	r := NewRestorer(m, dest, verifyOnly)
	if err := r.CreateDirectories(); err != nil {
		return err
	}
	if err := r.ExtractSection(bytes.NewReader(tarData), DecideOwn(m)); err != nil {
		return err
	}
	return r.Finish()
}

func setFileTimes(t *testing.T, path string, created, modified time.Time) {
	t.Helper()
	if err := setTimes(path, created.UnixNano(), modified.UnixNano()); err != nil {
		t.Fatalf("setTimes: %v", err)
	}
}

func TestBuildAndRestoreRoundTrip(t *testing.T) {
	t.Parallel()

	src := t.TempDir()
	mustWrite(t, filepath.Join(src, "a.txt"), []byte("alpha"))
	mustWrite(t, filepath.Join(src, "nested", "deep", "b.bin"), bytes.Repeat([]byte{9}, 100_000))
	mustWrite(t, filepath.Join(src, "empty.txt"), nil)
	mustWrite(t, filepath.Join(src, "ümlaut ß.txt"), []byte("unicode"))
	if err := os.Mkdir(filepath.Join(src, "empty-dir"), 0o750); err != nil {
		t.Fatal(err)
	}

	created := time.Date(2020, 1, 2, 3, 4, 5, 600, time.UTC)
	modified := time.Date(2021, 6, 7, 8, 9, 10, 1100, time.UTC)
	setFileTimes(t, filepath.Join(src, "a.txt"), created, modified)
	setFileTimes(t, filepath.Join(src, "nested", "deep"), created, modified)
	if err := setAttributes(filepath.Join(src, "a.txt"), manifest.AttrHidden|manifest.AttrReadOnly); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { clearReadOnly(filepath.Join(src, "a.txt")) })

	m, tarData := build(t, src)
	if m.Footer.Files != 4 || m.Footer.Dirs != 3 {
		t.Fatalf("unexpected counts: %+v", m.Footer)
	}

	dest := filepath.Join(t.TempDir(), "restore")
	if err := os.Mkdir(dest, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := restore(t, m, tarData, dest, false); err != nil {
		t.Fatalf("restore: %v", err)
	}
	t.Cleanup(func() { clearReadOnly(filepath.Join(dest, "a.txt")) })

	for _, rel := range []string{"a.txt", "nested/deep/b.bin", "empty.txt", "ümlaut ß.txt"} {
		want, _ := os.ReadFile(filepath.Join(src, filepath.FromSlash(rel)))
		got, err := os.ReadFile(filepath.Join(dest, filepath.FromSlash(rel)))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("%s: content mismatch (err %v)", rel, err)
		}
	}
	if fi, err := os.Stat(filepath.Join(dest, "empty-dir")); err != nil || !fi.IsDir() {
		t.Fatalf("empty directory not restored: %v", err)
	}

	for _, rel := range []string{"a.txt", "nested/deep"} {
		meta, err := statBasic(filepath.Join(dest, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		if meta.ModTime != modified.UnixNano()/100*100 || meta.CreationTime != created.UnixNano()/100*100 {
			t.Fatalf("%s: times not restored: %+v", rel, meta)
		}
	}
	meta, _ := statBasic(filepath.Join(dest, "a.txt"))
	if meta.Attributes != manifest.AttrHidden|manifest.AttrReadOnly {
		t.Fatalf("attributes not restored: 0x%x", meta.Attributes)
	}
}

func clearReadOnly(path string) {
	p, _ := windows.UTF16PtrFromString(path)
	_ = windows.SetFileAttributes(p, windows.FILE_ATTRIBUTE_NORMAL)
}

func TestOffsetsPointAtTarHeaders(t *testing.T) {
	t.Parallel()

	src := t.TempDir()
	mustWrite(t, filepath.Join(src, "one.txt"), bytes.Repeat([]byte{1}, 700))
	mustWrite(t, filepath.Join(src, "two.txt"), []byte("second"))
	m, tarData := build(t, src)

	for _, e := range m.Entries {
		if e.Type != manifest.TypeFile {
			continue
		}
		tr := tar.NewReader(bytes.NewReader(tarData[*e.Offset:]))
		hdr, err := tr.Next()
		if err != nil || hdr.Name != e.Path {
			t.Fatalf("offset of %q does not point at its header (got %v, %v)", e.Path, hdr, err)
		}
	}
}

func TestBuildSkipsExcludedDirectory(t *testing.T) {
	t.Parallel()

	src := t.TempDir()
	mustWrite(t, filepath.Join(src, "keep.txt"), []byte("keep"))
	mustWrite(t, filepath.Join(src, "backups", "old.enc"), []byte("skip"))
	m, _ := build(t, src, filepath.Join(src, "backups"))
	for _, e := range m.Entries {
		if strings.HasPrefix(e.Path, "backups") {
			t.Fatalf("excluded entry recorded: %q", e.Path)
		}
	}
}

func TestVerifyModeWritesNothingAndDetectsCorruption(t *testing.T) {
	t.Parallel()

	src := t.TempDir()
	mustWrite(t, filepath.Join(src, "dir", "f.txt"), []byte("verify me"))
	m, tarData := build(t, src)

	dest := filepath.Join(t.TempDir(), "never-created")
	if err := restore(t, m, tarData, dest, true); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("verify mode must not create the destination")
	}

	corrupt := bytes.Replace(tarData, []byte("verify me"), []byte("verify ME"), 1)
	err := restore(t, m, corrupt, dest, true)
	if err == nil || !strings.Contains(err.Error(), "does not match its checksum") {
		t.Fatalf("expected checksum error, got %v", err)
	}
}

func TestRestoreRejectsUnknownTarEntry(t *testing.T) {
	t.Parallel()

	src := t.TempDir()
	mustWrite(t, filepath.Join(src, "f.txt"), []byte("x"))
	m, _ := build(t, src)

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	tw.WriteHeader(&tar.Header{Name: "intruder.txt", Size: 1, Mode: 0o600, Typeflag: tar.TypeReg})
	tw.Write([]byte("x"))
	tw.Close()

	err := restore(t, m, buf.Bytes(), t.TempDir(), true)
	if err == nil || !strings.Contains(err.Error(), "not in the manifest") {
		t.Fatalf("expected unknown-entry error, got %v", err)
	}
}

func TestFinishReportsMissingFiles(t *testing.T) {
	t.Parallel()

	src := t.TempDir()
	mustWrite(t, filepath.Join(src, "a.txt"), []byte("a"))
	mustWrite(t, filepath.Join(src, "b.txt"), []byte("b"))
	m, _ := build(t, src)

	var empty bytes.Buffer
	tar.NewWriter(&empty).Close()
	err := restore(t, m, empty.Bytes(), t.TempDir(), true)
	if err == nil || !strings.Contains(err.Error(), "2 file(s)") {
		t.Fatalf("expected missing-files error, got %v", err)
	}
}

func TestTargetPathRejectsTraversal(t *testing.T) {
	t.Parallel()

	r := NewRestorer(&manifest.Manifest{}, t.TempDir(), false)
	for _, p := range []string{"../x", "a/../../x", "C:/x", `a\..\x`} {
		if _, err := r.targetPath(p); err == nil {
			t.Fatalf("targetPath(%q) accepted a traversal path", p)
		}
	}
}

func TestFiletimeConversionRoundTrip(t *testing.T) {
	t.Parallel()

	ns := time.Date(2026, 9, 26, 12, 0, 0, 123456700, time.UTC).UnixNano()
	ft := unixNanoToFiletime(ns)
	back := filetimeToUnixNano(int64(ft.HighDateTime)<<32 | int64(ft.LowDateTime))
	if back != ns {
		t.Fatalf("round trip %d -> %d", ns, back)
	}
	if filetimeToUnixNano(0) != 0 {
		t.Fatal("zero FILETIME must map to 0")
	}
}
