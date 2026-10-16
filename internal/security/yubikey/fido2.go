//go:build windows

// Package yubikey implements FIDO2 hmac-secret authentication via the Windows
// WebAuthn API (webauthn.dll).
// No external tool or elevated privileges are required. The authenticator must
// support CTAP2 with the hmac-secret extension (all YubiKey 5 series devices do).
package yubikey

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/phsc84/restoresafe/internal/problem"
	"github.com/phsc84/restoresafe/internal/security/cryptox"
)

// debugOutput receives details of the WebAuthn calls; nil (the default)
// turns them off. The mutex serializes the writes: the health check may log
// while the YubiKey test collects its trace.
var debugOutput struct {
	sync.Mutex
	w io.Writer
}

// SetDebugOutput sets where details of the WebAuthn calls go; nil turns
// them off. Only the YubiKey test sets it, for the duration of the test.
// No write reaches w after SetDebugOutput has returned with another writer.
func SetDebugOutput(w io.Writer) {
	debugOutput.Lock()
	debugOutput.w = w
	debugOutput.Unlock()
}

func fido2Log(format string, args ...any) {
	debugOutput.Lock()
	defer debugOutput.Unlock()
	if debugOutput.w != nil {
		fmt.Fprintf(debugOutput.w, "[fido2-debug] "+format+"\n", args...)
	}
}

// ── Sentinel errors ───────────────────────────────────────────────────────────

var ErrNotConnected = errors.New("no FIDO2 authenticator detected")
var ErrRequired = problem.New("FIDO2 authenticator is required but none was detected.").WithRemedy("Connect the YubiKey and retry.")

// ── Injectable vars (overridden in tests) ─────────────────────────────────────

// fido2MakeCredFn creates a new FIDO2 credential with the hmac-secret extension
// enabled and returns the raw credential ID.
var fido2MakeCredFn = realMakeCredential

// fido2GetHmacFn returns the 32-byte hmac-secret output of credential credID
// for salt.
var fido2GetHmacFn = realGetHmacSecret

// fido2RuntimeReady returns nil when the platform and authenticator are ready.
var fido2RuntimeReady = func() error {
	if err := checkWebAuthnAPIVersion(); err != nil {
		return err
	}
	if !yubiKeyVIDVisible() {
		return ErrNotConnected
	}
	return nil
}

// ── Public API ────────────────────────────────────────────────────────────────

// CheckConnected returns nil when a Yubico FIDO2 authenticator is
// detected and the WebAuthn platform is ready.
func CheckConnected() error {
	return fido2RuntimeReady()
}

// SeveralError says that n YubiKeys are connected, where a backup needs one:
// new keys would register the one the user touches, and nothing would say
// which.
func SeveralError(n int) error {
	return problem.Errorf("%d YubiKeys are connected.", n).WithRemedy("Connect only the YubiKey you back up with, so it's clear which one the backup uses.")
}

// CountConnected returns how many YubiKeys are connected: Yubico USB
// devices, each with one or more HID interfaces. It opens none of them.
func CountConnected() (int, error) {
	devices, err := hidDevicesFn()
	if err != nil {
		return 0, err
	}
	seen := map[string]bool{}
	for _, d := range devices {
		key := d.Parent
		if key == "" {
			key = d.InstanceID
		}
		seen[key] = true
	}
	return len(seen), nil
}

// ProbeCapability tells whether the connected YubiKey can protect backups:
// it registers a non-resident credential and derives one hmac-secret with a
// random salt (PIN and two touches). Nothing stays on the YubiKey, and the
// secret is zeroed.
func ProbeCapability() error {
	combined, _, err := CombineWithPassword(nil, false)
	cryptox.ZeroBytes(combined)
	return err
}

// CombineWithPassword creates a new FIDO2 credential, derives a 32-byte
// hmac-secret from it using a random salt, and appends it to password.
// noPassword must be true when the backup uses no password (YubiKey-only mode)
// so that restore can skip the password prompt.
// Returns the combined key and a JSON challenge string to store alongside the backup.
func CombineWithPassword(password []byte, noPassword bool) (combined []byte, challengeJSON string, err error) {
	salt := make([]byte, fido2SaltSize)
	if _, err := rand.Read(salt); err != nil {
		return nil, "", fmt.Errorf("failed to generate hmac-secret salt: %w", err)
	}
	credID, err := fido2MakeCredFn()
	if err != nil {
		return nil, "", fmt.Errorf("FIDO2 credential creation failed: %w", err)
	}

	secret, err := fido2GetHmacFn(credID, salt)
	if err != nil {
		return nil, "", fmt.Errorf("FIDO2 hmac-secret failed: %w", err)
	}
	defer cryptox.ZeroBytes(secret)
	if err := validateFIDO2Secret(secret); err != nil {
		return nil, "", err
	}

	cd := ChallengeData{
		Version:    1,
		NoPassword: noPassword,
		CredID:     base64.StdEncoding.EncodeToString(credID),
		Salt:       base64.StdEncoding.EncodeToString(salt),
	}
	cd.Sum = challengeChecksum(cd)
	jsonBytes, err := json.Marshal(cd)
	if err != nil {
		return nil, "", fmt.Errorf("failed to encode challenge data: %w", err)
	}

	return CombinePasswordWithSecret(password, secret), string(jsonBytes), nil
}

// DeriveFIDO2Secret asks the connected YubiKey for the hmac-secret of the
// challenge (PIN and one touch). The caller owns the returned slice and must
// zero it when done.
func DeriveFIDO2Secret(cd ChallengeData) ([]byte, error) {
	credID, err := base64.StdEncoding.DecodeString(cd.CredID)
	if err != nil || len(credID) == 0 {
		return nil, fmt.Errorf("invalid FIDO2 credential ID")
	}
	salt, err := base64.StdEncoding.DecodeString(cd.Salt)
	if err != nil || len(salt) != fido2SaltSize {
		return nil, fmt.Errorf("invalid FIDO2 salt")
	}

	secret, err := fido2GetHmacFn(credID, salt)
	if err != nil {
		return nil, fmt.Errorf("FIDO2 hmac-secret failed: %w", err)
	}
	if err := validateFIDO2Secret(secret); err != nil {
		cryptox.ZeroBytes(secret)
		return nil, err
	}
	return secret, nil
}

// CombinePasswordWithSecret returns password followed by secret. This is the key
// material input shared by backup and restore whenever a YubiKey factor is used.
func CombinePasswordWithSecret(password, secret []byte) []byte {
	combined := make([]byte, len(password)+len(secret))
	copy(combined, password)
	copy(combined[len(password):], secret)
	return combined
}
