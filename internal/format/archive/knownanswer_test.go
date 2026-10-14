package archive

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phsc84/restoresafe/internal/format/manifest"
)

// knownTree writes a fixed tree: nested folders, an empty folder, an empty
// file, and files across the TAR block boundary.
func knownTree(t *testing.T) string {
	t.Helper()
	src := t.TempDir()
	mustWrite(t, filepath.Join(src, "a.txt"), []byte("alpha"))
	mustWrite(t, filepath.Join(src, "empty.bin"), nil)
	mustWrite(t, filepath.Join(src, "docs", "block.bin"), bytes.Repeat([]byte{0x5A}, 512))
	mustWrite(t, filepath.Join(src, "docs", "deep", "z.bin"), bytes.Repeat([]byte("restoresafe"), 1000))
	mustWrite(t, filepath.Join(src, "docs", "Ä ü.txt"), []byte("unicode name"))
	if err := os.Mkdir(filepath.Join(src, "void"), 0o750); err != nil {
		t.Fatal(err)
	}
	return src
}

// The TAR stream and the manifest of a fixed tree, recorded before RF-68
// split BuildTar, so that the split changes neither. File times depend on
// the file system and are left out of the manifest summary.
func TestBuildTarKnownAnswer(t *testing.T) {
	t.Parallel()

	src := knownTree(t)
	mb := manifest.NewBuilder(manifest.Header{SetType: manifest.SetTypeFull, ChainID: "ABC123", DirectoryName: "src", SourcePath: src})
	var tarBuf bytes.Buffer
	if err := BuildTar(context.Background(), &tarBuf, BuildOptions{SourceDir: src}, mb); err != nil {
		t.Fatal(err)
	}
	tarSum := sha256.Sum256(tarBuf.Bytes())

	var summary strings.Builder
	for _, e := range roundTrip(t, mb).Entries {
		offset := int64(-1)
		if e.Offset != nil {
			offset = *e.Offset
		}
		fmt.Fprintf(&summary, "%s|%s|%d|%s|%s|%d\n", e.Path, e.Type, e.Size, e.Hash, e.Origin, offset)
	}

	const wantTar = "c230c8712174a86d89ec395c7c2e36ac2b3a0173c67cd5f54d83704a92a998c2"
	const wantSummary = `a.txt|f|5|8ed3f6ad685b959ead7022518e1af76cd816f8e8ec7ccdda1ed4018e8f2223f8|F|0
docs|d|0|||-1
docs/block.bin|f|512|a863e21577e54cd763729803a621804da4b5030afa35bcf879ea3b3413488a66|F|1024
docs/deep|d|0|||-1
docs/deep/z.bin|f|11000|9eb820f2882aa013908a1e8ce68a945691c8ea681e6e80762646abe595843b1e|F|2048
docs/Ä ü.txt|f|12|8898cd6a1c9777bbb832388868a66967c501a8ab02291e595816b7b23470c838|F|13824
empty.bin|f|0|e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855|F|15872
void|d|0|||-1
`
	if got := hex.EncodeToString(tarSum[:]); got != wantTar {
		t.Errorf("TAR stream: SHA-256 %s, want %s", got, wantTar)
	}
	if got := summary.String(); got != wantSummary {
		t.Errorf("manifest:\n%s\nwant:\n%s", got, wantSummary)
	}
}
