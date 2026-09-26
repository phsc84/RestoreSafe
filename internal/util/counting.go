package util

import (
	"context"
	"io"
	"sync/atomic"
)

// CountingWriter wraps an io.Writer, tracking bytes written and optional write
// call count. Total and Calls may be nil.
type CountingWriter struct {
	W     io.Writer
	Total *atomic.Int64
	Calls *atomic.Int64
}

// CountingReader wraps an io.Reader and tracks bytes read. Total may be nil.
type CountingReader struct {
	R     io.Reader
	Total *atomic.Int64
}

func (c *CountingReader) Read(p []byte) (int, error) {
	n, err := c.R.Read(p)
	if n > 0 && c.Total != nil {
		c.Total.Add(int64(n))
	}
	return n, err
}

func (c *CountingWriter) Write(p []byte) (int, error) {
	if c.Calls != nil {
		c.Calls.Add(1)
	}
	n, err := c.W.Write(p)
	if n > 0 && c.Total != nil {
		c.Total.Add(int64(n))
	}
	return n, err
}

// ContextWriter fails every write with the context's error once Ctx is done,
// which stops a copy or stream as soon as an operation is cancelled.
type ContextWriter struct {
	Ctx context.Context
	W   io.Writer
}

func (c *ContextWriter) Write(p []byte) (int, error) {
	if err := c.Ctx.Err(); err != nil {
		return 0, err
	}
	return c.W.Write(p)
}
