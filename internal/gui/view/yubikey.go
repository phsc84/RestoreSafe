package view

import (
	"fmt"
	"strings"
	"time"

	"github.com/phsc84/restoresafe/internal/config"
	"github.com/phsc84/restoresafe/internal/security/yubikey"
	"github.com/phsc84/restoresafe/internal/workflow/health"
	"github.com/phsc84/restoresafe/internal/workflow/keycheck"
)

// minFirmware is the first YubiKey firmware with what RestoreSafe needs
// (FIDO2 with hmac-secret), as USB device version (GUI spec YK-4).
const minFirmware = 0x0520

// CheckLine is one line of the Checks card (GUI spec YK-4).
type CheckLine struct {
	Label, Text string
	Tone        Tone
	Glyph       Glyph
	// Note is small text under the line, "" for none.
	Note string
}

// GuideStep is a part of the YubiKey help: what to do, and its button.
type GuideStep struct {
	Title, Text string
	// Action is the step's button, if it has one.
	Action *Button
}

// YubiKeyGuide is the card "YubiKey help" (GUI spec YK-8).
type YubiKeyGuide struct {
	Title    string
	Expanded bool
	Steps    []GuideStep
}

// YubiKeyPage is everything the YubiKey page shows (GUI spec 10.2).
type YubiKeyPage struct {
	Title      string
	CheckAgain Button
	Hero       Hero
	// ChecksTitle and Checks are the Checks card; no lines until the first
	// diagnosis.
	ChecksTitle string
	Checks      []CheckLine
	Guide       YubiKeyGuide
	// DetailsTitle and Details are the technical details (YK-9).
	DetailsTitle string
	Details      string
	CopyDetails  Button
}

// YubiKeyState is what the window knows about the YubiKey besides the
// snapshot.
type YubiKeyState struct {
	// Diagnosis is the last automatic check, nil until the first one
	// (YK-3).
	Diagnosis *yubikey.Diagnosis
	// Test is the result of the last test of this session, nil when there
	// is none (YK-6).
	Test *keycheck.Result
	// Testing is set while a test runs; Busy while an operation runs.
	Testing, Busy bool
	// AppVersion and WindowsVersion head the technical details.
	AppVersion, WindowsVersion string
}

// YubiKeyVisible tells whether the sidebar shows the YubiKey page: when the
// configuration uses a YubiKey or any backup's keys have one (YK-1).
func YubiKeyVisible(cfg *config.Config, s *health.Snapshot) bool {
	return cfg.UseYubiKey() || s != nil && s.Keys.YubiKeyInBackups
}

// YubiKeyPageOf computes the YubiKey page from the snapshot s (nil while
// the first check runs) and the state of the YubiKey.
func YubiKeyPageOf(s *health.Snapshot, st YubiKeyState, now time.Time) YubiKeyPage {
	var keys health.KeysSummary
	if s != nil {
		keys = s.Keys
	}
	p := YubiKeyPage{
		Title:        navYubiKey,
		CheckAgain:   Button{Text: buttonCheckAgain, Action: ActionCheckAgain, Enabled: !st.Testing},
		Hero:         yubiKeyHero(keys, st, now),
		ChecksTitle:  ykChecksTitle,
		Checks:       yubiKeyChecks(keys, st),
		Guide:        yubiKeyGuide(st),
		DetailsTitle: ykDetailsTitle,
		Details:      YubiKeyDetails(st),
		CopyDetails:  Button{Text: buttonCopyDetails, Action: ActionCopyDetails, Enabled: true},
	}
	if st.Testing {
		p.CheckAgain.Reason = reasonTestRunning
	}
	return p
}

// fault is what keeps the connected YubiKey from being tested, in the order
// the hero checks it.
type fault int

const (
	faultNone fault = iota
	faultChecking
	faultWindows
	faultNoKey
	faultNoFIDO
	faultFirmware
)

// faultOf returns the first fault of the diagnosis, and the firmware of a
// YubiKey that is too old.
func faultOf(d *yubikey.Diagnosis) (fault, string) {
	switch {
	case d == nil:
		return faultChecking, ""
	case d.APIErr != nil:
		return faultWindows, ""
	}
	return keyFault(d)
}

// keyFault returns what is wrong with the connected YubiKey, whatever
// Windows has.
func keyFault(d *yubikey.Diagnosis) (fault, string) {
	switch {
	case len(d.Keys()) == 0:
		return faultNoKey, ""
	case !d.FIDO():
		return faultNoFIDO, ""
	}
	if k := fidoKey(d); k.Revision != 0 && k.Revision < minFirmware {
		return faultFirmware, yubikey.FormatRevision(k.Revision)
	}
	return faultNone, ""
}

