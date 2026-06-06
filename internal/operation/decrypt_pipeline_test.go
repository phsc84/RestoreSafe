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
	parts, err := catalog.CollectParts(fx.BackupDir, fx.Entry)
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
	parts, err := catalog.CollectParts(fx.BackupDir, fx.Entry)
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
	parts, err := catalog.CollectParts(fx.BackupDir, fx.Entry)
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

// TestRunDecryptPipelineConsumerStopsEarly guards against a deadlock: a consumer
// that returns nil before draining the plaintext stream must not leave the
// decrypt goroutine blocked forever on pw.Write (which would in turn block the
// <-decErrCh receive). RunDecryptPipeline must close the read end once consume
// returns so the write unblocks.
//
// The call is made synchronously and the contract under test is simply that it
// returns at all. A genuine regression hangs the call, which Go's test-binary
// timeout then reports with a goroutine dump pointing at the blocked write. We
// deliberately do not impose a short wall-clock timer here: RunDecryptPipeline
// performs a full Argon2id key derivation, which can take many seconds under the
// CPU/memory contention of the parallel full-suite run, and a tight timer would
// turn that slowness into a flaky false "deadlock".
func TestRunDecryptPipelineConsumerStopsEarly(t *testing.T) {
	t.Parallel()

	fx := testutil.NewBackupFixture(t, []byte("correct-pass"))
	parts, err := catalog.CollectParts(fx.BackupDir, fx.Entry)
	if err != nil {
		t.Fatalf("failed to collect parts: %v", err)
	}

	// Returns instead of hanging — the deadlock is fixed. The exact result is
	// intentionally not asserted; the contract under test is that the call
	// terminates at all.
	_ = RunDecryptPipeline(
		parts,
		[]byte("correct-pass"),
		nil,
		fx.Entry.DirectoryName,
		"verified",
		"Archive validation",
		func(r io.Reader) error {
			// Read a single byte, then stop without draining the stream.
			_, _ = io.ReadFull(r, make([]byte, 1))
			return nil
		},
		nil,
	)
}
