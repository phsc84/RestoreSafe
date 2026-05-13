package restore

import (
	"RestoreSafe/internal/catalog"
	"RestoreSafe/internal/operation"
	"RestoreSafe/internal/testutil"
	"RestoreSafe/internal/util"
	"io"
	"path/filepath"
	"testing"
)

// TestBackupRestoreVerifyRoundTrip is the end-to-end integration test.
// It chains all three workflows:
//  1. Backup  – testutil fixture creates encrypted split archives from real source files.
//  2. Restore – decrypts archives and extracts files to a restore directory.
//  3. Verify  – re-runs the decrypt pipeline without extraction to confirm archive integrity.
//
// A failure here means a backup cannot be round-tripped, which is the highest-severity defect.
func TestBackupRestoreVerifyRoundTrip(t *testing.T) {
	password := []byte("e2e-roundtrip-password")
	fx := testutil.NewRestoreFixture(t, password)

	if fx.Parts < 2 {
		t.Fatalf("fixture must produce at least 2 split parts to exercise multi-part logic; got %d", fx.Parts)
	}

	// Step 2: Restore.
	partCount, err := restoreEntry(fx.Entry, fx.TargetDir, fx.RestoreRoot, password, nil)
	if err != nil {
		t.Fatalf("restoreEntry failed: %v", err)
	}
	if partCount != fx.Parts {
		t.Fatalf("expected %d parts processed during restore, got %d", fx.Parts, partCount)
	}

	// Step 3a: Assert restored files byte-for-byte match originals.
	restoredDir := filepath.Join(fx.RestoreRoot, fx.Entry.FolderName)
	testutil.AssertFileContentEqual(t,
		filepath.Join(fx.SrcDir, "nested", "small.txt"),
		filepath.Join(restoredDir, "nested", "small.txt"),
	)
	testutil.AssertFileContentEqual(t,
		filepath.Join(fx.SrcDir, "large.bin"),
		filepath.Join(restoredDir, "large.bin"),
	)

	// Step 3b: Verify the original backup archives are intact (the verify workflow).
	// This calls the same decrypt+TAR-validate pipeline used by verify.Run, confirming
	// the encrypted parts remain readable after the restore has consumed them.
	parts, err := catalog.CollectParts(fx.TargetDir, fx.Entry)
	if err != nil {
		t.Fatalf("CollectParts failed during verify step: %v", err)
	}
	if err := operation.RunDecryptPipeline(
		parts,
		password,
		nil,
		fx.Entry.FolderName,
		"verified",
		"Archive validation",
		util.ValidateTar,
	); err != nil {
		t.Fatalf("verify step (decrypt+TAR validation) failed: %v", err)
	}

	// Step 3c: Wrong password must be rejected cleanly at verify time.
	if err := operation.RunDecryptPipeline(
		parts,
		[]byte("wrong-password"),
		nil,
		fx.Entry.FolderName,
		"verified",
		"Archive validation",
		func(r io.Reader) error { return util.ValidateTar(r) },
	); err == nil {
		t.Fatal("verify with wrong password should have failed but succeeded")
	}
}
