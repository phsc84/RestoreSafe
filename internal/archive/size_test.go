package archive

import (
	"RestoreSafe/internal/util"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSourceSizeFollowsExcludeRules(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	write := func(rel string, n int) {
		p := filepath.Join(src, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, make([]byte, n), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("keep.txt", 10)
	write("sub/keep.bin", 20)
	write("Cache/skip.bin", 1000)
	write("sub/skip.tmp", 300)
	write("inner-backup/part.enc", 5000)

	exclude, err := util.NewExcludeMatcher([]string{"Cache", "*.tmp"})
	if err != nil {
		t.Fatal(err)
	}
	opts := BuildOptions{SourceDir: src, ExcludeDirs: []string{filepath.Join(src, "inner-backup")}, Exclude: exclude}
	if got := SourceSize(opts); got != 30 {
		t.Fatalf("SourceSize = %d, want 30", got)
	}
	if got := SourceSize(BuildOptions{SourceDir: src}); got != 6330 {
		t.Fatalf("SourceSize without excludes = %d, want 6330", got)
	}
}

func TestMeasureSourceCountsFilesChangedSince(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	fullBackup := time.Now().Add(-24 * time.Hour)
	before := fullBackup.Add(-24 * time.Hour)
	after := fullBackup.Add(time.Hour)
	for name, times := range map[string][2]time.Time{
		"unchanged.txt": {before, before}, // created and written before the full backup
		"modified.txt":  {before, after},  // written after it
		"copied.txt":    {after, before},  // copied in later: new creation time, old write time
		"skip.tmp":      {after, after},   // excluded
	} {
		p := filepath.Join(src, name)
		if err := os.WriteFile(p, make([]byte, 100), 0o600); err != nil {
			t.Fatal(err)
		}
		setFileTimes(t, p, times[0], times[1])
	}
	exclude, err := util.NewExcludeMatcher([]string{"*.tmp"})
	if err != nil {
		t.Fatal(err)
	}

	m, err := MeasureSource(BuildOptions{SourceDir: src, Exclude: exclude}, fullBackup)
	if err != nil || m.Total != 300 || m.Changed != 200 {
		t.Fatalf("MeasureSource = %+v, %v; want total 300, changed 200", m, err)
	}
	if m, _ := MeasureSource(BuildOptions{SourceDir: src, Exclude: exclude}, time.Time{}); m.Changed != m.Total {
		t.Fatalf("without a since time everything counts as changed: %+v", m)
	}
	if _, err := MeasureSource(BuildOptions{SourceDir: filepath.Join(src, "missing")}, fullBackup); err == nil {
		t.Fatal("a missing source directory must be reported")
	}
}
