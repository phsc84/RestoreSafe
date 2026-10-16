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

func TestAllowListPointsAtTheCredential(t *testing.T) {
	id := []byte("credential-id")
	list, cred := allowList(id)
	if list.cCredentials != 1 || list.pCredentials != cred {
		t.Fatalf("list %+v", list)
	}
	if cred.dwVersion != webauthnCredentialVersion || !bytes.Equal(unsafe.Slice(cred.pbId, cred.cbId), id) || cred.pwszCredentialType == nil {
		t.Fatalf("credential: %+v", cred)
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
	if err := Diagnose().APIErr; err != nil && !strings.Contains(err.Error(), "Windows 11") {
		t.Fatalf("Diagnose: an error of the WebAuthn API must say what Windows is needed, got %v", err)
	}
	devices, err := yubicoHIDDevices()
	if err != nil {
		t.Fatalf("yubicoHIDDevices: %v", err)
	}
	for _, dev := range devices {
		if !strings.Contains(strings.ToUpper(dev.InstanceID), fmt.Sprintf("VID_%04X", yubicoVID)) || dev.Path == "" {
			t.Fatalf("a device of another vendor or without a path: %+v", dev)
		}
	}
	if yubiKeyVIDVisible() != (len(devices) > 0) {
		t.Fatal("yubiKeyVIDVisible must agree with yubicoHIDDevices")
	}
	if d := Diagnose(); d.EnumErr != nil || len(d.Interfaces) != len(devices) {
		t.Fatalf("Diagnose: %d interfaces, error %v; want %d", len(d.Interfaces), d.EnumErr, len(devices))
	}
}

// stubWebAuthn replaces the WebAuthn calls for one test.
func stubWebAuthn(t *testing.T, makeCred func() ([]byte, error), getHmac func(credID, salt []byte) ([]byte, error)) {
	t.Helper()
	prevMake, prevGet := fido2MakeCredFn, fido2GetHmacFn
	t.Cleanup(func() { fido2MakeCredFn, fido2GetHmacFn = prevMake, prevGet })
	fido2MakeCredFn, fido2GetHmacFn = makeCred, getHmac
}

func TestRegistrationReportsWhatTheYubiKeyRefused(t *testing.T) {
	secret := func(_, _ []byte) ([]byte, error) { return make([]byte, fido2SaltSize), nil }

	stubWebAuthn(t, func() ([]byte, error) { return nil, errors.New("user cancelled") }, secret)
	if _, _, err := CombineWithPassword([]byte("pw"), false); err == nil || !strings.Contains(err.Error(), "credential creation failed") {
		t.Fatalf("cancelled registration: %v", err)
	}

	stubWebAuthn(t, func() ([]byte, error) { return []byte("cred"), nil }, func(_, _ []byte) ([]byte, error) {
		return nil, errors.New("touch timed out")
	})
	if _, _, err := CombineWithPassword([]byte("pw"), false); err == nil || !strings.Contains(err.Error(), "hmac-secret failed") {
		t.Fatalf("no hmac-secret: %v", err)
	}
}

func TestDeriveFIDO2SecretRefusesDamagedChallenges(t *testing.T) {
	good := ChallengeData{CredID: base64.StdEncoding.EncodeToString([]byte("cred")), Salt: base64.StdEncoding.EncodeToString(make([]byte, fido2SaltSize))}
	for name, cd := range map[string]ChallengeData{
		"credential": {CredID: "", Salt: good.Salt},
		"salt":       {CredID: good.CredID, Salt: "c2hvcnQ="},
	} {
		if _, err := DeriveFIDO2Secret(cd); err == nil || !strings.Contains(err.Error(), "invalid FIDO2") {
			t.Fatalf("damaged %s: %v", name, err)
		}
	}

	var asked []byte
	stubWebAuthn(t, nil, func(credID, _ []byte) ([]byte, error) {
		asked = credID
		return make([]byte, fido2SaltSize), nil
	})
	if _, err := DeriveFIDO2Secret(good); err != nil || string(asked) != "cred" {
		t.Fatalf("asked for %q: %v", asked, err)
	}
}
