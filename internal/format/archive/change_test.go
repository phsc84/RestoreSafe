package archive

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phsc84/restoresafe/internal/format/manifest"
)

// buildWithHook backs up src with SkipUnreadable while change alters the file
// named target at the moment of hook (testHookOpen or testHookCopy), then
// restores the result into a new folder. Tests with hooks don't run in
// parallel.
func buildWithHook(t *testing.T, src, target string, hook *func(string), change func(path string)) (*manifest.Manifest, BuildStats, string) {
	t.Helper()
	var stats BuildStats
	*hook = func(path string) {
		if filepath.Base(path) == target {
			change(path)
		}
	}
	t.Cleanup(func() { testHookOpen, testHookCopy = nil, nil })

	mb := manifest.NewBuilder(manifest.Header{SetType: manifest.SetTypeFull, ChainID: "ABC123", DirectoryName: "src", SourcePath: src})
	var tarBuf bytes.Buffer
	if err := BuildTar(context.Background(), &tarBuf, BuildOptions{SourceDir: src, SkipUnreadable: true, Stats: &stats}, mb); err != nil {
		t.Fatalf("BuildTar: %v", err)
	}
	data, err := mb.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	m, err := manifest.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()
	if err := restore(t, m, tarBuf.Bytes(), dest, false); err != nil {
		t.Fatalf("restore: %v", err)
	}
	return m, stats, dest
}

func entryOf(m *manifest.Manifest, p string) *manifest.Entry {
	for i := range m.Entries {
		if m.Entries[i].Path == p {
			return &m.Entries[i]
		}
	}
	return nil
}

func TestBuildTarFileVanishesBeforeItIsRead(t *testing.T) {
	src := t.TempDir()
	mustWrite(t, filepath.Join(src, "gone.txt"), []byte("here at the walk"))
	mustWrite(t, filepath.Join(src, "kept.txt"), []byte("kept"))
	change := func(path string) {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	m, stats, dest := buildWithHook(t, src, "gone.txt", &testHookOpen, change)

	if stats.Vanished != 1 || entryOf(m, "gone.txt") != nil {
		t.Fatalf("a vanished file is counted and left out: stats %+v, entry %+v", stats, entryOf(m, "gone.txt"))
	}
	if data, _ := os.ReadFile(filepath.Join(dest, "kept.txt")); string(data) != "kept" {
		t.Fatalf("the other file restored as %q", data)
	}
}

func TestBuildTarFileShrinksWhileRead(t *testing.T) {
	src := t.TempDir()
	mustWrite(t, filepath.Join(src, "log.txt"), []byte(strings.Repeat("x", 4096)))
	mustWrite(t, filepath.Join(src, "z-after.txt"), []byte("after"))
	change := func(path string) {
		if err := os.Truncate(path, 100); err != nil {
			t.Fatal(err)
		}
	}
	m, stats, dest := buildWithHook(t, src, "log.txt", &testHookCopy, change)

	e := entryOf(m, "log.txt")
	if e == nil || e.Type != manifest.TypeSkipped || !e.Void || !strings.Contains(e.Reason, "became smaller") || stats.Skipped != 1 {
		t.Fatalf("a file that shrank is skipped with a void TAR entry: %+v, stats %+v", e, stats)
	}
	// The TAR stream stays valid: the file after it restores.
	if data, _ := os.ReadFile(filepath.Join(dest, "z-after.txt")); string(data) != "after" {
		t.Fatalf("the file after the void entry restored as %q", data)
	}
	if _, err := os.Stat(filepath.Join(dest, "log.txt")); !os.IsNotExist(err) {
		t.Fatal("the skipped file must not be restored")
	}
}

func TestBuildTarFileGrowsWhileRead(t *testing.T) {
	src := t.TempDir()
	mustWrite(t, filepath.Join(src, "log.txt"), []byte("short"))
	mustWrite(t, filepath.Join(src, "z-after.txt"), []byte("after"))
	change := func(path string) {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if _, err := f.WriteString(" and longer"); err != nil {
			t.Fatal(err)
		}
	}
	m, _, dest := buildWithHook(t, src, "log.txt", &testHookCopy, change)

	e := entryOf(m, "log.txt")
	if e == nil || e.Type != manifest.TypeSkipped || !e.Void || !strings.Contains(e.Reason, "grew") {
		t.Fatalf("a file that grew is skipped with a void TAR entry: %+v", e)
	}
	if data, _ := os.ReadFile(filepath.Join(dest, "z-after.txt")); string(data) != "after" {
		t.Fatalf("the file after the void entry restored as %q", data)
	}
}
