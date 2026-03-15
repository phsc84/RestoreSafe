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
	if err := Encrypt(&encrypted, bytes.NewReader(plaintext), password); err != nil {
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
	if err := Encrypt(&encrypted, bytes.NewReader(plaintext), password); err != nil {
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
	if err := Encrypt(&encrypted, bytes.NewReader([]byte("hello")), password); err != nil {
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
