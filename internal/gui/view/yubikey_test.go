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
	want := []string{"Windows: Ready for YubiKeys", "Connected: 1 YubiKey, firmware 5.7.4", "FIDO2: On", "Your keys: Not tested"}
	if got := text(yubiKeys, YubiKeyState{Diagnosis: d}); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("connected: %q", got)
	}
	// More than one YubiKey blocks backups (decision 12): an error.
	if l := YubiKeyPageOf(snapshotWith(yubiKeys), YubiKeyState{Diagnosis: twoKeys()}, ykNow).Checks[1]; l.Tone != ToneError || l.Glyph != GlyphError || !strings.Contains(l.Text, "Connect only the one you back up with") {
		t.Fatalf("two YubiKeys: %+v", l)
	}
	// Which YubiKeys work is noted under Connected, connected or not.
	for _, st := range []YubiKeyState{{Diagnosis: d}, {Diagnosis: &yubikey.Diagnosis{}}, {Diagnosis: yubiKey5(0x0437, true)}} {
		if l := YubiKeyPageOf(snapshotWith(yubiKeys), st, ykNow).Checks[1]; l.Label != "Connected" || !strings.Contains(l.Note, "YubiKey 5 Series") {
			t.Fatalf("Connected note: %+v", l)
		}
	}

	for _, tc := range []struct {
		name string
		keys health.KeysSummary
		st   YubiKeyState
		want string
	}{
		{"Windows too old", yubiKeys, YubiKeyState{Diagnosis: &yubikey.Diagnosis{APIErr: errors.New("old")}}, "Windows: Needs Windows 11 version 22H2 or later"},
		{"none connected", yubiKeys, YubiKeyState{Diagnosis: &yubikey.Diagnosis{}}, "Connected: No YubiKey connected|FIDO2: Shown when a YubiKey is connected"},
		{"no FIDO", yubiKeys, YubiKeyState{Diagnosis: yubiKey5(0x0574, false)}, "Connected: 1 YubiKey, firmware 5.7.4|FIDO2: Off or missing"},
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

func TestConnectedTextWithoutFirmware(t *testing.T) {
	t.Parallel()
	if got := connectedText([]yubikey.Key{{ProductID: 0x9999}}); got != "1 YubiKey" {
		t.Fatalf("no USB device version: %q", got)
	}
	if got := connectedText([]yubikey.Key{{ProductID: 0x0120, Revision: 0x0571}}); got != "1 YubiKey, firmware 5.7.1" {
		t.Fatalf("Security Key: %q", got)
	}
}

func TestYubiKeyHelpIsAlwaysThere(t *testing.T) {
	t.Parallel()
	for _, st := range []YubiKeyState{{}, {Diagnosis: yubiKey5(0x0574, true)}, {Diagnosis: yubiKey5(0x0574, false)}} {
		g := YubiKeyPageOf(snapshotWith(yubiKeys), st, ykNow).Guide
		if g.Title != "YubiKey help" || len(g.Steps) != 2 || len(g.Steps[0].Points) != 3 || len(g.Steps[1].Points) != 3 || g.Steps[0].Action == nil || g.Steps[0].Action.Action != ActionOpenSignInSettings || g.Steps[1].Action == nil || g.Steps[1].Action.Action != ActionOpenAuthenticator {
			t.Fatalf("%+v", g)
		}
	}
}

// The technical details go from the overview to the raw data, in sections
// with the values in one column.
func TestYubiKeyDetailsForABugReport(t *testing.T) {
	t.Parallel()
	d := yubiKey5(0x0574, true)
	st := YubiKeyState{Diagnosis: d, AppVersion: "2.1.0", WindowsVersion: "10.0.26200",
		Test: result(keycheck.Match, errors.New("FIDO2 hmac-secret failed: WebAuthNAuthenticatorGetAssertion: HRESULT 0x800704C7."), true, 0x800704C7)}
	keys := yubiKeys
	keys.Methods, keys.RecoveryCode = "password + YubiKey", true
	got := YubiKeyDetails(keys, st)
	want := []string{
		"RestoreSafe 2.1.0\n\nSUMMARY\n",
		"  Windows         Ready for YubiKeys\n",
		"  YubiKeys        1 connected\n",
		"  FIDO2           On\n",
		"  Firmware        5.7.4 (RestoreSafe needs 5.2 or later)\n",
		"  Your keys       Password + YubiKey, created 2026-09-01, with recovery code\n",
		"  In backups      Some backups need a YubiKey\n",
		"  Last test       Match, Fri 16 Oct 2026, 08:52:00: cancelled\n",
		"\nYUBIKEY 1\n  Product ID      0x0407\n  Firmware        5.7.4 (USB device version 0x0574)\n  USB device      usb1\n",
		"  Interface       FIDO  usage page 0xF1D0, usage 0x0001, write access denied (normal: Windows keeps the FIDO interface for itself)\n                  " + `\\?\hid#vid_1050&pid_0407&mi_01#fido` + "\n",
		"  Interface       OTP   usage page 0x0001, usage 0x0006, write access granted\n",
		"\nLAST TEST\n  Kind            Match: asks the YubiKey",
		"  Result          Cancelled\n",
		"  Error code      0x800704C7: ended as cancelled.",
		"  Message         FIDO2 hmac-secret failed",
		"  WebAuthn trace  [fido2-debug] hmac-secret output length: 32 bytes\n",
		"\nWINDOWS\n  Version         Windows 11 (10.0.26200)\n  WebAuthn API    9 (RestoreSafe needs 4)\n",
		"  HID drivers     " + `VID_1050&PID_0407&MI_00\8&1&0&0000` + ": kbdhid\n",
	}
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("details lack %q:\n%s", w, got)
		}
	}

	// Before the first check, without keys or a test: the summary says so.
	got = YubiKeyDetails(noKeys, YubiKeyState{AppVersion: "2.1.0", WindowsVersion: "10.0.19045"})
	for _, w := range []string{"  Windows         Not checked yet\n", "  YubiKeys        Not checked yet\n", "  Your keys       None yet: your first backup creates them\n",
		"  In backups      No backup needs a YubiKey\n", "  Last test       None in this session\n", "  Version         Windows 10 (10.0.19045)\n"} {
		if !strings.Contains(got, w) {
			t.Errorf("empty details lack %q:\n%s", w, got)
		}
	}
	if strings.Contains(got, "LAST TEST") || strings.Contains(got, "YUBIKEY 1") || strings.Contains(got, "WebAuthn API") {
		t.Fatalf("sections without data:\n%s", got)
	}

	// Windows too old, no YubiKey, a listing error, a failed capability test.
	windows := &yubikey.Diagnosis{APIMinimum: 4, APIErr: errors.New("webauthn.dll not available"), EnumErr: errors.New("SetupDiGetClassDevsEx: denied")}
	got = YubiKeyDetails(noKeys, YubiKeyState{Diagnosis: windows, WindowsVersion: "odd", Test: result(keycheck.Capability, errors.New("failed"), false, 0x12345678)})
	for _, w := range []string{"Not ready: needs Windows 11 version 22H2 or later", "  YubiKeys        None connected\n", "  FIDO2           Unknown: no YubiKey connected\n",
		"Capability, Fri 16 Oct 2026, 08:52:00: failed", "  Kind            Capability: registers", "  Error code      0x12345678\n",
		"  Version         odd\n", "  WebAuthn error  webauthn.dll not available\n", "  Listing error   SetupDiGetClassDevsEx: denied\n", "  HID drivers     None\n"} {
		if !strings.Contains(got, w) {
			t.Errorf("details lack %q:\n%s", w, got)
		}
	}

	// Two YubiKeys, old firmware, an interface that can't be opened, a
	// passed test.
	two := twoKeys()
	two.Interfaces[0].WriteAccess, two.Interfaces[0].OpenErr = false, errors.New("in use")
	old := yubiKey5(0x0437, true)
	got = YubiKeyDetails(yubiKeys, YubiKeyState{Diagnosis: two, Test: result(keycheck.Match, nil, false, 0)})
	for _, w := range []string{"2 connected: no backup until only one is", "YUBIKEY 2", "write access denied: in use", "Match, Fri 16 Oct 2026, 08:52:00: passed", "  Result          Passed\n"} {
		if !strings.Contains(got, w) {
			t.Errorf("details lack %q:\n%s", w, got)
		}
	}
	if got := YubiKeyDetails(yubiKeys, YubiKeyState{Diagnosis: old}); !strings.Contains(got, "4.3.7: too old, RestoreSafe needs 5.2 or later") {
		t.Fatalf("old firmware:\n%s", got)
	}
	if got := windowsName("10.0.x"); got != "10.0.x" {
		t.Fatalf("windowsName of an odd version: %q", got)
	}
}

// With a YubiKey, the Keys card of Create backup says how the first backup
// sets it up, and never to reset it (OV-6).
func TestKeysCardWithAYubiKey(t *testing.T) {
	t.Parallel()
	yk := &config.Config{AuthenticationMode: config.AuthModeYubiKey}
	pw := &config.Config{AuthenticationMode: config.AuthModePassword}
	s := snapshotWith(noKeys)
	if c := keysOf(s, pw); c.Setup != "" || c.Warning != "" {
		t.Fatalf("Keys card without a YubiKey: %+v", c)
	}
	if c := keysOf(s, yk); !strings.Contains(c.Setup, "first backup sets up your YubiKey") || !strings.Contains(c.Warning, "Never reset your YubiKey") {
		t.Fatalf("Keys card with a YubiKey, no keys yet: %+v", c)
	}
	if c := keysOf(snapshotWith(yubiKeys), yk); c.Setup != "" || c.Warning == "" {
		t.Fatalf("Keys card with YubiKey keys: %+v", c)
	}
}
