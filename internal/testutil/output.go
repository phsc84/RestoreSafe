package testutil

import (
	"bytes"
	"sync"
)

// Output collects what an interacttest.Script writes in a test. It takes
// concurrent writes.
type Output struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

// Write appends p.
func (o *Output) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.buf.Write(p)
}

// String returns everything written so far.
func (o *Output) String() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.buf.String()
}
