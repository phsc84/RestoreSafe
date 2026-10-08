package cryptox

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

type failReader struct{ err error }

func (r *failReader) Read([]byte) (int, error) { return 0, r.err }

type failWriter struct{ err error }

func (w *failWriter) Write([]byte) (int, error) { return 0, w.err }

var testParams = Argon2Params{Time: MinArgonTime, MemoryKB: MinArgonMemoryKB, Threads: MinArgonThreads}

func testKey(t *testing.T) []byte {
	t.Helper()
	key, err := RandomBytes(KeyLen)
	if err != nil {
		t.Fatalf("RandomBytes: %v", err)
	}
	return key
}

func encryptToBytes(t *testing.T, plaintext, key, aad []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := EncryptStream(&buf, bytes.NewReader(plaintext), key, aad); err != nil {
		t.Fatalf("EncryptStream: %v", err)
	}
	return buf.Bytes()
}

func TestStreamRoundTrip(t *testing.T) {
	t.Parallel()

	key := testKey(t)
	aad := []byte("header-hash|section")
	for _, size := range []int{0, 1, ChunkSize - 1, ChunkSize, ChunkSize + 1, 2*ChunkSize + 17} {
		plaintext := bytes.Repeat([]byte{0xAB}, size)
		ciphertext := encryptToBytes(t, plaintext, key, aad)

		var out bytes.Buffer
		if err := DecryptStream(&out, bytes.NewReader(ciphertext), key, aad); err != nil {
			t.Fatalf("size %d: DecryptStream: %v", size, err)
		}
		if !bytes.Equal(out.Bytes(), plaintext) {
			t.Fatalf("size %d: round trip mismatch", size)
		}
	}
}

func TestStreamNonFinalChunksHaveFixedSize(t *testing.T) {
	t.Parallel()

	key := testKey(t)
	ciphertext := encryptToBytes(t, bytes.Repeat([]byte{1}, 2*ChunkSize+5), key, nil)
	lastChunk := chunkPrefixLen + 5 + gcmTagLen
	if want := 2*FullEncryptedChunkSize + lastChunk; len(ciphertext) != want {
		t.Fatalf("ciphertext length = %d, want %d", len(ciphertext), want)
	}
}

func TestDecryptStreamRejectsWrongKey(t *testing.T) {
	t.Parallel()

	ciphertext := encryptToBytes(t, []byte("secret"), testKey(t), nil)
	err := DecryptStream(io.Discard, bytes.NewReader(ciphertext), testKey(t), nil)
	if !errors.Is(err, ErrCorrupted) {
		t.Fatalf("expected ErrCorrupted, got %v", err)
	}
}

func TestDecryptStreamRejectsDifferentAADPrefix(t *testing.T) {
	t.Parallel()

	key := testKey(t)
	ciphertext := encryptToBytes(t, []byte("secret"), key, []byte("section-1"))
	err := DecryptStream(io.Discard, bytes.NewReader(ciphertext), key, []byte("section-2"))
	if !errors.Is(err, ErrCorrupted) {
		t.Fatalf("expected ErrCorrupted for swapped section, got %v", err)
	}
}

func TestDecryptStreamRejectsFlippedByte(t *testing.T) {
	t.Parallel()

	key := testKey(t)
	ciphertext := encryptToBytes(t, []byte("secret payload"), key, nil)
	ciphertext[len(ciphertext)-1] ^= 0x01
	if err := DecryptStream(io.Discard, bytes.NewReader(ciphertext), key, nil); !errors.Is(err, ErrCorrupted) {
		t.Fatalf("expected ErrCorrupted, got %v", err)
	}
}

func TestDecryptStreamRejectsTruncatedChunk(t *testing.T) {
	t.Parallel()

	key := testKey(t)
	ciphertext := encryptToBytes(t, []byte("secret payload"), key, nil)
	err := DecryptStream(io.Discard, bytes.NewReader(ciphertext[:len(ciphertext)-3]), key, nil)
	if err == nil || !strings.Contains(err.Error(), "Failed to read chunk data") {
		t.Fatalf("expected truncated-chunk error, got %v", err)
	}
}

func TestDecryptStreamRejectsMissingFinalChunk(t *testing.T) {
	t.Parallel()

	key := testKey(t)
	ciphertext := encryptToBytes(t, bytes.Repeat([]byte{2}, ChunkSize+10), key, nil)
	err := DecryptStream(io.Discard, bytes.NewReader(ciphertext[:FullEncryptedChunkSize]), key, nil)
	if err == nil || !strings.Contains(err.Error(), "Missing final encrypted chunk marker") {
		t.Fatalf("expected missing-final-chunk error, got %v", err)
	}
}

func TestDecryptStreamAuthenticatesFinalFlag(t *testing.T) {
	t.Parallel()

	key := testKey(t)
	ciphertext := encryptToBytes(t, []byte("tail"), key, nil)
	ciphertext[0] = 0 // clear the final flag
	if err := DecryptStream(io.Discard, bytes.NewReader(ciphertext), key, nil); !errors.Is(err, ErrCorrupted) {
		t.Fatalf("expected ErrCorrupted after clearing final flag, got %v", err)
	}
}

