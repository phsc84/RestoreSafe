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

func TestNewLoggerRecordsVersionOnFreshLogOnce(t *testing.T) {
	prev := util.AppVersion
	t.Cleanup(func() { util.AppVersion = prev })
	util.AppVersion = "9.9.9"

	logPath := filepath.Join(t.TempDir(), "2026-06-05_ABC123.log")

	// Fresh log: the version banner is written as the first line.
	log, err := util.NewLogger(logPath, "info")
	if err != nil {
		t.Fatalf("NewLogger returned error: %v", err)
	}
	log.Info("backup work")
	log.Close()

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "RestoreSafe v9.9.9") {
		t.Fatalf("expected version banner in fresh log, got %q", content)
	}
	firstLine := strings.SplitN(content, "\n", 2)[0]
	if !strings.Contains(firstLine, "RestoreSafe v9.9.9") {
		t.Fatalf("expected version banner on the first line, got first line %q", firstLine)
	}

	// Reopening the same log (as restore/verify do) appends without a second banner.
	log2, err := util.NewLogger(logPath, "info")
	if err != nil {
		t.Fatalf("NewLogger (reopen) returned error: %v", err)
	}
	log2.Info("restore work")
	log2.Close()

	data, err = os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log file after reopen: %v", err)
	}
	if got := strings.Count(string(data), "RestoreSafe v9.9.9"); got != 1 {
		t.Fatalf("expected exactly one version banner after reopen, got %d in %q", got, string(data))
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
