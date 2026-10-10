package gui

import (
	"github.com/phsc84/restoresafe/internal/gui/flow"
	"github.com/phsc84/restoresafe/internal/gui/view"
	"github.com/phsc84/restoresafe/internal/workflow/interact"
)

// questions shows the questions of an operation: the Create backup window,
// the Verify window, the Restore backup window and the credential dialogs.
type questions struct{ a *app }

var _ flow.Dialogs = questions{}

// BackupPlan shows the plan in the Create backup window.
func (q questions) BackupPlan(p interact.BackupPlan, answer func()) {
	q.a.machine.PlanShown(p)
	if q.a.plan != nil {
		q.a.plan.setPlan(p)
	}
	q.a.refreshRun()
	answer()
}

// ConfirmBackupStart offers Start (unless the plan is blocked), the other
// plans in opts, and Cancel in the Create backup window. Choosing another plan
// shows it, and this question is asked again.
func (q questions) ConfirmBackupStart(opts interact.BackupStartOptions, answer func(interact.BackupStart, error)) {
	if q.a.plan == nil {
		answer(interact.BackupCancel, nil)
		return
	}
	q.a.plan.ask(opts, answer)
}

// RestorePlan shows the workflow's plan in the Restore backup window.
func (q questions) RestorePlan(p interact.RestorePlan, answer func()) {
	q.a.machine.RestorePlanShown(p)
	if q.a.restore != nil {
		q.a.restore.setPlan(p)
	}
	answer()
}

// VerifyPlan records the plan and shows it in the Verify window.
func (q questions) VerifyPlan(p interact.VerifyPlan, answer func()) {
	q.a.machine.VerifyPlanShown(p)
	if q.a.verify != nil {
		q.a.verify.setPlan(p)
	}
	answer()
}

// ConfirmStart waits for Start in the Verify window (figure 7.3); for a
// restore, the Restore backup window answers it itself, as its Start was the
// confirmation.
func (q questions) ConfirmStart(action string, answer func(bool, error)) {
	if r := q.a.machine.Current(); r != nil && r.Op == flow.OpVerify {
		if q.a.verify == nil {
			answer(false, nil)
			return
		}
		q.a.verify.ask(answer)
		return
	}
	if q.a.restore == nil {
		answer(false, nil)
		return
	}
	q.a.restore.ask(answer)
}

// ChooseUnlockMethod shows the unlock dialog with the link to the recovery
// code (CR-1). For password-only keys it takes the password too.
func (q questions) ChooseUnlockMethod(qu interact.UnlockChoice, answer func(bool, []byte, error)) {
	d := view.UnlockChoiceOf(qu)
	res := q.a.runCredentialDialog(d)
	switch {
	case res.link:
		answer(true, nil, nil)
	case !res.ok:
		answer(false, nil, interact.ErrCancelled)
	case len(d.Fields) > 0:
		answer(false, res.values[0], nil)
	default:
		answer(false, nil, nil)
	}
}

// keys returns the key plan of the backup being run; nil for restore and
// verify.
func (q questions) keys() *interact.KeyPlan {
	if r := q.a.machine.Current(); r != nil && r.Plan != nil {
		return &r.Plan.Keys
	}
	return nil
}

// Password asks for the password or the recovery code (figure 9.1).
func (q questions) Password(qu interact.SecretQuestion, answer func([]byte, error)) {
	res := q.a.runCredentialDialog(view.UnlockDialogOf(qu))
	if !res.ok {
		answer(nil, interact.ErrCancelled)
		return
	}
	answer(res.values[0], nil)
}

// NewPassword asks for a new password and its confirmation (figure 9.2).
func (q questions) NewPassword(qu interact.NewPasswordQuestion, answer func(pw, confirm []byte, ok bool)) {
	res := q.a.runCredentialDialog(view.NewPasswordDialogOf(qu, q.newKeys()))
	if !res.ok {
		answer(nil, nil, false)
		return
	}
	answer(res.values[0], res.values[1], true)
}

// newKeys returns the plan of the keys being created.
func (q questions) newKeys() interact.KeyPlan {
	if k := q.keys(); k != nil {
		return *k
	}
	return interact.KeyPlan{New: true, Password: true}
}

// RecoveryCode shows the new recovery code once (figure 9.3). Closing the
// dialog cancels the backup.
func (q questions) RecoveryCode(code []byte, answer func(bool)) {
	answer(q.a.runCredentialDialog(view.RecoveryCodeDialogOf(code, q.newKeys())).ok)
}

// SpareYubiKey asks to connect the spare YubiKey.
func (q questions) SpareYubiKey(qu interact.SpareQuestion, answer func(bool)) {
	answer(q.a.runCredentialDialog(view.SpareYubiKeyDialogOf(qu, q.newKeys())).ok)
}
