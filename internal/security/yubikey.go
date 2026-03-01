// Package yubikey provides optional YubiKey HMAC-SHA1 challenge-response
// second factor authentication.
//
// The YubiKey is queried via the ykchalresp command-line tool which must be
// available on PATH when yubikey_enable is set to true.
// If the tool is not found, a clear error message is shown.
//
// The HMAC-SHA1 response is appended to the user password before key
// derivation so that both factors contribute to the encryption key.
package security

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

const challengeLen = 32

// ErrYubikeyNotFound is returned when ykchalresp is not on PATH.
var ErrYubikeyNotFound = errors.New(
	"ykchalresp not found – please install YubiKey-Manager: " +
		"(https://www.yubico.com/support/download/yubikey-manager/)")

// CombineWithPassword generates a random challenge, sends it to the YubiKey
// (slot 2 by default), and appends the HMAC-SHA1 response to password.
// The combined value is used as the actual encryption password so that
// physical possession of the YubiKey is required for both backup and restore.
//
// The challenge is stored alongside the backup so that it can be replayed
// during restore. Returns the combined password and the hex-encoded challenge.
func CombineWithPassword(password []byte) (combined []byte, challengeHex string, err error) {
	challenge := make([]byte, challengeLen)
	if _, err := rand.Read(challenge); err != nil {
		return nil, "", fmt.Errorf("Failed to generate challenge: %w", err)
	}

	response, err := queryYubikey(challenge)
	if err != nil {
		return nil, "", err
	}

	challengeHex = hex.EncodeToString(challenge)
	combined = append(password, response...)
	return combined, challengeHex, nil
}

// CombineWithPasswordForRestore reuses a previously stored challenge to
// derive the same combined password for restore.
func CombineWithPasswordForRestore(password []byte, challengeHex string) ([]byte, error) {
	challenge, err := hex.DecodeString(challengeHex)
	if err != nil {
		return nil, fmt.Errorf("Failed to decode challenge: %w", err)
	}

	response, err := queryYubikey(challenge)
	if err != nil {
		return nil, err
	}

	return append(password, response...), nil
}

// queryYubikey sends a raw challenge to YubiKey slot 2 and returns the
// HMAC-SHA1 response bytes.
func queryYubikey(challenge []byte) ([]byte, error) {
	_, err := exec.LookPath("ykchalresp")
	if err != nil {
		return nil, ErrYubikeyNotFound
	}

	challengeHex := hex.EncodeToString(challenge)
	out, err := exec.Command("ykchalresp", "-2", "-x", challengeHex).Output()
	if err != nil {
		return nil, fmt.Errorf("YubiKey query failed (please touch the key): %w", err)
	}

	responseHex := strings.TrimSpace(string(out))
	response, err := hex.DecodeString(responseHex)
	if err != nil {
		return nil, fmt.Errorf("Failed to decode YubiKey response: %w", err)
	}

	return response, nil
}
