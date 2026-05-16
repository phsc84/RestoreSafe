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
	"bytes"
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

var resolveYkmanExecutableFn = resolveYkmanExecutable

// ykmanExeDir is set at startup to the directory containing RestoreSafe.exe,
// allowing ykman.exe to be shipped alongside the application without requiring
// a system-wide YubiKey Manager installation.
var ykmanExeDir string

// SetYkmanExeDir registers the application's own directory so that ykman.exe
// placed next to RestoreSafe.exe is found automatically.
func SetYkmanExeDir(dir string) {
	ykmanExeDir = dir
}

var ykmanListOutput = func(ykmanPath string) ([]byte, error) {
	var stderr bytes.Buffer
	cmd := exec.Command(ykmanPath, "list")
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return out, ykmanAnnotateError(err, stderr.String())
	}
	return out, nil
}

var ykmanOtpCalculate = func(ykmanPath, challengeHex string) ([]byte, error) {
	var stderr bytes.Buffer
	cmd := exec.Command(ykmanPath, "otp", "calculate", "2", challengeHex)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, ykmanAnnotateError(err, stderr.String())
	}
	return out, nil
}

// ykmanAnnotateError enriches a ykman exit error with captured stderr text.
// Known ykman error patterns are translated to actionable messages.
func ykmanAnnotateError(err error, stderr string) error {
	stderr = strings.TrimSpace(stderr)
	if stderr == "" {
		return err
	}
	lower := strings.ToLower(stderr)
	switch {
	case strings.Contains(lower, "no yubikey") || strings.Contains(lower, "no device"):
		return fmt.Errorf("no YubiKey detected: %s", stderr)
	case strings.Contains(lower, "slot is empty") || strings.Contains(lower, "not programmed") || strings.Contains(lower, "not configured"):
		return fmt.Errorf("YubiKey slot 2 is not configured for HMAC-SHA1: %s. Remedy: Program slot 2 with HMAC-SHA1 using YubiKey Manager.", stderr)
	case strings.Contains(lower, "timeout"):
		return fmt.Errorf("YubiKey response timed out (touch the key): %s", stderr)
	}
	return fmt.Errorf("%w; ykman stderr: %s", err, stderr)
}

func resolveYkmanExecutable() (string, error) {
	return resolveYkmanExecutableWith(exec.LookPath, os.Stat, os.Getenv, runtime.GOOS, ykmanExeDir)
}

func resolveYkmanExecutableWith(
	lookPath func(string) (string, error),
	stat func(string) (os.FileInfo, error),
	getenv func(string) string,
	goos string,
	exeDir string,
) (string, error) {
	if path, err := lookPath("ykman"); err == nil {
		return path, nil
	}

	// Check alongside the RestoreSafe.exe (ship-alongside distribution).
	if exeDir != "" {
		candidate := filepath.Join(exeDir, "ykman.exe")
		if _, err := stat(candidate); err == nil {
			return candidate, nil
		}
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
		return nil, "", fmt.Errorf("Failed to generate challenge: %w", err)
	}

	response, err := queryYubikey(challenge)
	if err != nil {
		return nil, "", err
	}

	challengeHex = hex.EncodeToString(challenge)
	// Always allocate a new backing array so the caller can safely zero the
	// original password slice without corrupting the combined value.
	combined = make([]byte, len(password)+len(response))
	copy(combined, password)
	copy(combined[len(password):], response)
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

	// Always allocate a new backing array so the caller can safely zero the
	// original password slice without corrupting the combined value.
	combined := make([]byte, len(password)+len(response))
	copy(combined, password)
	copy(combined[len(password):], response)
	return combined, nil
}

// CheckYubiKeyAvailability verifies that the required ykman CLI is
// available without prompting or contacting the device.
func CheckYubiKeyAvailability() error {
	_, err := resolveYkmanExecutableFn()
	if err != nil {
		return ErrYubikeyNotFound
	}
	return nil
}

// ErrYubiKeyNotConnected is returned when ykman is available but no device is plugged in.
var ErrYubiKeyNotConnected = errors.New("no YubiKey detected")

// ErrYubiKeyRequired is returned when a YubiKey is configured but none is detected.
// Shared by backup and restore flows to avoid duplicating the message.
var ErrYubiKeyRequired = errors.New("YubiKey is required but no YubiKey was detected. Remedy: Connect the YubiKey and retry.")

// CheckYubiKeyConnected verifies that ykman is installed AND a YubiKey device
// is currently connected. Call this before prompting the user to touch the key.
func CheckYubiKeyConnected() error {
	ykmanPath, err := resolveYkmanExecutableFn()
	if err != nil {
		return ErrYubikeyNotFound
	}
	out, err := ykmanListOutput(ykmanPath)
	if err != nil || strings.TrimSpace(string(out)) == "" {
		return ErrYubiKeyNotConnected
	}
	return nil
}

// ValidateChallengeHex reports whether s is a well-formed hex-encoded challenge
// of the expected length. Returns a descriptive error if validation fails.
func ValidateChallengeHex(s string) error {
	decoded, err := hex.DecodeString(s)
	if err != nil {
		return fmt.Errorf("not valid hex: %w", err)
	}
	if len(decoded) != challengeLen {
		return fmt.Errorf("unexpected length: got %d bytes, want %d", len(decoded), challengeLen)
	}
	return nil
}

// queryYubikey sends a raw challenge to YubiKey slot 2 and returns the
// HMAC-SHA1 response bytes.
func queryYubikey(challenge []byte) ([]byte, error) {
	ykmanPath, err := resolveYkmanExecutableFn()
	if err != nil {
		return nil, err
	}

	challengeHex := hex.EncodeToString(challenge)
	out, err := ykmanOtpCalculate(ykmanPath, challengeHex)
	if err != nil {
		return nil, fmt.Errorf("YubiKey query failed (please touch the key): %w. Remedy: Keep the YubiKey connected, touch it, and verify HMAC-SHA1 is configured in slot 2.", err)
	}

	responseHex := strings.TrimSpace(string(out))
	response, err := hex.DecodeString(responseHex)
	if err != nil {
		return nil, fmt.Errorf("Failed to decode YubiKey response %q: %w. Remedy: Check ykman version and verify YubiKey configuration (slot 2, HMAC-SHA1).", responseHex, err)
	}

	return response, nil
}
