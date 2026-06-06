package operation

import (
	"RestoreSafe/internal/security"
	"RestoreSafe/internal/util"
	"errors"
	"fmt"
	"io"
	"sync/atomic"
)

// RunDecryptPipeline decrypts selected parts and streams plaintext to consume.
// onPartStart is called just before each part file is opened (1-based index, total count);
// pass nil to skip per-part callbacks.
//
// consume is expected to read the plaintext stream to EOF. RunDecryptPipeline
// always closes the read end of the pipe once consume returns, so the decrypt
// goroutine can never block forever writing to a consumer that has stopped
// reading. A consumer that returns nil *without* draining the stream therefore
// causes the decrypt goroutine's pending write to fail, and that failure is
// reported as a decryption error rather than success — drain the stream fully to
// obtain a clean result.
func RunDecryptPipeline(
	parts []string,
	password []byte,
	log *util.Logger,
	directoryName string,
	progressVerb string,
	consumeFailurePrefix string,
	consume func(io.Reader) error,
	onPartStart func(partIndex, partCount int),
) error {
	seqReader := util.NewSequentialReader(parts)
	defer seqReader.Close()

	seqReader.SetOnFileOpen(onPartStart)

	var inBytes atomic.Int64
	var outBytes atomic.Int64
	var outWriteCalls atomic.Int64
	stopProgress := StartProgressTracking(log, directoryName, progressVerb, &inBytes, &outBytes, &outWriteCalls)
	defer stopProgress()

	pr, pw := io.Pipe()
	decErrCh := make(chan error, 1)
	go func() {
		err := security.Decrypt(
			&CountingWriter{W: pw, Total: &outBytes, Calls: &outWriteCalls},
			&CountingReader{R: seqReader, Total: &inBytes},
			password,
		)
		pw.CloseWithError(err) //nolint:errcheck
		decErrCh <- err
	}()

	consumeErr := consume(pr)
	// Always close the read end once consume returns, regardless of outcome.
	// If consume drained the stream, the decrypt goroutine has already closed pw
	// and exited, so this is a no-op. If consume stopped early — whether it
	// failed or returned nil without reading to EOF — closing pr makes the
	// goroutine's pending pw.Write fail instead of blocking forever, which would
	// otherwise deadlock the <-decErrCh receive below. CloseWithError(nil) makes
	// pending writes fail with io.ErrClosedPipe.
	pr.CloseWithError(consumeErr) //nolint:errcheck
	decErr := <-decErrCh

	if decErr != nil {
		if errors.Is(decErr, security.ErrWrongPassword) {
			return fmt.Errorf("%w. Remedy: Check the password; for YubiKey backups, the matching .challenge file must be in the same directory as the .enc files.", security.ErrWrongPassword)
		}
		return fmt.Errorf("Decryption failed: %w", decErr)
	}
	if consumeErr != nil {
		return fmt.Errorf("%s failed: %w", consumeFailurePrefix, consumeErr)
	}

	return nil
}
