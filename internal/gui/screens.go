package gui

import (
	"RestoreSafe/internal/format/catalog"
	"RestoreSafe/internal/format/naming"
	"RestoreSafe/internal/gui/win32"
	"RestoreSafe/internal/workflow/interact"
	"path/filepath"
	"strings"
)

// showSelection shows the backup selection tree: backup runs, newest first,
// with their backup sets (replaced by the restore wizard, pages 1 and 2 in
// docs/SPEC-restoresafe-gui.md, section 8). done is
// called with the chosen sets, or with ok=false when the user cancelled.
func (a *app) showSelection(action string, runs []catalog.BackupRunSummary, done func(entries []naming.BackupEntry, ok bool)) {
	tree := a.op.tree
	win32.ClearTree(tree)
	a.treeNodes = make(map[win32.TreeItem]selectionNode)
	a.selectRuns = runs
	a.selectAction = action
	var first win32.TreeItem
	for i, run := range runs {
		item := win32.InsertTreeItem(tree, 0, runNodeLabel(run))
		a.treeNodes[item] = selectionNode{run: i, entry: -1}
		for j, e := range run.Entries {
			a.treeNodes[win32.InsertTreeItem(tree, item, setNodeLabel(e))] = selectionNode{run: i, entry: j}
		}
		win32.ExpandTreeItem(tree, item)
		if i == 0 {
			first = item
		}
	}

	verb := "&Restore"
	if action == "verify" {
		verb = "&Verify"
	}
	a.setOpScreen(opTitle(a.machine.Current().Op), interact.StatusNone, "", contentTree, false, []opButton{
		{verb + " selected", func() {
			entries := selectionEntries(a.selectRuns, a.treeNodes[win32.TreeSelection(tree)])
			if len(entries) == 0 {
				return
			}
			done(entries, true)
		}},
		{"Cancel", func() { done(nil, false) }},
	})
	win32.SelectTreeItem(tree, first)
	a.onTreeSelection()
	win32.SetFocus(tree)
}

// onTreeSelection explains the selected node in the detail line.
func (a *app) onTreeSelection() {
	node, ok := a.treeNodes[win32.TreeSelection(a.op.tree)]
	if !ok {
		node = selectionNode{run: -1}
	}
	win32.SetText(a.op.detail, selectionText(a.selectAction, a.selectRuns, node))
}

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

// clickDefault runs the first (default) button of the operation screen, if
// it is enabled (Enter).
func (a *app) clickDefault() {
	if len(a.opButtons) > 0 && win32.IsEnabled(a.op.buttons[0]) {
		a.opButtons[0].onClick()
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