// fidoKey returns the first connected YubiKey with a FIDO interface.
func fidoKey(d *yubikey.Diagnosis) yubikey.Key {
	for _, k := range d.Keys() {
		if k.FIDO {
			return k
		}
	}
	return yubikey.Key{}
}

func yubiKeyHero(keys health.KeysSummary, st YubiKeyState, now time.Time) Hero {
	f, firmware := faultOf(st.Diagnosis)
	switch f {
	case faultChecking:
		return Hero{Tone: ToneNeutral, Glyph: GlyphKey, Title: ykChecking}
	case faultWindows:
		return Hero{Tone: ToneError, Glyph: GlyphError, Title: ykWindows, Line: ykWindowsLine,
			Primary: Button{Text: buttonWindowsUpdate, Action: ActionOpenWindowsUpdate, Enabled: true}}
	case faultNoKey:
		return Hero{Tone: ToneNeutral, Glyph: GlyphKey, Title: ykConnect, Line: ykConnectLine}
	case faultNoFIDO:
		return Hero{Tone: ToneError, Glyph: GlyphError, Title: ykCantProtect, Line: ykNoFIDOLine}
	case faultFirmware:
		return Hero{Tone: ToneError, Glyph: GlyphError, Title: ykCantProtect, Line: fmt.Sprintf(ykOldFirmwareLine, firmware)}
	}

	test := Button{Text: buttonTestYubiKey, Action: ActionTestYubiKey, Enabled: !st.Testing && !st.Busy}
	switch {
	case st.Testing:
		test.Reason = reasonTestRunning
		return Hero{Tone: ToneNeutral, Glyph: GlyphKey, Title: ykPrompt, Primary: test}
	case st.Busy:
		test.Reason = reasonTestBusy
	}
	r := st.Test
	switch {
	case r == nil:
		line := ykConnectedProbe
		if keys.YubiKey {
			line = ykConnectedMatch
		}
		return Hero{Tone: ToneNeutral, Glyph: GlyphKey, Title: ykConnected, Line: line, Primary: test}
	case r.Passed() && r.Kind == keycheck.Match:
		return Hero{Tone: ToneSuccess, Glyph: GlyphCheck, Title: ykOpens, Line: fmt.Sprintf(ykTested, When(r.Time, now)), Primary: test}
	case r.Passed():
		return Hero{Tone: ToneSuccess, Glyph: GlyphCheck, Title: ykReady, Line: ykReadyLine, Primary: test}
	case r.Cancelled:
		return Hero{Tone: ToneNeutral, Glyph: GlyphKey, Title: ykCancelled, Line: ykCancelledLine, Primary: test}
	}
	return Hero{Tone: ToneError, Glyph: GlyphError, Title: ykFailed, Line: ykFailedLine, Primary: test}
}

func yubiKeyChecks(keys health.KeysSummary, st YubiKeyState) []CheckLine {
	d := st.Diagnosis
	if d == nil {
		return nil
	}
	ok := func(label, text string) CheckLine {
		return CheckLine{Label: label, Text: text, Tone: ToneSuccess, Glyph: GlyphCheck}
	}
	bad := func(label, text string) CheckLine {
		return CheckLine{Label: label, Text: text, Tone: ToneError, Glyph: GlyphError}
	}
	neutral := func(label, text string) CheckLine {
		return CheckLine{Label: label, Text: text, Tone: ToneNeutral}
	}
	warn := func(label, text string) CheckLine {
		return CheckLine{Label: label, Text: text, Tone: ToneWarning, Glyph: GlyphWarning}
	}

	lines := []CheckLine{ok(ykCheckWindows, ykWindowsReady)}
	if d.APIErr != nil {
		lines[0] = bad(ykCheckWindows, ykWindowsOld)
	}

	// Connected says which YubiKey it is, with a note on which kinds work;
	// FIDO2 whether its FIDO2 function is on.
	var connected, fido CheckLine
	f, firmware := keyFault(d)
	switch f {
	case faultNoKey:
		connected, fido = neutral(ykCheckConnected, ykNoneConnected), neutral(ykCheckFIDO, ykFIDOUnknown)
	case faultNoFIDO:
		connected, fido = ok(ykCheckConnected, connectedText(d.Keys())), bad(ykCheckFIDO, ykFIDOOff)
	case faultFirmware:
		connected, fido = bad(ykCheckConnected, fmt.Sprintf(ykFirmwareOld, firmware)), ok(ykCheckFIDO, ykFIDOOn)
	default:
		connected, fido = ok(ykCheckConnected, connectedText(d.Keys())), ok(ykCheckFIDO, ykFIDOOn)
	}
	// Two YubiKeys work, but the user may touch the one that isn't
	// registered.
	if len(d.Keys()) > 1 {
		connected = warn(ykCheckConnected, connectedText(d.Keys()))
	}
	connected.Note = ykSupported
	lines = append(lines, connected, fido)

	r := st.Test
	switch {
	case !keys.YubiKey && keys.YubiKeyInBackups:
		lines = append(lines, neutral(ykCheckKeys, ykKeysOlder))
	case !keys.YubiKey:
		lines = append(lines, neutral(ykCheckKeys, ykKeysNone))
	case r == nil || r.Kind != keycheck.Match:
		lines = append(lines, neutral(ykCheckKeys, ykKeysNotTested))
	case r.Passed():
		lines = append(lines, ok(ykCheckKeys, fmt.Sprintf(ykKeysAnswered, keys.Created.Local().Format("Mon 2 Jan 2006"))))
	case r.Cancelled:
		lines = append(lines, neutral(ykCheckKeys, ykKeysCancelled))
	default:
		lines = append(lines, bad(ykCheckKeys, ykKeysFailed))
	}
	return lines
}

