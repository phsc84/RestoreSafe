package util_test

import (
	"RestoreSafe/internal/testutil"
	"RestoreSafe/internal/util"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewConsoleLogger(t *testing.T) {
	output := testutil.CaptureStdout(t, func() {
		log := util.NewConsoleLogger("debug")
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
	var log *util.Logger
	log.Close()
	log.Info("ignored")
	log.Debug("ignored")
	log.Warn("ignored")
	log.WarnLogOnly("ignored")

	if log.IsConsoleOnly() {
		t.Fatal("nil logger should not report console-only")
	}
}

func TestWarnLogOnlyWritesFileWithoutStdout(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "restore.log")
	log, err := util.NewLogger(logPath, "info")
	if err != nil {
		t.Fatalf("NewLogger returned error: %v", err)
	}

	output := testutil.CaptureStdout(t, func() {
		log.WarnLogOnly("hidden warning %d", 1)
	})
	log.Close()

	if output != "" {
		t.Fatalf("expected no stdout for log-only warning, got %q", output)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}
	if !strings.Contains(string(data), "WARN  - hidden warning 1") {
		t.Fatalf("expected warning in log file, got %q", string(data))
	}
}
