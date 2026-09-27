package interact

import (
	"RestoreSafe/internal/security/cryptox"
	"bytes"
	"errors"
)

// Errors of ReadPasswordConfirmed that the user can correct by
// entering the password again.
var (
	ErrPasswordEmpty    = errors.New("Password must not be empty.")
	ErrPasswordMismatch = errors.New("Passwords do not match.")
)

// ReadPasswordConfirmed reads a password and its confirmation with read.
func ReadPasswordConfirmed(read func(prompt string) ([]byte, error), firstPrompt, confirmPrompt string) ([]byte, error) {
	pw1, err := read(firstPrompt)
	if err != nil {
		return nil, err
	}
	if len(pw1) == 0 {
		cryptox.ZeroBytes(pw1)
		return nil, ErrPasswordEmpty
	}

	pw2, err := read(confirmPrompt)
	if err != nil {
		cryptox.ZeroBytes(pw1)
		return nil, err
	}
	defer cryptox.ZeroBytes(pw2)

	if !bytes.Equal(pw1, pw2) {
		cryptox.ZeroBytes(pw1)
		return nil, ErrPasswordMismatch
	}

	return pw1, nil
}
