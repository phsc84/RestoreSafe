// Package verify checks that selected restore points can be decrypted and
// read back completely, without writing any files.
package verify

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/phsc84/restoresafe/internal/config"
	"github.com/phsc84/restoresafe/internal/format/catalog"
	"github.com/phsc84/restoresafe/internal/format/container"
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

// Request is what the user chose to verify.
type Request struct {
	// Sets are the backup sets to verify; a differential is verified together
	// with its full backup.
	Sets []naming.BackupEntry
}

// Run verifies the backup sets of req without restoring them to disk: every
// part is decrypted and every file is checked against its manifest hash. u is
// asked for confirmation and credentials and receives the progress.
// Cancelling ctx stops the verification; the returned error then matches
// context.Canceled.
func Run(ctx context.Context, u interact.UI, cfg *config.Config, exeDir string, req Request) error {
	out := u.Output()
	backupDir := fsx.ResolveDir(cfg.BackupDirectory, exeDir)

	lock, lockIssue, err := job.LockForReading(backupDir)
	if err != nil {
		return err
	}
	defer lock.Release()

	infos, err := catalog.Inventory(backupDir)
	if err != nil {
		return problem.Errorf("Failed to scan backup directory %q: %w.", backupDir, err).WithRemedy("Check the backup_directory path in config.yaml and ensure the directory is readable.")
	}
	selectedInfos, err := job.SelectSets(infos, req.Sets)
	if err != nil {
		return err
	}

	first := selectedInfos[0].Header
	logPath := naming.LogFileName(backupDir, first.Date, naming.BackupID(first.RunID))
	log := job.OpenLogger(cfg, backupDir, first.Date, naming.BackupID(first.RunID), out)
	warningCount := 0
	if !log.IsConsoleOnly() {
		u.LogStarted(logPath)
	}
	if log.IsConsoleOnly() {
		warningCount++
	}
	defer log.Close()

	preflight := job.SelectionPreflight(selectedInfos, infos)
	details := verifyPreflightReport(cfg, backupDir, preflight, first.KeySet.AuthMode, yubikey.CheckConnected)
	if lockIssue != nil {
		details.Issues = append(details.Issues, *lockIssue)
		log.Warn("%s", lockIssue.Full())
		warningCount++
	}
	u.ShowVerifyPlan(verifyPlan(preflight, &first.KeySet, details))
	if err := validateVerifyPreflight(preflight); err != nil {
		return err
	}

	confirmed, err := u.ConfirmStart("verification")
	if err != nil {
		return err
	}
	if !confirmed {
		log.InfoLogOnly("Verification cancelled by user before start")
		fmt.Fprintln(out, "Verification cancelled.")
		return nil
	}

	job.ReportPhase(u, interact.PhaseUnlocking, "Unlocking keys")
	masters, err := unlock.KeySets(u, selectedInfos, "verification", log)
	if err != nil {
		return err
	}
	defer masters.Zero()

	op := &operation{selected: selectedInfos, inventory: infos, backupDir: backupDir, logPath: logPath, masters: masters, log: log}
	return op.run(ctx, u, warningCount)
}

// operation is a verification whose keys are unlocked: it asks nothing more,
// so tests can drive it with master keys they made.
type operation struct {
	// selected are the sets to verify; inventory is the backup directory's,
	// where the full backup of a differential is found.
	selected, inventory []catalog.SetInfo
	backupDir           string
	logPath             string
	masters             unlock.MasterKeys
	log                 *logging.Logger
}

// run verifies the selected sets; u receives the progress and the summary.
func (o *operation) run(ctx context.Context, u interact.UI, warningCount int) error {
	out := u.Output()
	fmt.Fprintln(out)
	job.LogStart(o.log, "Verification", o.selected)

	skipped, err := o.verifyAll(ctx, u)
	if err != nil {
		if ctx.Err() != nil {
			o.log.Warn("Verification cancelled.")
			return job.Cancelled("Verification")
		}
		return err
	}
	if skipped > 0 {
		warningCount++
	}

	o.log.Info("Verification completed successfully.")
	u.ShowResult(interact.Result{Warnings: warningCount, LogPath: o.logPath})
	return nil
}

