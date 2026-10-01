package gui

import (
	"RestoreSafe/internal/config"
	"RestoreSafe/internal/format/catalog"
	"RestoreSafe/internal/fsx"
	"RestoreSafe/internal/workflow/interact"
	"RestoreSafe/internal/workflow/restore"
	"RestoreSafe/internal/workflow/verify"
	"context"
	"errors"
	"fmt"
)

// runRestore asks which backup to restore and where to, then runs the
// restore. It runs on the worker goroutine.
func (g *guiUI) runRestore(ctx context.Context, cfg *config.Config, exeDir string) error {
	backupDir := fsx.ResolveDir(cfg.BackupDirectory, exeDir)
	runs, err := g.backupRuns(backupDir)
	if err != nil || len(runs) == 0 {
		return err
	}
	sets, err := g.selectBackups("restore", runs)
	if err == nil {
		var dest string
		dest, err = g.restoreDestination(backupDir)
		if err == nil {
			return restore.Run(ctx, g, cfg, exeDir, restore.Request{Sets: sets, Destination: dest})
		}
	}
	if errors.Is(err, interact.ErrCancelled) {
		fmt.Fprintln(g.Output(), "Restore cancelled.")
		return nil
	}
	return err
}

// runVerify asks which backup to verify, then runs the verification. It runs
// on the worker goroutine.
func (g *guiUI) runVerify(ctx context.Context, cfg *config.Config, exeDir string) error {
	runs, err := g.backupRuns(fsx.ResolveDir(cfg.BackupDirectory, exeDir))
	if err != nil || len(runs) == 0 {
		return err
	}
	sets, err := g.selectBackups("verify", runs)
	if errors.Is(err, interact.ErrCancelled) {
		fmt.Fprintln(g.Output(), "Verification cancelled.")
		return nil
	}
	if err != nil {
		return err
	}
	return verify.Run(ctx, g, cfg, exeDir, verify.Request{Sets: sets})
}

// backupRuns lists the backup runs to choose from, newest first. Without
// any, it says so in the output and returns none.
func (g *guiUI) backupRuns(backupDir string) ([]catalog.BackupRunSummary, error) {
	infos, err := catalog.Inventory(backupDir)
	if err != nil {
		return nil, fmt.Errorf("Failed to scan backup directory %q: %w. Remedy: Check the backup_directory path in config.yaml and ensure the directory exists and is readable.", backupDir, err)
	}
	runs := catalog.BackupRunSummaries(infos)
	if len(runs) == 0 {
		fmt.Fprintln(g.Output(), "No complete backups found in backup directory. Remedy: Check whether .enc files are in the backup directory and whether the correct directory is configured.")
	}
	return runs, nil
}
