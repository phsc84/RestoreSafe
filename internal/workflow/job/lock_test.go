package job

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/phsc84/restoresafe/internal/fsx"
	"github.com/phsc84/restoresafe/internal/problem"
	"github.com/phsc84/restoresafe/internal/workflow/interact"
	"github.com/phsc84/restoresafe/internal/workflow/interact/interacttest"
)

func TestLockForReadingWarnsWhenTheDirectoryCannotBeLocked(t *testing.T) {
	t.Parallel()

	u := &interacttest.Script{}
	lock, issue, err := LockForReading(context.Background(), u, filepath.Join(t.TempDir(), "read-only medium"))
	if err != nil {
		t.Fatalf("a restore goes on without the lock, got %v", err)
	}
	defer lock.Release()
	if issue == nil || issue.Status != interact.StatusWarn || issue.Code != interact.CodeBackupDirNotLocked {
		t.Fatalf("expected the warning for the plan, got %+v", issue)
	}

	lock, issue, err = LockForReading(context.Background(), u, t.TempDir())
	if err != nil || issue != nil {
		t.Fatalf("a lockable directory gives no warning, got %+v %v", issue, err)
	}
	lock.Release()
}

// busyAcquire fails with busy for the first n calls, then takes the lock;
// it counts the calls.
func busyAcquire(busy *fsx.LockBusy, n int, calls *int) func() (*fsx.BackupLock, error) {
	return func() (*fsx.BackupLock, error) {
		*calls++
		if *calls <= n {
			return nil, &problem.Error{Msg: "locked", Err: busy}
		}
		return &fsx.BackupLock{}, nil
	}
}

// fastLockWait makes Lock try again every millisecond, for up to max.
func fastLockWait(t *testing.T, max time.Duration) {
	retry, wait := lockRetry, lockWaitMax
	lockRetry, lockWaitMax = time.Millisecond, max
	t.Cleanup(func() { lockRetry, lockWaitMax = retry, wait })
}

// A lock that is probably left by this PC after a lost connection is
// waited for, with the text once; any other holder fails at once (core
// spec 14, refactoring plan RF-126).
func TestLockWaitsOnlyForAnOrphan(t *testing.T) {
	fastLockWait(t, time.Minute)

	for _, busy := range []*fsx.LockBusy{{Dir: "B", OtherPC: true}, {Dir: "B", Running: true}} {
		u := &interacttest.Script{}
		calls := 0
		_, err := Lock(context.Background(), u, busyAcquire(busy, 3, &calls))
		if err == nil || calls != 1 || len(u.LockWaits) != 0 {
			t.Fatalf("%+v: expected the error at once without a wait, got %v after %d tries, waits %q", busy, err, calls, u.LockWaits)
		}
	}

	u := &interacttest.Script{}
	calls := 0
	lock, err := Lock(context.Background(), u, busyAcquire(&fsx.LockBusy{Dir: `\\nas\Backups`}, 3, &calls))
	if err != nil || lock == nil || calls != 4 {
		t.Fatalf("expected the lock on the fourth try, got %v, %v after %d", lock, err, calls)
	}
	if len(u.LockWaits) != 1 || !strings.Contains(u.LockWaits[0], `"//nas/Backups" is locked, probably by this PC after a lost connection`) || !strings.Contains(u.LockWaits[0], "every 5 seconds") {
		t.Fatalf("expected the wait once, got %q", u.LockWaits)
	}
}

func TestLockWaitEndsWithCancelAndAfterTheLimit(t *testing.T) {
	fastLockWait(t, time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	u := &interacttest.Script{OnLockWait: cancel}
	calls := 0
	if _, err := Lock(ctx, u, busyAcquire(&fsx.LockBusy{}, 1000, &calls)); !errors.Is(err, context.Canceled) {
		t.Fatalf("Cancel ends the wait, got %v", err)
	}

	fastLockWait(t, 20*time.Millisecond)
	calls = 0
	_, err := Lock(context.Background(), &interacttest.Script{}, busyAcquire(&fsx.LockBusy{}, 1_000_000, &calls))
	var busy *fsx.LockBusy
	if !errors.As(err, &busy) || calls < 2 {
		t.Fatalf("after the limit, the lock's error, got %v after %d tries", err, calls)
	}
}
