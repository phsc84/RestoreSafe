package job

import (
	"RestoreSafe/internal/config"
	"RestoreSafe/internal/format/naming"
	"io"
	"testing"
)

func TestOpenLoggerReturnsNonNilLogger(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{LogLevel: "info"}

	log := OpenLogger(cfg, tmpDir, "2026-03-14", naming.BackupID("ABC123"), io.Discard)
	if log == nil {
		t.Fatal("expected non-nil logger for valid target dir")
	}
	if log.IsConsoleOnly() {
		t.Fatal("expected file-backed logger for valid target dir")
	}
	log.Close()
}
