package job

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phsc84/restoresafe/internal/config"
	"github.com/phsc84/restoresafe/internal/format/container"
	"github.com/phsc84/restoresafe/internal/format/naming"
	"github.com/phsc84/restoresafe/internal/security/recovery"
	"github.com/phsc84/restoresafe/internal/testutil"
	"github.com/phsc84/restoresafe/internal/workflow/interact"
)

func TestOpenLoggerReturnsNonNilLogger(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{LogLevel: "info"}

	log := OpenLogger(cfg, naming.LogFileName(tmpDir, "2026-03-14", naming.BackupID("ABC123")), io.Discard)
	if log == nil {
		t.Fatal("expected non-nil logger for valid target dir")
	}
	if log.IsConsoleOnly() {
		t.Fatal("expected file-backed logger for valid target dir")
	}
	log.Close()
}

func TestOpenLoggerFallsBackToTheOutputWithoutATempDirectory(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-temp")
	t.Setenv("TMP", missing)
	t.Setenv("TEMP", missing)

	var out strings.Builder
	log := OpenLogger(&config.Config{LogLevel: "info"}, naming.LogFileName(t.TempDir(), "2026-03-14", naming.BackupID("ABC123")), &out)
	defer log.Close()
	if !log.IsConsoleOnly() || !strings.Contains(out.String(), "operation continues without a log file") {
		t.Fatalf("expected the fallback with a warning, got console-only=%v output=%q", log.IsConsoleOnly(), out.String())
	}
}

func TestCancelledMatchesContextCanceled(t *testing.T) {
	t.Parallel()

	err := Cancelled("Backup")
	if err.Error() != "Backup cancelled." || !errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Cancelled(\"Backup\") = %v", err)
	}
	if !errors.Is(fmt.Errorf("restore: %w", err), context.Canceled) {
		t.Fatal("a wrapped cancellation must still match context.Canceled")
	}
}

func TestSourceProblemCodeTellsMissingFromInvalid(t *testing.T) {
	t.Parallel()

	if got := SourceProblemCode(fmt.Errorf("stat: %w", fs.ErrNotExist)); got != interact.CodeSourceMissing {
		t.Fatalf("missing folder: got %v", got)
	}
	if got := SourceProblemCode(errors.New("not a directory")); got != interact.CodeSourceInvalid {
		t.Fatalf("invalid folder: got %v", got)
	}
}

func TestUnlockPlanFollowsTheKeySet(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		mode                        config.AuthMode
		recovery                    bool
		password, yubiKey, recovers bool
	}{
		{config.AuthModePassword, false, true, false, false},
		{config.AuthModePasswordYubiKey, true, true, true, true},
		{config.AuthModeYubiKey, false, false, true, false},
	} {
		ks, master, err := container.NewKeySet(tc.mode)
		if err != nil {
			t.Fatal(err)
		}
		if tc.recovery {
			code, err := recovery.Generate()
			if err != nil {
				t.Fatal(err)
			}
			if err := ks.AddSlot(master, container.SlotRecovery, "Recovery code", code.Secret(), testutil.FastArgon2, nil, code.Check()); err != nil {
				t.Fatal(err)
			}
		}
		p := UnlockPlan(ks)
		if p.Methods != tc.mode.Label() || p.Password != tc.password || p.YubiKey != tc.yubiKey || p.RecoveryCode != tc.recovers {
			t.Fatalf("%v: UnlockPlan = %+v", tc.mode, p)
		}
	}
}
