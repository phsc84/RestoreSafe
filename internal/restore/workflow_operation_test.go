package restore

import (
	"RestoreSafe/internal/operation"
	"RestoreSafe/internal/testutil"
	"RestoreSafe/internal/util"
	"path/filepath"
	"strings"
	"testing"
)

// TestRunRestoreOperationRestoresWithSuppliedPassword exercises the operation
// half of the restore workflow directly: the password is passed in as a
// parameter, so no interactive prompt / stdin mocking is needed.
func TestRunRestoreOperationRestoresWithSuppliedPassword(t *testing.T) {
	password := []byte("op-restore-pw")
	fx := testutil.NewRestoreFixture(t, password)

	logPath := filepath.Join(fx.RestoreRoot, "operation.log")
	logger, err := util.NewLogger(logPath, "info")
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}

	var runErr error
	output := testutil.CaptureStdout(t, func() {
		runErr = runRestoreOperation([]util.BackupEntry{fx.Entry}, fx.BackupDir, fx.RestoreRoot, logPath, password, logger, operation.LocalStagingPlan{}, 0)
	})
	logger.Close()
	if runErr != nil {
		t.Fatalf("runRestoreOperation failed: %v", runErr)
	}
	if !strings.Contains(output, "Restore completed successfully.") {
		t.Fatalf("expected completion message in output, got: %q", output)
	}

	restoredDir := filepath.Join(fx.RestoreRoot, fx.Entry.DirectoryName)
	testutil.AssertFileContentEqual(t,
		filepath.Join(fx.SrcDir, "nested", "small.txt"),
		filepath.Join(restoredDir, "nested", "small.txt"),
	)
	testutil.AssertFileContentEqual(t,
		filepath.Join(fx.SrcDir, "large.bin"),
		filepath.Join(restoredDir, "large.bin"),
	)
}

// TestRunRestoreOperationSurfacesWrongPassword confirms the operation returns an
// error (rather than prompting again) when handed an incorrect password.
func TestRunRestoreOperationSurfacesWrongPassword(t *testing.T) {
	fx := testutil.NewRestoreFixture(t, []byte("correct-pw"))

	var runErr error
	testutil.CaptureStdout(t, func() {
		runErr = runRestoreOperation([]util.BackupEntry{fx.Entry}, fx.BackupDir, fx.RestoreRoot, "", []byte("wrong-pw"), nil, operation.LocalStagingPlan{}, 0)
	})
	if runErr == nil {
		t.Fatal("expected error when restoring with wrong password, got nil")
	}
}
