package view

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/phsc84/restoresafe/internal/config"
	"github.com/phsc84/restoresafe/internal/security/yubikey"
	"github.com/phsc84/restoresafe/internal/workflow/health"
	"github.com/phsc84/restoresafe/internal/workflow/keycheck"
)

var ykNow = time.Date(2026, 10, 16, 9, 30, 0, 0, time.Local)

// yubiKey5 is the diagnosis of one YubiKey 5 with firmware rev and the
// given interfaces; fido adds its FIDO interface.
func yubiKey5(rev uint16, fido bool) *yubikey.Diagnosis {
	d := &yubikey.Diagnosis{APIVersion: 9, APIMinimum: 4}
	d.Interfaces = append(d.Interfaces, yubikey.Interface{Path: `\\?\hid#vid_1050&pid_0407&mi_00#kbd`, Device: "usb1", UsagePage: 0x01, Usage: 0x06, ProductID: 0x0407, Revision: rev, WriteAccess: true})
	if fido {
		d.Interfaces = append(d.Interfaces, yubikey.Interface{Path: `\\?\hid#vid_1050&pid_0407&mi_01#fido`, Device: "usb1", UsagePage: 0xF1D0, Usage: 0x01, ProductID: 0x0407, Revision: rev, OpenErr: errors.New("Access is denied.")})
	}
	d.Drivers = []yubikey.DriverEntry{{Key: `VID_1050&PID_0407&MI_00\8&1&0&0000`, Service: "kbdhid"}}
	return d
}

// snapshotWith is a snapshot whose keys are k.
func snapshotWith(k health.KeysSummary) *health.Snapshot { return &health.Snapshot{Keys: k} }

var (
	yubiKeys    = health.KeysSummary{Exists: true, Created: time.Date(2026, 9, 1, 8, 0, 0, 0, time.Local), YubiKey: true, YubiKeyInBackups: true}
	noKeys      = health.KeysSummary{}
	passwordNow = health.KeysSummary{Exists: true, YubiKeyInBackups: true}
)

func result(kind keycheck.Kind, err error, cancelled bool, hresult uint32) *keycheck.Result {
	return &keycheck.Result{Kind: kind, Time: ykNow.Add(-38 * time.Minute), Err: err, Cancelled: cancelled, HRESULT: hresult, Trace: "[fido2-debug] hmac-secret output length: 32 bytes\n"}
}

func TestYubiKeyVisibleWhileYubiKeysAreInUse(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		mode     config.AuthMode
		s        *health.Snapshot
		want     bool
		describe string
	}{
		{config.AuthModePassword, nil, false, "password, first check running"},
		{config.AuthModePassword, snapshotWith(noKeys), false, "password, no YubiKey backups"},
		{config.AuthModePassword, snapshotWith(passwordNow), true, "password now, YubiKey backups before"},
		{config.AuthModePasswordYubiKey, nil, true, "password + YubiKey"},
		{config.AuthModeYubiKey, snapshotWith(noKeys), true, "YubiKey only, no backups yet"},
	} {
		if got := YubiKeyVisible(&config.Config{AuthenticationMode: tc.mode}, tc.s); got != tc.want {
			t.Errorf("%s: visible %v, want %v", tc.describe, got, tc.want)
		}
	}
}

