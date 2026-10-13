//go:build windows

package yubikey

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"testing"
	"unsafe"
)

// Tests of the code around the WebAuthn calls that need no YubiKey.

func TestCredentialListPointsAtEveryCredential(t *testing.T) {
	if list, creds := credentialList(nil); list.cCredentials != 0 || list.pCredentials != nil || creds != nil {
		t.Fatalf("no credentials: %+v, %v", list, creds)
	}

	ids := [][]byte{[]byte("first"), []byte("second-id")}
	list, creds := credentialList(ids)
	if list.cCredentials != 2 || list.pCredentials != &creds[0] || len(creds) != 2 {
		t.Fatalf("list %+v, creds %d", list, len(creds))
	}
	for i, c := range creds {
		if c.dwVersion != webauthnCredentialVersion || !bytes.Equal(unsafe.Slice(c.pbId, c.cbId), ids[i]) || c.pwszCredentialType == nil {
			t.Fatalf("credential %d: %+v", i, c)
		}
	}
}

func TestAnsweringCredentialFindsTheYubiKeyThatAnswered(t *testing.T) {
	ids := [][]byte{[]byte("primary"), []byte("spare")}
	_, creds := credentialList(ids)

	if i, err := answeringCredential(ids, creds[1]); err != nil || i != 1 {
		t.Fatalf("the spare answered: %d, %v", i, err)
	}
	if i, err := answeringCredential(ids[:1], webauthnCredential{}); err != nil || i != 0 {
		t.Fatalf("one allowed credential, none reported: %d, %v", i, err)
	}
	if _, err := answeringCredential(ids, webauthnCredential{}); err == nil {
		t.Fatal("two allowed credentials, none reported: the answer is ambiguous")
	}
	_, other := credentialList([][]byte{[]byte("unknown")})
	if _, err := answeringCredential(ids, other[0]); err == nil || !strings.Contains(err.Error(), "unknown credential") {
		t.Fatalf("an unknown credential: %v", err)
	}
}

// Not parallel: DebugOutput is a package variable.
func TestFido2LogWritesOnlyWithDebugOutput(t *testing.T) {
	prev := DebugOutput
	t.Cleanup(func() { DebugOutput = prev })

	DebugOutput = nil
	fido2Log("silent %d", 1)

	var buf bytes.Buffer
	DebugOutput = &buf
	fido2Log("API version: %d", 7)
	if buf.String() != "[fido2-debug] API version: 7\n" {
		t.Fatalf("got %q", buf.String())
	}
}

// The checks of the platform run on any Windows machine, with or without a
// YubiKey; what they find depends on the machine.
func TestPlatformChecksRunWithoutAYubiKey(t *testing.T) {
	if err := CheckAvailability(); err != nil && !strings.Contains(err.Error(), "Windows 11") {
		t.Fatalf("CheckAvailability: an error must say what Windows is needed, got %v", err)
	}
	paths, err := HIDDevicePaths()
	if err != nil {
		t.Fatalf("HIDDevicePaths: %v", err)
	}
	for _, p := range paths {
		if !strings.Contains(strings.ToUpper(p), fmt.Sprintf("VID_%04X", yubicoVID)) {
			t.Fatalf("a path of another vendor: %s", p)
		}
	}
	if yubiKeyVIDVisible() != (len(paths) > 0) {
		t.Fatal("yubiKeyVIDVisible must agree with HIDDevicePaths")
	}
}

// stubWebAuthn replaces the WebAuthn calls for one test.
func stubWebAuthn(t *testing.T, makeCred func([][]byte) ([]byte, error), getHmac func([][]byte, []byte) (int, []byte, error)) {
	t.Helper()
	prevMake, prevGet := fido2MakeCredFn, fido2GetHmacFn
	t.Cleanup(func() { fido2MakeCredFn, fido2GetHmacFn = prevMake, prevGet })
	fido2MakeCredFn, fido2GetHmacFn = makeCred, getHmac
}

func TestRegistrationReportsWhatTheYubiKeyRefused(t *testing.T) {
	secret := func([][]byte, []byte) (int, []byte, error) { return 0, make([]byte, fido2SaltSize), nil }

	stubWebAuthn(t, func([][]byte) ([]byte, error) { return nil, errors.New("user cancelled") }, secret)
	if _, _, err := CombineWithPassword([]byte("pw"), false); err == nil || !strings.Contains(err.Error(), "credential creation failed") {
		t.Fatalf("cancelled registration: %v", err)
	}

	stubWebAuthn(t, func([][]byte) ([]byte, error) { return []byte("cred"), nil }, func([][]byte, []byte) (int, []byte, error) {
		return 0, nil, errors.New("touch timed out")
	})
	if _, _, err := CombineWithPassword([]byte("pw"), false); err == nil || !strings.Contains(err.Error(), "hmac-secret failed") {
		t.Fatalf("no hmac-secret: %v", err)
	}

	primary := ChallengeData{CredID: base64.StdEncoding.EncodeToString([]byte("primary")), Salt: base64.StdEncoding.EncodeToString(make([]byte, fido2SaltSize))}
	stubWebAuthn(t, func([][]byte) ([]byte, error) { return nil, ErrAlreadyRegistered }, secret)
	if _, _, err := RegisterSpare([]byte("pw"), primary); !errors.Is(err, ErrAlreadyRegistered) {
		t.Fatalf("the primary YubiKey as spare: %v", err)
	}
}

func TestRegisterSpareRefusesADamagedPrimaryChallenge(t *testing.T) {
	salt := base64.StdEncoding.EncodeToString(make([]byte, fido2SaltSize))
	for name, primary := range map[string]ChallengeData{
		"credential": {CredID: "not base64!", Salt: salt},
		"salt":       {CredID: base64.StdEncoding.EncodeToString([]byte("cred")), Salt: base64.StdEncoding.EncodeToString([]byte("short"))},
	} {
		if _, _, err := RegisterSpare([]byte("pw"), primary); err == nil || !strings.Contains(err.Error(), "invalid primary YubiKey") {
			t.Fatalf("damaged %s: %v", name, err)
		}
	}
}

func TestDeriveFIDO2SecretAnyRefusesDamagedChallengesAndUnknownAnswers(t *testing.T) {
	good := ChallengeData{CredID: base64.StdEncoding.EncodeToString([]byte("cred")), Salt: base64.StdEncoding.EncodeToString(make([]byte, fido2SaltSize))}
	for name, cd := range map[string]ChallengeData{
		"credential": {CredID: "", Salt: good.Salt},
		"salt":       {CredID: good.CredID, Salt: "c2hvcnQ="},
	} {
		if _, _, err := DeriveFIDO2SecretAny([]ChallengeData{cd}); err == nil || !strings.Contains(err.Error(), "invalid FIDO2") {
			t.Fatalf("damaged %s: %v", name, err)
		}
	}

	stubWebAuthn(t, nil, func([][]byte, []byte) (int, []byte, error) { return 5, make([]byte, fido2SaltSize), nil })
	if _, _, err := DeriveFIDO2SecretAny([]ChallengeData{good}); err == nil || !strings.Contains(err.Error(), "unknown credential") {
		t.Fatalf("an answer for a credential that was not asked for: %v", err)
	}
}
