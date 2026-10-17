package gui

import (
	"time"

	"github.com/phsc84/restoresafe/internal/config"
	"github.com/phsc84/restoresafe/internal/fsx"
	"github.com/phsc84/restoresafe/internal/gui/view"
	"github.com/phsc84/restoresafe/internal/gui/win32"
)

// reloaded is the result of reading the configuration file again. updating
// is set when the file was brought up to date first; copyPath is the copy
// of the previous file that it saved, "" when there was nothing to do.
type reloaded struct {
	cfg      *config.Config
	err      error
	updating bool
	copyPath string
}

// reload reads and validates the configuration file again on a worker
// goroutine (GUI spec ST-2, 11.6): the same checks as at start.
func (a *app) reload() { a.reloadWith(false) }

// updateConfig brings the configuration file up to date and reads it again
// (GUI spec ST-10).
func (a *app) updateConfig() { a.reloadWith(true) }

func (a *app) reloadWith(update bool) {
	if a.reloading || a.machine.Busy() || a.yk.testing {
		return
	}
	a.reloading = true
	a.refreshShell()
	path := a.opts.ConfigPath
	go func() {
		r := &reloaded{updating: update}
		if update {
			r.copyPath, r.err = config.Update(path, time.Now())
		}
		if r.err == nil {
			r.cfg, r.err = config.Load(path)
		}
		a.mu.Lock()
		a.pendingReload = r
		a.mu.Unlock()
		win32.PostMessage(a.hwnd, msgReloaded, 0, 0) //nolint:errcheck
	}()
}

// reloadDone uses the configuration read again, or keeps the previous one
// and shows why the file did not load. A configuration that arrives while
// an operation runs is used once it has finished. An update that failed is
// told in a dialog, not on the Settings page: the file wasn't changed.
func (a *app) reloadDone() {
	a.mu.Lock()
	r := a.pendingReload
	a.pendingReload = nil
	a.mu.Unlock()
	if r == nil {
		return
	}
	a.reloading = false
	if r.err != nil {
		if r.updating {
			a.refreshShell()
			a.inform(view.ConfigNotUpdated(a.opts.ConfigPath, r.err), win32.TD_ERROR_ICON)
			return
		}
		a.reloadErr = r.err
		a.refreshShell()
		return
	}
	a.reloadErr = nil
	if a.machine.Busy() {
		a.deferredConfig = r.cfg
		a.refreshShell()
		return
	}
	a.useConfig(r.cfg)
	if r.copyPath != "" {
		a.inform(view.ConfigUpdated(a.opts.ConfigPath, r.copyPath), win32.TD_INFORMATION_ICON)
	}
}

// useConfig swaps the configuration and checks the backups with it.
func (a *app) useConfig(cfg *config.Config) {
	a.opts.Config = cfg
	a.backupDir = fsx.ResolveDir(cfg.BackupDirectory, a.opts.ExeDir)
	a.deferredConfig = nil
	a.recheck = a.checking
	a.startCheck()
	// The YubiKey is checked again too; a test result may be of other keys
	// (GUI spec YK-3, YK-6).
	a.clearYubiKeyTest()
	if a.yubiKeyVisible() {
		a.diagnoseYubiKey()
	}
	a.refreshShell()
	a.offerConfigUpdate()
}

// offerConfigUpdate asks, once the current message is handled, to bring the
// configuration file up to date when it lacks settings or has retired ones
// (GUI spec ST-10): at start and with each configuration read again, until
// the file is up to date.
func (a *app) offerConfigUpdate() {
	if view.ConfigNoticeOf(a.opts.Config, a.opts.ConfigPath) != nil {
		win32.PostMessage(a.hwnd, msgAskUpdate, 0, 0) //nolint:errcheck
	}
}

// askConfigUpdate shows the dialog of offerConfigUpdate and updates the
// file when the user chooses to.
func (a *app) askConfigUpdate() {
	n := view.ConfigNoticeOf(a.opts.Config, a.opts.ConfigPath)
	if n == nil || a.askingUpdate {
		return
	}
	a.askingUpdate = true
	button, _ := a.taskDialog(a.dialogOwner(), win32.TaskDialog{
		Instruction: n.Instruction,
		Content:     n.Content,
		Icon:        win32.TD_INFORMATION_ICON,
		Buttons:     []win32.TaskButton{{ID: win32.IDOK, Text: n.Update}, {ID: win32.IDCANCEL, Text: n.Later}},
		Default:     win32.IDOK,
	})
	a.askingUpdate = false
	if button == win32.IDOK {
		a.updateConfig()
	}
}

// inform shows m in a task dialog with icon and one button, m.Yes.
func (a *app) inform(m view.Confirm, icon uintptr) {
	a.taskDialog(a.dialogOwner(), win32.TaskDialog{
		Instruction: m.Instruction,
		Content:     m.Content,
		Icon:        icon,
		Buttons:     []win32.TaskButton{{ID: win32.IDOK, Text: m.Yes}},
		Default:     win32.IDOK,
	})
}
