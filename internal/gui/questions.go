package gui

import (
	"RestoreSafe/internal/gui/flow"
	"RestoreSafe/internal/gui/win32"
	"RestoreSafe/internal/workflow/interact"
	"strings"
)

// questions shows the questions of an operation on the first GUI's
// operation screen and its dialogs (replaced in plan phase 6c).
type questions struct{ a *app }

var _ flow.Dialogs = questions{}

// BackupPlan shows the plan as its preflight report.
func (q questions) BackupPlan(p interact.BackupPlan, answer func()) {
	q.a.machine.PlanShown(p)
	q.a.showPreflight(p.Details)
	answer()
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

// ConfirmBackupStart offers Start (unless the plan is blocked), the other
// plans in opts, and Cancel. Choosing another plan shows it, and this
// question is asked again.
func (q questions) ConfirmBackupStart(opts interact.BackupStartOptions, answer func(interact.BackupStart, error)) {
	a := q.a
	choose := func(choice interact.BackupStart) func() { return func() { answer(choice, nil) } }
	var buttons []opButton
	if !opts.Blocked {
		buttons = append(buttons, opButton{"&Start backup", func() { a.startRunning(); answer(interact.BackupAsPlanned, nil) }})
	}
	if opts.OfferFull {
		buttons = append(buttons, opButton{"&Full backup", choose(interact.BackupFull)})
	}
	if opts.OfferNewKeys {
		buttons = append(buttons, opButton{"&New keys + full backup", func() {
			button, _ := a.taskDialog(win32.TaskDialog{
				Instruction: "Create new keys?",
				Content: "New keys will be created and every source directory gets a full backup. " +
					"Passwords, YubiKey registrations, and recovery codes of the current keys will not open the new backups (they still open older backups).",
				Icon:    win32.TD_WARNING_ICON,
				Buttons: []win32.TaskButton{{ID: win32.IDOK, Text: "Create new keys"}, {ID: win32.IDCANCEL, Text: "Back"}},
			})
			if button == win32.IDOK {
				answer(interact.BackupNewKeys, nil)
			}
		}})
	}
	if opts.OfferAutomatic {
		buttons = append(buttons, opButton{"&Automatic plan", choose(interact.BackupAutomatic)})
	}
	buttons = append(buttons, opButton{"Cancel", choose(interact.BackupCancel)})
	a.offerStart(buttons)
}

// ChooseUnlockMethod offers the regular credentials and the recovery code.
func (q questions) ChooseUnlockMethod(regular string, answer func(bool, error)) {
	button, _ := q.a.taskDialog(win32.TaskDialog{
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

// Password asks for a secret without echo.
func (q questions) Password(qu flow.Question, answer func([]byte, error)) {
	label := "Password:"
	if strings.Contains(strings.ToLower(qu.Prompt), "recovery code") {
		label = "Recovery code:"
	}
	values, ok := q.a.runInputDialog(inputDialog{
		title:          "RestoreSafe",
		heading:        strings.TrimRight(strings.TrimSpace(qu.Prompt), ":"),
		message:        qu.Message,
		messageIsError: qu.Retry,
		fields:         []inputField{{label: label, masked: true}},
	})
	if !ok {
		answer(nil, interact.ErrCancelled)
		return
	}
	answer(values[0], nil)
}

// NewPassword asks for a new password and its confirmation in one dialog.
func (q questions) NewPassword(qu flow.Question, confirmPrompt string, answer func(pw, confirm []byte, ok bool)) {
	values, ok := q.a.runInputDialog(inputDialog{
		title:          "RestoreSafe - new keys",
		heading:        "Choose the backup password",
		message:        qu.Message,
		messageIsError: qu.Retry,
		fields: []inputField{
			{label: strings.TrimRight(strings.TrimSpace(qu.Prompt), ":") + ":", masked: true},
			{label: strings.TrimRight(strings.TrimSpace(confirmPrompt), ":") + ":", masked: true},
		},
	})
	if !ok {
		answer(nil, nil, false)
		return
	}
	answer(values[0], values[1], true)
}

// RecoveryCode shows the new recovery code once, in a dialog that cannot
// copy it (docs/SPEC-restoresafe-gui.md, section 13.3).
func (q questions) RecoveryCode(code string, answer func()) {
	q.a.runInputDialog(inputDialog{
		title:   "RestoreSafe - recovery code",
		heading: "Your recovery code",
		code:    codeLines(code),
		note: "This code alone restores every backup made with these keys, even without password or YubiKey. " +
			"Treat it like the key to a safe.\r\n\r\nWrite it down on paper and store it in a safe place, never next to your backups. It is shown only this once.",
		okText:   "I have written it down",
		noCancel: true,
	})
	answer()
}

// RetypeRecoveryCode asks for the recovery code shown before.
func (q questions) RetypeRecoveryCode(qu flow.Question, answer func(string, error)) {
	values, ok := q.a.runInputDialog(inputDialog{
		title:          "RestoreSafe - recovery code",
		heading:        "Type the recovery code",
		message:        qu.Message,
		messageIsError: qu.Retry,
		fields:         []inputField{{label: "Type the code you wrote down, to confirm it is correct:"}},
	})
	if !ok {
		answer("", interact.ErrCancelled)
		return
	}
	answer(string(values[0]), nil)
}

// SpareYubiKey asks to connect the spare YubiKey.
func (q questions) SpareYubiKey(qu flow.Question, answer func(bool)) {
	content := "Remove YubiKey 1 and insert your spare YubiKey, then click Continue."
	if qu.Message != "" {
		content = qu.Message + "\n\n" + content
	}
	button, _ := q.a.taskDialog(win32.TaskDialog{
		Instruction: "Insert your spare YubiKey",
		Content:     content,
		Icon:        win32.TD_INFORMATION_ICON,
		Buttons:     []win32.TaskButton{{ID: win32.IDOK, Text: "Continue"}, {ID: win32.IDCANCEL, Text: "Cancel"}},
	})
	answer(button == win32.IDOK)
}

// codeLines splits a recovery code (groups separated by dashes) into two
// lines of equal group count, so it fits the dialog in a large font.
func codeLines(code string) string {
	groups := strings.Split(code, "-")
	if len(groups) < 2 {
		return code
	}
	half := (len(groups) + 1) / 2
	return strings.Join(groups[:half], "-") + "\r\n" + strings.Join(groups[half:], "-")
}
