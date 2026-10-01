package gui

import (
	"RestoreSafe/internal/config"
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

// runRestore asks where to restore the sets chosen on the Backups page,
// then runs the restore. It runs on the worker goroutine.
func (a *app) runRestore(ctx context.Context, u *flow.UI, cfg *config.Config, exeDir string, sets []naming.BackupEntry) error {
	backupDir := fsx.ResolveDir(cfg.BackupDirectory, exeDir)
	dest, err := a.restoreDestination(u.Bridge(), backupDir)
	if errors.Is(err, interact.ErrCancelled) {
		fmt.Fprintln(u.Output(), "Restore cancelled.")
		return nil
	}
	if err != nil {
		return err
	}
	return restore.Run(ctx, u, cfg, exeDir, restore.Request{Sets: sets, Destination: dest})
}

// runVerify verifies the sets chosen on the Backups page. It runs on the
// worker goroutine.
func (a *app) runVerify(ctx context.Context, u *flow.UI, cfg *config.Config, exeDir string, sets []naming.BackupEntry) error {
	return verify.Run(ctx, u, cfg, exeDir, verify.Request{Sets: sets})
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
