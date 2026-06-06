package security

import (
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

func TestReadPasswordConfirmedReturnsErrorWhenReadPasswordFails(t *testing.T) {
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

	_, err = ReadPasswordConfirmedWithPrompts("Password: ", "Confirm: ")
	if err == nil {
		t.Fatal("expected error when ReadPassword fails, got nil")
	}
	if !strings.Contains(err.Error(), "Failed to read password") {
		t.Fatalf("expected 'Failed to read password' in error, got: %v", err)
	}
}
