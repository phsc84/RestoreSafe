package operation

import (
	"io"
	"sync/atomic"
)

// CountingWriter wraps an io.Writer, tracking bytes written and optional write call count.
type CountingWriter struct {
	W     io.Writer
	Total *atomic.Int64
	Calls *atomic.Int64
}

// CountingReader wraps an io.Reader and tracks bytes read.
type CountingReader struct {
	R     io.Reader
	Total *atomic.Int64
}

func (c *CountingReader) Read(p []byte) (int, error) {
	n, err := c.R.Read(p)
	if n > 0 {
		c.Total.Add(int64(n))
	}
	return n, err
}

func (c *CountingWriter) Write(p []byte) (int, error) {
	if c.Calls != nil {
		c.Calls.Add(1)
	}
	n, err := c.W.Write(p)
	if n > 0 {
		c.Total.Add(int64(n))
	}
	return n, err
}
