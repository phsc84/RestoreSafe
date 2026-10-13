package interact

import (
	"bytes"
	"errors"

	"github.com/phsc84/restoresafe/internal/security/cryptox"
)

// Errors of CheckNewPassword that the user can correct by entering the
// password again.
var (
	ErrPasswordEmpty    = errors.New("Password must not be empty.")
	ErrPasswordMismatch = errors.New("Passwords do not match.")
)

// CheckNewPassword checks a new password against its confirmation, zeroes
// the confirmation, and zeroes the password when it is refused.
func CheckNewPassword(pw, confirm []byte) ([]byte, error) {
	defer cryptox.ZeroBytes(confirm)
	switch {
	case len(pw) == 0:
		cryptox.ZeroBytes(pw)
		return nil, ErrPasswordEmpty
	case !bytes.Equal(pw, confirm):
		cryptox.ZeroBytes(pw)
		return nil, ErrPasswordMismatch
	}
	return pw, nil
}
