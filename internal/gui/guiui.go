package gui

import (
	"RestoreSafe/internal/catalog"
	"RestoreSafe/internal/security"
	"RestoreSafe/internal/ui"
	"RestoreSafe/internal/util"
	"RestoreSafe/internal/win32"
	"bytes"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

// guiUI implements ui.UI for one operation: every question is forwarded to
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

var _ ui.UI = (*guiUI)(nil)

func (g *guiUI) Output() io.Writer      { return g.b }
func (g *guiUI) Progress(p ui.Progress) { g.b.Progress(p) }
func (g *guiUI) ShowResult(r ui.Result) { g.b.setResult(r) }
func (g *guiUI) ShowRecoveryCode(code string) {
	g.b.ask(func(answer func(any, error)) {
		g.app.taskDialog(win32.TaskDialog{
			Instruction: "Your recovery code",
			Content: code + "\n\nThis code alone restores every backup made with these keys, even without password or YubiKey. " +
				"Treat it like the key to a safe. Write it down on paper and store it in a safe place, never next to your backups. It is shown only this once.",
			Icon:    win32.TD_WARNING_ICON,
			Buttons: []win32.TaskButton{{ID: win32.IDOK, Text: "I have written it down"}},
		})
		answer(nil, nil)
	}, nil, nil)
}

// ShowReport shows the preflight; it returns once the report is on screen.
func (g *guiUI) ShowReport(r ui.Report) {
	g.b.ask(func(answer func(any, error)) {
		g.app.showPreflight(r)
		answer(nil, nil)
	}, nil, nil)
}

// SelectBackups lets the user choose a backup run. The tree with single
// backup sets follows in GUI phase G5.
func (g *guiUI) SelectBackups(action string, runs []catalog.BackupRunSummary) ([]util.BackupEntry, error) {
	v, err := g.b.ask(func(answer func(any, error)) {
		radios := make([]win32.TaskButton, len(runs))
		for i, run := range runs {
			radios[i] = win32.TaskButton{ID: int32(100 + i), Text: runLabel(run)}
		}
		verb := "Restore"
		if action == "verify" {
			verb = "Verify"
		}
		button, radio := g.app.taskDialog(win32.TaskDialog{
			Instruction:  "Choose the backup run to " + action,
			Content:      "All backup sets of the chosen run are " + completedAction(action) + ".",
			Buttons:      []win32.TaskButton{{ID: win32.IDOK, Text: verb}, {ID: win32.IDCANCEL, Text: "Cancel"}},
			Radios:       radios,
			DefaultRadio: 100,
		})
		if button != win32.IDOK || radio < 100 || int(radio-100) >= len(runs) {
			answer(nil, ui.ErrCancelled)
			return
		}
		answer(runs[radio-100].Entries, nil)
	}, nil, ui.ErrCancelled)
	if err != nil {
		return nil, err
	}
	return v.([]util.BackupEntry), nil
}

// runLabel describes a backup run in one line.
func runLabel(run catalog.BackupRunSummary) string {
	names := make([]string, len(run.Entries))
	diff := false
	for i, e := range run.Entries {
		names[i] = e.DirectoryName
		diff = diff || e.IsDiff()
	}
	kind := "full"
	if diff {
		kind = "differential"
	}
	return fmt.Sprintf("%s  %s  (%s: %s)", run.RunID, run.Created.Local().Format("2006-01-02 15:04"), kind, strings.Join(names, ", "))
}

func completedAction(action string) string {
	if action == "verify" {
		return "verified"
	}
	return "restored"
}

// RestoreDestination asks for the folder to restore into. The folder picker
// follows in GUI phase G5.
func (g *guiUI) RestoreDestination(backupDir string) (string, error) {
	v, err := g.b.ask(func(answer func(any, error)) {
		values, ok := g.app.runInputDialog(inputDialog{
			title:   "Restore destination",
			heading: "Restore into which folder?",
			message: "Leave the field empty to restore into the backup directory itself.",
			fields:  []inputField{{label: "Folder:"}},
			okText:  "Next",
		})
		if !ok {
			answer(nil, ui.ErrCancelled)
			return
		}
		path := strings.TrimSpace(string(values[0]))
		if path == "" {
			path = backupDir
		}
		answer(filepath.Clean(path), nil)
	}, nil, ui.ErrCancelled)
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

// ConfirmBackupStart offers Start, the alternatives in opts, and Cancel.
func (g *guiUI) ConfirmBackupStart(opts ui.BackupStartOptions) (ui.BackupStart, error) {
	v, err := g.b.ask(func(answer func(any, error)) {
		start := func(choice ui.BackupStart) func() {
			return func() { g.app.startRunning(); answer(choice, nil) }
		}
		buttons := []opButton{{"&Start backup", start(ui.BackupAsPlanned)}}
		if opts.OfferNewKeys && opts.OfferFull {
			buttons = append(buttons, opButton{"&Full backup", start(ui.BackupFull)})
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
					start(ui.BackupNewKeys)()
				}
			}})
		}
		buttons = append(buttons, opButton{"Cancel", func() { answer(ui.BackupCancel, nil) }})
		g.app.offerStart(buttons)
	}, ui.BackupCancel, nil)
	return v.(ui.BackupStart), err
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
			answer(false, ui.ErrCancelled)
		}
	}, false, ui.ErrCancelled)
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
		label := strings.TrimRight(strings.TrimSpace(prompt), ":")
		values, ok := g.app.runInputDialog(inputDialog{
			title:          "RestoreSafe",
			heading:        label,
			message:        message,
			messageIsError: retry,
			fields:         []inputField{{label: label + ":", masked: true}},
		})
		if !ok {
			answer(nil, ui.ErrCancelled)
			return
		}
		answer(values[0], nil)
	}, nil, ui.ErrCancelled)
	if err != nil {
		return nil, err
	}
	return v.([]byte), nil
}

// NewPassword asks for a new password and its confirmation in one dialog,
// with the rules and errors of security.ReadPasswordConfirmed.
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
			answer(nil, ui.ErrCancelled)
			return
		}
		pw, confirm := values[0], values[1]
		defer security.ZeroBytes(confirm)
		switch {
		case len(pw) == 0:
			security.ZeroBytes(pw)
			answer(nil, security.ErrPasswordEmpty)
		case !bytes.Equal(pw, confirm):
			security.ZeroBytes(pw)
			answer(nil, security.ErrPasswordMismatch)
		default:
			answer(pw, nil)
		}
	}, nil, ui.ErrCancelled)
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
			answer(nil, ui.ErrCancelled)
			return
		}
		answer(string(values[0]), nil)
	}, nil, ui.ErrCancelled)
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
