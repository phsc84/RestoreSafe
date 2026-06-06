//go:build windows

package security

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"
)

// ── CheckYubiKey* tests ───────────────────────────────────────────────────────

func TestCheckYubiKeyConnectedSuccess(t *testing.T) {
	prev := fido2RuntimeReady
	t.Cleanup(func() { fido2RuntimeReady = prev })
	fido2RuntimeReady = func() error { return nil }

	if err := CheckYubiKeyConnected(); err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
}

func TestCheckYubiKeyConnectedNotFound(t *testing.T) {
	prev := fido2RuntimeReady
	t.Cleanup(func() { fido2RuntimeReady = prev })
	fido2RuntimeReady = func() error { return ErrYubiKeyNotConnected }

	err := CheckYubiKeyConnected()
	if !errors.Is(err, ErrYubiKeyNotConnected) {
		t.Fatalf("expected ErrYubiKeyNotConnected, got: %v", err)
	}
}

func TestCheckYubiKeyConnectedPropagatesError(t *testing.T) {
	prev := fido2RuntimeReady
	t.Cleanup(func() { fido2RuntimeReady = prev })
	fido2RuntimeReady = func() error { return errors.New("webauthn error") }

	err := CheckYubiKeyConnected()
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
}

func TestWebAuthnGetAssertionOptionsLayout(t *testing.T) {
	if got, want := unsafe.Sizeof(webauthnGetAssertionOptions{}), uintptr(120); got != want {
		t.Fatalf("webauthnGetAssertionOptions size = %d, want %d", got, want)
	}
	if got, want := unsafe.Offsetof(webauthnGetAssertionOptions{}.pHmacSecretSaltValues), uintptr(104); got != want {
		t.Fatalf("pHmacSecretSaltValues offset = %d, want %d", got, want)
	}
	if got, want := unsafe.Offsetof(webauthnGetAssertionOptions{}.bBrowserInPrivateMode), uintptr(112); got != want {
		t.Fatalf("bBrowserInPrivateMode offset = %d, want %d", got, want)
	}
}

func TestWebAuthnAssertionLayout(t *testing.T) {
	if got, want := unsafe.Sizeof(webauthnAssertion{}), uintptr(120); got != want {
		t.Fatalf("webauthnAssertion size = %d, want %d", got, want)
	}
	if got, want := unsafe.Offsetof(webauthnAssertion{}.pHmacSecret), uintptr(112); got != want {
		t.Fatalf("pHmacSecret offset = %d, want %d", got, want)
	}
}

// ── ValidateChallengeJSON tests ───────────────────────────────────────────────

