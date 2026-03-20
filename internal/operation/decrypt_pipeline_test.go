package operation

import (
	"RestoreSafe/internal/catalog"
	"RestoreSafe/internal/security"
	"RestoreSafe/internal/testutil"
	"RestoreSafe/internal/util"
	"errors"
	"testing"
)

func TestRunDecryptPipelineSuccess(t *testing.T) {
	t.Parallel()

	fx := testutil.NewBackupFixture(t, []byte("correct-pass"))
	parts, err := catalog.CollectParts(fx.TargetDir, fx.Entry)
	if err != nil {
		t.Fatalf("failed to collect parts: %v", err)
	}

	err = RunDecryptPipeline(
		parts,
		[]byte("correct-pass"),
		nil,
		fx.Entry.FolderName,
		"verified",
		"Archive validation",
		util.ValidateTar,
	)
	if err != nil {
		t.Fatalf("expected successful decrypt pipeline, got: %v", err)
	}
}

func TestRunDecryptPipelineWrongPassword(t *testing.T) {
	t.Parallel()

	fx := testutil.NewBackupFixture(t, []byte("correct-pass"))
	parts, err := catalog.CollectParts(fx.TargetDir, fx.Entry)
	if err != nil {
		t.Fatalf("failed to collect parts: %v", err)
	}

	err = RunDecryptPipeline(
		parts,
		[]byte("wrong-pass"),
		nil,
		fx.Entry.FolderName,
		"verified",
		"Archive validation",
		util.ValidateTar,
	)
	if err == nil {
		t.Fatal("expected wrong-password error, got nil")
	}
	if !errors.Is(err, security.ErrWrongPassword) {
		t.Fatalf("expected ErrWrongPassword, got: %v", err)
	}
}
