package gui

import (
	"RestoreSafe/internal/format/catalog"
	"RestoreSafe/internal/format/naming"
	"RestoreSafe/internal/gui/win32"
	"RestoreSafe/internal/security/cryptox"
	"RestoreSafe/internal/workflow/interact"
	"bytes"
	"io"
	"strings"
)

// guiUI implements interact.UI for one operation: every question is forwarded to
// the UI thread through the bridge.
type guiUI struct {
	app *app
	b   *bridge
	op  operation
	// seenOutput marks the output already shown next to a question, so a
	// dialog only repeats a message written since the previous one.
	seenOutput int
	// Retries of the same question show the message as an error.
	lastPasswordPrompt string
	newPasswordAsked   bool
	retypeAsked        bool
}

var _ interact.UI = (*guiUI)(nil)

func (g *guiUI) Output() io.Writer            { return g.b }
func (g *guiUI) Progress(p interact.Progress) { g.b.Progress(p) }
func (g *guiUI) ShowResult(r interact.Result) { g.b.setResult(r) }

// ShowRecoveryCode shows the new recovery code once, in a dialog that cannot
// copy it (docs/SPEC-restoresafe-gui.md, section 13.3).
func (g *guiUI) ShowRecoveryCode(code string) {
	g.b.ask(func(answer func(any, error)) {
		g.app.runInputDialog(inputDialog{
			title:   "RestoreSafe - recovery code",
			heading: "Your recovery code",
			code:    codeLines(code),
			note: "This code alone restores every backup made with these keys, even without password or YubiKey. " +
				"Treat it like the key to a safe.\r\n\r\nWrite it down on paper and store it in a safe place, never next to your backups. It is shown only this once.",
			okText:   "I have written it down",
			noCancel: true,
		})
		answer(nil, nil)
	}, nil, nil)
}

// ShowBackupPlan, ShowRestorePlan and ShowVerifyPlan show the plan as its
// preflight report.
func (g *guiUI) ShowBackupPlan(p interact.BackupPlan)   { g.showReport(p.Details) }
func (g *guiUI) ShowRestorePlan(p interact.RestorePlan) { g.showReport(p.Details) }
func (g *guiUI) ShowVerifyPlan(p interact.VerifyPlan)   { g.showReport(p.Details) }

// showReport shows the preflight; it returns once the report is on screen.
func (g *guiUI) showReport(r interact.Report) {
	g.b.ask(func(answer func(any, error)) {
		g.app.showPreflight(r)
		answer(nil, nil)
	}, nil, nil)
}

// selectBackups shows the selection tree: a whole backup run or a single
// backup set. It returns ErrCancelled when the user cancels.
func (g *guiUI) selectBackups(action string, runs []catalog.BackupRunSummary) ([]naming.BackupEntry, error) {
	v, err := g.b.ask(func(answer func(any, error)) {
		g.app.showSelection(action, runs, func(entries []naming.BackupEntry, ok bool) {
			if !ok {
				answer(nil, interact.ErrCancelled)
				return
			}
			answer(entries, nil)
		})
	}, nil, interact.ErrCancelled)
	if err != nil {
		return nil, err
	}
	return v.([]naming.BackupEntry), nil
}

// restoreDestination shows the destination screen. It returns ErrCancelled
// when the user cancels.
func (g *guiUI) restoreDestination(backupDir string) (string, error) {
	v, err := g.b.ask(func(answer func(any, error)) {
		g.app.showDestination(backupDir, func(path string, ok bool) {
			if !ok {
				answer(nil, interact.ErrCancelled)
				return
			}
			answer(path, nil)
		})
	}, nil, interact.ErrCancelled)
	if err != nil {
		return "", err
	}
	return v.(string), nil
}

// ConfirmStart offers Start and Cancel under the preflight.
func (g *guiUI) ConfirmStart(action string) (bool, error) {
	v, err := g.b.ask(func(answer func(any, error)) {
		label := "&Start restore"
		if action == "verification" {
			label = "&Start verification"
		}
		g.app.offerStart([]opButton{
			{label, func() { g.app.startRunning(); answer(true, nil) }},
			{"Cancel", func() { answer(false, nil) }},
		})
	}, false, nil)
	return v.(bool), err
}

