package archive

import (
	"RestoreSafe/internal/manifest"
	"RestoreSafe/internal/util"
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// lockExclusively opens path without sharing, so other opens fail with a
// sharing violation (like a PST file open in Outlook).
func lockExclusively(t *testing.T, path string) {
	t.Helper()
	p, _ := windows.UTF16PtrFromString(path)
	h, err := windows.CreateFile(p, windows.GENERIC_READ, 0, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatalf("lock %s: %v", path, err)
	}
	t.Cleanup(func() { windows.CloseHandle(h) })
}

func buildWith(t *testing.T, opts BuildOptions) (*manifest.Manifest, []byte, error) {
	t.Helper()
	mb := manifest.NewBuilder(manifest.Header{SetType: manifest.SetTypeFull, ChainID: "ABC123", DirectoryName: "src", SourcePath: opts.SourceDir})
	var buf bytes.Buffer
	if err := BuildTar(&buf, opts, mb); err != nil {
		return nil, nil, err
	}
	data, err := mb.Bytes()
	if err != nil {
		t.Fatalf("manifest Bytes: %v", err)
	}
	m, err := manifest.Parse(data)
	if err != nil {
		t.Fatalf("manifest Parse: %v", err)
	}
	return m, buf.Bytes(), nil
}

func TestBuildTarAppliesExcludePatterns(t *testing.T) {
	t.Parallel()

	src := t.TempDir()
	mustWrite(t, filepath.Join(src, "keep.txt"), []byte("keep"))
	mustWrite(t, filepath.Join(src, "scratch.TMP"), []byte("x"))
	mustWrite(t, filepath.Join(src, "web", "node_modules", "lib", "a.js"), []byte("x"))
	mustWrite(t, filepath.Join(src, "Cache", "c.bin"), []byte("x"))
	mustWrite(t, filepath.Join(src, "sub", "Cache", "kept.bin"), []byte("x"))

	matcher, err := util.NewExcludeMatcher([]string{"*.tmp", "node_modules", "/Cache"})
	if err != nil {
		t.Fatal(err)
	}
	var stats BuildStats
	m, _, err := buildWith(t, BuildOptions{SourceDir: src, Exclude: matcher, Stats: &stats})
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string]bool{}
	for _, e := range m.Entries {
		paths[e.Path] = true
	}
	for _, p := range []string{"keep.txt", "web", "sub", "sub/Cache", "sub/Cache/kept.bin"} {
		if !paths[p] {
			t.Fatalf("expected %q in manifest, got %v", p, paths)
		}
	}
	for _, p := range []string{"scratch.TMP", "web/node_modules", "Cache"} {
		if paths[p] {
			t.Fatalf("%q must be excluded", p)
		}
	}
	if stats.Excluded != 3 {
		t.Fatalf("expected 3 excluded entries, got %d", stats.Excluded)
	}
}

func TestBuildTarFailsOnLockedFileByDefault(t *testing.T) {
	t.Parallel()

	src := t.TempDir()
	locked := filepath.Join(src, "mail.pst")
	mustWrite(t, locked, []byte("mail"))
	lockExclusively(t, locked)

	_, _, err := buildWith(t, BuildOptions{SourceDir: src})
	if err == nil || !strings.Contains(err.Error(), "mail.pst") || !strings.Contains(err.Error(), "on_unreadable_file: skip") {
		t.Fatalf("expected an unreadable-file error with remedy, got %v", err)
	}
}

func TestBuildTarSkipsLockedFileWhenConfigured(t *testing.T) {
	t.Parallel()

	src := t.TempDir()
	mustWrite(t, filepath.Join(src, "a.txt"), []byte("alpha"))
	locked := filepath.Join(src, "box", "mail.pst")
	mustWrite(t, locked, []byte("mail"))
	mustWrite(t, filepath.Join(src, "z.txt"), []byte("zulu"))
	lockExclusively(t, locked)

	var skipped []string
	var stats BuildStats
	m, tarData, err := buildWith(t, BuildOptions{
		SourceDir: src, SkipUnreadable: true, Stats: &stats,
		OnSkip: func(rel, reason string) { skipped = append(skipped, rel) },
	})
	if err != nil {
		t.Fatalf("BuildTar: %v", err)
	}
	if len(skipped) != 1 || skipped[0] != "box/mail.pst" || stats.Skipped != 1 || m.Footer.Skipped != 1 {
		t.Fatalf("unexpected skip report: %v %+v %+v", skipped, stats, m.Footer)
	}
	if got := SkippedFiles(m); len(got) != 1 || got[0] != "box/mail.pst" {
		t.Fatalf("SkippedFiles = %v", got)
	}

	dest := filepath.Join(t.TempDir(), "out")
	if err := os.Mkdir(dest, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := restore(t, m, tarData, dest, false); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "box", "mail.pst")); !os.IsNotExist(err) {
		t.Fatal("skipped file must not be restored")
	}
	if data, _ := os.ReadFile(filepath.Join(dest, "z.txt")); string(data) != "zulu" {
		t.Fatal("files after the skipped one must be restored")
	}
}

// A read that fails after the TAR header was written leaves a void entry;
// restore must skip it and restore the rest.
func TestRestoreSkipsVoidEntries(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	write := func(name string, data []byte) {
		tw.WriteHeader(&tar.Header{Name: name, Size: int64(len(data)), Mode: 0o600, Typeflag: tar.TypeReg})
		tw.Write(data)
	}
	write("broken.db", make([]byte, 10))
	offset := int64(1024)
	write("good.txt", []byte("good"))
	tw.Close()

	mb := manifest.NewBuilder(manifest.Header{SetType: manifest.SetTypeFull, ChainID: "ABC123", DirectoryName: "src"})
	mb.Add(manifest.Entry{Path: "broken.db", Type: manifest.TypeSkipped, Reason: "read error", Void: true})
	mb.Add(manifest.Entry{Path: "good.txt", Type: manifest.TypeFile, Size: 4, Hash: sha256Hex("good"), Origin: manifest.OriginFull, Offset: &offset})
	data, err := mb.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	m, err := manifest.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := restore(t, m, buf.Bytes(), t.TempDir(), true); err != nil {
		t.Fatalf("restore with void entry: %v", err)
	}
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
