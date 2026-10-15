package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/phsc84/restoresafe/internal/config"
	"github.com/phsc84/restoresafe/internal/format/catalog"
	"github.com/phsc84/restoresafe/internal/format/container"
	"github.com/phsc84/restoresafe/internal/logging"
	"github.com/phsc84/restoresafe/internal/testutil"
	"github.com/phsc84/restoresafe/internal/testutil/filelock"
	"github.com/phsc84/restoresafe/internal/workflow/job"
	"github.com/phsc84/restoresafe/internal/workflow/plan"
)

// fileLogger is a logger of a log file in a new directory, with its path.
func fileLogger(t *testing.T) (*logging.Logger, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "2026-03-14_RUN001.log")
	log, err := logging.NewLogger(path, "info", nil)
	if err != nil {
		t.Fatal(err)
	}
	return log, path
}

func TestBackupFailedRecordsAFailureOrTheCancellation(t *testing.T) {
	t.Parallel()

	log, path := fileLogger(t)
	err := backupFailed(context.Background(), log, time.Now(), errors.New("disk full"))
	log.Close()
	facts, readErr := logging.ReadFacts(path)
	if err == nil || err.Error() != "disk full" || readErr != nil || facts.Backup == nil || facts.Backup.Result != logging.ResultFailed || facts.Backup.Error != "disk full" {
		t.Fatalf("a failure: err %v, fact %+v (%v)", err, facts.Backup, readErr)
	}
	if lines := testutil.LogErrors(t, path); len(lines) != 1 || lines[0] != "disk full" {
		t.Fatalf("a failure must be an ERROR line with its reason, got %q", lines)
	}

	log, path = fileLogger(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = backupFailed(ctx, log, time.Now(), context.Canceled)
	log.Close()
	facts, readErr = logging.ReadFacts(path)
	if !errors.Is(err, context.Canceled) || err.Error() != job.Cancelled("Backup").Error() || readErr != nil || facts.Backup == nil || facts.Backup.Result != logging.ResultCancelled {
		t.Fatalf("a cancellation: err %v, fact %+v (%v)", err, facts.Backup, readErr)
	}
	if lines := testutil.LogErrors(t, path); len(lines) != 0 {
		t.Fatalf("a cancellation is no error, got %q", lines)
	}
}

func TestLoadBaseRefusesAFullBackupOfOtherKeys(t *testing.T) {
	t.Parallel()

	fx := testutil.NewBackupFixture(t, []byte("pw"))
	infos, err := catalog.Inventory(fx.BackupDir)
	if err != nil || len(infos) != 1 {
		t.Fatalf("inventory: %v, %d sets", err, len(infos))
	}
	other := &container.KeySet{ID: "other"}
	if _, err := loadBase(fx.BackupDir, &infos[0], other, fx.Master); err == nil || !strings.Contains(err.Error(), "different keys") {
		t.Fatalf("expected the other keys to be refused, got %v", err)
	}
	base, err := loadBase(fx.BackupDir, &infos[0], fx.KeySet, fx.Master)
	if err != nil || base.Manifest == nil || base.Header.ChainID != string(fx.Entry.ChainID) {
		t.Fatalf("the fixture's own keys: %+v, %v", base, err)
	}
}

func TestKeyPlanForSaysWhichKeysLockTheBackups(t *testing.T) {
	t.Parallel()

	existing := &container.KeySet{AuthMode: config.AuthModePasswordYubiKey, CreatedUTC: "2026-03-14T09:00:00Z"}
	if kp := keyPlanFor(&config.Config{}, plan.Keys{Existing: existing}); kp.New || !kp.Password || kp.YubiKeys != 1 {
		t.Fatalf("existing password and YubiKey keys: %+v", kp)
	}
	yubiOnly := &container.KeySet{AuthMode: config.AuthModeYubiKey}
	if kp := keyPlanFor(&config.Config{}, plan.Keys{Existing: yubiOnly}); kp.Password || kp.YubiKeys != 1 {
		t.Fatalf("existing YubiKey-only keys: %+v", kp)
	}
	cfg := &config.Config{AuthenticationMode: config.AuthModePasswordYubiKey, YubiKeySpare: true, RecoveryCode: true}
	if kp := keyPlanFor(cfg, plan.Keys{NewKeysReason: "recovery_code enabled"}); !kp.New || kp.NewKeysReason != "recovery_code enabled" || !kp.Password || kp.YubiKeys != 2 || !kp.RecoveryCode {
		t.Fatalf("new keys with a spare YubiKey and a recovery code: %+v", kp)
	}
}

func TestDeleteSetFilesSkipsGoneFilesAndStopsAtALockedOne(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	paths := []string{filepath.Join(dir, "a.enc"), filepath.Join(dir, "gone.enc"), filepath.Join(dir, "b.enc")}
	for _, p := range []string{paths[0], paths[2]} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	release := filelock.Hold(t, paths[2])
	removed, err := deleteSetFiles(paths)
	if err == nil || len(removed) != 1 || removed[0] != "a.enc" {
		t.Fatalf("a locked part: removed %v, err %v", removed, err)
	}
	release()
	if removed, err := deleteSetFiles(paths); err != nil || len(removed) != 1 || removed[0] != "b.enc" {
		t.Fatalf("after the lock: removed %v, err %v", removed, err)
	}
}
