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
func RunDecryptPipeline(
	parts []string,
	password []byte,
	log *util.Logger,
	folderName string,
	progressVerb string,
	consumeFailurePrefix string,
	consume func(io.Reader) error,
) error {
	seqReader := util.NewSequentialReader(parts)
	defer seqReader.Close()

	var inBytes atomic.Int64
	var outBytes atomic.Int64
	var outWriteCalls atomic.Int64
	progressDone := make(chan struct{})
	progressStopped := make(chan struct{})
	go func() {
		LogProgressUntilDone(log, folderName, progressVerb, &inBytes, &outBytes, &outWriteCalls, progressDone)
		close(progressStopped)
	}()
	defer func() {
		close(progressDone)
		<-progressStopped
	}()

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
	if consumeErr != nil {
		pr.CloseWithError(consumeErr) //nolint:errcheck
	}
	decErr := <-decErrCh

	if decErr != nil {
		if errors.Is(decErr, security.ErrWrongPassword) {
			return fmt.Errorf("%w. Remedy: Check the password; for YubiKey backups, the matching .challenge file must be in the same folder as the .enc files.", security.ErrWrongPassword)
		}
		return fmt.Errorf("Decryption failed: %w", decErr)
	}
	if consumeErr != nil {
		return fmt.Errorf("%s failed: %w", consumeFailurePrefix, consumeErr)
	}

	return nil
}