// verifyPreflightReport describes the verification: the selected backups
// (with the full backup a differential needs) and the issues that block it.
func verifyPreflightReport(
	cfg *config.Config,
	backupDir string,
	items []job.SelectionItem,
	mode config.AuthMode,
	checkYubiKeyConnected func() error,
) interact.Report {
	rows, issues := job.SelectionRows(backupDir, items)
	rows = append(rows, job.AuthRows(mode.Label(), mode.UsesYubiKey(), "verification", checkYubiKeyConnected)...)

	size := "unknown"
	if totalBytes := job.SelectionBytes(items); totalBytes > 0 {
		size = fsx.FormatBytesBinary(uint64(totalBytes))
	}
	summary := []interact.Row{interact.Field("Backup size", size), interact.Field("Log level", strings.ToLower(cfg.LogLevel))}

	return interact.Report{Title: "Verification preflight", Sections: []interact.Section{{Rows: rows}, {Rows: summary}}, Issues: issues}
}

func validateVerifyPreflight(items []job.SelectionItem) error {
	return job.ValidatePreflightItems(
		items,
		func(item job.SelectionItem) bool { return item.Err != nil },
		"Verification preflight failed: %d selected item(s) are incomplete or invalid.",
		"Fix the [ERROR] entries above and start verification again.",
	)
}

// verifyAll verifies each selected set and returns the number of
// files missing from the restore points because they could not be read
// during backup.
func (o *operation) verifyAll(ctx context.Context, rep interact.ProgressReporter) (int, error) {
	skipped := 0
	err := job.EachRestorePoint(o.selected, o.inventory, func(n int, info catalog.SetInfo, base *naming.BackupEntry) error {
		missing, err := o.verifyEntry(ctx, job.Stamp(rep, interact.PhaseVerifying, n, len(o.selected)), info.Entry, base, o.masters[info.Header.KeySet.ID])
		if err != nil {
			if ctx.Err() == nil {
				o.log.Fact(logging.Fact{Kind: logging.FactVerify, Result: logging.ResultFailed, Set: info.Entry.String(), Error: err.Error()})
			}
			return fmt.Errorf("Failed to verify directory %q: %w", info.Entry.String(), err)
		}
		o.log.Fact(logging.Fact{Kind: logging.FactVerify, Result: logging.ResultOK, Set: info.Entry.String()})
		skipped += missing
		return nil
	})
	if err != nil {
		return 0, err
	}
	return skipped, nil
}

// verifyEntry verifies one restore point: a full backup, or a differential
// together with its full backup base.
func (o *operation) verifyEntry(ctx context.Context, rep interact.ProgressReporter, entry naming.BackupEntry, base *naming.BackupEntry, master []byte) (int, error) {
	set, baseSet, parts, closeAll, err := job.OpenRestorePoint(o.backupDir, entry, base)
	if err != nil {
		return 0, err
	}
	defer closeAll()

	o.log.Info("Processing backup directory: %s", entry.DirectoryName)
	var done atomic.Int64
	stopReport := job.TrackProgress(rep, interact.Progress{Step: "Verifying", Item: entry.DirectoryName, Total: restorepoint.SectionSize(set, baseSet)}, &done)
	m, err := restorepoint.Verify(ctx, set, baseSet, master, restorepoint.Output{Log: o.log, Done: &done})
	stopReport()
	if err != nil {
		return 0, err
	}
	o.log.Info("  Verified: %d file(s), %d directory(s) in %d part file(s) - [%s] successfully verified", m.Footer.Files, m.Footer.Dirs, parts, entry.DirectoryName)
	return restorepoint.ReportSkippedFiles(m, entry.DirectoryName, o.log), nil
}

// verifyPlan describes the verification for the user, from the values the
// details report was built from.
func verifyPlan(items []job.SelectionItem, ks *container.KeySet, details interact.Report) interact.VerifyPlan {
	p := interact.VerifyPlan{Bytes: job.SelectionBytes(items), Unlock: job.UnlockPlan(ks), Issues: details.Issues, Details: details}
	for _, item := range items {
		p.Sets = append(p.Sets, item.SetPlan())
	}
	return p
}
