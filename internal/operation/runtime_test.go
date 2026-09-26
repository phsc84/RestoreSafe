package operation

import (
	"io"
	"testing"

	"RestoreSafe/internal/util"
)

func TestOpenLoggerReturnsNonNilLogger(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &util.Config{LogLevel: "info"}

	log := OpenLogger(cfg, tmpDir, "2026-03-14", util.BackupID("ABC123"), io.Discard)
	if log == nil {
		t.Fatal("expected non-nil logger for valid target dir")
	}
	if log.IsConsoleOnly() {
		t.Fatal("expected file-backed logger for valid target dir")
	}
	log.Close()
}

func TestPasswordFailurePrefix(t *testing.T) {
	t.Parallel()
	if got := PasswordFailurePrefix(true, false); got != "Wrong password or invalid YubiKey response." {
		t.Fatalf("unexpected prefix for YubiKey: %q", got)
	}
	if got := PasswordFailurePrefix(false, false); got != "Wrong password." {
		t.Fatalf("unexpected prefix without YubiKey: %q", got)
	}
	if got := PasswordFailurePrefix(true, true); got != "Wrong YubiKey or corrupted file." {
		t.Fatalf("unexpected prefix for YubiKey-only: %q", got)
	}
}

