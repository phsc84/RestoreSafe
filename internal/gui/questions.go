package gui

import (
	"RestoreSafe/internal/gui/flow"
	"RestoreSafe/internal/gui/view"
	"RestoreSafe/internal/gui/win32"
	"RestoreSafe/internal/workflow/interact"
)

// questions shows the questions of an operation: the backup plan dialog
// and the credential dialogs; restore and verify still show their plans on
// the first GUI's operation screen (replaced in plan phases 7 and 8).
type questions struct{ a *app }

var _ flow.Dialogs = questions{}

// BackupPlan shows the plan in the plan dialog.
func (q questions) BackupPlan(p interact.BackupPlan, answer func()) {
	q.a.machine.PlanShown(p)
	if q.a.plan != nil {
		q.a.plan.setPlan(p)
	}
	q.a.refreshRun()
	answer()
}

// ConfirmBackupStart offers Start (unless the plan is blocked), the other
// plans in opts, and Cancel in the plan dialog. Choosing another plan
// shows it, and this question is asked again.
func (q questions) ConfirmBackupStart(opts interact.BackupStartOptions, answer func(interact.BackupStart, error)) {
	if q.a.plan == nil {
		answer(interact.BackupCancel, nil)
		return
	}
	q.a.plan.ask(opts, answer)
}

func (q questions) RestorePlan(p interact.RestorePlan, answer func()) {
	q.a.showPreflight(p.Details)
	answer()
}

func (q questions) VerifyPlan(p interact.VerifyPlan, answer func()) {
	q.a.showPreflight(p.Details)
	answer()
}

// ConfirmStart offers Start and Cancel under the preflight.
func (q questions) ConfirmStart(action string, answer func(bool, error)) {
	label := "&Start restore"
	if action == "verification" {
		label = "&Start verification"
	}
	q.a.offerStart([]opButton{
		{label, func() { q.a.startRunning(); answer(true, nil) }},
		{"Cancel", func() { answer(false, nil) }},
	})
}

// ChooseUnlockMethod offers the regular credentials and the recovery code
// (restore and verify; the unlock dialog takes it over in plan phase 7).
func (q questions) ChooseUnlockMethod(regular string, answer func(bool, error)) {
	button, _ := q.a.taskDialog(q.a.hwnd, win32.TaskDialog{
		Instruction:  "How do you want to unlock the backup?",
		Buttons:      []win32.TaskButton{{ID: 100, Text: "Unlock with " + regular}, {ID: 101, Text: "Unlock with the recovery code"}},
		CommandLinks: true,
	})
	switch button {
	case 100:
		answer(false, nil)
	case 101:
		answer(true, nil)
	default:
		answer(false, interact.ErrCancelled)
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
func (q questions) Password(qu flow.Question, answer func([]byte, error)) {
	values, ok := q.a.runCredentialDialog(view.UnlockDialogOf(qu))
	if !ok {
		answer(nil, interact.ErrCancelled)
		return
	}
	answer(values[0], nil)
}

// NewPassword asks for a new password and its confirmation (figure 9.2).
func (q questions) NewPassword(qu flow.Question, _ string, answer func(pw, confirm []byte, ok bool)) {
	values, ok := q.a.runCredentialDialog(view.NewPasswordDialogOf(qu, q.newKeys()))
	if !ok {
		answer(nil, nil, false)
		return
	}
	answer(values[0], values[1], true)
}

// newKeys returns the plan of the keys being created.
func (q questions) newKeys() interact.KeyPlan {
	if k := q.keys(); k != nil {
		return *k
	}
	return interact.KeyPlan{New: true, Password: true}
}

// RecoveryCode shows the new recovery code once (figure 9.3).
func (q questions) RecoveryCode(code string, answer func()) {
	q.a.runCredentialDialog(view.RecoveryCodeDialogOf(code, q.newKeys()))
	answer()
}

// RetypeRecoveryCode asks for the recovery code shown before.
func (q questions) RetypeRecoveryCode(qu flow.Question, answer func(string, error)) {
	values, ok := q.a.runCredentialDialog(view.RetypeDialogOf(qu, q.newKeys()))
	if !ok {
		answer("", interact.ErrCancelled)
		return
	}
	answer(string(values[0]), nil)
}

// SpareYubiKey asks to connect the spare YubiKey.
func (q questions) SpareYubiKey(qu flow.Question, answer func(bool)) {
	_, ok := q.a.runCredentialDialog(view.SpareYubiKeyDialogOf(qu, q.newKeys()))
	answer(ok)
}
