package gui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/phsc84/restoresafe/internal/buildinfo"
	"github.com/phsc84/restoresafe/internal/format/catalog"
	"github.com/phsc84/restoresafe/internal/format/container"
	"github.com/phsc84/restoresafe/internal/gui/view"
	"github.com/phsc84/restoresafe/internal/gui/win32"
	"github.com/phsc84/restoresafe/internal/security/yubikey"
	"github.com/phsc84/restoresafe/internal/workflow/keycheck"

	"golang.org/x/sys/windows"
)

// Messages and the timer of the YubiKey page (GUI spec 10.2).
const (
	msgYubiKeyDiagnosed = win32.WM_APP + 8 // a diagnosis is done
	msgYubiKeyTested    = win32.WM_APP + 9 // the YubiKey test is done

	// yubiKeyTimerID waits for the last of the device changes a plugged
	// in or pulled out YubiKey sends (YK-3).
	yubiKeyTimerID = 3
	yubiKeySettle  = 500 // milliseconds
)

// yubiKeyState is what the window knows about the YubiKey besides the
// snapshot.
type yubiKeyState struct {
	diagnosis *yubikey.Diagnosis
	// test is the result of the last test of this session (YK-6).
	test    *keycheck.Result
	testing bool
	// request numbers the diagnoses; a result of an older request than the
	// newest is dropped (YK-3).
	request int
	// keysCreated is the creation time of the current keys at the last
	// snapshot: new keys clear the test result.
	keysCreated time.Time
	windows     string

	// Set by the workers, under app.mu.
	pendingDiagnosis *yubikey.Diagnosis
	pendingRequest   int
	pendingTest      *keycheck.Result
}

// yubiKeyVisible tells whether the sidebar shows the YubiKey page (YK-1).
func (a *app) yubiKeyVisible() bool {
	return view.YubiKeyVisible(a.opts.Config, a.snapshot)
}

// yubiKeyView is the state the YubiKey page shows.
func (a *app) yubiKeyView() view.YubiKeyState {
	y := &a.yk
	if y.windows == "" {
		v := windows.RtlGetVersion()
		y.windows = fmt.Sprintf("%d.%d.%d", v.MajorVersion, v.MinorVersion, v.BuildNumber)
	}
	return view.YubiKeyState{
		Diagnosis: y.diagnosis, Test: y.test,
		Testing: y.testing, Busy: a.machine.Busy(),
		AppVersion: buildinfo.Version, WindowsVersion: y.windows,
	}
}

// diagnoseYubiKey checks the YubiKey without any prompt on a worker
// goroutine (YK-3).
func (a *app) diagnoseYubiKey() {
	a.yk.request++
	n := a.yk.request
	go func() {
		d := yubikey.Diagnose()
		a.mu.Lock()
		if n > a.yk.pendingRequest {
			a.yk.pendingDiagnosis, a.yk.pendingRequest = &d, n
		}
		a.mu.Unlock()
		win32.PostMessage(a.hwnd, msgYubiKeyDiagnosed, 0, 0) //nolint:errcheck
	}()
}

// yubiKeyDiagnosed shows the diagnosis a worker posted, unless a newer one
// was asked for.
func (a *app) yubiKeyDiagnosed() {
	a.mu.Lock()
	d, n := a.yk.pendingDiagnosis, a.yk.pendingRequest
	a.yk.pendingDiagnosis = nil
	a.mu.Unlock()
	if d == nil || n != a.yk.request {
		return
	}
	a.yk.diagnosis = d
	a.shell.yubiKey.update()
}

// testYubiKey runs the YubiKey test of YK-5 on a worker goroutine. It takes
// the place of an operation: none starts while it runs, and it doesn't
// start while one runs (YK-6).
func (a *app) testYubiKey() {
	if a.yk.testing || a.machine.Busy() || a.reloading {
		return
	}
	var ks *container.KeySet
	if a.snapshot != nil {
		ks = catalog.CurrentKeySet(a.snapshot.Sets)
	}
	a.yk.testing = true
	a.refreshShell()
	go func() {
		r := keycheck.Run(ks, time.Now)
		a.mu.Lock()
		a.yk.pendingTest = &r
		a.mu.Unlock()
		win32.PostMessage(a.hwnd, msgYubiKeyTested, 0, 0) //nolint:errcheck
	}()
}

// yubiKeyTested shows the result of the test.
func (a *app) yubiKeyTested() {
	a.mu.Lock()
	r := a.yk.pendingTest
	a.yk.pendingTest = nil
	a.mu.Unlock()
	if r == nil {
		return
	}
	a.yk.testing = false
	a.yk.test = r
	a.refreshShell()
	a.focusPage()
}

// deviceChanged waits until the devices settle: a YubiKey sends several
// changes when it is plugged in or pulled out.
func (a *app) deviceChanged() {
	if a.yubiKeyVisible() {
		win32.SetTimer(a.hwnd, yubiKeyTimerID, yubiKeySettle)
	}
}

// devicesSettled clears the test result, which may be of another YubiKey,
// and checks again (YK-3, YK-6): the YubiKey for its page, the backups for
// "YubiKey connected" on the Keys card of Create backup.
func (a *app) devicesSettled() {
	win32.KillTimer(a.hwnd, yubiKeyTimerID)
	a.clearYubiKeyTest()
	a.diagnoseYubiKey()
	if a.run == nil {
		a.startCheck()
	}
	a.shell.yubiKey.update()
}

// clearYubiKeyTest forgets the test result, unless a test is running.
func (a *app) clearYubiKeyTest() {
	if !a.yk.testing {
		a.yk.test = nil
	}
}

// keysChecked clears the test result when the snapshot shows new keys
// (YK-6).
func (a *app) keysChecked() {
	if a.snapshot == nil {
		return
	}
	created := a.snapshot.Keys.Created
	if !a.yk.keysCreated.IsZero() && !created.Equal(a.yk.keysCreated) {
		a.clearYubiKeyTest()
	}
	a.yk.keysCreated = created
}

// authenticatorPaths are where Yubico Authenticator installs itself: the
// current version (authenticator.exe) and older ones (yubioath-desktop.exe).
func authenticatorPaths() []string {
	var out []string
	for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)"} {
		dir := os.Getenv(env)
		if dir == "" {
			continue
		}
		for _, exe := range []string{"authenticator.exe", "yubioath-desktop.exe"} {
			out = append(out, filepath.Join(dir, "Yubico", "Yubico Authenticator", exe))
		}
	}
	return out
}

// openAuthenticator starts Yubico Authenticator as administrator, which it
// needs to turn FIDO2 on; without it, its download page opens (YK-8). When
// the user refuses the permission, nothing else opens.
func (a *app) openAuthenticator() {
	for _, p := range authenticatorPaths() {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		if err := win32.ShellRunAs(a.hwnd, p); err == nil || errors.Is(err, windows.ERROR_CANCELLED) {
			return
		}
	}
	a.open(view.YubicoAuthenticatorURL, false)
}

// copyYubiKeyDetails copies the technical details for a bug report (YK-9).
// They hold no secret, so the clipboard needs no protection.
func (a *app) copyYubiKeyDetails() {
	text := view.YubiKeyDetails(a.yubiKeyView())
	win32.CopyText(a.hwnd, strings.ReplaceAll(text, "\n", "\r\n")) //nolint:errcheck // nothing to copy then
}