func TestYubiKeyHeroVariants(t *testing.T) {
	t.Parallel()
	windows := yubiKey5(0x0574, true)
	windows.APIErr = errors.New("webauthn.dll API version 3 is too old (need 4+).")
	for _, tc := range []struct {
		name   string
		keys   health.KeysSummary
		st     YubiKeyState
		tone   Tone
		title  string
		line   string // a part of the line
		action Action
	}{
		{"checking", yubiKeys, YubiKeyState{}, ToneNeutral, "Checking your YubiKey…", "", ActionNone},
		{"Windows", yubiKeys, YubiKeyState{Diagnosis: windows}, ToneError, "Windows can't use your YubiKey", "22H2", ActionOpenWindowsUpdate},
		{"not connected", yubiKeys, YubiKeyState{Diagnosis: &yubikey.Diagnosis{APIVersion: 9}}, ToneNeutral, "Connect your YubiKey", "USB port", ActionNone},
		{"no FIDO2", yubiKeys, YubiKeyState{Diagnosis: yubiKey5(0x0574, false)}, ToneError, "This YubiKey can't protect your backups", "FIDO2 function", ActionNone},
		{"old firmware", yubiKeys, YubiKeyState{Diagnosis: yubiKey5(0x0437, true)}, ToneError, "This YubiKey can't protect your backups", "firmware 4.3.7 is older than 5.2", ActionNone},
		{"connected, keys", yubiKeys, YubiKeyState{Diagnosis: yubiKey5(0x0574, true)}, ToneNeutral, "Your YubiKey is connected", "PIN and a touch", ActionTestYubiKey},
		{"connected, no keys", noKeys, YubiKeyState{Diagnosis: yubiKey5(0x0574, true)}, ToneNeutral, "Your YubiKey is connected", "PIN twice and two touches", ActionTestYubiKey},
		{"testing", yubiKeys, YubiKeyState{Diagnosis: yubiKey5(0x0574, true), Testing: true}, ToneNeutral, "Follow the Windows Security prompt", "", ActionTestYubiKey},
		{"match", yubiKeys, YubiKeyState{Diagnosis: yubiKey5(0x0574, true), Test: result(keycheck.Match, nil, false, 0)}, ToneSuccess, "Your YubiKey opens your backups", "Tested today, 08:52", ActionTestYubiKey},
		{"ready", noKeys, YubiKeyState{Diagnosis: yubiKey5(0x0574, true), Test: result(keycheck.Capability, nil, false, 0)}, ToneSuccess, "Your YubiKey is ready", "first backup registers it", ActionTestYubiKey},
		{"cancelled", yubiKeys, YubiKeyState{Diagnosis: yubiKey5(0x0574, true), Test: result(keycheck.Match, errors.New("cancelled"), true, 0x800704C7)}, ToneNeutral, "The test was cancelled", "entered wrong too often", ActionTestYubiKey},
		{"failed", yubiKeys, YubiKeyState{Diagnosis: yubiKey5(0x0574, true), Test: result(keycheck.Match, errors.New("failed"), false, 0x80090029)}, ToneError, "The test didn't work", "error code", ActionTestYubiKey},
	} {
		p := YubiKeyPageOf(snapshotWith(tc.keys), tc.st, ykNow)
		h := p.Hero
		if h.Tone != tc.tone || h.Title != tc.title || !strings.Contains(h.Line, tc.line) || h.Primary.Action != tc.action {
			t.Errorf("%s: %+v", tc.name, h)
		}
		p.Details = "" // written for a bug report, not in the page's words
		checkWriting(t, p)
	}
}

func TestYubiKeyTestIsDisabledWhileBusy(t *testing.T) {
	t.Parallel()
	d := yubiKey5(0x0574, true)
	p := YubiKeyPageOf(snapshotWith(yubiKeys), YubiKeyState{Diagnosis: d}, ykNow)
	if !p.Hero.Primary.Enabled || !p.CheckAgain.Enabled {
		t.Fatalf("idle: %+v, %+v", p.Hero.Primary, p.CheckAgain)
	}
	p = YubiKeyPageOf(snapshotWith(yubiKeys), YubiKeyState{Diagnosis: d, Testing: true}, ykNow)
	if p.Hero.Primary.Enabled || p.CheckAgain.Enabled || p.Hero.Primary.Reason == "" || p.CheckAgain.Reason == "" {
		t.Fatalf("testing: %+v, %+v", p.Hero.Primary, p.CheckAgain)
	}
	p = YubiKeyPageOf(snapshotWith(yubiKeys), YubiKeyState{Diagnosis: d, Busy: true}, ykNow)
	if p.Hero.Primary.Enabled || p.Hero.Primary.Reason != reasonTestBusy || !p.CheckAgain.Enabled {
		t.Fatalf("operation running: %+v, %+v", p.Hero.Primary, p.CheckAgain)
	}
}

