package security

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestReadLineReadsLineFromStdin(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	if _, err := fmt.Fprintln(w, "hello world"); err != nil {
		t.Fatalf("failed to write to pipe: %v", err)
	}
	w.Close()

	oldStdin := os.Stdin
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = oldStdin
		r.Close()
	})

	line, err := ReadLine("prompt: ")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if line != "hello world" {
		t.Fatalf("expected %q, got %q", "hello world", line)
	}
}

func TestReadLineReturnsErrorOnEOF(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	w.Close()

	oldStdin := os.Stdin
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = oldStdin
		r.Close()
	})

	_, err = ReadLine("prompt: ")
	if err == nil {
		t.Fatal("expected error for closed stdin, got nil")
	}
	if !strings.Contains(err.Error(), "Failed to read input") {
		t.Fatalf("expected 'Failed to read input' in error, got: %v", err)
	}
}

func TestReadPasswordReturnsErrorForNonTerminalStdin(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	defer w.Close()

	oldStdin := os.Stdin
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = oldStdin
		r.Close()
	})

	_, err = ReadPassword("Password: ")
	if err == nil {
		t.Fatal("expected error for non-terminal stdin, got nil")
	}
	if !strings.Contains(err.Error(), "Failed to read password") {
		t.Fatalf("expected 'Failed to read password' in error, got: %v", err)
	}
}

// answers returns a password reader that returns inputs in order.
func answers(inputs ...string) func(string) ([]byte, error) {
	return func(string) ([]byte, error) {
		if len(inputs) == 0 {
			return nil, errors.New("Failed to read password: EOF")
		}
		in := inputs[0]
		inputs = inputs[1:]
		return []byte(in), nil
	}
}

func TestReadPasswordConfirmed(t *testing.T) {
	t.Parallel()

	if pw, err := ReadPasswordConfirmed(answers("secret", "secret"), "Password: ", "Confirm: "); err != nil || string(pw) != "secret" {
		t.Fatalf("matching passwords: got %q, %v", pw, err)
	}
	if _, err := ReadPasswordConfirmed(answers("secret", "other"), "Password: ", "Confirm: "); !errors.Is(err, ErrPasswordMismatch) {
		t.Fatalf("mismatch: expected ErrPasswordMismatch, got %v", err)
	}
	if _, err := ReadPasswordConfirmed(answers(""), "Password: ", "Confirm: "); !errors.Is(err, ErrPasswordEmpty) {
		t.Fatalf("empty: expected ErrPasswordEmpty, got %v", err)
	}
	for _, in := range [][]string{nil, {"secret"}} {
		_, err := ReadPasswordConfirmed(answers(in...), "Password: ", "Confirm: ")
		if err == nil || !strings.Contains(err.Error(), "Failed to read password") {
			t.Fatalf("read error after %d answer(s): got %v", len(in), err)
		}
	}
}
