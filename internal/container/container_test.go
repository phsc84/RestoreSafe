package container

import (
	"RestoreSafe/internal/manifest"
	"RestoreSafe/internal/security"
	"RestoreSafe/internal/util"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var testParams = security.Argon2Params{Time: security.MinArgonTime, MemoryKB: security.MinArgonMemoryKB, Threads: security.MinArgonThreads}

var testHash = strings.Repeat("cd", 32)

func newPasswordKeySet(t *testing.T, password string) (*KeySet, []byte) {
	t.Helper()
	ks, master, err := NewKeySet(AuthModePassword)
	if err != nil {
		t.Fatalf("NewKeySet: %v", err)
	}
	if err := ks.AddSlot(master, SlotPassword, "Password", []byte(password), testParams, nil, ""); err != nil {
		t.Fatalf("AddSlot: %v", err)
	}
	return ks, master
}

func testManifest(t *testing.T, h *Header, dataLen int) []byte {
	t.Helper()
	b := manifest.NewBuilder(manifest.Header{SetType: h.SetType, ChainID: h.ChainID, DiffNumber: h.DiffNumber, DirectoryName: h.DirectoryName, SourcePath: "C:/src"})
	zero := int64(0)
	b.Add(manifest.Entry{Path: "data.bin", Type: manifest.TypeFile, Size: int64(dataLen), Hash: testHash, Origin: manifest.OriginFull, Offset: &zero})
	data, err := b.Bytes()
	if err != nil {
		t.Fatalf("manifest Bytes: %v", err)
	}
	return data
}

// writeTestSet writes a full set with the given payload into dir and returns
// the part paths.
func writeTestSet(t *testing.T, dir string, ks *KeySet, master, payload []byte, splitSize int64) ([]string, *WriteResult) {
	t.Helper()
	h, err := NewHeader(manifest.SetTypeFull, "ABC123", "ABC123", "Documents", "2026-09-26", *ks)
	if err != nil {
		t.Fatalf("NewHeader: %v", err)
	}
	sw := util.NewWriter(func(seq int) string {
		return filepath.Join(dir, fmt.Sprintf("part-%03d.enc", seq))
	}, splitSize)
	res, err := Write(sw, h, master, splitSize, bytes.NewReader(payload), func() ([]byte, error) { return testManifest(t, h, len(payload)), nil })
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := sw.Close(); err != nil {
		t.Fatalf("close split writer: %v", err)
	}
	if res.Trailer.PartCount != len(sw.Paths()) {
		t.Fatalf("trailer part count %d, split writer wrote %d parts", res.Trailer.PartCount, len(sw.Paths()))
	}
	return sw.Paths(), res
}

func TestWriteOpenRoundTripAcrossParts(t *testing.T) {
	t.Parallel()

	ks, master := newPasswordKeySet(t, "correct horse")
	payload := bytes.Repeat([]byte("restoresafe"), 300_000) // ~3.3 MB
	paths, res := writeTestSet(t, t.TempDir(), ks, master, payload, 1024*1024)
	if len(paths) < 3 {
		t.Fatalf("expected several parts, got %d", len(paths))
	}

	set, err := Open(paths)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer set.Close()

	unlocked, err := set.Header.KeySet.Unlock(0, []byte("correct horse"))
	if err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	keys, err := set.SectionKeys(unlocked)
	if err != nil {
		t.Fatalf("SectionKeys: %v", err)
	}
	m, sum, err := set.ReadManifest(keys)
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if sum != res.ManifestSHA256 || len(m.Entries) != 1 {
		t.Fatalf("unexpected manifest: sum=%s entries=%d", sum, len(m.Entries))
	}
	var out bytes.Buffer
	if err := set.DecryptData(keys, &out); err != nil {
		t.Fatalf("DecryptData: %v", err)
	}
	if !bytes.Equal(out.Bytes(), payload) {
		t.Fatal("decrypted data differs from payload")
	}
}

func TestOpenHandlesTrailerSpanningTwoParts(t *testing.T) {
	t.Parallel()

	ks, master := newPasswordKeySet(t, "pw")
	// Find a payload size whose total stream length ends 10 bytes into a
	// new part, so the 64-byte trailer spans the part boundary.
	dir := t.TempDir()
	probePaths, probe := writeTestSet(t, filepath.Join(dir), ks, master, []byte("x"), 1<<30)
	for _, p := range probePaths {
		os.Remove(p)
	}
	const split = 4096
	overheadWithoutPayload := probe.TotalSize - 1
	payloadLen := int(2*split + 10 - overheadWithoutPayload)

	paths, _ := writeTestSet(t, dir, ks, master, bytes.Repeat([]byte{7}, payloadLen), split)
	// The manifest length depends on the digits of the payload size, so the
	// total may be off by a byte or two; the trailer must still span parts.
	last, err := os.Stat(paths[len(paths)-1])
	if err != nil {
		t.Fatal(err)
	}
	if last.Size() >= TrailerLen {
		t.Fatalf("test setup: last part has %d bytes, trailer does not span two parts", last.Size())
	}
	set, err := Open(paths)
	if err != nil {
		t.Fatalf("Open with spanning trailer: %v", err)
	}
	set.Close()
}

func TestUnlockRejectsWrongPassword(t *testing.T) {
	t.Parallel()

	ks, _ := newPasswordKeySet(t, "right")
	if _, err := ks.Unlock(0, []byte("wrong")); !errors.Is(err, security.ErrWrongPassword) {
		t.Fatalf("expected ErrWrongPassword, got %v", err)
	}
}

func TestSlotSwapFailsAuthentication(t *testing.T) {
	t.Parallel()

	ks, master, err := NewKeySet(AuthModeYubiKey)
	if err != nil {
		t.Fatalf("NewKeySet: %v", err)
	}
	// Two slots sealed with the same secret: swapping them changes the index
	// in the slot AAD, so unlocking must fail.
	for i := 0; i < 2; i++ {
		if err := ks.AddSlot(master, SlotYubiKey, fmt.Sprintf("YubiKey %d", i+1), []byte("secret"), testParams, testChallenge(true), ""); err != nil {
			t.Fatalf("AddSlot: %v", err)
		}
	}
	ks.Slots[0], ks.Slots[1] = ks.Slots[1], ks.Slots[0]
	if _, err := ks.Unlock(0, []byte("secret")); !errors.Is(err, security.ErrWrongPassword) {
		t.Fatalf("expected swapped slot to fail, got %v", err)
	}
}

func testChallenge(noPassword bool) *security.ChallengeData {
	cd := &security.ChallengeData{
		Version:    1,
		NoPassword: noPassword,
		CredID:     "Y3JlZC1pZA==",
		Salt:       "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=",
	}
	return cd
}

func TestHeaderTamperingFailsAuthentication(t *testing.T) {
	t.Parallel()

	ks, master := newPasswordKeySet(t, "pw")
	dir := t.TempDir()
	paths, _ := writeTestSet(t, dir, ks, master, []byte("payload"), 1<<20)

	// Change one character of the directory name inside the header JSON; the
	// header stays valid but its hash changes.
	raw, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	tampered := bytes.Replace(raw, []byte(`"directory_name":"Documents"`), []byte(`"directory_name":"Documentz"`), 1)
	if bytes.Equal(raw, tampered) {
		t.Fatal("tamper target not found")
	}
	if err := os.WriteFile(paths[0], tampered, 0o600); err != nil {
		t.Fatal(err)
	}

	set, err := Open(paths)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer set.Close()
	unlocked, err := set.Header.KeySet.Unlock(0, []byte("pw"))
	if err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	keys, _ := set.SectionKeys(unlocked)
	if _, _, err := set.ReadManifest(keys); !errors.Is(err, security.ErrCorrupted) {
		t.Fatalf("expected ErrCorrupted after header tampering, got %v", err)
	}
}

func TestDataKeyCannotDecryptManifest(t *testing.T) {
	t.Parallel()

	ks, master := newPasswordKeySet(t, "pw")
	paths, _ := writeTestSet(t, t.TempDir(), ks, master, []byte("payload"), 1<<20)
	set, err := Open(paths)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer set.Close()
	keys, _ := set.SectionKeys(master)
	swapped := &SectionKeys{Data: keys.Manifest, Manifest: keys.Data}
	if _, _, err := set.ReadManifest(swapped); !errors.Is(err, security.ErrCorrupted) {
		t.Fatalf("expected manifest decryption with data key to fail, got %v", err)
	}
}

func TestOpenDetectsIncompleteSets(t *testing.T) {
	t.Parallel()

	ks, master := newPasswordKeySet(t, "pw")
	payload := bytes.Repeat([]byte{1}, 3*1024*1024)

	t.Run("missing last part", func(t *testing.T) {
		paths, _ := writeTestSet(t, t.TempDir(), ks, master, payload, 1024*1024)
		_, err := Open(paths[:len(paths)-1])
		var inc *ErrIncomplete
		if !errors.As(err, &inc) {
			t.Fatalf("expected ErrIncomplete, got %v", err)
		}
	})
	t.Run("truncated", func(t *testing.T) {
		paths, _ := writeTestSet(t, t.TempDir(), ks, master, payload, 1024*1024)
		last := paths[len(paths)-1]
		fi, _ := os.Stat(last)
		if err := os.Truncate(last, fi.Size()-1); err != nil {
			t.Fatal(err)
		}
		var inc *ErrIncomplete
		if _, err := Open(paths); !errors.As(err, &inc) {
			t.Fatalf("expected ErrIncomplete, got %v", err)
		}
	})
	t.Run("extra part", func(t *testing.T) {
		dir := t.TempDir()
		paths, _ := writeTestSet(t, dir, ks, master, payload, 1024*1024)
		extra := filepath.Join(dir, "extra.enc")
		if err := os.WriteFile(extra, []byte("junk"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(append(paths, extra)); err == nil {
			t.Fatal("expected error for extra part")
		}
	})
	t.Run("middle part missing", func(t *testing.T) {
		paths, _ := writeTestSet(t, t.TempDir(), ks, master, payload, 1024*1024)
		var inc *ErrIncomplete
		if _, err := Open(append([]string{paths[0]}, paths[2:]...)); !errors.As(err, &inc) {
			t.Fatalf("expected ErrIncomplete, got %v", err)
		}
	})
}

func TestTrailerRoundTripAndChecksum(t *testing.T) {
	t.Parallel()

	tr := Trailer{DataOffset: 100, DataLength: 200, ManifestOffset: 300, ManifestLength: 50, PartCount: 2}
	raw := tr.Encode()
	if len(raw) != TrailerLen {
		t.Fatalf("trailer length %d", len(raw))
	}
	got, err := DecodeTrailer(raw)
	if err != nil || got != tr {
		t.Fatalf("round trip: got %+v, %v", got, err)
	}
	raw[20] ^= 1
	if _, err := DecodeTrailer(raw); err == nil {
		t.Fatal("expected checksum mismatch")
	}
}

func TestReadHeaderRejectsOtherVersions(t *testing.T) {
	t.Parallel()

	v1 := append([]byte("RSBKP\x00\x01\x00"), make([]byte, 64)...)
	if _, _, err := ReadHeader(bytes.NewReader(v1)); err == nil || !strings.Contains(err.Error(), "RestoreSafe 1.x") {
		t.Fatalf("expected 1.x error, got %v", err)
	}
	v3 := append([]byte("RSBKP\x00\x03\x00"), make([]byte, 64)...)
	if _, _, err := ReadHeader(bytes.NewReader(v3)); err == nil || !strings.Contains(err.Error(), "newer") {
		t.Fatalf("expected newer-format error, got %v", err)
	}
	if _, _, err := ReadHeader(bytes.NewReader([]byte("NOTABACKUPFILE"))); err == nil || !strings.Contains(err.Error(), "not a RestoreSafe backup") {
		t.Fatalf("expected format error, got %v", err)
	}
}

func TestHeaderValidation(t *testing.T) {
	t.Parallel()

	ks, _ := newPasswordKeySet(t, "pw")
	base := func() *Header {
		h, err := NewHeader(manifest.SetTypeFull, "ABC123", "ABC123", "Documents", "2026-09-26", *ks)
		if err != nil {
			t.Fatal(err)
		}
		return h
	}
	if err := base().Validate(); err != nil {
		t.Fatalf("valid header rejected: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(*Header)
		want   string
	}{
		{"compression", func(h *Header) { h.Compression = "zstd" }, "compression"},
		{"chunk size", func(h *Header) { h.ChunkSize = 1024 }, "chunk size"},
		{"chain id", func(h *Header) { h.ChainID = "abc" }, "chain or run ID"},
		{"diff fields on full", func(h *Header) { h.DiffNumber = 1 }, "differential fields"},
		{"diff without base", func(h *Header) { h.SetType = manifest.SetTypeDiff; h.DiffNumber = 1 }, "base date"},
		{"directory name", func(h *Header) { h.DirectoryName = ".." }, "relative path element"},
		{"nonce", func(h *Header) { h.SetNonce = []byte{1} }, "set nonce"},
		{"slot type mismatch", func(h *Header) { h.KeySet.AuthMode = AuthModeYubiKey }, "does not match authentication mode"},
		{"bad argon", func(h *Header) { h.KeySet.Slots[0].KDF.MemoryKiB = 1 }, "Argon2 memory"},
		{"no slots", func(h *Header) { h.KeySet.Slots = nil }, "no slots"},
	}
	for _, tc := range cases {
		h := base()
		h.KeySet.Slots = append([]Slot(nil), h.KeySet.Slots...)
		tc.mutate(h)
		if err := h.Validate(); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: expected error containing %q, got %v", tc.name, tc.want, err)
		}
	}
}

func TestReadHeaderRejectsUnknownFields(t *testing.T) {
	t.Parallel()

	ks, _ := newPasswordKeySet(t, "pw")
	h, _ := NewHeader(manifest.SetTypeFull, "ABC123", "ABC123", "Documents", "2026-09-26", *ks)
	encoded, err := h.Encode()
	if err != nil {
		t.Fatal(err)
	}
	body := bytes.Replace(encoded[headerPrefixLen:], []byte(`{`), []byte(`{"extra":1,`), 1)
	prefix := append([]byte(nil), encoded[:headerPrefixLen]...)
	prefix[8], prefix[9], prefix[10], prefix[11] = byte(len(body)>>24), byte(len(body)>>16), byte(len(body)>>8), byte(len(body))
	if _, _, err := ReadHeader(bytes.NewReader(append(prefix, body...))); err == nil {
		t.Fatal("expected unknown header field to be rejected")
	}
}

func FuzzReadHeader(f *testing.F) {
	ks, _, _ := NewKeySet(AuthModePassword)
	h, _ := NewHeader(manifest.SetTypeFull, "ABC123", "ABC123", "Documents", "2026-09-26", *ks)
	h.KeySet.Slots = []Slot{{Type: SlotPassword, Label: "Password", KDF: KDF{Alg: kdfAlgArgon2, Salt: make([]byte, 32), Time: 2, MemoryKiB: 65536, Threads: 1}, Nonce: make([]byte, 12), Wrapped: make([]byte, 48)}}
	seed, _ := h.Encode()
	f.Add(seed)
	f.Fuzz(func(t *testing.T, data []byte) {
		h, _, err := ReadHeader(bytes.NewReader(data))
		if err != nil {
			return
		}
		if err := h.Validate(); err != nil {
			t.Fatalf("accepted header fails validation: %v", err)
		}
	})
}
