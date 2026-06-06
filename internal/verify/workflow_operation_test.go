package verify

import (
	"RestoreSafe/internal/testutil"
	"RestoreSafe/internal/util"
	"path/filepath"
	"strings"
	"testing"
)

// TestRunVerifyOperationVerifiesWithSuppliedPassword exercises the operation
// half of the verify workflow directly: the password is passed in as a
// parameter, so no interactive prompt / stdin mocking is needed.
func TestRunVerifyOperationVerifiesWithSuppliedPassword(t *testing.T) {
	password := []byte("op-verify-pw")
	fx := testutil.NewBackupFixture(t, password)

	logPath := filepath.Join(t.TempDir(), "operation.log")
	logger, err := util.NewLogger(logPath, "info")
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}

	var runErr error
	output := testutil.CaptureStdout(t, func() {
		runErr = runVerifyOperation([]util.BackupEntry{fx.Entry}, fx.BackupDir, logPath, password, logger, 0)
	})
	logger.Close()
	if runErr != nil {
		t.Fatalf("runVerifyOperation failed: %v", runErr)
	}
	if !strings.Contains(output, "Verification completed successfully.") {
		t.Fatalf("expected completion message in output, got: %q", output)
	}
}

// TestRunVerifyOperationSurfacesWrongPassword confirms the operation returns an
// error (rather than prompting again) when handed an incorrect password.
func TestRunVerifyOperationSurfacesWrongPassword(t *testing.T) {
	fx := testutil.NewBackupFixture(t, []byte("correct-pw"))

	var runErr error
	testutil.CaptureStdout(t, func() {
		runErr = runVerifyOperation([]util.BackupEntry{fx.Entry}, fx.BackupDir, "", []byte("wrong-pw"), nil, 0)
	})
	if runErr == nil {
		t.Fatal("expected error when verifying with wrong password, got nil")
	}
}
