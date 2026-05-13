package security

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"testing"
)

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

func TestEncryptDecryptCustomArgon2Params(t *testing.T) {
	password := []byte("custom-params-pw")
	plaintext := []byte("testing custom argon2 parameters")

	params := Argon2Params{Time: 1, MemoryKB: 8 * 1024, Threads: 1}

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
	// Build a header with the correct prefix but version byte = 1 (old format).
	buf := bytes.NewBuffer(nil)
	buf.WriteString(magicPrefix)
	buf.WriteByte(1) // old format version
	buf.WriteByte(0) // reserved

	err := Decrypt(io.Discard, bytes.NewReader(buf.Bytes()), []byte("pw"))
	if err == nil {
		t.Fatal("expected version mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "Incompatible backup format") {
		t.Fatalf("expected version mismatch error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "version 1") {
		t.Fatalf("expected file version 1 in error, got: %v", err)
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

func TestDecryptRejectsOversizedChunkLength(t *testing.T) {
	password := []byte("pw")
	var encrypted bytes.Buffer
	if err := Encrypt(&encrypted, bytes.NewReader([]byte("hello")), password, DefaultArgon2Params); err != nil {
		t.Fatalf("Encrypt returned error: %v", err)
	}

	data := encrypted.Bytes()
	// v2 header: 8 (magic) + 4 (saltLen) + 32 (salt) + 4 (chunkSize) + 4 (time) + 4 (memKB) + 4 (threads)
	headerLen := len(magic) + 4 + saltLen + 4 + 4 + 4 + 4
	binary.BigEndian.PutUint32(data[headerLen:headerLen+4], uint32(maxEncryptedChunkSize+1))

	err := Decrypt(io.Discard, bytes.NewReader(data), password)
	if err == nil {
		t.Fatal("expected oversized chunk length error, got nil")
	}
	if !strings.Contains(err.Error(), "Invalid encrypted chunk length") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCombineWithPasswordForRestoreRejectsInvalidChallengeHex(t *testing.T) {
	_, err := CombineWithPasswordForRestore([]byte("pw"), "this-is-not-hex")
	if err == nil {
		t.Fatal("expected decode error, got nil")
	}
	if !strings.Contains(err.Error(), "Failed to decode challenge") {
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
