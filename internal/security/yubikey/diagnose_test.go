//go:build windows

package yubikey

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/phsc84/restoresafe/internal/problem"
	"golang.org/x/sys/windows"
)

// stubDiagnose replaces what Diagnose reads from Windows for one test.
// openErrs maps a path to the error of opening it for writing.
func stubDiagnose(t *testing.T, devices []hidDevice, openErrs map[string]error, version uint32, loadErr error) {
	t.Helper()
	prevDevices, prevOpen, prevDrivers, prevVersion := hidDevicesFn, hidOpenFn, hidDriversFn, webauthnVersionFn
	t.Cleanup(func() {
		hidDevicesFn, hidOpenFn, hidDriversFn, webauthnVersionFn = prevDevices, prevOpen, prevDrivers, prevVersion
	})
	hidDevicesFn = func() ([]hidDevice, error) { return devices, nil }
	hidOpenFn = func(path string) error { return openErrs[path] }
	hidDriversFn = func() []DriverEntry {
		return []DriverEntry{{Key: `VID_1050&PID_0407&MI_01\7&1&0&0000`, Service: "HidUsb"}}
	}
	webauthnVersionFn = func() (uint32, error) { return version, loadErr }
}

// yubiKey5 returns the HID interfaces of a YubiKey 5 with OTP, FIDO and
// smart card on the USB device parent.
func yubiKey5(parent string) []hidDevice {
	return []hidDevice{
		{
			Path:        `\\?\hid#vid_1050&pid_0407&mi_00#` + parent + `#{4d1e55b2}`,
			InstanceID:  `HID\VID_1050&PID_0407&MI_00\` + parent,
			Parent:      `USB\VID_1050&PID_0407\` + parent,
			HardwareIDs: []string{`HID\VID_1050&PID_0407&REV_0543&MI_00`, `HID\VID_1050&UP:0001_U:0006`, `HID_DEVICE_SYSTEM_KEYBOARD`, `HID_DEVICE_UP:0001_U:0006`, `HID_DEVICE`},
		},
		{
			Path:        `\\?\hid#vid_1050&pid_0407&mi_01#` + parent + `#{4d1e55b2}`,
			InstanceID:  `HID\VID_1050&PID_0407&MI_01\` + parent,
			Parent:      `USB\VID_1050&PID_0407\` + parent,
			HardwareIDs: []string{`HID\VID_1050&PID_0407&REV_0543&MI_01`, `HID\VID_1050&UP:F1D0_U:0001`, `HID_DEVICE_UP:F1D0_U:0001`, `HID_DEVICE`},
		},
	}
}

func TestDiagnoseFindsTheFIDOInterfaceByItsUsagePage(t *testing.T) {
	stubDiagnose(t, yubiKey5("1"), map[string]error{yubiKey5("1")[0].Path: windows.ERROR_ACCESS_DENIED}, 4, nil)
	d := Diagnose()
	if d.APIErr != nil || d.APIVersion != 4 || d.APIMinimum != webauthnMinAPIVersion || d.EnumErr != nil {
		t.Fatalf("platform: %+v", d)
	}
	if len(d.Interfaces) != 2 {
		t.Fatalf("interfaces: %+v", d.Interfaces)
	}
	otp, fido := d.Interfaces[0], d.Interfaces[1]
	if otp.Function() != FunctionOTP || fido.Function() != FunctionFIDO {
		t.Fatalf("functions: %v, %v", otp.Function(), fido.Function())
	}
	if fido.ProductID != 0x0407 || fido.Revision != 0x0543 || fido.UsagePage != fidoUsagePage || fido.Usage != 1 {
		t.Fatalf("FIDO interface: %+v", fido)
	}
	if otp.WriteAccess || !errors.Is(otp.OpenErr, windows.ERROR_ACCESS_DENIED) || !fido.WriteAccess || fido.OpenErr != nil {
		t.Fatalf("access: OTP %+v, FIDO %+v", otp, fido)
	}
	if !d.FIDO() || len(d.Drivers) != 1 {
		t.Fatalf("FIDO %v, drivers %v", d.FIDO(), d.Drivers)
	}
	keys := d.Keys()
	if len(keys) != 1 || !keys[0].FIDO || !keys[0].Known || keys[0].Functions != (USBFunctions{OTP: true, FIDO: true, CCID: true}) || keys[0].Revision != 0x0543 {
		t.Fatalf("keys: %+v", keys)
	}
}

func TestDiagnoseFindsFIDOOnAKeyWithoutMI(t *testing.T) {
	sky := hidDevice{
		Path:        `\\?\hid#vid_1050&pid_0120#6&abc&0&0000#{4d1e55b2}`,
		InstanceID:  `HID\VID_1050&PID_0120\6&abc&0&0000`,
		Parent:      `USB\VID_1050&PID_0120\5&def&0&1`,
		HardwareIDs: []string{`HID\VID_1050&PID_0120&REV_0571`, `HID\VID_1050&PID_0120`, `HID\VID_1050&UP:F1D0_U:0001`, `HID_DEVICE_UP:F1D0_U:0001`, `HID_DEVICE`},
	}
	stubDiagnose(t, []hidDevice{sky}, nil, 7, nil)
	d := Diagnose()
	keys := d.Keys()
	if !d.FIDO() || len(keys) != 1 || !keys[0].FIDO || keys[0].Functions != (USBFunctions{FIDO: true}) {
		t.Fatalf("Security Key: %+v", keys)
	}
}

func TestDiagnoseTellsYubiKeysApart(t *testing.T) {
	stubDiagnose(t, append(yubiKey5("1"), yubiKey5("2")...), nil, 4, nil)
	if keys := Diagnose().Keys(); len(keys) != 2 {
		t.Fatalf("two YubiKeys: %+v", keys)
	}

	// Without a USB parent, each interface counts as a key of its own.
	devices := yubiKey5("1")
	for i := range devices {
		devices[i].Parent = ""
	}
	stubDiagnose(t, devices, nil, 4, nil)
	if keys := Diagnose().Keys(); len(keys) != 2 || keys[0].FIDO || !keys[1].FIDO {
		t.Fatalf("without a parent: %+v", keys)
	}
}

func TestDiagnoseWithoutAYubiKeyOrFIDO(t *testing.T) {
	stubDiagnose(t, nil, nil, 4, nil)
	if d := Diagnose(); len(d.Interfaces) != 0 || d.FIDO() || len(d.Keys()) != 0 {
		t.Fatalf("no YubiKey: %+v", d)
	}

	// FIDO turned off: only the keyboard interface is left.
	stubDiagnose(t, yubiKey5("1")[:1], nil, 4, nil)
	if d := Diagnose(); d.FIDO() || len(d.Keys()) != 1 || d.Keys()[0].FIDO {
		t.Fatalf("no FIDO: %+v", d.Keys())
	}
}

func TestDiagnoseReportsAnEnumerationError(t *testing.T) {
	stubDiagnose(t, nil, nil, 4, nil)
	hidDevicesFn = func() ([]hidDevice, error) { return nil, errors.New("SetupDiGetClassDevsEx: denied") }
	if d := Diagnose(); d.EnumErr == nil || len(d.Interfaces) != 0 {
		t.Fatalf("enumeration error: %+v", d)
	}
}

func TestDiagnoseReportsWhatWindowsLacks(t *testing.T) {
	stubDiagnose(t, nil, nil, 0, errors.New("The specified module could not be found."))
	if d := Diagnose(); d.APIErr == nil || !strings.Contains(d.APIErr.Error(), "webauthn.dll not available") || !strings.Contains(d.APIErr.Error(), "22H2") {
		t.Fatalf("missing: %v", d.APIErr)
	}
	stubDiagnose(t, nil, nil, 3, nil)
	if d := Diagnose(); d.APIErr == nil || !strings.Contains(d.APIErr.Error(), "version 3 is too old") || d.APIVersion != 3 {
		t.Fatalf("too old: %v", d.APIErr)
	}
	if err := CheckConnected(); err == nil || !strings.Contains(err.Error(), "too old") {
		t.Fatalf("CheckConnected must report the same error, got %v", err)
	}
}

func TestFunctionsOfProductIDs(t *testing.T) {
	for pid, want := range map[uint16]USBFunctions{
		0x0401: {OTP: true},
		0x0402: {FIDO: true},
		0x0403: {OTP: true, FIDO: true},
		0x0406: {FIDO: true, CCID: true},
		0x0407: {OTP: true, FIDO: true, CCID: true},
		0x0110: {OTP: true},
		0x0116: {OTP: true, FIDO: true, CCID: true},
		0x0120: {FIDO: true},
		0x0410: {OTP: true, FIDO: true},
		0x0010: {OTP: true},
	} {
		if got, known := FunctionsOf(pid); !known || got != want {
			t.Errorf("FunctionsOf(%#04x) = %+v, %v; want %+v", pid, got, known, want)
		}
	}
	if _, known := FunctionsOf(0x9999); known {
		t.Error("an unknown product ID must not be known")
	}
}

func TestParseHardwareIDsIgnoresWhatIsMissing(t *testing.T) {
	var iface Interface
	parseHardwareIDs(&iface, []string{`HID\VID_1050&PID_04`, `HID_DEVICE`, `HID\VID_1050&UP:ZZZZ`})
	if iface != (Interface{}) {
		t.Fatalf("parsed from incomplete IDs: %+v", iface)
	}
}

func TestFormatRevision(t *testing.T) {
	if got := FormatRevision(0x0543); got != "5.4.3" {
		t.Fatalf("FormatRevision = %q", got)
	}
	if FunctionFIDO.String() != "FIDO" || FunctionOTP.String() != "OTP" || FunctionOther.String() != "other" {
		t.Fatal("function names")
	}
}

func TestHRESULTErrorSurvivesTheWrapping(t *testing.T) {
	cause := problem.Errorf("%w.", &HRESULTError{Op: "WebAuthNAuthenticatorGetAssertion", Code: hresultNTEUserCancelled}).WithRemedy("Ensure a YubiKey registered for this backup is connected.")
	if !strings.HasPrefix(cause.Error(), "WebAuthNAuthenticatorGetAssertion: HRESULT 0x80090036.") {
		t.Fatalf("text: %q", cause.Error())
	}
	stubWebAuthn(t, nil, func(_, _ []byte) ([]byte, error) { return nil, cause })
	good := ChallengeData{CredID: base64.StdEncoding.EncodeToString([]byte("cred")), Salt: base64.StdEncoding.EncodeToString(make([]byte, fido2SaltSize))}
	_, err := DeriveFIDO2Secret(good)
	var hr *HRESULTError
	if !errors.As(fmt.Errorf("YubiKey authentication failed: %w", err), &hr) || !hr.Cancelled() || hr.Code != hresultNTEUserCancelled {
		t.Fatalf("errors.As through the wrapping: %v", err)
	}
	for code, cancelled := range map[uint32]bool{hresultNTEUserCancelled: true, hresultCancelled: true, 0x80090029: false} {
		if (&HRESULTError{Code: code}).Cancelled() != cancelled {
			t.Errorf("Cancelled() for %#x", code)
		}
	}
}

func TestProbeCapabilityZeroesTheSecret(t *testing.T) {
	var secret []byte
	stubWebAuthn(t, func() ([]byte, error) {
		return []byte("cred"), nil
	}, func(credID, salt []byte) ([]byte, error) {
		if string(credID) != "cred" || len(salt) != fido2SaltSize || bytes.Equal(salt, make([]byte, fido2SaltSize)) {
			return nil, fmt.Errorf("asked for %q with salt %x", credID, salt)
		}
		secret = bytes.Repeat([]byte{0xAB}, fido2SaltSize)
		return secret, nil
	})
	if err := ProbeCapability(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(secret, make([]byte, fido2SaltSize)) {
		t.Fatalf("secret after the probe %x", secret)
	}

	stubWebAuthn(t, func() ([]byte, error) {
		return nil, &HRESULTError{Op: "WebAuthNAuthenticatorMakeCredential", Code: hresultCancelled}
	}, nil)
	var hr *HRESULTError
	if err := ProbeCapability(); !errors.As(err, &hr) || !hr.Cancelled() {
		t.Fatalf("cancelled probe: %v", err)
	}
}
