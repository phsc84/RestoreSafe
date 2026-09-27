package operation

import (
	"RestoreSafe/internal/logging"
	"RestoreSafe/internal/workflow/interact"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestLogStreamProgressWritesDebugLine(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	logPath := filepath.Join(dir, "progress.log")
	logger, err := logging.NewLogger(logPath, "debug", nil)
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

func TestLogProgressUntilDoneLogsWhenDoneClosedImmediately(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	logPath := filepath.Join(dir, "progress2.log")
	logger, err := logging.NewLogger(logPath, "debug", nil)
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}

	done := make(chan struct{})
	close(done)

	var inBytes atomic.Int64
	var outBytes atomic.Int64
	var outWriteCalls atomic.Int64
	inBytes.Store(1024 * 1024)

	LogProgressUntilDone(logger, "Docs", "encrypted", &inBytes, &outBytes, &outWriteCalls, done)
	logger.Close()

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}
	if !strings.Contains(string(data), "I/O progress [Docs] final") {
		t.Fatalf("expected final progress line in log, got: %s", string(data))
	}
}

func TestStartProgressTrackingStopsCleanly(t *testing.T) {
	t.Parallel()

	var inBytes atomic.Int64
	var outBytes atomic.Int64
	var outWriteCalls atomic.Int64

	stop := StartProgressTracking(nil, "Docs", "encrypted", &inBytes, &outBytes, &outWriteCalls)
	stop() // must not deadlock
}

type progressRecorder struct {
	mu      sync.Mutex
	reports []interact.Progress
}

func (r *progressRecorder) Progress(p interact.Progress) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reports = append(r.reports, p)
}

func TestTrackProgressReportsStartAndFinalValue(t *testing.T) {
	t.Parallel()
	var done atomic.Int64
	rec := &progressRecorder{}
	stop := TrackProgress(rec, interact.Progress{Step: "Backing up", Item: "Docs", Total: 100}, &done)
	done.Store(100)
	stop()

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.reports) < 2 {
		t.Fatalf("expected at least a start and a final report, got %+v", rec.reports)
	}
	first, last := rec.reports[0], rec.reports[len(rec.reports)-1]
	if first.Done != 0 || last.Done != 100 || last.Total != 100 || last.Step != "Backing up" || last.Item != "Docs" {
		t.Fatalf("unexpected reports: first %+v, last %+v", first, last)
	}
}

func TestTrackProgressWithoutReporterDoesNothing(t *testing.T) {
	t.Parallel()
	var done atomic.Int64
	TrackProgress(nil, interact.Progress{}, &done)()
}
