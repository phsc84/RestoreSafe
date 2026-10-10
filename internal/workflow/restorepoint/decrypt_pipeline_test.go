package restorepoint

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/phsc84/restoresafe/internal/format/catalog"
	"github.com/phsc84/restoresafe/internal/format/container"
	"github.com/phsc84/restoresafe/internal/security/cryptox"
	"github.com/phsc84/restoresafe/internal/testutil"
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

func TestDecryptSectionSuccess(t *testing.T) {
	t.Parallel()

	fx, set, keys := openFixtureSet(t)
	var n int64
	err := reading{name: fx.Entry.DirectoryName, verifyOnly: true}.decrypt(context.Background(), set, keys, func(r io.Reader) error {
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

func TestDecryptSectionConsumeErrorWrapsMessage(t *testing.T) {
	t.Parallel()

	fx, set, keys := openFixtureSet(t)
	consumeErr := errors.New("validation failed")
	err := reading{name: fx.Entry.DirectoryName, verifyOnly: true}.decrypt(context.Background(), set, keys, func(r io.Reader) error {
		io.Copy(io.Discard, r) //nolint:errcheck
		return consumeErr
	})
	if err == nil || !strings.Contains(err.Error(), "Verification failed") || !errors.Is(err, consumeErr) {
		t.Fatalf("expected wrapped consume error, got %v", err)
	}
}

// A consumer failing early must be reported as the cause, not the resulting
// pipe error on the decrypt side.
func TestDecryptSectionReportsEarlyConsumerFailure(t *testing.T) {
	t.Parallel()

	fx, set, keys := openFixtureSet(t)
	consumeErr := errors.New("checksum mismatch")
	err := reading{name: fx.Entry.DirectoryName, verifyOnly: true}.decrypt(context.Background(), set, keys, func(r io.Reader) error {
		_, _ = io.ReadFull(r, make([]byte, 10))
		return consumeErr
	})
	if !errors.Is(err, consumeErr) {
		t.Fatalf("expected consumer error, got %v", err)
	}
}

func TestDecryptSectionWrongKeyReportsCorruption(t *testing.T) {
	t.Parallel()

	fx, set, _ := openFixtureSet(t)
	wrong, _ := cryptox.RandomBytes(cryptox.KeyLen)
	keys, _ := set.SectionKeys(wrong)
	err := reading{name: fx.Entry.DirectoryName, verifyOnly: true}.decrypt(context.Background(), set, keys, func(r io.Reader) error {
		_, err := io.Copy(io.Discard, r)
		return err
	})
	if !errors.Is(err, cryptox.ErrCorrupted) || !strings.Contains(err.Error(), "Decryption failed") {
		t.Fatalf("expected decryption failure, got %v", err)
	}
}

// TestDecryptSectionConsumerStopsEarly guards against a deadlock: a
// consumer that returns nil before draining the plaintext stream must not
// leave the decrypt goroutine blocked forever on the pipe write. The contract
// under test is that the call returns at all; a regression hangs and is
// reported by the test binary timeout with a goroutine dump.
func TestDecryptSectionConsumerStopsEarly(t *testing.T) {
	t.Parallel()

	fx, set, keys := openFixtureSet(t)
	_ = reading{name: fx.Entry.DirectoryName, verifyOnly: true}.decrypt(context.Background(), set, keys, func(r io.Reader) error {
		_, _ = io.ReadFull(r, make([]byte, 1))
		return nil
	})
}

func TestDecryptSectionCountsDoneAndStopsWhenCancelled(t *testing.T) {
	t.Parallel()

	fx, set, keys := openFixtureSet(t)
	var done atomic.Int64
	var n int64
	err := reading{name: fx.Entry.DirectoryName, verifyOnly: true, out: Output{Done: &done}}.decrypt(context.Background(), set, keys, func(r io.Reader) error {
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
	err = reading{name: fx.Entry.DirectoryName, verifyOnly: true}.decrypt(ctx, set, keys, func(r io.Reader) error {
		_, err := io.Copy(io.Discard, r)
		return err
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}
