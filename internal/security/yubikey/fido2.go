//go:build windows

// Package yubikey implements FIDO2 hmac-secret authentication via the Windows
// WebAuthn API (webauthn.dll).
// No external tool or elevated privileges are required. The authenticator must
// support CTAP2 with the hmac-secret extension (all YubiKey 5 series devices do).
package yubikey

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/phsc84/restoresafe/internal/problem"
	"github.com/phsc84/restoresafe/internal/security/cryptox"
)

// DebugOutput receives details of the WebAuthn calls; nil (the default) turns
// them off. Only the YubiKey test sets it, for the duration of the test.
var DebugOutput io.Writer

func fido2Log(format string, args ...any) {
	if DebugOutput != nil {
		fmt.Fprintf(DebugOutput, "[fido2-debug] "+format+"\n", args...)
	}
}

// ── Sentinel errors ───────────────────────────────────────────────────────────

var ErrNotConnected = errors.New("no FIDO2 authenticator detected")
var ErrRequired = problem.New("FIDO2 authenticator is required but none was detected.").WithRemedy("Connect the YubiKey and retry.")

// ── Injectable vars (overridden in tests) ─────────────────────────────────────

// fido2MakeCredFn creates a new FIDO2 credential with the hmac-secret extension
// enabled and returns the raw credential ID. An authenticator that already
// holds one of the exclude credentials refuses the registration.
var fido2MakeCredFn = realMakeCredential

// fido2GetHmacFn returns the 32-byte hmac-secret output for salt from
// whichever of the allowed credentials the connected authenticator holds, and
// the index of that credential in credIDs.
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
	return registerWithSalt(password, noPassword, salt, nil)
}

// ErrAlreadyRegistered is returned when a spare YubiKey registration is
// refused because the connected YubiKey already holds the first registration.
var ErrAlreadyRegistered = errors.New("this YubiKey is already registered for these keys")

// RegisterSpare registers a second YubiKey for the same keys as
// primary. The connected YubiKey must not be the primary one: the primary
// credential is passed as exclude list, so the primary YubiKey refuses and
// ErrAlreadyRegistered is returned. The spare uses the primary's
// hmac-secret salt; its output still differs because hmac-secret is keyed by
// each credential's own secret, and a shared salt lets restore ask for either
// YubiKey in one request.
func RegisterSpare(password []byte, primary ChallengeData) (combined []byte, challengeJSON string, err error) {
	primaryCred, err := base64.StdEncoding.DecodeString(primary.CredID)
	if err != nil {
		return nil, "", fmt.Errorf("invalid primary YubiKey credential: %w", err)
	}
	salt, err := base64.StdEncoding.DecodeString(primary.Salt)
	if err != nil || len(salt) != fido2SaltSize {
		return nil, "", fmt.Errorf("invalid primary YubiKey salt")
	}
	return registerWithSalt(password, primary.NoPassword, salt, [][]byte{primaryCred})
}

func registerWithSalt(password []byte, noPassword bool, salt []byte, exclude [][]byte) (combined []byte, challengeJSON string, err error) {
	credID, err := fido2MakeCredFn(exclude)
	if err != nil {
		if errors.Is(err, ErrAlreadyRegistered) {
			return nil, "", err
		}
		return nil, "", fmt.Errorf("FIDO2 credential creation failed: %w", err)
	}

	_, secret, err := fido2GetHmacFn([][]byte{credID}, salt)
	if err != nil {
		return nil, "", fmt.Errorf("FIDO2 hmac-secret failed: %w", err)
	}
	if err := validateFIDO2Secret(secret); err != nil {
		cryptox.ZeroBytes(secret)
		return nil, "", err
	}
	defer cryptox.ZeroBytes(secret)

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

// DeriveFIDO2SecretAny asks the connected YubiKey for the hmac-secret of any
// of the given challenges in one request (one touch) and returns the index of
// the challenge that answered with its secret. All challenges must share the
// same hmac-secret salt (see RegisterSpare). The caller owns the
// returned slice and must zero it when done.
func DeriveFIDO2SecretAny(challenges []ChallengeData) (int, []byte, error) {
	if len(challenges) == 0 {
		return 0, nil, fmt.Errorf("no YubiKey challenge given")
	}
	credIDs := make([][]byte, len(challenges))
	var salt []byte
	for i, cd := range challenges {
		credID, err := base64.StdEncoding.DecodeString(cd.CredID)
		if err != nil || len(credID) == 0 {
			return 0, nil, fmt.Errorf("invalid FIDO2 credential ID in challenge %d", i)
		}
		s, err := base64.StdEncoding.DecodeString(cd.Salt)
		if err != nil || len(s) != fido2SaltSize {
			return 0, nil, fmt.Errorf("invalid FIDO2 salt in challenge %d", i)
		}
		if salt != nil && !bytes.Equal(salt, s) {
			return 0, nil, problem.New("registered YubiKeys use different salts.").WithRemedy("Use an unmodified backup created by RestoreSafe.")
		}
		salt = s
		credIDs[i] = credID
	}

	index, secret, err := fido2GetHmacFn(credIDs, salt)
	if err != nil {
		return 0, nil, fmt.Errorf("FIDO2 hmac-secret failed: %w", err)
	}
	if err := validateFIDO2Secret(secret); err != nil {
		cryptox.ZeroBytes(secret)
		return 0, nil, err
	}
	if index < 0 || index >= len(challenges) {
		cryptox.ZeroBytes(secret)
		return 0, nil, fmt.Errorf("YubiKey answered with an unknown credential")
	}
	return index, secret, nil
}

// CombinePasswordWithSecret returns password followed by secret. This is the key
// material input shared by backup and restore whenever a YubiKey factor is used.
func CombinePasswordWithSecret(password, secret []byte) []byte {
	combined := make([]byte, len(password)+len(secret))
	copy(combined, password)
	copy(combined[len(password):], secret)
	return combined
}
