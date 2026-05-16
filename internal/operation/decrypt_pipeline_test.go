package operation

import (
	"RestoreSafe/internal/catalog"
	"RestoreSafe/internal/security"
	"RestoreSafe/internal/testutil"
	"RestoreSafe/internal/util"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestRunDecryptPipelineConsumeErrorWrapsMessage(t *testing.T) {
	t.Parallel()

	fx := testutil.NewBackupFixture(t, []byte("correct-pass"))
	parts, err := catalog.CollectParts(fx.TargetDir, fx.Entry)
	if err != nil {
		t.Fatalf("failed to collect parts: %v", err)
	}

	consumeErr := errors.New("validation failed")
	err = RunDecryptPipeline(
		parts,
		[]byte("correct-pass"),
		nil,
		fx.Entry.DirectoryName,
		"verified",
		"Archive validation",
		func(r io.Reader) error {
			io.Copy(io.Discard, r) //nolint:errcheck
			return consumeErr
		},
		nil,
	)
	if err == nil {
		t.Fatal("expected consume error to propagate, got nil")
	}
	if !strings.Contains(err.Error(), "Archive validation failed") {
		t.Fatalf("expected consume error wrapped with prefix, got: %v", err)
	}
}

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
		fx.Entry.DirectoryName,
		"verified",
		"Archive validation",
		util.ValidateTar,
		nil,
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
		fx.Entry.DirectoryName,
		"verified",
		"Archive validation",
		util.ValidateTar,
		nil,
	)
	if err == nil {
		t.Fatal("expected wrong-password error, got nil")
	}
	if !errors.Is(err, security.ErrWrongPassword) {
		t.Fatalf("expected ErrWrongPassword, got: %v", err)
	}
}
