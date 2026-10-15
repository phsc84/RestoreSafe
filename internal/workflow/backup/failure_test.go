package backup

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/phsc84/restoresafe/internal/format/catalog"
	"github.com/phsc84/restoresafe/internal/format/container"
	"github.com/phsc84/restoresafe/internal/logging"
	"github.com/phsc84/restoresafe/internal/testutil"
	"github.com/phsc84/restoresafe/internal/workflow/job"
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
