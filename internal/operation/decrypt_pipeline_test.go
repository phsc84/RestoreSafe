package operation

import (
	"RestoreSafe/internal/catalog"
	"RestoreSafe/internal/container"
	"RestoreSafe/internal/security"
	"RestoreSafe/internal/testutil"
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
)

func openFixtureSet(t *testing.T) (*testutil.BackupFixture, *container.Set, *container.SectionKeys) {
	t.Helper()
	fx := testutil.NewBackupFixture(t, []byte("correct-pass"))
	set, err := catalog.OpenSet(fx.BackupDir, fx.Entry)
	if err != nil {
		t.Fatalf("OpenSet: %v", err)
	}
	t.Cleanup(func() { set.Close() })
	keys, err := set.SectionKeys(fx.Master)
	if err != nil {
		t.Fatalf("SectionKeys: %v", err)
	}
	return fx, set, keys
}

func TestRunSectionPipelineSuccess(t *testing.T) {
	t.Parallel()

	fx, set, keys := openFixtureSet(t)
	var n int64
	err := RunSectionPipeline(context.Background(), set, keys, nil, fx.Entry.DirectoryName, "verified", "Archive validation", nil, func(r io.Reader) error {
		var err error
		n, err = io.Copy(io.Discard, r)
		return err
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if n == 0 {
		t.Fatal("expected plaintext bytes")
	}
}

func TestRunSectionPipelineConsumeErrorWrapsMessage(t *testing.T) {
	t.Parallel()

	fx, set, keys := openFixtureSet(t)
	consumeErr := errors.New("validation failed")
	err := RunSectionPipeline(context.Background(), set, keys, nil, fx.Entry.DirectoryName, "verified", "Archive validation", nil, func(r io.Reader) error {
		io.Copy(io.Discard, r) //nolint:errcheck
		return consumeErr
	})
	if err == nil || !strings.Contains(err.Error(), "Archive validation failed") || !errors.Is(err, consumeErr) {
		t.Fatalf("expected wrapped consume error, got %v", err)
	}
}

// A consumer failing early must be reported as the cause, not the resulting
// pipe error on the decrypt side.
func TestRunSectionPipelineReportsEarlyConsumerFailure(t *testing.T) {
	t.Parallel()

	fx, set, keys := openFixtureSet(t)
	consumeErr := errors.New("checksum mismatch")
	err := RunSectionPipeline(context.Background(), set, keys, nil, fx.Entry.DirectoryName, "verified", "Archive validation", nil, func(r io.Reader) error {
		_, _ = io.ReadFull(r, make([]byte, 10))
		return consumeErr
	})
	if !errors.Is(err, consumeErr) {
		t.Fatalf("expected consumer error, got %v", err)
	}
}

func TestRunSectionPipelineWrongKeyReportsCorruption(t *testing.T) {
	t.Parallel()

	fx, set, _ := openFixtureSet(t)
	wrong, _ := security.RandomBytes(security.KeyLen)
	keys, _ := set.SectionKeys(wrong)
	err := RunSectionPipeline(context.Background(), set, keys, nil, fx.Entry.DirectoryName, "verified", "Archive validation", nil, func(r io.Reader) error {
		_, err := io.Copy(io.Discard, r)
		return err
	})
	if !errors.Is(err, security.ErrCorrupted) || !strings.Contains(err.Error(), "Decryption failed") {
		t.Fatalf("expected decryption failure, got %v", err)
	}
}

// TestRunSectionPipelineConsumerStopsEarly guards against a deadlock: a
// consumer that returns nil before draining the plaintext stream must not
// leave the decrypt goroutine blocked forever on the pipe write. The contract
// under test is that the call returns at all; a regression hangs and is
// reported by the test binary timeout with a goroutine dump.
func TestRunSectionPipelineConsumerStopsEarly(t *testing.T) {
	t.Parallel()

	fx, set, keys := openFixtureSet(t)
	_ = RunSectionPipeline(context.Background(), set, keys, nil, fx.Entry.DirectoryName, "verified", "Archive validation", nil, func(r io.Reader) error {
		_, _ = io.ReadFull(r, make([]byte, 1))
		return nil
	})
}

func TestRunSectionPipelineCountsDoneAndStopsWhenCancelled(t *testing.T) {
	t.Parallel()

	fx, set, keys := openFixtureSet(t)
	var done atomic.Int64
	var n int64
	err := RunSectionPipeline(context.Background(), set, keys, nil, fx.Entry.DirectoryName, "verified", "Archive validation", &done, func(r io.Reader) error {
		var err error
		n, err = io.Copy(io.Discard, r)
		return err
	})
	if err != nil || done.Load() != n || n == 0 {
		t.Fatalf("expected done=%d plaintext bytes, got done=%d err=%v", n, done.Load(), err)
	}
	if size := SectionSize(set, nil); size < n {
		t.Fatalf("SectionSize %d must cover the %d plaintext bytes", size, n)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = RunSectionPipeline(ctx, set, keys, nil, fx.Entry.DirectoryName, "verified", "Archive validation", nil, func(r io.Reader) error {
		_, err := io.Copy(io.Discard, r)
		return err
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}
