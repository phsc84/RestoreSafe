package view

import (
	"fmt"
	"strconv"
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

// GuideStep is a part of the YubiKey help: its title, short points, and its
// button.
type GuideStep struct {
	Title  string
	Points []string
	// Action is the step's button, if it has one.
	Action *Button
}

// YubiKeyGuide is the card "YubiKey help" (GUI spec YK-8).
type YubiKeyGuide struct {
	Title string
	Steps []GuideStep
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
		Guide:        yubiKeyGuide(),
		DetailsTitle: ykDetailsTitle,
		Details:      YubiKeyDetails(keys, st),
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

// yubiKeyChecks are the lines of the Checks card and of the summary of the
// technical details, in the same words (YK-4, YK-9): Windows, the YubiKey,
// its FIDO2 function and firmware, and the current keys with the test. The
// notes explain a line in the Checks card. No lines before the first
// diagnosis.
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

	windows := ok(ykCheckWindows, ykWindowsReady)
	if d.APIErr != nil {
		windows = bad(ykCheckWindows, ykWindowsOld)
	}
	windows.Note = ykWindowsNote

	// More than one YubiKey blocks backups: new keys would register the one
	// the user touches (decision 12).
	var yubiKey CheckLine
	switch n := len(d.Keys()); {
	case n == 0:
		yubiKey = neutral(ykCheckYubiKey, ykNotConnected)
	case n == 1:
		yubiKey = ok(ykCheckYubiKey, ykOneConnected)
	default:
		yubiKey = bad(ykCheckYubiKey, fmt.Sprintf(ykManyConnected, n))
	}
	yubiKey.Note = ykSupported
	lines := []CheckLine{windows, yubiKey}

	switch {
	case len(d.Keys()) == 0:
		lines = append(lines, neutral(ykCheckFIDO, ykFIDOUnknown))
	case d.FIDO():
		lines = append(lines, ok(ykCheckFIDO, ykFIDOOn))
	default:
		lines = append(lines, bad(ykCheckFIDO, ykFIDOOff))
	}
	if k := firmwareKey(d); k.Revision != 0 {
		firmware := yubikey.FormatRevision(k.Revision)
		if k.Revision < minFirmware {
			lines = append(lines, bad(ykCheckFirmware, fmt.Sprintf(ykFirmwareOld, firmware)))
		} else {
			lines = append(lines, ok(ykCheckFirmware, fmt.Sprintf(ykFirmware, firmware)))
		}
	}

	if !keys.YubiKey {
		text := ykKeysNone
		if keys.YubiKeyInBackups {
			text = ykKeysOlder
		}
		return append(lines, neutral(ykCheckKeys, text))
	}
	// The keys name the registration the YubiKey must hold; the test says
	// whether it does.
	text := func(status string) string {
		return fmt.Sprintf(ykKeys, capitalize(keys.Methods), keys.Created.Local().Format("Mon 2 Jan 2006"), status)
	}
	switch r := st.Test; {
	case r == nil || r.Kind != keycheck.Match:
		lines = append(lines, neutral(ykCheckKeys, text(ykKeysNotTested)))
	case r.Passed():
		lines = append(lines, ok(ykCheckKeys, text(ykKeysAnswered)))
	case r.Cancelled:
		lines = append(lines, neutral(ykCheckKeys, text(ykKeysCancelled)))
	default:
		lines = append(lines, bad(ykCheckKeys, text(ykKeysFailed)))
	}
	return lines
}

// firmwareKey returns the YubiKey whose firmware the checks show: the one
// with FIDO2, or else the first.
func firmwareKey(d *yubikey.Diagnosis) yubikey.Key {
	if k := fidoKey(d); k.Revision != 0 {
		return k
	}
	if keys := d.Keys(); len(keys) > 0 {
		return keys[0]
	}
	return yubikey.Key{}
}

func yubiKeyGuide() YubiKeyGuide {
	return YubiKeyGuide{
		Title: ykGuideTitle,
		Steps: []GuideStep{
			{Title: ykStep1, Points: []string{ykPIN1, ykPIN2, ykPIN3}, Action: &Button{Text: buttonSignIn, Action: ActionOpenSignInSettings, Enabled: true}},
			{Title: ykStep2, Points: []string{ykFIDO1, ykFIDO2, ykFIDO3}, Action: &Button{Text: buttonAuthenticator, Action: ActionOpenAuthenticator, Enabled: true}},
		},
	}
}

// detailsLabelWidth is the width of the labels in the technical details, in
// characters: the values start in one column.
const detailsLabelWidth = 16

// details writes the technical details: sections of labeled lines, the
// values in one column, continuation lines under the value.
type details struct{ b strings.Builder }

func (d *details) section(title string) {
	if d.b.Len() > 0 {
		d.b.WriteString("\n")
	}
	d.b.WriteString(title + "\n")
}

// line writes label and value; a value of several lines goes on under the
// first.
func (d *details) line(label, value string) {
	for i, v := range strings.Split(strings.TrimRight(value, "\n"), "\n") {
		if i > 0 {
			label = ""
		}
		fmt.Fprintf(&d.b, "  %-*s%s\n", detailsLabelWidth, label, v)
	}
}

// YubiKeyDetails is the text of the technical details (YK-9), from the
// overview to the raw data: the summary, each connected YubiKey, the last
// test and Windows. It holds no PIN, secret, salt or credential ID: the
// diagnosis, the test result and the key summary have none.
func YubiKeyDetails(keys health.KeysSummary, st YubiKeyState) string {
	var d details
	dg := st.Diagnosis

	// The summary says what the Checks card says, without its notes; keys
	// without a YubiKey diagnose nothing.
	d.section(ydSummary)
	if dg == nil {
		d.line(ykCheckWindows, ydNotChecked)
		d.line(ykCheckYubiKey, ydNotChecked)
	}
	for _, l := range yubiKeyChecks(keys, st) {
		if l.Label != ykCheckKeys || keys.YubiKey {
			d.line(l.Label, l.Text)
		}
	}

	if dg != nil {
		for i, k := range dg.Keys() {
			d.section(fmt.Sprintf(ydYubiKey, i+1))
			d.line(ydLabelProduct, fmt.Sprintf("0x%04X", k.ProductID))
			if k.Revision != 0 {
				d.line(ydLabelFirmware, fmt.Sprintf(ydFirmwareRev, yubikey.FormatRevision(k.Revision), k.Revision))
			}
			if k.Device != "" {
				d.line(ydLabelDevice, k.Device)
			}
			for _, iface := range k.Interfaces {
				d.line(ydLabelInterface, detailsInterface(iface))
			}
		}
	}

	if r := st.Test; r != nil {
		d.section(ydLastTest)
		kind := ydCapabilityMeans
		if r.Kind == keycheck.Match {
			kind = ydMatchMeans
		}
		d.line(ydLabelKind, kind)
		d.line(ydLabelTime, Exact(r.Time))
		d.line(ydLabelResult, capitalize(testResult(r)))
		if r.HRESULT != 0 {
			d.line(ydLabelCode, hresultText(r.HRESULT))
		}
		if r.Err != nil {
			d.line(ydLabelMessage, r.Err.Error())
		}
		if r.Trace != "" {
			d.line(ydLabelTrace, r.Trace)
		}
	}

	d.section(ydWindows)
	d.line(ydLabelVersion, windowsName(st.WindowsVersion))
	if dg != nil {
		d.line(ydLabelAPI, fmt.Sprintf(ydAPI, dg.APIVersion, dg.APIMinimum))
		if dg.APIErr != nil {
			d.line(ydLabelAPIError, dg.APIErr.Error())
		}
		if dg.EnumErr != nil {
			d.line(ydLabelListing, dg.EnumErr.Error())
		}
		drivers := ydNone
		if len(dg.Drivers) > 0 {
			var lines []string
			for _, e := range dg.Drivers {
				lines = append(lines, e.Key+": "+e.Service)
			}
			drivers = strings.Join(lines, "\n")
		}
		d.line(ydLabelDrivers, drivers)
	}
	return "RestoreSafe " + st.AppVersion + "\n\n" + d.b.String()
}

func testResult(r *keycheck.Result) string {
	switch {
	case r.Cancelled:
		return ydCancelled
	case !r.Passed():
		return ydFailed
	}
	return ydPassed
}

// detailsInterface describes one HID interface on two lines: its function,
// usage and access, then its path.
func detailsInterface(i yubikey.Interface) string {
	access := ydWriteGranted
	switch {
	case !i.WriteAccess && i.Function() == yubikey.FunctionFIDO:
		access = ydWriteDeniedFIDO
	case !i.WriteAccess:
		access = fmt.Sprintf(ydWriteDenied, i.OpenErr)
	}
	return fmt.Sprintf(ydUsage, i.Function(), i.UsagePage, i.Usage) + ", " + access + "\n" + i.Path
}

func hresultText(code uint32) string {
	if meaning, ok := hresultMeanings[code]; ok {
		return fmt.Sprintf(ydCodeKnown, code, meaning)
	}
	return fmt.Sprintf(ydCodeUnknown, code)
}

// windowsName names Windows by its version "10.0.26200": builds from 22000
// on are Windows 11.
func windowsName(version string) string {
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return version
	}
	build, err := strconv.Atoi(parts[2])
	if err != nil {
		return version
	}
	major := 10
	if build >= 22000 {
		major = 11
	}
	return fmt.Sprintf(ydWindowsName, major, version)
}
