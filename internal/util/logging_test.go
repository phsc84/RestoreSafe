package util

import (
	"io"
	"os"
	"strings"
	"testing"
)

func TestNewConsoleLogger(t *testing.T) {
	output := captureStdout(t, func() {
		log := NewConsoleLogger("debug")
		if !log.IsConsoleOnly() {
			t.Fatal("expected console-only logger")
		}
		log.Info("console info %d", 1)
		log.Debug("console debug %d", 2)
		log.Warn("console warn %d", 3)
		log.Close()
	})

	if !strings.Contains(output, "console info 1") {
		t.Fatalf("expected info output, got %q", output)
	}
	if !strings.Contains(output, "console debug 2") {
		t.Fatalf("expected debug output, got %q", output)
	}
	if !strings.Contains(output, "console warn 3") {
		t.Fatalf("expected warn output, got %q", output)
	}
}

func TestNilLoggerMethodsAreSafe(t *testing.T) {
	var log *Logger
	log.Close()
	log.Info("ignored")
	log.Debug("ignored")
	log.Warn("ignored")

	if log.IsConsoleOnly() {
		t.Fatal("nil logger should not report console-only")
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	originalStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create stdout pipe: %v", err)
	}
	os.Stdout = w

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("failed to close stdout writer: %v", err)
	}
	os.Stdout = originalStdout

	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("failed to read captured output: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("failed to close stdout reader: %v", err)
	}

	return string(data)
}
