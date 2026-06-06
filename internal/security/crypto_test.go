package security

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"testing"
)

type failReader struct{ err error }

func (r *failReader) Read([]byte) (int, error) { return 0, r.err }

func TestDecryptRejectsWrongStoredChunkSize(t *testing.T) {
	t.Parallel()

	buf := bytes.NewBuffer(nil)
	buf.WriteString(magic)
	if err := binary.Write(buf, binary.BigEndian, uint32(saltLen)); err != nil {
		t.Fatalf("failed to write salt length: %v", err)
	}
	buf.Write(bytes.Repeat([]byte{1}, saltLen))
	if err := binary.Write(buf, binary.BigEndian, uint32(chunkSize+1)); err != nil {
		t.Fatalf("failed to write wrong chunk size: %v", err)
	}

	err := Decrypt(io.Discard, bytes.NewReader(buf.Bytes()), []byte("pw"))
	if err == nil {
		t.Fatal("expected error for wrong stored chunk size, got nil")
	}
	if !strings.Contains(err.Error(), "Unsupported chunk size") {
		t.Fatalf("expected unsupported-chunk-size error, got: %v", err)
	}
}

// writeArgonHeader writes a valid v1 header with the given Argon2 parameters.
// Threads is taken as a uint32 so out-of-range values (e.g. 256, 0) can be
// written to exercise header validation.
func writeArgonHeader(t *testing.T, time, memoryKB, threads uint32) []byte {
	t.Helper()
	buf := bytes.NewBuffer(nil)
	buf.WriteString(magic)
	if err := binary.Write(buf, binary.BigEndian, uint32(saltLen)); err != nil {
		t.Fatalf("failed to write salt length: %v", err)
	}
	buf.Write(bytes.Repeat([]byte{1}, saltLen))
	for _, v := range []uint32{uint32(chunkSize), time, memoryKB, threads} {
		if err := binary.Write(buf, binary.BigEndian, v); err != nil {
			t.Fatalf("failed to write header value %d: %v", v, err)
		}
	}
	return buf.Bytes()
}

// TestDecryptRejectsOutOfBoundsArgon2Params ensures a corrupted or hostile
// header yields a clean "invalid" error rather than a panic (parallelism 0 via
// uint8 truncation) or an OOM (multi-terabyte memory request) inside argon2.
func TestDecryptRejectsOutOfBoundsArgon2Params(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name           string
		time, mem, thr uint32
		want           string
	}{
		{"time too low", MinArgonTime - 1, MinArgonMemoryKB, MinArgonThreads, "Invalid Argon2 time"},
		{"time too high", MaxArgonTime + 1, MinArgonMemoryKB, MinArgonThreads, "Invalid Argon2 time"},
		{"memory too low", MinArgonTime, MinArgonMemoryKB - 1, MinArgonThreads, "Invalid Argon2 memory"},
		{"memory too high", MinArgonTime, MaxArgonMemoryKB + 1, MinArgonThreads, "Invalid Argon2 memory"},
		{"memory max uint32 (OOM guard)", MinArgonTime, 0xFFFFFFFF, MinArgonThreads, "Invalid Argon2 memory"},
		{"threads zero (panic guard)", MinArgonTime, MinArgonMemoryKB, 0, "Invalid Argon2 threads"},
		{"threads 256 truncates to 0 (panic guard)", MinArgonTime, MinArgonMemoryKB, 256, "Invalid Argon2 threads"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			header := writeArgonHeader(t, tc.time, tc.mem, tc.thr)
			err := Decrypt(io.Discard, bytes.NewReader(header), []byte("pw"))
			if err == nil {
				t.Fatal("expected error for out-of-bounds Argon2 params, got nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q error, got: %v", tc.want, err)
			}
		})
	}
}

func TestDecryptReturnsWriteError(t *testing.T) {
	t.Parallel()

	password := []byte("pw")
	var encrypted bytes.Buffer
	if err := Encrypt(&encrypted, bytes.NewReader([]byte("hello world")), password, Argon2Params{Time: MinArgonTime, MemoryKB: MinArgonMemoryKB, Threads: MinArgonThreads}); err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	writeErr := errors.New("disk full")
	err := Decrypt(&failWriter{err: writeErr}, bytes.NewReader(encrypted.Bytes()), password)
	if err == nil {
		t.Fatal("expected write error, got nil")
	}
	if !strings.Contains(err.Error(), "Failed to write decrypted data") {
		t.Fatalf("expected write-error message, got: %v", err)
	}
}

