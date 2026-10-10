package manifest

import (
	"bytes"
	"fmt"
	"io"
	"testing"
)

const benchEntries = 100_000

func benchBuilder() *Builder {
	b := NewBuilder(fullHeader())
	b.Add(Entry{Path: "docs", Type: TypeDir})
	for i := range benchEntries - 1 {
		b.Add(Entry{
			Path: fmt.Sprintf("docs/report-%06d.docx", i), Type: TypeFile, Size: 1000,
			ModTime: 1_790_000_000_000_000_000, ChangeTime: 1_790_000_000_000_000_000,
			Hash: hashOf(i), Origin: OriginFull, Offset: off(int64(i) * 1536),
		})
	}
	return b
}

func hashOf(i int) string { return fmt.Sprintf("%064x", i) }

func BenchmarkManifestEncode(b *testing.B) {
	mb := benchBuilder()
	b.ReportAllocs()
	for b.Loop() {
		if err := mb.Encode(io.Discard); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkManifestDecode(b *testing.B) {
	data, err := benchBuilder().Bytes()
	if err != nil {
		b.Fatal(err)
	}
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Decode(bytes.NewReader(data)); err != nil {
			b.Fatal(err)
		}
	}
}
