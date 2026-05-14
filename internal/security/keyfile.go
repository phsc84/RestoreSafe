// Package security: keyfile provides password + keyfile second-factor authentication.
// The keyfile's raw bytes are appended to the password before Argon2id key derivation,
// so physical possession of the keyfile is required for both backup and restore.
package security

import (
	"fmt"
	"os"
)

// CombineWithKeyfile reads the keyfile at keyfilePath and appends its contents
// to password. The combined value is used as the actual encryption password so
// that physical possession of the keyfile is required for backup and restore.
func CombineWithKeyfile(password []byte, keyfilePath string) ([]byte, error) {
	data, err := os.ReadFile(keyfilePath)
	if err != nil {
		return nil, fmt.Errorf("Failed to read keyfile %q: %w. Remedy: Ensure the keyfile exists at the configured keyfile_path and is readable.", keyfilePath, err)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("Keyfile %q is empty. Remedy: Use a non-empty keyfile or generate a new one.", keyfilePath)
	}
	return append(password, data...), nil
}
