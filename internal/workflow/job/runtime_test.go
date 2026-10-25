package job

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phsc84/restoresafe/internal/config"
	"github.com/phsc84/restoresafe/internal/format/container"
	"github.com/phsc84/restoresafe/internal/format/naming"
	"github.com/phsc84/restoresafe/internal/security/recovery"
	"github.com/phsc84/restoresafe/internal/testutil"
	"github.com/phsc84/restoresafe/internal/workflow/interact"
	"github.com/phsc84/restoresafe/internal/workflow/interact/interacttest"
)

func TestOpenLoggerReturnsNonNilLogger(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{LogLevel: "info"}

	u := &interacttest.Script{}
	log := OpenLogger(cfg, naming.LogFileName(tmpDir, "2026-03-14", naming.BackupID("ABC123")), u)
	if log == nil {
		t.Fatal("expected non-nil logger for valid target dir")
	}
	if !log.HasFile() {
		t.Fatal("expected file-backed logger for valid target dir")
	}
	if CloseLog(u, log) || len(u.LogFailures) != 0 {
		t.Fatalf("a log that was written reports a problem: %+v", u.LogFailures)
	}
}

func TestOpenLoggerFallsBackToTheOutputWithoutATempDirectory(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-temp")
	t.Setenv("TMP", missing)
	t.Setenv("TEMP", missing)

	u := &interacttest.Script{}
	log := OpenLogger(&config.Config{LogLevel: "info"}, naming.LogFileName(t.TempDir(), "2026-03-14", naming.BackupID("ABC123")), u)
	defer log.Close()
	if log.HasFile() || len(u.LogFailures) != 1 || u.LogFailures[0].Kind != interact.LogNotOpened || u.LogFailures[0].Err == nil {
		t.Fatalf("expected the fallback, reported as not opened; got a log file=%v, failures=%+v", log.HasFile(), u.LogFailures)
	}
}

// TestCloseLogReportsTheCopyInTemp makes the append fail: the log file's
// path is a directory. The run's lines stay in TEMP, and CloseLog names that
// copy once (RF-109).
func TestCloseLogReportsTheCopyInTemp(t *testing.T) {
	logPath := naming.LogFileName(t.TempDir(), "2026-03-14", naming.BackupID("ABC123"))
	if err := os.Mkdir(logPath, 0o750); err != nil {
		t.Fatal(err)
	}
	u := &interacttest.Script{}
	log := OpenLogger(&config.Config{LogLevel: "info"}, logPath, u)
	log.Info("a line of this run")
	if !CloseLog(u, log) {
		t.Fatal("CloseLog reported no problem")
	}
	if CloseLog(u, log) {
		t.Fatal("a second CloseLog reported again")
	}
	if len(u.LogFailures) != 1 {
		t.Fatalf("failures = %+v, want one", u.LogFailures)
	}
	f := u.LogFailures[0]
	if f.Kind != interact.LogKept || f.Err == nil || f.Kept == "" {
		t.Fatalf("failure = %+v, want LogKept with the copy", f)
	}
	t.Cleanup(func() { os.Remove(f.Kept) })
	data, err := os.ReadFile(f.Kept)
	if err != nil || !strings.Contains(string(data), "a line of this run") {
		t.Fatalf("the copy in TEMP lacks the run's line: %q, %v", data, err)
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
