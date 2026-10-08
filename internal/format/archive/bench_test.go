package archive

import (
	"RestoreSafe/internal/format/manifest"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// BenchmarkBuildTar reads a tree of 10 directories of 20 files of 128 KiB
// (25 MiB) into a TAR stream.
func BenchmarkBuildTar(b *testing.B) {
	src := b.TempDir()
	content := bytes.Repeat([]byte("restoresafe"), 128<<10/11)
	var total int64
	for d := range 10 {
		dir := filepath.Join(src, fmt.Sprintf("dir%02d", d))
		if err := os.Mkdir(dir, 0o750); err != nil {
			b.Fatal(err)
		}
		for f := range 20 {
			if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("file%02d.bin", f)), content, 0o600); err != nil {
				b.Fatal(err)
			}
			total += int64(len(content))
		}
	}

	b.SetBytes(total)
	b.ReportAllocs()
	for b.Loop() {
		mb := manifest.NewBuilder(manifest.Header{SetType: manifest.SetTypeFull, ChainID: "ABC123", DirectoryName: "src", SourcePath: src})
		if err := BuildTar(context.Background(), io.Discard, BuildOptions{SourceDir: src}, mb); err != nil {
			b.Fatal(err)
		}
	}
}
