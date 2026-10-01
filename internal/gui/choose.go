package gui

import (
	"RestoreSafe/internal/config"
	"RestoreSafe/internal/format/catalog"
	"RestoreSafe/internal/format/naming"
	"RestoreSafe/internal/fsx"
	"RestoreSafe/internal/gui/flow"
	"RestoreSafe/internal/workflow/interact"
	"RestoreSafe/internal/workflow/restore"
	"RestoreSafe/internal/workflow/verify"
	"context"
	"errors"
	"fmt"
)

// runRestore asks which backup to restore and where to, then runs the
// restore. It runs on the worker goroutine.
func (a *app) runRestore(ctx context.Context, u *flow.UI, cfg *config.Config, exeDir string) error {
	backupDir := fsx.ResolveDir(cfg.BackupDirectory, exeDir)
	runs, err := backupRuns(u, backupDir)
	if err != nil || len(runs) == 0 {
		return err
	}
	sets, err := a.selectBackups(u.Bridge(), "restore", runs)
	if err == nil {
		var dest string
		dest, err = a.restoreDestination(u.Bridge(), backupDir)
		if err == nil {
			return restore.Run(ctx, u, cfg, exeDir, restore.Request{Sets: sets, Destination: dest})
		}
	}
	if errors.Is(err, interact.ErrCancelled) {
		fmt.Fprintln(u.Output(), "Restore cancelled.")
		return nil
	}
	return err
}

// runVerify asks which backup to verify, then runs the verification. It runs
// on the worker goroutine.
func (a *app) runVerify(ctx context.Context, u *flow.UI, cfg *config.Config, exeDir string) error {
	runs, err := backupRuns(u, fsx.ResolveDir(cfg.BackupDirectory, exeDir))
	if err != nil || len(runs) == 0 {
		return err
	}
	sets, err := a.selectBackups(u.Bridge(), "verify", runs)
	if errors.Is(err, interact.ErrCancelled) {
		fmt.Fprintln(u.Output(), "Verification cancelled.")
		return nil
	}
	if err != nil {
		return err
	}
	return verify.Run(ctx, u, cfg, exeDir, verify.Request{Sets: sets})
}

// backupRuns lists the backup runs to choose from, newest first. Without
// any, it says so in the output and returns none.
func backupRuns(u *flow.UI, backupDir string) ([]catalog.BackupRunSummary, error) {
	infos, err := catalog.Inventory(backupDir)
	if err != nil {
		return nil, fmt.Errorf("Failed to scan backup directory %q: %w. Remedy: Check the backup_directory path in config.yaml and ensure the directory exists and is readable.", backupDir, err)
	}
	runs := catalog.BackupRunSummaries(infos)
	if len(runs) == 0 {
		fmt.Fprintln(u.Output(), "No complete backups found in backup directory. Remedy: Check whether .enc files are in the backup directory and whether the correct directory is configured.")
	}
	return runs, nil
}

// selectBackups shows the selection tree: a whole backup run or a single
// backup set. It returns ErrCancelled when the user cancels.
func (a *app) selectBackups(b *flow.Bridge, action string, runs []catalog.BackupRunSummary) ([]naming.BackupEntry, error) {
	v, err := b.Ask(func(answer func(any, error)) {
		a.showSelection(action, runs, func(entries []naming.BackupEntry, ok bool) {
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
func (a *app) restoreDestination(b *flow.Bridge, backupDir string) (string, error) {
	v, err := b.Ask(func(answer func(any, error)) {
		a.showDestination(backupDir, func(path string, ok bool) {
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
