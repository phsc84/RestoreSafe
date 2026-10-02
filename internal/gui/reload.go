package gui

import (
	"RestoreSafe/internal/config"
	"RestoreSafe/internal/fsx"
	"RestoreSafe/internal/gui/win32"
)

// reloaded is the result of reading the configuration file again.
type reloaded struct {
	cfg *config.Config
	err error
}

// reload reads and validates the configuration file again on a worker
// goroutine (spec ST-2, 11.6): the same checks as at start.
func (a *app) reload() {
	if a.reloading || a.machine.Busy() {
		return
	}
	a.reloading = true
	a.refreshShell()
	path := a.opts.ConfigPath
	go func() {
		cfg, err := config.Load(path)
		a.mu.Lock()
		a.pendingReload = &reloaded{cfg: cfg, err: err}
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
