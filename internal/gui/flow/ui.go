package flow

import (
	"RestoreSafe/internal/security/cryptox"
	"RestoreSafe/internal/workflow/interact"
	"bytes"
	"io"
)

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
	// ChooseUnlockMethod answers true for the recovery code. A dialog that
	// takes the password with the choice answers it as secret, which the
	// caller zeroes; nil otherwise.
	ChooseUnlockMethod(q interact.UnlockChoice, answer func(recovery bool, secret []byte, err error))
	// Password answers the secret; the caller zeroes it.
	Password(q interact.SecretQuestion, answer func(secret []byte, err error))
	// NewPassword answers the password and its confirmation, or ok=false
	// when the user cancelled.
	NewPassword(q interact.NewPasswordQuestion, answer func(password, confirm []byte, ok bool))
	// RecoveryCode shows a new recovery code once; ok=false cancels.
	RecoveryCode(code string, answer func(ok bool))
	// SpareYubiKey asks to connect the spare YubiKey; ok=false cancels.
	SpareYubiKey(q interact.SpareQuestion, answer func(ok bool))
}

// UI implements interact.UI for one operation: every question goes through
// the bridge to the UI thread, where Dialogs shows it.
type UI struct {
	b *Bridge
	d Dialogs
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
func (u *UI) ChooseUnlockMethod(q interact.UnlockChoice) (bool, error) {
	type choice struct {
		recovery bool
		secret   []byte
	}
	v, err := u.b.Ask(func(answer func(any, error)) {
		u.d.ChooseUnlockMethod(q, func(recovery bool, secret []byte, err error) {
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

// Password asks for a secret without echo.
func (u *UI) Password(q interact.SecretQuestion) ([]byte, error) {
	if s := u.pendingSecret; s != nil && q.Kind == interact.SecretPassword {
		// Typed with the choice of the unlock method.
		u.pendingSecret = nil
		return s, nil
	}
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
func (u *UI) NewPassword(q interact.NewPasswordQuestion) ([]byte, error) {
	v, err := u.b.Ask(func(answer func(any, error)) {
		u.d.NewPassword(q, func(pw, confirm []byte, ok bool) {
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
func (u *UI) ShowRecoveryCode(code string) error {
	_, err := u.b.Ask(func(answer func(any, error)) {
		u.d.RecoveryCode(code, func(ok bool) {
			if !ok {
				answer(nil, interact.ErrCancelled)
				return
			}
			answer(nil, nil)
		})
	}, nil, interact.ErrCancelled)
	return err
}

// WaitForSpareYubiKey waits until the user confirms the spare YubiKey is
// connected.
func (u *UI) WaitForSpareYubiKey(q interact.SpareQuestion) (bool, error) {
	v, err := u.b.Ask(func(answer func(any, error)) {
		u.d.SpareYubiKey(q, func(ok bool) { answer(ok, nil) })
	}, false, nil)
	return v.(bool), err
}