type failWriter struct{ err error }

func (fw *failWriter) Write([]byte) (int, error) { return 0, fw.err }

func TestEncryptDecryptRoundTrip(t *testing.T) {
	password := []byte("super-secret")
	plaintext := []byte("RestoreSafe round-trip payload")

	var encrypted bytes.Buffer
	if err := Encrypt(&encrypted, bytes.NewReader(plaintext), password, DefaultArgon2Params); err != nil {
		t.Fatalf("Encrypt returned error: %v", err)
	}

	var decrypted bytes.Buffer
	if err := Decrypt(&decrypted, bytes.NewReader(encrypted.Bytes()), password); err != nil {
		t.Fatalf("Decrypt returned error: %v", err)
	}

	if !bytes.Equal(decrypted.Bytes(), plaintext) {
		t.Fatalf("decrypted payload mismatch: expected %q, got %q", plaintext, decrypted.Bytes())
	}
}

func TestEncryptDecryptEmptyPayload(t *testing.T) {
	password := []byte("super-secret")

	var encrypted bytes.Buffer
	if err := Encrypt(&encrypted, bytes.NewReader(nil), password, Argon2Params{Time: MinArgonTime, MemoryKB: MinArgonMemoryKB, Threads: MinArgonThreads}); err != nil {
		t.Fatalf("Encrypt returned error: %v", err)
	}

	var decrypted bytes.Buffer
	if err := Decrypt(&decrypted, bytes.NewReader(encrypted.Bytes()), password); err != nil {
		t.Fatalf("Decrypt returned error: %v", err)
	}

	if len(decrypted.Bytes()) != 0 {
		t.Fatalf("expected empty payload, got %q", decrypted.Bytes())
	}
}

func TestEncryptDecryptCustomArgon2Params(t *testing.T) {
	password := []byte("custom-params-pw")
	plaintext := []byte("testing custom argon2 parameters")

	params := Argon2Params{Time: MinArgonTime, MemoryKB: MinArgonMemoryKB, Threads: MinArgonThreads}

	var encrypted bytes.Buffer
	if err := Encrypt(&encrypted, bytes.NewReader(plaintext), password, params); err != nil {
		t.Fatalf("Encrypt returned error: %v", err)
	}

	var decrypted bytes.Buffer
	if err := Decrypt(&decrypted, bytes.NewReader(encrypted.Bytes()), password); err != nil {
		t.Fatalf("Decrypt returned error: %v", err)
	}

	if !bytes.Equal(decrypted.Bytes(), plaintext) {
		t.Fatalf("decrypted payload mismatch: expected %q, got %q", plaintext, decrypted.Bytes())
	}
}

func TestDecryptWrongPassword(t *testing.T) {
	password := []byte("correct-password")
	plaintext := []byte("payload")

	var encrypted bytes.Buffer
	if err := Encrypt(&encrypted, bytes.NewReader(plaintext), password, DefaultArgon2Params); err != nil {
		t.Fatalf("Encrypt returned error: %v", err)
	}

	err := Decrypt(io.Discard, bytes.NewReader(encrypted.Bytes()), []byte("wrong-password"))
	if !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("expected ErrWrongPassword, got: %v", err)
	}
}