// connectedText words the connected YubiKeys: "1 YubiKey, firmware 5.7.4".
// Its other USB functions don't matter to RestoreSafe, and FIDO2 has a
// line of its own (YK-4).
func connectedText(keys []yubikey.Key) string {
	if len(keys) > 1 {
		return fmt.Sprintf(ykManyKeys, len(keys))
	}
	k := keys[0]
	text := ykOneKey
	if k.Revision != 0 {
		text = fmt.Sprintf(ykWithFirmware, text, yubikey.FormatRevision(k.Revision))
	}
	return text
}

func yubiKeyGuide(st YubiKeyState) YubiKeyGuide {
	f, _ := faultOf(st.Diagnosis)
	r := st.Test
	return YubiKeyGuide{
		Title: ykGuideTitle,
		// Open when it helps: FIDO2 is off, or a test didn't pass, which may
		// be the PIN (YK-7, YK-8).
		Expanded: f == faultNoFIDO || r != nil && !r.Passed(),
		Steps: []GuideStep{
			{Title: ykStep1, Text: ykStep1Text, Action: &Button{Text: buttonSignIn, Action: ActionOpenSignInSettings, Enabled: true}},
			{Title: ykStep2, Text: ykStep2Text, Action: &Button{Text: buttonAuthenticator, Action: ActionOpenAuthenticator, Enabled: true}},
		},
	}
}

// YubiKeyDetails is the text of the technical details, for a bug report
// (YK-9). It holds no PIN, secret, salt or credential ID: the diagnosis and
// the test result have none.
func YubiKeyDetails(st YubiKeyState) string {
	var b strings.Builder
	line := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }
	line("RestoreSafe %s", st.AppVersion)
	line("Windows %s", st.WindowsVersion)

	d := st.Diagnosis
	if d == nil {
		line("")
		line("Not checked yet.")
	} else {
		line("WebAuthn API version %d (needs %d)", d.APIVersion, d.APIMinimum)
		if d.APIErr != nil {
			line("WebAuthn: %v", d.APIErr)
		}
		line("")
		line("Yubico HID interfaces")
		if d.EnumErr != nil {
			line("  Listing failed: %v", d.EnumErr)
		}
		if len(d.Interfaces) == 0 {
			line("  None")
		}
		for _, i := range d.Interfaces {
			line("  %s  %s", i.Function(), i.Path)
			line("    product ID 0x%04X, USB device version 0x%04X (%s), usage page 0x%04X, usage 0x%04X",
				i.ProductID, i.Revision, yubikey.FormatRevision(i.Revision), i.UsagePage, i.Usage)
			if i.WriteAccess {
				line("    write access granted")
			} else {
				line("    write access denied: %v", i.OpenErr)
			}
		}
		line("")
		line(`HID drivers (HKLM\SYSTEM\CurrentControlSet\Enum\HID)`)
		if len(d.Drivers) == 0 {
			line("  None")
		}
		for _, e := range d.Drivers {
			line("  %s: %s", e.Key, e.Service)
		}
	}

	line("")
	line("Last test")
	r := st.Test
	if r == nil {
		line("  None")
		return b.String()
	}
	kind := "Capability"
	if r.Kind == keycheck.Match {
		kind = "Match"
	}
	result := "passed"
	switch {
	case r.Cancelled:
		result = "cancelled"
	case !r.Passed():
		result = "failed"
	}
	line("  %s, %s, %s", kind, Exact(r.Time), result)
	if r.HRESULT != 0 {
		line("  HRESULT 0x%08X", r.HRESULT)
	}
	if r.Err != nil {
		line("  %v", r.Err)
	}
	if r.Trace != "" {
		line("")
		line("WebAuthn trace")
		for _, t := range strings.Split(strings.TrimRight(r.Trace, "\n"), "\n") {
			line("  %s", t)
		}
	}
	return b.String()
}