func TestDecryptStreamRejectsTrailingData(t *testing.T) {
	t.Parallel()

	key := testKey(t)
	ciphertext := append(encryptToBytes(t, []byte("data"), key, nil), 0x00)
	err := DecryptStream(io.Discard, bytes.NewReader(ciphertext), key, nil)
	if err == nil || !strings.Contains(err.Error(), "Unexpected data after final encrypted chunk") {
		t.Fatalf("expected trailing-data error, got %v", err)
	}
}

func TestDecryptStreamRejectsInvalidFlags(t *testing.T) {
	t.Parallel()

	key := testKey(t)
	ciphertext := encryptToBytes(t, []byte("data"), key, nil)
	ciphertext[0] = 7
	err := DecryptStream(io.Discard, bytes.NewReader(ciphertext), key, nil)
	if err == nil || !strings.Contains(err.Error(), "Invalid encrypted chunk flags") {
		t.Fatalf("expected invalid-flags error, got %v", err)
	}
}

func TestDecryptStreamRejectsOversizedChunkLength(t *testing.T) {
	t.Parallel()

	key := testKey(t)
	ciphertext := encryptToBytes(t, []byte("data"), key, nil)
	binary.BigEndian.PutUint32(ciphertext[1:5], maxEncryptedChunkSize+1)
	err := DecryptStream(io.Discard, bytes.NewReader(ciphertext), key, nil)
	if err == nil || !strings.Contains(err.Error(), "Invalid encrypted chunk length") {
		t.Fatalf("expected invalid-length error, got %v", err)
	}
}

func TestDecryptStreamReturnsWriteError(t *testing.T) {
	t.Parallel()

	key := testKey(t)
	ciphertext := encryptToBytes(t, []byte("data"), key, nil)
	writeErr := errors.New("disk full")
	err := DecryptStream(&failWriter{err: writeErr}, bytes.NewReader(ciphertext), key, nil)
	if !errors.Is(err, writeErr) {
		t.Fatalf("expected write error, got %v", err)
	}
}

func TestEncryptStreamReturnsReadError(t *testing.T) {
	t.Parallel()

	readErr := errors.New("read failed")
	err := EncryptStream(io.Discard, &failReader{err: readErr}, testKey(t), nil)
	if !errors.Is(err, readErr) {
		t.Fatalf("expected read error, got %v", err)
	}
}

func TestEncryptStreamReturnsWriteError(t *testing.T) {
	t.Parallel()

	writeErr := errors.New("write failed")
	err := EncryptStream(&failWriter{err: writeErr}, bytes.NewReader([]byte("x")), testKey(t), nil)
	if !errors.Is(err, writeErr) {
		t.Fatalf("expected write error, got %v", err)
	}
}

func TestStreamRejectsWrongKeyLength(t *testing.T) {
	t.Parallel()

	if err := EncryptStream(io.Discard, bytes.NewReader(nil), []byte("short"), nil); err == nil {
		t.Fatal("expected error for short key")
	}
	if err := DecryptStream(io.Discard, bytes.NewReader(nil), []byte("short"), nil); err == nil {
		t.Fatal("expected error for short key")
	}
}

func TestSealOpenKeyRoundTrip(t *testing.T) {
	t.Parallel()

	kek := testKey(t)
	nonce, _ := RandomBytes(NonceLen)
	master := testKey(t)
	sealed, err := SealKey(kek, nonce, master, []byte("aad"))
	if err != nil {
		t.Fatalf("SealKey: %v", err)
	}
	opened, err := OpenKey(kek, nonce, sealed, []byte("aad"))
	if err != nil {
		t.Fatalf("OpenKey: %v", err)
	}
	if !bytes.Equal(opened, master) {
		t.Fatal("opened key differs from sealed key")
	}
}

func TestOpenKeyRejectsWrongKEKAndAAD(t *testing.T) {
	t.Parallel()

	kek := testKey(t)
	nonce, _ := RandomBytes(NonceLen)
	sealed, err := SealKey(kek, nonce, testKey(t), []byte("aad"))
	if err != nil {
		t.Fatalf("SealKey: %v", err)
	}
	if _, err := OpenKey(testKey(t), nonce, sealed, []byte("aad")); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("wrong KEK: expected ErrWrongPassword, got %v", err)
	}
	if _, err := OpenKey(kek, nonce, sealed, []byte("other")); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("wrong AAD: expected ErrWrongPassword, got %v", err)
	}
}

func TestDeriveKEKIsDeterministicAndSaltDependent(t *testing.T) {
	t.Parallel()

	salt1 := bytes.Repeat([]byte{1}, SaltLen)
	salt2 := bytes.Repeat([]byte{2}, SaltLen)
	a, err := DeriveKEK([]byte("pw"), salt1, testParams)
	if err != nil {
		t.Fatalf("DeriveKEK: %v", err)
	}
	b, _ := DeriveKEK([]byte("pw"), salt1, testParams)
	c, _ := DeriveKEK([]byte("pw"), salt2, testParams)
	if !bytes.Equal(a, b) {
		t.Fatal("DeriveKEK is not deterministic")
	}
	if bytes.Equal(a, c) {
		t.Fatal("DeriveKEK ignores the salt")
	}
}

func TestDeriveKEKRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	salt := bytes.Repeat([]byte{1}, SaltLen)
	cases := []struct {
		name   string
		salt   []byte
		params Argon2Params
		want   string
	}{
		{"short salt", salt[:10], testParams, "salt length"},
		{"time too low", salt, Argon2Params{Time: 1, MemoryKB: MinArgonMemoryKB, Threads: 1}, "Argon2 time"},
		{"memory too high", salt, Argon2Params{Time: 2, MemoryKB: MaxArgonMemoryKB + 1, Threads: 1}, "Argon2 memory"},
		{"zero threads", salt, Argon2Params{Time: 2, MemoryKB: MinArgonMemoryKB, Threads: 0}, "Argon2 threads"},
	}
	for _, tc := range cases {
		if _, err := DeriveKEK([]byte("pw"), tc.salt, tc.params); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: expected error containing %q, got %v", tc.name, tc.want, err)
		}
	}
}

func TestValidateArgon2ParamsRejectsThreadsBeforeTruncation(t *testing.T) {
	t.Parallel()

	// 256 would truncate to 0 as uint8; it must be rejected as a raw value.
	if err := ValidateArgon2Params(2, MinArgonMemoryKB, 256, "in test", "none."); err == nil {
		t.Fatal("expected threads=256 to be rejected")
	}
}

func TestDeriveSubkeySeparatesInfoAndSalt(t *testing.T) {
	t.Parallel()

	master := testKey(t)
	a, err := DeriveSubkey(master, []byte("salt"), "data")
	if err != nil {
		t.Fatalf("DeriveSubkey: %v", err)
	}
	b, _ := DeriveSubkey(master, []byte("salt"), "manifest")
	c, _ := DeriveSubkey(master, []byte("other"), "data")
	if bytes.Equal(a, b) || bytes.Equal(a, c) {
		t.Fatal("subkeys must differ by info and salt")
	}
	if _, err := DeriveSubkey([]byte("short"), nil, "data"); err == nil {
		t.Fatal("expected error for short master key")
	}
}

func TestChunkNonceDeterministic(t *testing.T) {
	t.Parallel()

	p := newChunkParams(nil)
	seven := bytes.Clone(p.nonce(7))
	eight := bytes.Clone(p.nonce(8))
	if !bytes.Equal(seven, p.nonce(7)) {
		t.Fatal("chunk nonce is not deterministic")
	}
	if bytes.Equal(seven, eight) {
		t.Fatal("chunk nonce must differ per index")
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

// TestStreamAllocationsIndependentOfSize checks that no chunk allocates: a
// stream of 16 chunks allocates as much as a stream of 2.
func TestStreamAllocationsIndependentOfSize(t *testing.T) {
	key := bytes.Repeat([]byte{7}, KeyLen)
	encrypted := func(size int64) []byte {
		var out bytes.Buffer
		if err := EncryptStream(&out, io.LimitReader(zeroReader{}, size), key, []byte("aad")); err != nil {
			t.Fatal(err)
		}
		return out.Bytes()
	}
	small, large := encrypted(2*ChunkSize), encrypted(16*ChunkSize)

	allocs := func(f func() error) float64 {
		return testing.AllocsPerRun(2, func() {
			if err := f(); err != nil {
				t.Fatal(err)
			}
		})
	}
	encrypt := func(size int64) float64 {
		return allocs(func() error {
			return EncryptStream(io.Discard, io.LimitReader(zeroReader{}, size), key, []byte("aad"))
		})
	}
	decrypt := func(data []byte) float64 {
		return allocs(func() error {
			return DecryptStream(io.Discard, bytes.NewReader(data), key, []byte("aad"))
		})
	}
	if s, l := encrypt(2*ChunkSize), encrypt(16*ChunkSize); s != l {
		t.Errorf("EncryptStream allocations: %v for 2 chunks, %v for 16 chunks", s, l)
	}
	if s, l := decrypt(small), decrypt(large); s != l {
		t.Errorf("DecryptStream allocations: %v for 2 chunks, %v for 16 chunks", s, l)
	}
}

// TestEncryptStreamKnownAnswer pins the bytes EncryptStream writes for a fixed
// key and input of two full chunks and a partial one, so that a change to the
// writer cannot change the backup format unnoticed.
func TestEncryptStreamKnownAnswer(t *testing.T) {
	t.Parallel()

	key := bytes.Repeat([]byte{7}, KeyLen)
	src := make([]byte, 2*ChunkSize+1000)
	for i := range src {
		src[i] = byte(i * 31)
	}
	var out bytes.Buffer
	if err := EncryptStream(&out, bytes.NewReader(src), key, []byte("aad")); err != nil {
		t.Fatal(err)
	}
	const want = "cb34cf92b29b910eae57968b5597b0c5514e9f41a068d892ba3c1134d8da6d97"
	if got := fmt.Sprintf("%x", sha256.Sum256(out.Bytes())); got != want {
		t.Fatalf("EncryptStream output changed: sha256 %s, want %s", got, want)
	}
}