func TestDecryptRejectsInvalidMagic(t *testing.T) {
	buf := bytes.NewBuffer(nil)
	buf.WriteString("NOTRSBK!")
	if err := binary.Write(buf, binary.BigEndian, uint32(saltLen)); err != nil {
		t.Fatalf("failed to write salt length: %v", err)
	}
	buf.Write(bytes.Repeat([]byte{1}, saltLen))
	if err := binary.Write(buf, binary.BigEndian, uint32(chunkSize)); err != nil {
		t.Fatalf("failed to write chunk size: %v", err)
	}

	err := Decrypt(io.Discard, bytes.NewReader(buf.Bytes()), []byte("pw"))
	if err == nil {
		t.Fatal("expected invalid format error, got nil")
	}
	if !strings.Contains(err.Error(), "Invalid file format") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDecryptRejectsWrongFormatVersion(t *testing.T) {
	// Build a header with the correct prefix but an unsupported version byte.
	buf := bytes.NewBuffer(nil)
	buf.WriteString(magicPrefix)
	buf.WriteByte(2) // unsupported format version
	buf.WriteByte(0) // reserved

	err := Decrypt(io.Discard, bytes.NewReader(buf.Bytes()), []byte("pw"))
	if err == nil {
		t.Fatal("expected version mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "Incompatible backup format") {
		t.Fatalf("expected version mismatch error, got: %v", err)
	}
}

func TestDecryptRejectsInvalidSaltLength(t *testing.T) {
	buf := bytes.NewBuffer(nil)
	buf.WriteString(magic)
	if err := binary.Write(buf, binary.BigEndian, uint32(1)); err != nil {
		t.Fatalf("failed to write salt length: %v", err)
	}
	buf.WriteByte(0x42)
	if err := binary.Write(buf, binary.BigEndian, uint32(chunkSize)); err != nil {
		t.Fatalf("failed to write chunk size: %v", err)
	}

	err := Decrypt(io.Discard, bytes.NewReader(buf.Bytes()), []byte("pw"))
	if err == nil {
		t.Fatal("expected invalid salt length error, got nil")
	}
	if !strings.Contains(err.Error(), "Invalid salt length") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDecryptRejectsTruncatedChunk(t *testing.T) {
	password := []byte("pw")
	var encrypted bytes.Buffer
	if err := Encrypt(&encrypted, bytes.NewReader([]byte("hello")), password, DefaultArgon2Params); err != nil {
		t.Fatalf("Encrypt returned error: %v", err)
	}

	truncated := encrypted.Bytes()
	truncated = truncated[:len(truncated)-1]

	err := Decrypt(io.Discard, bytes.NewReader(truncated), password)
	if err == nil {
		t.Fatal("expected error for truncated chunk, got nil")
	}
	if !strings.Contains(err.Error(), "Failed to read chunk data") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDecryptRejectsTruncatedWholeFinalChunk(t *testing.T) {
	password := []byte("pw")
	plaintext := bytes.Repeat([]byte("x"), chunkSize+1)
	var encrypted bytes.Buffer
	if err := Encrypt(&encrypted, bytes.NewReader(plaintext), password, Argon2Params{Time: MinArgonTime, MemoryKB: MinArgonMemoryKB, Threads: MinArgonThreads}); err != nil {
		t.Fatalf("Encrypt returned error: %v", err)
	}

	data := encrypted.Bytes()
	headerLen := len(magic) + 4 + saltLen + 4 + 4 + 4 + 4
	firstChunkLen := int(binary.BigEndian.Uint32(data[headerLen+1 : headerLen+5]))
	truncatedLen := headerLen + 1 + 4 + firstChunkLen

	err := Decrypt(io.Discard, bytes.NewReader(data[:truncatedLen]), password)
	if err == nil {
		t.Fatal("expected error for missing final chunk marker, got nil")
	}
	if !strings.Contains(err.Error(), "Missing final encrypted chunk marker") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDecryptAuthenticatesFinalChunkFlag(t *testing.T) {
	password := []byte("pw")
	var encrypted bytes.Buffer
	if err := Encrypt(&encrypted, bytes.NewReader([]byte("hello")), password, Argon2Params{Time: MinArgonTime, MemoryKB: MinArgonMemoryKB, Threads: MinArgonThreads}); err != nil {
		t.Fatalf("Encrypt returned error: %v", err)
	}

	data := encrypted.Bytes()
	headerLen := len(magic) + 4 + saltLen + 4 + 4 + 4 + 4
	data[headerLen] = 0

	err := Decrypt(io.Discard, bytes.NewReader(data), password)
	if !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("expected ErrWrongPassword for tampered final flag, got: %v", err)
	}
}

func TestDecryptRejectsOversizedChunkLength(t *testing.T) {
	password := []byte("pw")
	var encrypted bytes.Buffer
	if err := Encrypt(&encrypted, bytes.NewReader([]byte("hello")), password, DefaultArgon2Params); err != nil {
		t.Fatalf("Encrypt returned error: %v", err)
	}

	data := encrypted.Bytes()
	// v1 header: 8 (magic) + 4 (saltLen) + 32 (salt) + 4 (chunkSize) + 4 (time) + 4 (memKB) + 4 (threads)
	headerLen := len(magic) + 4 + saltLen + 4 + 4 + 4 + 4
	binary.BigEndian.PutUint32(data[headerLen+1:headerLen+5], uint32(maxEncryptedChunkSize+1))

	err := Decrypt(io.Discard, bytes.NewReader(data), password)
	if err == nil {
		t.Fatal("expected oversized chunk length error, got nil")
	}
	if !strings.Contains(err.Error(), "Invalid encrypted chunk length") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestChunkNonceDeterministic(t *testing.T) {
	nonceA := chunkNonce(1)
	nonceB := chunkNonce(1)
	nonceC := chunkNonce(2)

	if !bytes.Equal(nonceA, nonceB) {
		t.Fatal("expected same nonce for same index")
	}
	if bytes.Equal(nonceA, nonceC) {
		t.Fatal("expected different nonce for different indexes")
	}
	if len(nonceA) != nonceLen {
		t.Fatalf("expected nonce length %d, got %d", nonceLen, len(nonceA))
	}
}

func TestEncryptFailsWhenWriterFailsOnHeader(t *testing.T) {
	t.Parallel()
	params := Argon2Params{Time: MinArgonTime, MemoryKB: MinArgonMemoryKB, Threads: MinArgonThreads}
	err := Encrypt(&failWriter{err: errors.New("disk full")}, bytes.NewReader([]byte("hello")), []byte("pw"), params)
	if err == nil {
		t.Fatal("expected error for failing writer, got nil")
	}
	if !strings.Contains(err.Error(), "Failed to write magic") {
		t.Fatalf("expected magic-write failure, got: %v", err)
	}
}

// TestEncryptRejectsOutOfBoundsArgon2Params ensures invalid parameters from an
// internal caller or test are rejected before reaching argon2.IDKey, so they
// cannot trigger an OOM (multi-terabyte memory request) or a panic.
func TestEncryptRejectsOutOfBoundsArgon2Params(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		params Argon2Params
		want   string
	}{
		{"time too low", Argon2Params{Time: MinArgonTime - 1, MemoryKB: MinArgonMemoryKB, Threads: MinArgonThreads}, "Invalid Argon2 time"},
		{"time too high", Argon2Params{Time: MaxArgonTime + 1, MemoryKB: MinArgonMemoryKB, Threads: MinArgonThreads}, "Invalid Argon2 time"},
		{"memory too low", Argon2Params{Time: MinArgonTime, MemoryKB: MinArgonMemoryKB - 1, Threads: MinArgonThreads}, "Invalid Argon2 memory"},
		{"memory too high", Argon2Params{Time: MinArgonTime, MemoryKB: MaxArgonMemoryKB + 1, Threads: MinArgonThreads}, "Invalid Argon2 memory"},
		{"threads zero", Argon2Params{Time: MinArgonTime, MemoryKB: MinArgonMemoryKB, Threads: 0}, "Invalid Argon2 threads"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := Encrypt(io.Discard, bytes.NewReader([]byte("payload")), []byte("pw"), tc.params)
			if err == nil {
				t.Fatal("expected error for out-of-bounds Argon2 params, got nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q error, got: %v", tc.want, err)
			}
		})
	}
}

func TestEncryptFailsWhenReaderFails(t *testing.T) {
	t.Parallel()
	params := Argon2Params{Time: MinArgonTime, MemoryKB: MinArgonMemoryKB, Threads: MinArgonThreads}
	err := Encrypt(&bytes.Buffer{}, &failReader{err: errors.New("read error")}, []byte("pw"), params)
	if err == nil {
		t.Fatal("expected error for failing reader, got nil")
	}
	if !strings.Contains(err.Error(), "Failed to read plaintext") {
		t.Fatalf("expected read-plaintext failure, got: %v", err)
	}
}
