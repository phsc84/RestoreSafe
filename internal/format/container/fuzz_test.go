package container

import (
	"bytes"
	"io"
	"testing"

	"github.com/phsc84/restoresafe/internal/config"
	"github.com/phsc84/restoresafe/internal/format/manifest"
)

// FuzzDecodeTrailer checks that an accepted trailer is exactly the encoding
// of what it decoded to.
func FuzzDecodeTrailer(f *testing.F) {
	f.Add(Trailer{DataOffset: 100, DataLength: 50, ManifestOffset: 150, ManifestLength: 40, PartCount: 1}.Encode())
	f.Fuzz(func(t *testing.T, b []byte) {
		tr, err := DecodeTrailer(b)
		if err != nil {
			return
		}
		if !bytes.Equal(tr.Encode(), b) {
			t.Fatalf("%x decoded as %+v, which encodes differently", b, tr)
		}
	})
}

// FuzzOpen reads the structure of arbitrary bytes as a one-part set, as Open
// does, but from memory: a file per input would make the virus scanner the
// bottleneck. An accepted set has its sections inside the file, one after the
// other, whatever the trailer says.
func FuzzOpen(f *testing.F) {
	// No Argon2 and no file: every fuzz worker runs this first.
	ks, master, _ := NewKeySet(config.AuthModePassword)
	ks.Slots = []Slot{{Type: SlotPassword, Label: "Password", KDF: KDF{Alg: kdfAlgArgon2, Salt: make([]byte, 32), Time: 2, MemoryKiB: 65536, Threads: 1}, Nonce: make([]byte, 12), Wrapped: make([]byte, 48)}}
	h, _ := NewHeader(manifest.SetTypeFull, "ABC123", "ABC123", "Documents", "2026-09-26", *ks)
	var seed bytes.Buffer
	if _, err := Write(&seed, h, master, 1<<20, bytes.NewReader([]byte("restoresafe")), func(w io.Writer) error {
		_, err := w.Write([]byte("manifest"))
		return err
	}); err != nil {
		f.Fatal(err)
	}
	f.Add(seed.Bytes())
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _, tr, err := readStructure(bytes.NewReader(data), int64(len(data)), 1)
		if err != nil {
			return
		}
		size := int64(len(data))
		if tr.DataOffset < 0 || tr.DataLength < 0 || tr.ManifestLength < 0 ||
			tr.ManifestOffset != tr.DataOffset+tr.DataLength ||
			tr.ManifestOffset+tr.ManifestLength+TrailerLen != size || tr.ManifestOffset > size {
			t.Fatalf("accepted trailer %+v for a file of %d bytes", tr, size)
		}
	})
}
