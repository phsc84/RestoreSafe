package flow

import (
	"RestoreSafe/internal/security/cryptox"
	"RestoreSafe/internal/workflow/interact"
	"bytes"
	"io"
	"strings"
)

// Question is the context of a credential question.
type Question struct {
	// Prompt is the workflow's prompt, e.g. "Enter backup password: ".
	Prompt string
	// Message is the last message the workflow wrote since the previous
	// question, e.g. "Wrong password. 2 attempt(s) remaining.", or "".
	Message string
	// Retry is set when the same question is asked again: Message explains
	// what was wrong.
	Retry bool
}

// Dialogs shows the questions of an operation; the window implements it.
// Every method runs on the UI thread and must call answer exactly once,
// before it returns or later, when the user has chosen.
type Dialogs interface {
	BackupPlan(p interact.BackupPlan, answer func())
	ConfirmBackupStart(opts interact.BackupStartOptions, answer func(interact.BackupStart, error))
	RestorePlan(p interact.RestorePlan, answer func())
	VerifyPlan(p interact.VerifyPlan, answer func())
	// ConfirmStart asks to start "restore" or "verification".
	ConfirmStart(action string, answer func(bool, error))
	// ChooseUnlockMethod answers true for the recovery code; regular names
	// the regular credentials, e.g. "password + YubiKey". A dialog that
	// takes the password with the choice answers it as secret, which the
	// caller zeroes; nil otherwise.
	ChooseUnlockMethod(q Question, regular string, answer func(recovery bool, secret []byte, err error))
	// Password answers the secret; the caller zeroes it.
	Password(q Question, answer func(secret []byte, err error))
	// NewPassword answers the password and its confirmation, or ok=false
	// when the user cancelled.
	NewPassword(q Question, confirmPrompt string, answer func(password, confirm []byte, ok bool))
	// RecoveryCode shows a new recovery code once.
	RecoveryCode(code string, answer func())
	RetypeRecoveryCode(q Question, answer func(code string, err error))
	// SpareYubiKey asks to connect the spare YubiKey; ok=false cancels.
	SpareYubiKey(q Question, answer func(ok bool))
}

// UI implements interact.UI for one operation: every question goes through
// the bridge to the UI thread, where Dialogs shows it.
type UI struct {
	b *Bridge
	d Dialogs
	// seenOutput marks the output already shown with a question, so a
	// dialog only repeats a message written since the previous one.
	seenOutput         int
	lastPasswordPrompt string
	newPasswordAsked   bool
	retypeAsked        bool
	spareAsked         bool
	// pendingSecret is a password typed with the choice of the unlock
	// method, for the next Password question.
	pendingSecret []byte
}

var _ interact.UI = (*UI)(nil)

// NewUI returns the interact.UI of an operation.
func NewUI(b *Bridge, d Dialogs) *UI { return &UI{b: b, d: d} }

// Bridge returns the bridge of the operation.
func (u *UI) Bridge() *Bridge { return u.b }

func (u *UI) Output() io.Writer            { return u.b }
func (u *UI) Progress(p interact.Progress) { u.b.Progress(p) }
func (u *UI) ShowResult(r interact.Result) { u.b.SetResult(r) }
func (u *UI) LogStarted(path string)       { u.b.SetLogPath(path) }

// ShowBackupPlan shows the plan; it returns once the plan is on screen.
func (u *UI) ShowBackupPlan(p interact.BackupPlan) {
	u.b.Ask(func(answer func(any, error)) { u.d.BackupPlan(p, func() { answer(nil, nil) }) }, nil, nil) //nolint:errcheck
}

// ShowRestorePlan shows the plan; it returns once the plan is on screen.
func (u *UI) ShowRestorePlan(p interact.RestorePlan) {
	u.b.Ask(func(answer func(any, error)) { u.d.RestorePlan(p, func() { answer(nil, nil) }) }, nil, nil) //nolint:errcheck
}

// ShowVerifyPlan shows the plan; it returns once the plan is on screen.
func (u *UI) ShowVerifyPlan(p interact.VerifyPlan) {
	u.b.Ask(func(answer func(any, error)) { u.d.VerifyPlan(p, func() { answer(nil, nil) }) }, nil, nil) //nolint:errcheck
}

// ConfirmBackupStart asks whether to start the backup of the plan shown.
func (u *UI) ConfirmBackupStart(opts interact.BackupStartOptions) (interact.BackupStart, error) {
	v, err := u.b.Ask(func(answer func(any, error)) {
		u.d.ConfirmBackupStart(opts, func(choice interact.BackupStart, err error) { answer(choice, err) })
	}, interact.BackupCancel, nil)
	return v.(interact.BackupStart), err
}

// ConfirmStart asks whether to start the restore or verification shown.
func (u *UI) ConfirmStart(action string) (bool, error) {
	v, err := u.b.Ask(func(answer func(any, error)) {
		u.d.ConfirmStart(action, func(ok bool, err error) { answer(ok, err) })
	}, false, nil)
	return v.(bool), err
}

