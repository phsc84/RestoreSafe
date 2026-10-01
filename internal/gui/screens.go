package gui

import (
	"RestoreSafe/internal/gui/win32"
	"RestoreSafe/internal/workflow/interact"
	"path/filepath"
	"strings"
)

// showDestination shows the restore destination (section 6.3): a folder
// field with Browse..., and restoring into the backup directory itself.
// done is called with the folder, or with ok=false when the user cancelled.
func (a *app) showDestination(backupDir string, done func(path string, ok bool)) {
	o := &a.op
	a.destDefault = backupDir
	win32.SetText(o.destLabel, "Restore into this &folder:")
	win32.SetText(o.destEdit, "")
	win32.Enable(o.destEdit, true)
	win32.SetText(o.destBrowse, "&Browse...")
	win32.Enable(o.destBrowse, true)
	win32.SetText(o.destCheck, "Restore into the backup &directory itself")
	win32.SetChecked(o.destCheck, false)
	win32.SetText(o.destNote, "RestoreSafe creates one folder per backup set in it, named like the backed-up folder (e.g. Documents). These folders must not exist yet; the preflight checks it.")

	a.setOpScreen(opTitle(a.machine.Current().Op), interact.StatusNone, "Where should the backup be restored?", contentDestination, false, []opButton{
		{"&Next", func() {
			path := a.destinationPath()
			if path == "" {
				return
			}
			done(path, true)
		}},
		{"Cancel", func() { done("", false) }},
	})
	a.updateDestination()
	win32.SetFocus(o.destEdit)
}

// destinationPath returns the chosen restore folder, or "" when none is
// entered.
func (a *app) destinationPath() string {
	if win32.Checked(a.op.destCheck) {
		return a.destDefault
	}
	path := strings.TrimSpace(win32.Text(a.op.destEdit))
	if path == "" {
		return ""
	}
	return filepath.Clean(path)
}

// updateDestination enables the field and Next according to the checkbox
// and the entered path.
func (a *app) updateDestination() {
	o := &a.op
	into := win32.Checked(o.destCheck)
	win32.Enable(o.destEdit, !into)
	win32.Enable(o.destBrowse, !into)
	if into {
		win32.SetText(o.destEdit, a.destDefault)
	}
	if len(a.opButtons) > 0 {
		win32.Enable(o.buttons[0], a.destinationPath() != "")
	}
}

// browseDestination opens the folder picker.
func (a *app) browseDestination() {
	start := strings.TrimSpace(win32.Text(a.op.destEdit))
	if start == "" {
		start = filepath.Dir(a.destDefault)
	}
	path, ok, err := win32.PickFolder(a.hwnd, "Restore into this folder", start)
	if err != nil {
		win32.MessageBox(a.hwnd, err.Error(), "RestoreSafe", win32.MB_OK|win32.MB_ICONERROR)
		return
	}
	if ok {
		win32.SetText(a.op.destEdit, path)
		a.updateDestination()
	}
}

// clickCancel runs the screen's Cancel button, if it has one (Esc).
func (a *app) clickCancel() {
	for i, b := range a.opButtons {
		if b.text == "Cancel" && win32.IsEnabled(a.op.buttons[i]) {
			b.onClick()
			return
		}
	}
}
