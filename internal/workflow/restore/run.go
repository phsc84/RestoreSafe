// Package restore restores the backup sets the user chose before Run (with
// the full backup a differential needs) into a destination folder:
//  1. Lock the backup directory shared, so no backup runs at the same time
//  2. Check the sets and the destination, show the plan, and ask to start
//  3. Unlock the keys (password and/or YubiKey, or the recovery code; up to
//     3 attempts)
//  4. Decrypt, extract, and check every file against its manifest hash
package restore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/phsc84/restoresafe/internal/config"
	"github.com/phsc84/restoresafe/internal/format/catalog"
	"github.com/phsc84/restoresafe/internal/format/naming"
	"github.com/phsc84/restoresafe/internal/fsx"
	"github.com/phsc84/restoresafe/internal/logging"
	"github.com/phsc84/restoresafe/internal/problem"
	"github.com/phsc84/restoresafe/internal/security/yubikey"
	"github.com/phsc84/restoresafe/internal/workflow/interact"
	"github.com/phsc84/restoresafe/internal/workflow/job"
	"github.com/phsc84/restoresafe/internal/workflow/restorepoint"
	"github.com/phsc84/restoresafe/internal/workflow/unlock"
)

// Request is what the user chose to restore.
type Request struct {
	// Sets are the backup sets to restore; each is restored into its own
	// folder, named like the backed-up folder.
	Sets []naming.BackupEntry
	// Destination is the folder the sets are restored into.
	Destination string
}

// Run executes the restore of req, asking u for confirmation and credentials
// and reporting progress to it. Cancelling ctx stops the restore; the
// returned error then matches context.Canceled.
func Run(ctx context.Context, u interact.UI, cfg *config.Config, exeDir string, req Request) error {
	out := u.Output()
	backupDir := fsx.ResolveDir(cfg.BackupDirectory, exeDir)
	if strings.TrimSpace(req.Destination) == "" {
		return problem.New("No restore destination chosen.").WithRemedy("Choose the folder to restore into.")
	}
	restorePath := req.Destination

	lock, lockIssue, err := job.LockForReading(ctx, u, backupDir)
	if err != nil {
		return err
	}
	defer lock.Release()

	infos, err := catalog.Inventory(backupDir)
	if err != nil {
		return problem.Errorf("Failed to scan backup directory %q: %w.", backupDir, err).WithRemedy("Check the backup_directory path in config.yaml and ensure the directory exists and is readable.")
	}
	selectedInfos, err := job.SelectSets(infos, req.Sets)
	if err != nil {
		return err
	}

	first := selectedInfos[0].Header
	logPath := naming.LogFileName(backupDir, first.Date, naming.BackupID(first.RunID))
	log := job.OpenLogger(cfg, logPath, u)
	warningCount := 0
	if log.HasFile() {
		u.LogStarted(logPath)
	}
	if !log.HasFile() {
		warningCount++
	}
	defer job.CloseLog(u, log)

	preflight := buildRestorePreflight(selectedInfos, infos, restorePath)
	details := restorePreflightReport(cfg, backupDir, restorePath, preflight, first.KeySet.AuthMode, yubikey.CheckConnected)
	if lockIssue != nil {
		details.Issues = append(details.Issues, *lockIssue)
		log.Warn("%s", lockIssue.Full())
		warningCount++
	}
	u.ShowRestorePlan(restorePlan(preflight, restorePath, &first.KeySet, details))
	if err := validateRestorePreflight(preflight); err != nil {
		return err
	}
	if err := validateRestoreTargetSpace(restorePath, preflight); err != nil {
		return err
	}

	confirmed, err := u.ConfirmStart("restore")
	if err != nil {
		return err
	}
	if !confirmed {
		log.InfoLogOnly("Restore cancelled by user before start")
		fmt.Fprintln(out, "Restore cancelled.")
		return nil
	}

	job.ReportPhase(u, interact.PhaseUnlocking, "Unlocking keys")
	masters, err := unlock.KeySets(u, selectedInfos, "restore", log)
	if err != nil {
		return err
	}
	defer masters.Zero()

	op := &operation{selected: selectedInfos, inventory: infos, backupDir: backupDir, restorePath: restorePath, logPath: logPath, masters: masters, log: log}
	return op.run(ctx, u, warningCount)
}

// operation is a restore whose keys are unlocked: it asks nothing more, so
// tests can drive it with master keys they made.
type operation struct {
	// selected are the sets to restore; inventory is the backup directory's,
	// where the full backup of a differential is found.
	selected, inventory []catalog.SetInfo
	backupDir           string
	// restorePath is the destination; every set is restored into a folder
	// of its name inside it.
	restorePath string
	logPath     string
	masters     unlock.MasterKeys
	log         *logging.Logger
}