// ChooseUnlockMethod asks for the regular credentials or the recovery code.
// The dialog may take the password with the choice (password-only keys);
// the next Password question then returns it without asking again.
func (u *UI) ChooseUnlockMethod(regular string) (bool, error) {
	q := Question{Message: u.recentMessage()}
	type choice struct {
		recovery bool
		secret   []byte
	}
	v, err := u.b.Ask(func(answer func(any, error)) {
		u.d.ChooseUnlockMethod(q, regular, func(recovery bool, secret []byte, err error) {
			answer(choice{recovery, secret}, err)
		})
	}, choice{}, interact.ErrCancelled)
	c, _ := v.(choice)
	if err != nil {
		cryptox.ZeroBytes(c.secret)
		return false, err
	}
	u.pendingSecret = c.secret
	return c.recovery, nil
}

// recentMessage returns the last output line written since the previous
// question (e.g. "Wrong password. 2 attempt(s) remaining."); log lines,
// which start with a timestamp, are left out.
func (u *UI) recentMessage() string {
	seq, last := u.b.OutputMark()
	if seq == u.seenOutput || strings.HasPrefix(last, "[") {
		u.seenOutput = seq
		return ""
	}
	u.seenOutput = seq
	return last
}

// Password asks for a secret without echo.
func (u *UI) Password(prompt string) ([]byte, error) {
	if s := u.pendingSecret; s != nil && !strings.Contains(strings.ToLower(prompt), "recovery code") {
		// Typed with the choice of the unlock method.
		u.pendingSecret = nil
		u.lastPasswordPrompt = prompt
		u.recentMessage()
		return s, nil
	}
	q := Question{Prompt: prompt, Message: u.recentMessage(), Retry: u.lastPasswordPrompt == prompt}
	u.lastPasswordPrompt = prompt
	v, err := u.b.Ask(func(answer func(any, error)) {
		u.d.Password(q, func(secret []byte, err error) { answer(secret, err) })
	}, nil, interact.ErrCancelled)
	if err != nil {
		return nil, err
	}
	return v.([]byte), nil
}

// NewPassword asks for a new password and its confirmation in one dialog,
// with the rules and errors of interact.ReadPasswordConfirmed.
func (u *UI) NewPassword(prompt, confirmPrompt string) ([]byte, error) {
	q := Question{Prompt: prompt, Message: u.recentMessage(), Retry: u.newPasswordAsked}
	u.newPasswordAsked = true
	v, err := u.b.Ask(func(answer func(any, error)) {
		u.d.NewPassword(q, confirmPrompt, func(pw, confirm []byte, ok bool) {
			if !ok {
				answer(nil, interact.ErrCancelled)
				return
			}
			pw, err := checkNewPassword(pw, confirm)
			answer(pw, err)
		})
	}, nil, interact.ErrCancelled)
	if err != nil {
		return nil, err
	}
	return v.([]byte), nil
}

// checkNewPassword checks a new password against its confirmation, zeroes
// the confirmation, and zeroes the password when it is refused.
func checkNewPassword(pw, confirm []byte) ([]byte, error) {
	defer cryptox.ZeroBytes(confirm)
	switch {
	case len(pw) == 0:
		cryptox.ZeroBytes(pw)
		return nil, interact.ErrPasswordEmpty
	case !bytes.Equal(pw, confirm):
		cryptox.ZeroBytes(pw)
		return nil, interact.ErrPasswordMismatch
	}
	return pw, nil
}

// ShowRecoveryCode shows the new recovery code once.
func (u *UI) ShowRecoveryCode(code string) {
	u.b.Ask(func(answer func(any, error)) { u.d.RecoveryCode(code, func() { answer(nil, nil) }) }, nil, nil) //nolint:errcheck
}

// RetypeRecoveryCode asks for the recovery code shown before.
func (u *UI) RetypeRecoveryCode() (string, error) {
	q := Question{Message: u.recentMessage(), Retry: u.retypeAsked}
	u.retypeAsked = true
	v, err := u.b.Ask(func(answer func(any, error)) {
		u.d.RetypeRecoveryCode(q, func(code string, err error) { answer(code, err) })
	}, "", interact.ErrCancelled)
	if err != nil {
		return "", err
	}
	return v.(string), nil
}

// WaitForSpareYubiKey waits until the user confirms the spare YubiKey is
// connected.
func (u *UI) WaitForSpareYubiKey() (bool, error) {
	q := Question{Message: u.recentMessage(), Retry: u.spareAsked}
	u.spareAsked = true
	v, err := u.b.Ask(func(answer func(any, error)) {
		u.d.SpareYubiKey(q, func(ok bool) { answer(ok, nil) })
	}, false, nil)
	return v.(bool), err
}
