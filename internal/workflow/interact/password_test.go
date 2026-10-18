package interact

import (
	"bytes"
	"errors"
	"testing"
)

func TestCheckNewPassword(t *testing.T) {
	t.Parallel()

	confirm := []byte("secret")
	if pw, err := CheckNewPassword([]byte("secret"), confirm); err != nil || string(pw) != "secret" {
		t.Fatalf("matching passwords: got %q, %v", pw, err)
	}
	if !bytes.Equal(confirm, make([]byte, len(confirm))) {
		t.Fatal("the confirmation must be zeroed")
	}

	pw := []byte("secret")
	if _, err := CheckNewPassword(pw, []byte("other")); !errors.Is(err, ErrPasswordMismatch) {
		t.Fatalf("mismatch: expected ErrPasswordMismatch, got %v", err)
	}
	if !bytes.Equal(pw, make([]byte, len(pw))) {
		t.Fatal("a refused password must be zeroed")
	}
	if _, err := CheckNewPassword(nil, nil); !errors.Is(err, ErrPasswordEmpty) {
		t.Fatalf("empty: expected ErrPasswordEmpty, got %v", err)
	}
}