func TestYubiKeyChecksLines(t *testing.T) {
	t.Parallel()
	text := func(keys health.KeysSummary, st YubiKeyState) []string {
		var out []string
		for _, l := range YubiKeyPageOf(snapshotWith(keys), st, ykNow).Checks {
			out = append(out, fmt.Sprintf("%s: %s", l.Label, l.Text))
		}
		return out
	}
	if got := text(yubiKeys, YubiKeyState{}); got != nil {
		t.Fatalf("before the first check: %q", got)
	}

	d := yubiKey5(0x0574, true)
	want := []string{"Windows: Ready for YubiKeys", "Connected: 1 YubiKey with FIDO2, OTP and smart card, firmware 5.7.4", "Your keys: Not tested"}
	if got := text(yubiKeys, YubiKeyState{Diagnosis: d}); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("connected: %q", got)
	}

	for _, tc := range []struct {
		name string
		keys health.KeysSummary
		st   YubiKeyState
		want string
	}{
		{"Windows too old", yubiKeys, YubiKeyState{Diagnosis: &yubikey.Diagnosis{APIErr: errors.New("old")}}, "Windows: Needs Windows 11 version 22H2 or later"},
		{"none connected", yubiKeys, YubiKeyState{Diagnosis: &yubikey.Diagnosis{}}, "Connected: No YubiKey connected"},
		{"no FIDO", yubiKeys, YubiKeyState{Diagnosis: yubiKey5(0x0574, false)}, "Connected: No FIDO2 function"},
		{"old firmware", yubiKeys, YubiKeyState{Diagnosis: yubiKey5(0x0437, true)}, "Connected: Firmware 4.3.7: FIDO2 needs 5.2 or later"},
		{"two keys", yubiKeys, YubiKeyState{Diagnosis: twoKeys()}, "Connected: 2 YubiKeys"},
		{"no keys", noKeys, YubiKeyState{Diagnosis: d}, "Your keys: None yet. Your first backup registers your YubiKey."},
		{"older keys", passwordNow, YubiKeyState{Diagnosis: d}, "Your keys: Your current keys use no YubiKey; older backups do."},
		{"answered", yubiKeys, YubiKeyState{Diagnosis: d, Test: result(keycheck.Match, nil, false, 0)}, "Your keys: Your YubiKey answered (keys created Tue 1 Sep 2026)"},
		{"cancelled", yubiKeys, YubiKeyState{Diagnosis: d, Test: result(keycheck.Match, errors.New("c"), true, 0x800704C7)}, "Your keys: The test was cancelled"},
		{"failed", yubiKeys, YubiKeyState{Diagnosis: d, Test: result(keycheck.Match, errors.New("f"), false, 1)}, "Your keys: The test didn't work"},
		{"a capability result", yubiKeys, YubiKeyState{Diagnosis: d, Test: result(keycheck.Capability, nil, false, 0)}, "Your keys: Not tested"},
	} {
		if got := text(tc.keys, tc.st); !strings.Contains(strings.Join(got, "|"), tc.want) {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}

func twoKeys() *yubikey.Diagnosis {
	d := yubiKey5(0x0574, true)
	other := yubiKey5(0x0574, true)
	for _, i := range other.Interfaces {
		i.Device = "usb2"
		d.Interfaces = append(d.Interfaces, i)
	}
	return d
}

func TestConnectedTextWithoutKnownFunctionsOrFirmware(t *testing.T) {
	t.Parallel()
	if got := connectedText([]yubikey.Key{{ProductID: 0x9999}}); got != "1 YubiKey" {
		t.Fatalf("unknown product ID: %q", got)
	}
	if got := connectedText([]yubikey.Key{{ProductID: 0x0120, Known: true, Functions: yubikey.USBFunctions{FIDO: true}, Revision: 0x0571}}); got != "1 YubiKey with FIDO2, firmware 5.7.1" {
		t.Fatalf("Security Key: %q", got)
	}
}

func TestYubiKeyGuideOpensWhenItHelps(t *testing.T) {
	t.Parallel()
	d := yubiKey5(0x0574, true)
	for _, tc := range []struct {
		name string
		keys health.KeysSummary
		st   YubiKeyState
		want bool
	}{
		{"no YubiKey keys", noKeys, YubiKeyState{Diagnosis: d}, true},
		{"YubiKey keys", yubiKeys, YubiKeyState{Diagnosis: d}, false},
		{"no FIDO2", yubiKeys, YubiKeyState{Diagnosis: yubiKey5(0x0574, false)}, true},
		{"old firmware", yubiKeys, YubiKeyState{Diagnosis: yubiKey5(0x0437, true)}, true},
		{"passed", yubiKeys, YubiKeyState{Diagnosis: d, Test: result(keycheck.Match, nil, false, 0)}, false},
		{"cancelled", yubiKeys, YubiKeyState{Diagnosis: d, Test: result(keycheck.Match, errors.New("c"), true, 0x800704C7)}, true},
		{"failed", yubiKeys, YubiKeyState{Diagnosis: d, Test: result(keycheck.Match, errors.New("f"), false, 1)}, true},
	} {
		g := YubiKeyPageOf(snapshotWith(tc.keys), tc.st, ykNow).Guide
		if g.Expanded != tc.want {
			t.Errorf("%s: expanded %v, want %v", tc.name, g.Expanded, tc.want)
		}
		if len(g.Steps) != 4 || g.Steps[1].Action == nil || g.Steps[1].Action.Action != ActionOpenSignInSettings || g.FootnoteLink.Action != ActionGetAuthenticator {
			t.Fatalf("%s: %+v", tc.name, g)
		}
	}
}

func TestYubiKeyDetailsForABugReport(t *testing.T) {
	t.Parallel()
	d := yubiKey5(0x0574, true)
	st := YubiKeyState{Diagnosis: d, AppVersion: "2.1.0", WindowsVersion: "10.0.26200",
		Test: result(keycheck.Match, errors.New("FIDO2 hmac-secret failed: WebAuthNAuthenticatorGetAssertion: HRESULT 0x800704C7."), true, 0x800704C7)}
	got := YubiKeyDetails(st)
	for _, want := range []string{
		"RestoreSafe 2.1.0", "Windows 10.0.26200", "WebAuthn API version 9 (needs 4)",
		`FIDO  \\?\hid#vid_1050&pid_0407&mi_01#fido`, "product ID 0x0407, USB device version 0x0574 (5.7.4), usage page 0xF1D0",
		"write access denied: Access is denied.", "write access granted", `VID_1050&PID_0407&MI_00\8&1&0&0000: kbdhid`,
		"Match, Fri 16 Oct 2026, 08:52:00, cancelled", "HRESULT 0x800704C7", "WebAuthn trace", "hmac-secret output length: 32 bytes",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("details lack %q:\n%s", want, got)
		}
	}

	// Without a diagnosis or a test, the details say so.
	got = YubiKeyDetails(YubiKeyState{AppVersion: "2.1.0"})
	if !strings.Contains(got, "Not checked yet.") || !strings.Contains(got, "Last test\n  None") {
		t.Fatalf("empty details:\n%s", got)
	}
	windows := &yubikey.Diagnosis{APIMinimum: 4, APIErr: errors.New("webauthn.dll not available"), EnumErr: errors.New("SetupDiGetClassDevsEx: denied")}
	got = YubiKeyDetails(YubiKeyState{Diagnosis: windows, Test: result(keycheck.Capability, nil, false, 0)})
	for _, want := range []string{"WebAuthn: webauthn.dll not available", "Listing failed: SetupDiGetClassDevsEx: denied\n  None", "Capability", "passed"} {
		if !strings.Contains(got, want) {
			t.Errorf("details lack %q:\n%s", want, got)
		}
	}
	got = YubiKeyDetails(YubiKeyState{Diagnosis: d, Test: result(keycheck.Match, errors.New("failed"), false, 0x80090029)})
	if !strings.Contains(got, "failed") || !strings.Contains(got, "HRESULT 0x80090029") {
		t.Fatalf("failed test:\n%s", got)
	}
}

func TestCheckYubiKeyLinks(t *testing.T) {
	t.Parallel()
	yk := &config.Config{AuthenticationMode: config.AuthModeYubiKey}
	pw := &config.Config{AuthenticationMode: config.AuthModePassword}
	s := snapshotWith(noKeys)
	if c := keysOf(s, yk); c.Link == nil || c.Link.Action != ActionOpenYubiKeyPage {
		t.Fatalf("Keys card with a YubiKey: %+v", c.Link)
	}
	if c := keysOf(s, pw); c.Link != nil {
		t.Fatalf("Keys card without a YubiKey: %+v", c.Link)
	}
	if p := SettingsOf(yk, "config.yaml", "C:/Backup", s, nil, false, ""); p.Keys.Link == nil || p.Keys.Link.Text != "Check your YubiKey" {
		t.Fatalf("Settings with a YubiKey: %+v", p.Keys.Link)
	}
	if p := SettingsOf(pw, "config.yaml", "C:/Backup", s, nil, false, ""); p.Keys.Link != nil {
		t.Fatalf("Settings without a YubiKey: %+v", p.Keys.Link)
	}
}
