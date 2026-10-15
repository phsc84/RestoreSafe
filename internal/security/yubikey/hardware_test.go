//go:build windows && yubikeyhw

package yubikey

// Manual tests with a real YubiKey (plan of the YubiKey page, phase 2; GUI
// spec YK-11). They show Windows Security prompts, so they never run in CI:
//
//	go test -tags yubikeyhw -count=1 -v -run TestHW<Name> ./internal/security/yubikey/
//
// Each test prints what it found, the WebAuthn trace included; none of them
// fails on what the YubiKey or the user answers.

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"os"
	"testing"
)

func traceToStdout(t *testing.T) {
	t.Helper()
	DebugOutput = os.Stdout
	t.Cleanup(func() { DebugOutput = nil })
}

// logResult prints an error with its HRESULT, if it has one.
func logResult(t *testing.T, what string, err error) {
	t.Helper()
	var hr *HRESULTError
	switch {
	case err == nil:
		t.Logf("%s: OK", what)
	case errors.As(err, &hr):
		t.Logf("%s: HRESULT 0x%08X (cancelled: %v)\n  %v", what, hr.Code, hr.Cancelled(), err)
	default:
		t.Logf("%s: %v (no HRESULT)", what, err)
	}
}

// TestHWDiagnose prints what Diagnose finds; no prompt.
func TestHWDiagnose(t *testing.T) {
	d := Diagnose()
	t.Logf("WebAuthn API version %d (minimum %d), error: %v", d.APIVersion, d.APIMinimum, d.APIErr)
	t.Logf("enumeration error: %v", d.EnumErr)
	for _, i := range d.Interfaces {
		t.Logf("interface %s\n  instance %s\n  device %s\n  usage page 0x%04X usage 0x%04X -> %s\n  PID 0x%04X, REV 0x%04X (%s)\n  write access %v, open error %v",
			i.Path, i.InstanceID, i.Device, i.UsagePage, i.Usage, i.Function(), i.ProductID, i.Revision, FormatRevision(i.Revision), i.WriteAccess, i.OpenErr)
	}
	for _, k := range d.Keys() {
		t.Logf("key: PID 0x%04X, REV %s, FIDO interface %v, functions %+v (known %v)", k.ProductID, FormatRevision(k.Revision), k.FIDO, k.Functions, k.Known)
	}
	for _, e := range d.Drivers {
		t.Logf("driver HID\\%s: %s", e.Key, e.Service)
	}
	if devices, err := yubicoHIDDevices(); err == nil {
		for _, dev := range devices {
			t.Logf("hardware IDs of %s: %q", dev.InstanceID, dev.HardwareIDs)
		}
	}
}

// TestHWCapability registers a throwaway credential and derives one secret:
// PIN and two touches. Cancel in a dialog to record the cancel HRESULT; run
// it on a key without a PIN to see whether Windows asks for a new one.
func TestHWCapability(t *testing.T) {
	traceToStdout(t)
	logResult(t, "ProbeCapability", ProbeCapability())
}

// TestHWMatch registers a credential, then asks for its secret as unlock
// does: the Match test of YK-5. Enter a wrong PIN at the second prompt to
// record the HRESULT of a wrong or blocked PIN.
func TestHWMatch(t *testing.T) {
	traceToStdout(t)
	combined, challengeJSON, err := CombineWithPassword(nil, true)
	clear(combined)
	logResult(t, "registration", err)
	if err != nil {
		return
	}
	cd, err := ParseChallengeJSON(challengeJSON)
	if err != nil {
		t.Fatal(err)
	}
	index, secret, err := DeriveFIDO2SecretAny([]ChallengeData{cd})
	clear(secret)
	logResult(t, "assertion", err)
	t.Logf("answering index %d", index)
}

// TestHWUnknownCredential asks for the secret of a credential no YubiKey
// holds, as an unregistered YubiKey would be asked: what does Windows show,
// and which HRESULT does it return?
func TestHWUnknownCredential(t *testing.T) {
	traceToStdout(t)
	credID := make([]byte, 64)
	salt := make([]byte, fido2SaltSize)
	if _, err := rand.Read(credID); err != nil {
		t.Fatal(err)
	}
	if _, err := rand.Read(salt); err != nil {
		t.Fatal(err)
	}
	cd := ChallengeData{CredID: base64.StdEncoding.EncodeToString(credID), Salt: base64.StdEncoding.EncodeToString(salt)}
	_, secret, err := DeriveFIDO2SecretAny([]ChallengeData{cd})
	clear(secret)
	logResult(t, "assertion with an unknown credential", err)
}