// ConfirmBackupStart offers Start (unless the plan is blocked), the other
// plans in opts, and Cancel. Choosing another plan shows it, and this
// question is asked again.
func (g *guiUI) ConfirmBackupStart(opts interact.BackupStartOptions) (interact.BackupStart, error) {
	v, err := g.b.ask(func(answer func(any, error)) {
		choose := func(choice interact.BackupStart) func() { return func() { answer(choice, nil) } }
		var buttons []opButton
		if !opts.Blocked {
			buttons = append(buttons, opButton{"&Start backup", func() { g.app.startRunning(); answer(interact.BackupAsPlanned, nil) }})
		}
		if opts.OfferFull {
			buttons = append(buttons, opButton{"&Full backup", choose(interact.BackupFull)})
		}
		if opts.OfferNewKeys {
			buttons = append(buttons, opButton{"&New keys + full backup", func() {
				button, _ := g.app.taskDialog(win32.TaskDialog{
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
		g.app.offerStart(buttons)
	}, interact.BackupCancel, nil)
	return v.(interact.BackupStart), err
}

// ChooseUnlockMethod offers the regular credentials and the recovery code.
func (g *guiUI) ChooseUnlockMethod(regular string) (bool, error) {
	v, err := g.b.ask(func(answer func(any, error)) {
		button, _ := g.app.taskDialog(win32.TaskDialog{
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
	}, false, interact.ErrCancelled)
	return v.(bool), err
}

// recentMessage returns the last output line written since the previous
// question, for showing it in the next dialog (e.g. "Wrong password. 2
// attempt(s) remaining."). Log lines (starting with a timestamp) are left
// out.
func (g *guiUI) recentMessage() string {
	seq, last := g.b.outputMark()
	if seq == g.seenOutput || strings.HasPrefix(last, "[") {
		g.seenOutput = seq
		return ""
	}
	g.seenOutput = seq
	return last
}

// Password asks for a secret without echo.
func (g *guiUI) Password(prompt string) ([]byte, error) {
	retry := g.lastPasswordPrompt == prompt
	g.lastPasswordPrompt = prompt
	message := g.recentMessage()
	v, err := g.b.ask(func(answer func(any, error)) {
		heading := strings.TrimRight(strings.TrimSpace(prompt), ":")
		label := "Password:"
		if strings.Contains(strings.ToLower(prompt), "recovery code") {
			label = "Recovery code:"
		}
		values, ok := g.app.runInputDialog(inputDialog{
			title:          "RestoreSafe",
			heading:        heading,
			message:        message,
			messageIsError: retry,
			fields:         []inputField{{label: label, masked: true}},
		})
		if !ok {
			answer(nil, interact.ErrCancelled)
			return
		}
		answer(values[0], nil)
	}, nil, interact.ErrCancelled)
	if err != nil {
		return nil, err
	}
	return v.([]byte), nil
}

// NewPassword asks for a new password and its confirmation in one dialog,
// with the rules and errors of interact.ReadPasswordConfirmed.
func (g *guiUI) NewPassword(prompt, confirmPrompt string) ([]byte, error) {
	retry := g.newPasswordAsked
	g.newPasswordAsked = true
	message := g.recentMessage()
	v, err := g.b.ask(func(answer func(any, error)) {
		values, ok := g.app.runInputDialog(inputDialog{
			title:          "RestoreSafe - new keys",
			heading:        "Choose the backup password",
			message:        message,
			messageIsError: retry,
			fields: []inputField{
				{label: strings.TrimRight(strings.TrimSpace(prompt), ":") + ":", masked: true},
				{label: strings.TrimRight(strings.TrimSpace(confirmPrompt), ":") + ":", masked: true},
			},
		})
		if !ok {
			answer(nil, interact.ErrCancelled)
			return
		}
		pw, confirm := values[0], values[1]
		defer cryptox.ZeroBytes(confirm)
		switch {
		case len(pw) == 0:
			cryptox.ZeroBytes(pw)
			answer(nil, interact.ErrPasswordEmpty)
		case !bytes.Equal(pw, confirm):
			cryptox.ZeroBytes(pw)
			answer(nil, interact.ErrPasswordMismatch)
		default:
			answer(pw, nil)
		}
	}, nil, interact.ErrCancelled)
	if err != nil {
		return nil, err
	}
	return v.([]byte), nil
}

// RetypeRecoveryCode asks for the recovery code shown before.
func (g *guiUI) RetypeRecoveryCode() (string, error) {
	retry := g.retypeAsked
	g.retypeAsked = true
	message := g.recentMessage()
	v, err := g.b.ask(func(answer func(any, error)) {
		values, ok := g.app.runInputDialog(inputDialog{
			title:          "RestoreSafe - recovery code",
			heading:        "Type the recovery code",
			message:        message,
			messageIsError: retry,
			fields:         []inputField{{label: "Type the code you wrote down, to confirm it is correct:"}},
		})
		if !ok {
			answer(nil, interact.ErrCancelled)
			return
		}
		answer(string(values[0]), nil)
	}, nil, interact.ErrCancelled)
	if err != nil {
		return "", err
	}
	return v.(string), nil
}

// WaitForSpareYubiKey waits until the user confirms the spare YubiKey is
// connected.
func (g *guiUI) WaitForSpareYubiKey() (bool, error) {
	message := g.recentMessage()
	v, err := g.b.ask(func(answer func(any, error)) {
		content := "Remove YubiKey 1 and insert your spare YubiKey, then click Continue."
		if message != "" {
			content = message + "\n\n" + content
		}
		button, _ := g.app.taskDialog(win32.TaskDialog{
			Instruction: "Insert your spare YubiKey",
			Content:     content,
			Icon:        win32.TD_INFORMATION_ICON,
			Buttons:     []win32.TaskButton{{ID: win32.IDOK, Text: "Continue"}, {ID: win32.IDCANCEL, Text: "Cancel"}},
		})
		answer(button == win32.IDOK, nil)
	}, false, nil)
	return v.(bool), err
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
