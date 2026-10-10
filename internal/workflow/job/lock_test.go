package job

import (
	"path/filepath"
	"testing"

	"github.com/phsc84/restoresafe/internal/workflow/interact"
)

func TestLockForReadingWarnsWhenTheDirectoryCannotBeLocked(t *testing.T) {
	t.Parallel()

	lock, issue, err := LockForReading(filepath.Join(t.TempDir(), "read-only medium"))
	if err != nil {
		t.Fatalf("a restore goes on without the lock, got %v", err)
	}
	defer lock.Release()
	if issue == nil || issue.Status != interact.StatusWarn || issue.Code != interact.CodeBackupDirNotLocked {
		t.Fatalf("expected the warning for the plan, got %+v", issue)
	}

	lock, issue, err = LockForReading(t.TempDir())
	if err != nil || issue != nil {
		t.Fatalf("a lockable directory gives no warning, got %+v %v", issue, err)
	}
	lock.Release()
}