func makeValidChallengeJSON(t *testing.T) string {
	t.Helper()
	cd := ChallengeData{
		Version: 1,
		CredID:  base64.StdEncoding.EncodeToString(make([]byte, 64)),
		Salt:    base64.StdEncoding.EncodeToString(make([]byte, fido2SaltSize)),
	}
	b, err := json.Marshal(cd)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestValidateChallengeJSONAcceptsValid(t *testing.T) {
	t.Parallel()
	if err := ValidateChallengeJSON(makeValidChallengeJSON(t)); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestValidateChallengeJSONRejectsEmpty(t *testing.T) {
	t.Parallel()
	if err := ValidateChallengeJSON(""); err == nil {
		t.Fatal("expected error for empty input")
	}
}

func TestValidateChallengeJSONRejectsMalformedJSON(t *testing.T) {
	t.Parallel()
	err := ValidateChallengeJSON("not-json")
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
	if !strings.Contains(err.Error(), "not valid JSON") {
		t.Fatalf("expected JSON decode error, got: %v", err)
	}
}

func TestValidateChallengeJSONRejectsMissingCredID(t *testing.T) {
	t.Parallel()
	cd := ChallengeData{
		Version: 1,
		CredID:  "",
		Salt:    base64.StdEncoding.EncodeToString(make([]byte, fido2SaltSize)),
	}
	b, _ := json.Marshal(cd)
	if err := ValidateChallengeJSON(string(b)); err == nil {
		t.Fatal("expected error for missing cred_id")
	}
}

func TestValidateChallengeJSONRejectsOversizedCredID(t *testing.T) {
	t.Parallel()
	cd := ChallengeData{
		Version: 1,
		CredID:  base64.StdEncoding.EncodeToString(make([]byte, fido2CredIDMax+1)),
		Salt:    base64.StdEncoding.EncodeToString(make([]byte, fido2SaltSize)),
	}
	b, _ := json.Marshal(cd)
	if err := ValidateChallengeJSON(string(b)); err == nil {
		t.Fatal("expected error for oversized cred_id")
	}
}

func TestValidateChallengeJSONRejectsWrongSaltLength(t *testing.T) {
	t.Parallel()
	cd := ChallengeData{
		Version: 1,
		CredID:  base64.StdEncoding.EncodeToString(make([]byte, 64)),
		Salt:    base64.StdEncoding.EncodeToString(make([]byte, 16)), // wrong size
	}
	b, _ := json.Marshal(cd)
	if err := ValidateChallengeJSON(string(b)); err == nil {
		t.Fatal("expected error for wrong salt length")
	}
}

func TestParseChallengeAcceptsMatchingChecksum(t *testing.T) {
	t.Parallel()
	cd := ChallengeData{
		Version: 1,
		CredID:  base64.StdEncoding.EncodeToString(make([]byte, 64)),
		Salt:    base64.StdEncoding.EncodeToString(make([]byte, fido2SaltSize)),
	}
	cd.Sum = challengeChecksum(cd)
	b, _ := json.Marshal(cd)
	if err := ValidateChallengeJSON(string(b)); err != nil {
		t.Fatalf("expected valid challenge with matching checksum, got: %v", err)
	}
}

func TestParseChallengeRejectsCorruptedChecksum(t *testing.T) {
	t.Parallel()
	salt := make([]byte, fido2SaltSize)
	cd := ChallengeData{
		Version: 1,
		CredID:  base64.StdEncoding.EncodeToString(make([]byte, 64)),
		Salt:    base64.StdEncoding.EncodeToString(salt),
	}
	cd.Sum = challengeChecksum(cd)
	// Corrupt the salt after the checksum was computed, keeping a valid length.
	salt[0] = 0xFF
	cd.Salt = base64.StdEncoding.EncodeToString(salt)
	b, _ := json.Marshal(cd)

	err := ValidateChallengeJSON(string(b))
	if err == nil {
		t.Fatal("expected error for corrupted challenge checksum")
	}
	if !strings.Contains(err.Error(), "corrupted") {
		t.Fatalf("expected corruption error, got: %v", err)
	}
}

func TestCombineWithPasswordWritesChecksum(t *testing.T) {
	prevMake := fido2MakeCredFn
	prevGet := fido2GetHmacFn
	t.Cleanup(func() { fido2MakeCredFn = prevMake; fido2GetHmacFn = prevGet })

	fido2MakeCredFn = func() ([]byte, error) { return []byte("cred"), nil }
	fido2GetHmacFn = func(_, _ []byte) ([]byte, error) { return make([]byte, fido2SaltSize), nil }

	_, chalJSON, err := CombineWithPassword([]byte("pw"), false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cd, err := parseChallengeData(chalJSON)
	if err != nil {
		t.Fatalf("failed to parse challenge JSON: %v", err)
	}
	if cd.Sum == "" {
		t.Fatal("expected non-empty integrity checksum in challenge JSON")
	}
	if cd.Sum != challengeChecksum(cd) {
		t.Fatal("written checksum does not match recomputed checksum")
	}
}

// ── CombineWithPassword* tests ────────────────────────────────────────────────

func TestCombineWithPasswordSucceeds(t *testing.T) {
	prevMake := fido2MakeCredFn
	prevGet := fido2GetHmacFn
	t.Cleanup(func() { fido2MakeCredFn = prevMake; fido2GetHmacFn = prevGet })

	wantCredID := []byte("fake-cred-id")
	wantSecret := make([]byte, fido2SaltSize)
	wantSecret[0] = 0xAB

	fido2MakeCredFn = func() ([]byte, error) { return wantCredID, nil }
	fido2GetHmacFn = func(credID, _ []byte) ([]byte, error) {
		if !bytes.Equal(credID, wantCredID) {
			t.Errorf("unexpected credID passed to GetHmac: %x", credID)
		}
		return wantSecret, nil
	}

	password := []byte("backup-pw")
	combined, chalJSON, err := CombineWithPassword(password, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chalJSON == "" {
		t.Fatal("expected non-empty challenge JSON")
	}
	if len(combined) != len(password)+len(wantSecret) {
		t.Fatalf("combined length: got %d, want %d", len(combined), len(password)+len(wantSecret))
	}
	if err := ValidateChallengeJSON(chalJSON); err != nil {
		t.Fatalf("returned challenge JSON is invalid: %v", err)
	}
}

func TestCombineWithPasswordRejectsWrongSecretLength(t *testing.T) {
	prevMake := fido2MakeCredFn
	prevGet := fido2GetHmacFn
	t.Cleanup(func() { fido2MakeCredFn = prevMake; fido2GetHmacFn = prevGet })

	fido2MakeCredFn = func() ([]byte, error) { return []byte("cred"), nil }
	fido2GetHmacFn = func(_, _ []byte) ([]byte, error) { return make([]byte, fido2SaltSize-1), nil }

	_, _, err := CombineWithPassword([]byte("pw"), false)
	if err == nil {
		t.Fatal("expected error for wrong hmac-secret length")
	}
}

func TestCombineWithPasswordSetsNoPWFlag(t *testing.T) {
	prevMake := fido2MakeCredFn
	prevGet := fido2GetHmacFn
	t.Cleanup(func() { fido2MakeCredFn = prevMake; fido2GetHmacFn = prevGet })

	fido2MakeCredFn = func() ([]byte, error) { return []byte("cred"), nil }
	fido2GetHmacFn = func(_, _ []byte) ([]byte, error) { return make([]byte, fido2SaltSize), nil }

	_, chalJSON, err := CombineWithPassword([]byte{}, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cd, err := parseChallengeData(chalJSON)
	if err != nil {
		t.Fatalf("failed to parse challenge JSON: %v", err)
	}
	if !cd.NoPassword {
		t.Fatal("expected NoPassword=true in YubiKey-only mode")
	}
}

func TestDeriveFIDO2SecretForRestoreRejectsWrongSecretLength(t *testing.T) {
	prevGet := fido2GetHmacFn
	t.Cleanup(func() { fido2GetHmacFn = prevGet })
	fido2GetHmacFn = func(_, _ []byte) ([]byte, error) { return make([]byte, fido2SaltSize+1), nil }

	_, err := DeriveFIDO2SecretForRestore(makeValidChallengeJSON(t))
	if err == nil {
		t.Fatal("expected error for wrong hmac-secret length")
	}
}

func TestDeriveFIDO2SecretForRestoreReturnsGetHmacError(t *testing.T) {
	prevGet := fido2GetHmacFn
	t.Cleanup(func() { fido2GetHmacFn = prevGet })
	fido2GetHmacFn = func(_, _ []byte) ([]byte, error) { return nil, errors.New("device error") }

	_, err := DeriveFIDO2SecretForRestore(makeValidChallengeJSON(t))
	if err == nil {
		t.Fatal("expected error from fido2GetHmacFn, got nil")
	}
}

func TestCombinePasswordWithSecret(t *testing.T) {
	t.Parallel()
	password := []byte("pw")
	secret := []byte("0123456789")
	got := CombinePasswordWithSecret(password, secret)
	want := append(append([]byte{}, password...), secret...)
	if !bytes.Equal(got, want) {
		t.Fatalf("CombinePasswordWithSecret = %q, want %q", got, want)
	}
}

func TestDeriveFIDO2SecretForRestoreReturnsSecret(t *testing.T) {
	prevGet := fido2GetHmacFn
	t.Cleanup(func() { fido2GetHmacFn = prevGet })

	want := make([]byte, fido2SaltSize)
	want[0] = 0xCD
	fido2GetHmacFn = func(_, _ []byte) ([]byte, error) {
		s := make([]byte, fido2SaltSize)
		s[0] = 0xCD
		return s, nil
	}

	secret, err := DeriveFIDO2SecretForRestore(makeValidChallengeJSON(t))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(secret, want) {
		t.Fatalf("DeriveFIDO2SecretForRestore = %x, want %x", secret, want)
	}
}

func TestDeriveFIDO2SecretForRestoreRejectsBadJSON(t *testing.T) {
	t.Parallel()
	if _, err := DeriveFIDO2SecretForRestore("not-valid-json"); err == nil {
		t.Fatal("expected error for invalid challenge JSON")
	}
}

func TestDeriveFIDO2SecretForRestoreRejectsInvalidChallengeJSON(t *testing.T) {
	t.Parallel()
	_, err := DeriveFIDO2SecretForRestore("this-is-not-json")
	if err == nil {
		t.Fatal("expected decode error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid FIDO2 challenge") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ── Pure helper tests ─────────────────────────────────────────────────────────

func TestWebauthnErrorString(t *testing.T) {
	t.Parallel()
	if got := webauthnErrorString(0x80090029); got != "HRESULT 0x80090029" {
		t.Fatalf("unexpected webauthnErrorString output: %q", got)
	}
	if got := webauthnErrorString(0); got != "HRESULT 0x00000000" {
		t.Fatalf("expected zero-padded HRESULT, got: %q", got)
	}
}

func TestBuildClientData(t *testing.T) {
	t.Parallel()

	const opType = "webauthn.get"
	data, err := buildClientData(opType)
	if err != nil {
		t.Fatalf("buildClientData returned error: %v", err)
	}

	var parsed struct {
		Type        string `json:"type"`
		Challenge   string `json:"challenge"`
		Origin      string `json:"origin"`
		CrossOrigin bool   `json:"crossOrigin"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("buildClientData produced invalid JSON %q: %v", data, err)
	}
	if parsed.Type != opType {
		t.Fatalf("expected type %q, got %q", opType, parsed.Type)
	}
	if parsed.Origin != fido2Origin {
		t.Fatalf("expected origin %q, got %q", fido2Origin, parsed.Origin)
	}
	if parsed.CrossOrigin {
		t.Fatal("expected crossOrigin to be false")
	}
	// The challenge is a 16-byte random value, base64url (raw) encoded.
	challenge, err := base64.RawURLEncoding.DecodeString(parsed.Challenge)
	if err != nil {
		t.Fatalf("challenge is not valid raw base64url: %v", err)
	}
	if len(challenge) != 16 {
		t.Fatalf("expected 16-byte challenge, got %d bytes", len(challenge))
	}

	// Each call must use a fresh random challenge.
	other, err := buildClientData(opType)
	if err != nil {
		t.Fatalf("second buildClientData returned error: %v", err)
	}
	if bytes.Equal(data, other) {
		t.Fatal("expected a fresh random challenge on each call")
	}
}

func TestParseChallengeFileReadsValidFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "run.challenge")
	// Surrounding whitespace must be tolerated (ParseChallengeFile trims it).
	if err := os.WriteFile(path, []byte("\n  "+makeValidChallengeJSON(t)+"  \n"), 0o600); err != nil {
		t.Fatalf("failed to write challenge file: %v", err)
	}

	cd, err := ParseChallengeFile(path)
	if err != nil {
		t.Fatalf("ParseChallengeFile returned error: %v", err)
	}
	if cd.Version != 1 {
		t.Fatalf("expected version 1, got %d", cd.Version)
	}
}

func TestParseChallengeFileMissingFile(t *testing.T) {
	t.Parallel()
	if _, err := ParseChallengeFile(filepath.Join(t.TempDir(), "missing.challenge")); err == nil {
		t.Fatal("expected error for missing challenge file")
	}
}

func TestParseChallengeFileInvalidContent(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "bad.challenge")
	if err := os.WriteFile(path, []byte("not-json"), 0o600); err != nil {
		t.Fatalf("failed to write challenge file: %v", err)
	}
	if _, err := ParseChallengeFile(path); err == nil {
		t.Fatal("expected error for invalid challenge content")
	}
}
