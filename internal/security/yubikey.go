// Package yubikey provides optional YubiKey HMAC-SHA1 challenge-response
// second factor authentication.
//
// The YubiKey is queried via the ykman command-line tool (from YubiKey Manager v5+)
// which is resolved from PATH and standard Windows install locations when
// yubikey_enable is set to true.
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
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const challengeLen = 32

// ErrYubikeyNotFound is returned when ykman cannot be resolved.
var ErrYubikeyNotFound = errors.New(
	"ykman not found - please install YubiKey Manager: " +
		"(https://www.yubico.com/support/download/yubikey-manager/)")

func resolveYkmanExecutable() (string, error) {
	return resolveYkmanExecutableWith(exec.LookPath, os.Stat, os.Getenv, runtime.GOOS)
}

func resolveYkmanExecutableWith(
	lookPath func(string) (string, error),
	stat func(string) (os.FileInfo, error),
	getenv func(string) string,
	goos string,
) (string, error) {
	if path, err := lookPath("ykman"); err == nil {
		return path, nil
	}

	if goos == "windows" {
		for _, candidate := range ykmanWindowsCandidatesWith(getenv) {
			if _, err := stat(candidate); err == nil {
				return candidate, nil
			}
		}
	}

	return "", ErrYubikeyNotFound
}

func ykmanWindowsCandidatesWith(getenv func(string) string) []string {
	bases := uniqueStrings([]string{
		getenv("ProgramFiles"),
		getenv("ProgramW6432"),
		getenv("ProgramFiles(x86)"),
	})

	candidates := make([]string, 0, len(bases)*2)
	for _, base := range bases {
		candidates = append(candidates,
			filepath.Join(base, "Yubico", "YubiKey Manager", "ykman.exe"),
			filepath.Join(base, "Yubico", "YubiKey Manager", "ykman", "ykman.exe"),
		)
	}

	return candidates
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

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
		return nil, "", fmt.Errorf("Failed to generate challenge: %w. Remedy: Retry the operation and ensure the OS cryptographic provider is available.", err)
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
		return nil, fmt.Errorf("Failed to decode challenge: %w. Remedy: Ensure the .challenge file is unchanged and belongs to the same backup run as the .enc files.", err)
	}

	response, err := queryYubikey(challenge)
	if err != nil {
		return nil, err
	}

	return append(password, response...), nil
}

// CheckYubiKeyAvailability verifies that the required ykman CLI is
// available without prompting or contacting the device.
func CheckYubiKeyAvailability() error {
	_, err := resolveYkmanExecutable()
	if err != nil {
		return ErrYubikeyNotFound
	}
	return nil
}

// queryYubikey sends a raw challenge to YubiKey slot 2 and returns the
// HMAC-SHA1 response bytes.
func queryYubikey(challenge []byte) ([]byte, error) {
	ykmanPath, err := resolveYkmanExecutable()
	if err != nil {
		return nil, err
	}

	challengeHex := hex.EncodeToString(challenge)
	out, err := exec.Command(ykmanPath, "otp", "calculate", "2", challengeHex).Output()
	if err != nil {
		return nil, fmt.Errorf("YubiKey query failed (please touch the key): %w. Remedy: Keep the YubiKey connected, touch it, and verify HMAC-SHA1 is configured in slot 2.", err)
	}

	responseHex := strings.TrimSpace(string(out))
	response, err := hex.DecodeString(responseHex)
	if err != nil {
		return nil, fmt.Errorf("Failed to decode YubiKey response: %w. Remedy: Check ykman version and verify YubiKey configuration (slot 2, HMAC-SHA1).", err)
	}

	return response, nil
}
