package operation

import (
	"RestoreSafe/internal/container"
	"RestoreSafe/internal/util"
	"fmt"
	"io"
	"sync/atomic"
)

// recordingWriter remembers whether a write to the pipe failed, which means
// the consumer stopped reading (it is the root cause, not the decryption).
type recordingWriter struct {
	w      io.Writer
	failed atomic.Bool
}

func (r *recordingWriter) Write(p []byte) (int, error) {
	n, err := r.w.Write(p)
	if err != nil {
		r.failed.Store(true)
	}
	return n, err
}

// RunSectionPipeline decrypts the data section of set and streams the
// plaintext (a TAR stream) to consume.
//
// consume is expected to read the stream to EOF. The read end of the pipe is
// always closed once consume returns, so the decrypt goroutine can never block
// forever writing to a consumer that has stopped reading. When both sides
// fail, the error of the side that failed first is reported.
func RunSectionPipeline(
	set *container.Set,
	keys *container.SectionKeys,
	log *util.Logger,
	directoryName string,
	progressVerb string,
	consumeFailurePrefix string,
	consume func(io.Reader) error,
) error {
	var outBytes atomic.Int64
	var outWriteCalls atomic.Int64
	stopProgress := StartProgressTracking(log, directoryName, progressVerb, &outBytes, &outBytes, &outWriteCalls)
	defer stopProgress()

	pr, pw := io.Pipe()
	rw := &recordingWriter{w: pw}
	decErrCh := make(chan error, 1)
	go func() {
		err := set.DecryptData(keys, &util.CountingWriter{W: rw, Total: &outBytes, Calls: &outWriteCalls})
		pw.CloseWithError(err) //nolint:errcheck
		decErrCh <- err
	}()

	consumeErr := consume(pr)
	pr.CloseWithError(consumeErr) //nolint:errcheck
	decErr := <-decErrCh

	if consumeErr != nil && (decErr == nil || rw.failed.Load()) {
		return fmt.Errorf("%s failed: %w", consumeFailurePrefix, consumeErr)
	}
	if decErr != nil {
		return fmt.Errorf("Decryption failed: %w", decErr)
	}
	return nil
}
