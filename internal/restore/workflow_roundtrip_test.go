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

// TestRestoreRoundTripPreservesContent is a white-box round-trip test of the
// restore path. The surrounding stages are simulated rather than driven through
// their packages:
//  1. Backup  – simulated: a testutil fixture writes encrypted split archives from real source files (the backup package is not invoked).
//  2. Restore – real: restoreEntry decrypts archives and extracts files to a restore directory.
//  3. Verify  – simulated: operation.RunDecryptPipeline is called directly to confirm archive integrity (the verify package is not invoked).
//
// A failure here means a backup cannot be round-tripped, which is the highest-severity defect.
func TestRestoreRoundTripPreservesContent(t *testing.T) {
	password := []byte("roundtrip-password")
	fx := testutil.NewRestoreFixture(t, password)

	if fx.Parts < 2 {
		t.Fatalf("fixture must produce at least 2 split parts to exercise multi-part logic; got %d", fx.Parts)
	}

	// Step 2: Restore.
	partCount, err := restoreEntry(fx.Entry, fx.BackupDir, fx.RestoreRoot, password, nil)
	if err != nil {
		t.Fatalf("restoreEntry failed: %v", err)
	}
	if partCount != fx.Parts {
		t.Fatalf("expected %d parts processed during restore, got %d", fx.Parts, partCount)
	}

	// Step 3a: Assert restored files byte-for-byte match originals.
	restoredDir := filepath.Join(fx.RestoreRoot, fx.Entry.DirectoryName)
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
	parts, err := catalog.CollectParts(fx.BackupDir, fx.Entry)
	if err != nil {
		t.Fatalf("CollectParts failed during verify step: %v", err)
	}
	if err := operation.RunDecryptPipeline(
		parts,
		password,
		nil,
		fx.Entry.DirectoryName,
		"verified",
		"Archive validation",
		util.ValidateTar,
		nil,
	); err != nil {
		t.Fatalf("verify step (decrypt+TAR validation) failed: %v", err)
	}

	// Step 3c: Wrong password must be rejected cleanly at verify time.
	if err := operation.RunDecryptPipeline(
		parts,
		[]byte("wrong-password"),
		nil,
		fx.Entry.DirectoryName,
		"verified",
		"Archive validation",
		func(r io.Reader) error { return util.ValidateTar(r) },
		nil,
	); err == nil {
		t.Fatal("verify with wrong password should have failed but succeeded")
	}
}
