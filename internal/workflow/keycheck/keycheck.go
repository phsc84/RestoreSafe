// Package keycheck is the YubiKey test of the YubiKey page (GUI spec YK-5).
// With YubiKey keys it tells whether the connected YubiKey opens them
// (Match); without, whether the YubiKey can protect backups at all
// (Capability). It shows Windows Security prompts, so it runs only when the
// user asks. No secret leaves it: the derived secret is zeroed at once.
package keycheck

import (
	"bytes"
	"errors"
	"time"

	"github.com/phsc84/restoresafe/internal/format/container"
	"github.com/phsc84/restoresafe/internal/security/cryptox"
	"github.com/phsc84/restoresafe/internal/security/yubikey"
)

// Kind is the kind of test.
type Kind int

const (
	// Match asks the YubiKey for the secret of the current keys: the PIN
	// and one touch.
	Match Kind = iota + 1
	// Capability registers a credential that stays on no YubiKey and
	// derives one secret: the PIN twice and two touches.
	Capability
)

// Result is what the test found. It holds no secret, salt or credential ID.
type Result struct {
	Kind Kind
	// Time is when the test ended.
	Time time.Time
	// Err is nil when the test passed.
	Err error
	// Cancelled is set when Windows ended the test as cancelled: the user
	// cancelled, or Windows refused the YubiKey in its own dialog (YK-7).
	Cancelled bool
	// HRESULT is the error code of the WebAuthn call that failed; 0 when
	// none failed or the error has no code.
	HRESULT uint32
	// Trace is the WebAuthn trace of the test (yubikey.DebugOutput).
	Trace string
}

// Passed tells whether the test passed.
func (r Result) Passed() bool { return r.Err == nil }

// The YubiKey calls; replaced in tests.
var (
	deriveSecretFn = yubikey.DeriveFIDO2Secret
	probeFn        = yubikey.ProbeCapability
)

// Run tests the connected YubiKey against ks, the current keys (nil when
// there are none): Match when ks has a YubiKey slot, Capability otherwise.
// It blocks until the Windows Security prompts are closed; now gives the
// time of the result. Only one test may run at a time: it sets
// yubikey.DebugOutput for its duration.
func Run(ks *container.KeySet, now func() time.Time) Result {
	var trace bytes.Buffer
	yubikey.DebugOutput = &trace
	defer func() { yubikey.DebugOutput = nil }()

	r := Result{Kind: Capability}
	if slot, ok := yubiKeySlot(ks); ok {
		r.Kind = Match
		secret, err := deriveSecretFn(*slot.Challenge)
		cryptox.ZeroBytes(secret)
		r.Err = err
	} else {
		r.Err = probeFn()
	}

	var hr *yubikey.HRESULTError
	if errors.As(r.Err, &hr) {
		r.HRESULT, r.Cancelled = hr.Code, hr.Cancelled()
	}
	r.Time = now()
	r.Trace = trace.String()
	return r
}

// yubiKeySlot returns the YubiKey slot of ks, if it has one.
func yubiKeySlot(ks *container.KeySet) (container.Slot, bool) {
	if ks == nil || !ks.AuthMode.UsesYubiKey() {
		return container.Slot{}, false
	}
	for _, s := range ks.Slots {
		if s.UsesYubiKey() && s.Challenge != nil {
			return s, true
		}
	}
	return container.Slot{}, false
}
