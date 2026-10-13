package fsx

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"
)

func TestCountingReaderTracksBytes(t *testing.T) {
	t.Parallel()

	data := []byte("hello world")
	var total atomic.Int64
	r := &CountingReader{R: bytes.NewReader(data), Total: &total}

	buf := make([]byte, len(data))
	n, err := r.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("unexpected read error: %v", err)
	}
	if n != len(data) {
		t.Fatalf("expected %d bytes read, got %d", len(data), n)
	}
	if got := total.Load(); got != int64(len(data)) {
		t.Fatalf("expected total %d, got %d", len(data), got)
	}
}

func TestCountingWriterTracksBytesAndCalls(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	var total atomic.Int64
	var calls atomic.Int64
	w := &CountingWriter{W: &buf, Total: &total, Calls: &calls}

	data := []byte("hello world")
	n, err := w.Write(data)
	if err != nil {
		t.Fatalf("unexpected write error: %v", err)
	}
	if n != len(data) {
		t.Fatalf("expected %d bytes written, got %d", len(data), n)
	}
	if got := total.Load(); got != int64(len(data)) {
		t.Fatalf("expected total %d, got %d", len(data), got)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("expected 1 write call, got %d", got)
	}

	_, _ = w.Write([]byte("more"))
	if got := calls.Load(); got != 2 {
		t.Fatalf("expected 2 write calls after second write, got %d", got)
	}
}

func TestCountingWriterNilCallsIsNoOp(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	var total atomic.Int64
	w := &CountingWriter{W: &buf, Total: &total} // Calls is nil

	if _, err := w.Write([]byte("test")); err != nil {
		t.Fatalf("unexpected write error: %v", err)
	}
	if got := total.Load(); got != 4 {
		t.Fatalf("expected total 4, got %d", got)
	}
}

func TestContextWriterStopsWritingOnceCancelled(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	w := &ContextWriter{Ctx: ctx, W: &buf}
	if _, err := io.WriteString(w, "before"); err != nil {
		t.Fatal(err)
	}
	cancel()
	if n, err := io.WriteString(w, "after"); n != 0 || !errors.Is(err, context.Canceled) {
		t.Fatalf("after cancel: wrote %d, err %v", n, err)
	}
	if buf.String() != "before" {
		t.Fatalf("got %q", buf.String())
	}
}
