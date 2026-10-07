package archive

import (
	"RestoreSafe/internal/format/manifest"
	"RestoreSafe/internal/testutil/filelock"
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func buildDiff(t *testing.T, opts BuildOptions) (*manifest.Manifest, []byte) {
	t.Helper()
	mb := manifest.NewBuilder(manifest.Header{SetType: manifest.SetTypeDiff, ChainID: "ABC123", DiffNumber: 1, DirectoryName: "src", SourcePath: opts.SourceDir})
	var buf bytes.Buffer
	if err := BuildTar(&buf, opts, mb); err != nil {
		t.Fatalf("BuildTar (differential): %v", err)
	}
	data, err := mb.Bytes()
	if err != nil {
		t.Fatalf("differential manifest invalid: %v", err)
	}
	m, err := manifest.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return m, buf.Bytes()
}

func entryByPath(m *manifest.Manifest) map[string]manifest.Entry {
	out := make(map[string]manifest.Entry, len(m.Entries))
	for _, e := range m.Entries {
		out[e.Path] = e
	}
	return out
}

func TestDifferentialDetectsChanges(t *testing.T) {
	t.Parallel()

	src := t.TempDir()
	mustWrite(t, filepath.Join(src, "same.txt"), []byte("unchanged"))
	mustWrite(t, filepath.Join(src, "grow.txt"), []byte("short"))
	mustWrite(t, filepath.Join(src, "touch.txt"), []byte("same content"))
	mustWrite(t, filepath.Join(src, "sneaky.txt"), []byte("original"))
	mustWrite(t, filepath.Join(src, "gone.txt"), []byte("deleted later"))
	mustWrite(t, filepath.Join(src, "dir", "keep.txt"), []byte("keep"))
	base, _ := build(t, src)
	baseEntries := entryByPath(base)

	mustWrite(t, filepath.Join(src, "grow.txt"), []byte("much longer now"))
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Join(src, "touch.txt"), later, later); err != nil {
		t.Fatal(err)
	}
	// Same size, new content, old last-write time restored: only the NTFS
	// change time reveals the edit.
	sneaky := filepath.Join(src, "sneaky.txt")
	mustWrite(t, sneaky, []byte("tampered"))
	orig := baseEntries["sneaky.txt"]
	if err := setTimes(sneaky, orig.CreationTime, orig.ModTime); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(src, "gone.txt")); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(src, "new.txt"), []byte("brand new"))

	var stats BuildStats
	m, tarData := buildDiff(t, BuildOptions{SourceDir: src, Base: base, Stats: &stats})
	got := entryByPath(m)

	for _, p := range []string{"same.txt", "dir/keep.txt"} {
		if e := got[p]; e.Origin != manifest.OriginFull || e.Offset != nil || e.Hash != baseEntries[p].Hash {
			t.Fatalf("%s must refer to the full backup, got %+v", p, e)
		}
	}
	for _, p := range []string{"grow.txt", "touch.txt", "sneaky.txt", "new.txt"} {
		if e := got[p]; e.Origin != manifest.OriginDiff || e.Offset == nil {
			t.Fatalf("%s must be stored in the differential, got %+v", p, e)
		}
	}
	if _, ok := got["gone.txt"]; ok {
		t.Fatal("deleted file must not be in the differential manifest")
	}
	if stats.Unchanged != 2 || stats.Stored != 4 {
		t.Fatalf("unexpected stats %+v", stats)
	}
	if m.Footer.DataBytes != int64(len("much longer now")+len("same content")+len("tampered")+len("brand new")) {
		t.Fatalf("unexpected data bytes %d", m.Footer.DataBytes)
	}

	// The differential's own section verifies on its own.
	r := NewRestorer(m, "", true)
	r.ExpectOwnContentOnly()
	if err := r.ExtractSection(bytes.NewReader(tarData), DecideOwn(m)); err != nil {
		t.Fatalf("verify own section: %v", err)
	}
	if err := r.Finish(); err != nil {
		t.Fatalf("finish own section: %v", err)
	}
}

func TestDifferentialKeepsOlderVersionOfLockedChangedFile(t *testing.T) {
	t.Parallel()

	src := t.TempDir()
	db := filepath.Join(src, "busy.db")
	mustWrite(t, db, []byte("version 1"))
	mustWrite(t, filepath.Join(src, "fresh.db"), []byte("x"))
	base, _ := build(t, src)

	mustWrite(t, db, []byte("version 2 longer"))
	mustWrite(t, filepath.Join(src, "new.db"), []byte("new"))
	filelock.Hold(t, db)
	filelock.Hold(t, filepath.Join(src, "new.db"))

	var stale []string
	var stats BuildStats
	m, _ := buildDiff(t, BuildOptions{
		SourceDir: src, Base: base, SkipUnreadable: true, Stats: &stats,
		OnSkip: func(rel, _ string, isStale bool) {
			if isStale {
				stale = append(stale, rel)
			}
		},
	})
	got := entryByPath(m)
	if e := got["busy.db"]; !e.Stale || e.Origin != manifest.OriginFull || e.Size != int64(len("version 1")) {
		t.Fatalf("locked changed file must keep the full backup's version, got %+v", e)
	}
	if e := got["new.db"]; e.Type != manifest.TypeSkipped {
		t.Fatalf("locked new file must be skipped, got %+v", e)
	}
	if len(stale) != 1 || stats.Stale != 1 || stats.Skipped != 1 || m.Footer.Stale != 1 {
		t.Fatalf("unexpected report: stale=%v stats=%+v footer=%+v", stale, stats, m.Footer)
	}
}
