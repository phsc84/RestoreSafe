package logging_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phsc84/restoresafe/internal/buildinfo"
	"github.com/phsc84/restoresafe/internal/logging"
	"github.com/phsc84/restoresafe/internal/testutil"
)

func TestNewConsoleLogger(t *testing.T) {
	var out testutil.Output
	log := logging.NewConsoleLogger("debug", &out)
	if !log.IsConsoleOnly() {
		t.Fatal("expected console-only logger")
	}
	log.Info("console info %d", 1)
	log.Debug("console debug %d", 2)
	log.Warn("console warn %d", 3)
	log.Close()
	output := out.String()

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
	var log *logging.Logger
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
	prev := buildinfo.Version
	t.Cleanup(func() { buildinfo.Version = prev })
	buildinfo.Version = "9.9.9"

	logPath := filepath.Join(t.TempDir(), "2026-06-05_ABC123.log")

	// Fresh log: the version banner is written as the first line.
	log, err := logging.NewLogger(logPath, "info", nil)
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
	log2, err := logging.NewLogger(logPath, "info", nil)
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

// A restore and a verification of one backup run may log to the same file at
// the same time; neither may lose the other's lines.
func TestLoggersOfTheSameFileAtOnceKeepEachOthersLines(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "2026-06-05_TWO001.log")
	if err := os.WriteFile(logPath, []byte("[2026-06-05 10:00:00] INFO  - backup work\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	restore, err := logging.NewLogger(logPath, "info", nil)
	if err != nil {
		t.Fatal(err)
	}
	restore.Info("restore work")
	verify, err := logging.NewLogger(logPath, "info", nil) // starts while the restore runs
	if err != nil {
		t.Fatal(err)
	}
	verify.Info("verify work")
	verify.Close()
	restore.Close()

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"backup work", "restore work", "verify work"} {
		if strings.Count(string(data), want) != 1 {
			t.Fatalf("expected %q once in the log, got %q", want, data)
		}
	}
}

func TestCloseKeepsTheTempFileWhenTheLogCannotBeWritten(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "gone", "2026-06-05_GONE01.log")
	var out testutil.Output
	log, err := logging.NewLogger(logPath, "info", &out)
	if err != nil {
		t.Fatal(err)
	}
	log.InfoLogOnly("the only copy")
	log.Close()

	output := out.String()
	_, tempPath, found := strings.Cut(output, "Log file is located in temp directory: ")
	tempPath = strings.TrimSpace(tempPath)
	if !strings.Contains(output, "Error writing log file to backup directory") || !found {
		t.Fatalf("expected the failure and the temp file to be reported, got %q", output)
	}
	t.Cleanup(func() { os.Remove(tempPath) })
	if data, err := os.ReadFile(tempPath); err != nil || !strings.Contains(string(data), "the only copy") {
		t.Fatalf("expected the temp file to keep the lines, got %q (%v)", data, err)
	}
}

func TestWarnLogOnlyWritesFileWithoutConsole(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "restore.log")
	var out testutil.Output
	log, err := logging.NewLogger(logPath, "info", &out)
	if err != nil {
		t.Fatalf("NewLogger returned error: %v", err)
	}

	log.WarnLogOnly("hidden warning %d", 1)
	output := out.String()
	log.Close()

	if output != "" {
		t.Fatalf("expected no console output for log-only warning, got %q", output)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}
	if !strings.Contains(string(data), "WARN  - hidden warning 1") {
		t.Fatalf("expected warning in log file, got %q", string(data))
	}
}
