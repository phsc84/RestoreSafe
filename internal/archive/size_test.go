package archive

import (
	"RestoreSafe/internal/util"
	"os"
	"path/filepath"
	"testing"
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
