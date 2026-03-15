package operation

import (
	"RestoreSafe/internal/util"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestLogStreamProgressWritesDebugLine(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	logPath := filepath.Join(dir, "progress.log")
	logger, err := util.NewLogger(logPath, "debug")
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}

	var inBytes atomic.Int64
	var outBytes atomic.Int64
	var outWriteCalls atomic.Int64
	inBytes.Store(4 * 1024 * 1024)
	outBytes.Store(2 * 1024 * 1024)
	outWriteCalls.Store(2)

	LogStreamProgress(logger, "Docs", "verified", &inBytes, &outBytes, &outWriteCalls, true)
	logger.Close()

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}
	text := string(data)

	if !strings.Contains(text, "I/O progress [Docs] final") {
		t.Fatalf("expected final progress marker in log, got: %s", text)
	}
	if !strings.Contains(text, "verified=2.00 MB") {
		t.Fatalf("expected processed label and value in log, got: %s", text)
	}
	if !strings.Contains(text, "avg write=1024.00 KB") {
		t.Fatalf("expected avg write size in log, got: %s", text)
	}
}

func TestLogProgressUntilDoneHandlesClosedDoneWithNilLogger(t *testing.T) {
	t.Parallel()

	done := make(chan struct{})
	close(done)

	var inBytes atomic.Int64
	var outBytes atomic.Int64
	var outWriteCalls atomic.Int64

	// Should return immediately without panic when logger is nil.
	LogProgressUntilDone(nil, "Docs", "verified", &inBytes, &outBytes, &outWriteCalls, done)
}