// run restores the selected sets; u receives the progress and the summary.
func (o *operation) run(ctx context.Context, u interact.UI, warningCount int) error {
	fmt.Fprintln(u.Output())
	job.LogStart(o.log, "Restore", o.selected)

	unread, err := o.restoreAll(ctx, u)
	if err != nil {
		if ctx.Err() != nil {
			o.log.Warn("Restore cancelled. Directories restored before cancelling are complete; a directory that was being restored is incomplete (see the warning above).")
			return job.Cancelled("Restore")
		}
		o.log.Error("%v", err)
		return err
	}
	if unread > 0 {
		warningCount++
	}

	o.log.Info("Restore completed successfully.")
	if job.CloseLog(u, o.log) {
		warningCount++
	}
	u.ShowResult(interact.Result{Warnings: warningCount, LogPath: o.logPath})
	return nil
}

// restoreAll restores each selected set and returns the number of files
// that are missing from the restore points because they could not be read
// during backup. It stops when ctx is cancelled and reports its progress to
// rep.
func (o *operation) restoreAll(ctx context.Context, rep interact.ProgressReporter) (int, error) {
	skipped := 0
	err := job.EachRestorePoint(o.selected, o.inventory, func(n int, info catalog.SetInfo, base *naming.BackupEntry) error {
		missing, err := o.restoreEntry(ctx, job.Stamp(rep, interact.PhaseRestoring, n, len(o.selected)), info.Entry, base, o.masters[info.Header.KeySet.ID])
		if err != nil {
			return fmt.Errorf("Failed to restore directory %q: %w", info.Entry.String(), err)
		}
		skipped += missing
		return nil
	})
	if err != nil {
		return 0, err
	}
	return skipped, nil
}

// restoreEntry decrypts one backup set (for a differential together with its
// full backup base) and extracts it into the destination, checking every file against
// its manifest hash. It returns the number of files that could not be read
// during backup: missing from the restore point, or restored in an older
// version (stale). A restore fact records both counts.
func (o *operation) restoreEntry(ctx context.Context, rep interact.ProgressReporter, entry naming.BackupEntry, base *naming.BackupEntry, master []byte) (int, error) {
	if err := naming.ValidateBackupEntryName(entry.DirectoryName); err != nil {
		return 0, err
	}
	set, baseSet, parts, closeAll, err := job.OpenRestorePoint(o.backupDir, entry, base)
	if err != nil {
		return 0, err
	}
	defer closeAll()

	o.log.Info("Processing backup directory: %s", entry.DirectoryName)

	// Verify restore directory can be created before starting decryption.
	// Ensure the parent exists, then create the entry's directory atomically:
	// os.Mkdir fails with os.ErrExist if it already exists, re-enforcing the
	// preflight invariant against a TOCTOU race or two entries resolving to the
	// same DirectoryName. os.MkdirAll would silently merge into an existing tree.
	outDir := filepath.Join(o.restorePath, entry.DirectoryName)
	// Cancelled before the first byte: leave no empty, incomplete folder.
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if err := os.MkdirAll(o.restorePath, 0o750); err != nil {
		return 0, problem.Errorf("Failed to create restore directory: %w.", err).WithRemedy("Check write permissions and use a valid destination path.")
	}
	if err := os.Mkdir(outDir, 0o750); err != nil {
		if errors.Is(err, os.ErrExist) {
			return 0, problem.Errorf("Restore directory already exists: %s.", filepath.ToSlash(outDir)).WithRemedy("Choose a different restore destination or rename/delete the existing restore directory.")
		}
		return 0, problem.Errorf("Failed to create restore directory: %w.", err).WithRemedy("Check write permissions and use a valid destination path.")
	}

	var done atomic.Int64
	stopReport := job.TrackProgress(rep, interact.Progress{Step: "Restoring", Item: entry.DirectoryName, Total: restorepoint.SectionSize(set, baseSet)}, &done)
	m, err := restorepoint.Restore(ctx, set, baseSet, master, outDir, restorepoint.Output{Log: o.log, Done: &done})
	stopReport()
	if err != nil {
		// A cancel is the user's choice, not a failure.
		incomplete := o.log.Error
		if ctx.Err() != nil {
			incomplete = o.log.Warn
		}
		incomplete("  The restore of [%s] is INCOMPLETE: %s may contain only part of the backup.", entry.DirectoryName, filepath.ToSlash(outDir))
		return 0, err
	}
	o.log.Info("  Restored: %d file(s), %d directory(s) from %d part file(s) - [%s] successfully restored and checked", m.Footer.Files, m.Footer.Dirs, parts, entry.DirectoryName)
	stale := restorepoint.ReportStaleFiles(m, entry.DirectoryName, o.log)
	skipped := restorepoint.ReportSkippedFiles(m, entry.DirectoryName, o.log)
	if skipped+stale > 0 {
		o.log.Fact(logging.Fact{Kind: logging.FactRestore, Result: logging.ResultWarnings, Set: entry.String(), Skipped: skipped, Stale: stale})
	}
	return skipped + stale, nil
}
