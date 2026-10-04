package gui

import (
	"RestoreSafe/internal/config"
	"RestoreSafe/internal/fsx"
	"RestoreSafe/internal/gui/win32"
	"time"
)

// reloaded is the result of reading the configuration file again; added is
// the copy that adding the missing settings saved.
type reloaded struct {
	cfg   *config.Config
	err   error
	added string
}

// reload reads and validates the configuration file again on a worker
// goroutine (spec ST-2, 11.6): the same checks as at start.
func (a *app) reload() { a.reloadWith(false) }

// addMissing adds the settings the configuration file lacks and reads it
// again (spec ST-10).
func (a *app) addMissing() { a.reloadWith(true) }

func (a *app) reloadWith(add bool) {
	if a.reloading || a.machine.Busy() {
		return
	}
	a.reloading = true
	a.refreshShell()
	path := a.opts.ConfigPath
	go func() {
		r := &reloaded{}
		if add {
			r.added, r.err = config.AddMissing(path, time.Now())
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
// an operation runs is used once it has finished.
func (a *app) reloadDone() {
	a.mu.Lock()
	r := a.pendingReload
	a.pendingReload = nil
	a.mu.Unlock()
	if r == nil {
		return
	}
	a.reloading = false
	a.addedCopy = r.added
	if r.err != nil {
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
}

// useConfig swaps the configuration and checks the backups with it.
func (a *app) useConfig(cfg *config.Config) {
	a.opts.Config = cfg
	a.backupDir = fsx.ResolveDir(cfg.BackupDirectory, a.opts.ExeDir)
	a.deferredConfig = nil
	a.recheck = a.checking
	a.startCheck()
	a.refreshShell()
}
