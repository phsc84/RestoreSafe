// Package verify checks that selected restore points can be decrypted and
// read back completely, without writing any files.
package verify

import (
	"RestoreSafe/internal/config"
	"RestoreSafe/internal/format/catalog"
	"RestoreSafe/internal/format/container"
	"RestoreSafe/internal/format/naming"
	"RestoreSafe/internal/fsx"
	"RestoreSafe/internal/logging"
	"RestoreSafe/internal/security/yubikey"
	"RestoreSafe/internal/workflow/interact"
	"RestoreSafe/internal/workflow/job"
	"RestoreSafe/internal/workflow/restorepoint"
	"RestoreSafe/internal/workflow/unlock"
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
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

	infos, err := catalog.Inventory(backupDir)
	if err != nil {
		return fmt.Errorf("Failed to scan backup directory %q: %w. Remedy: Check the backup_directory path in config.yaml and ensure the directory is readable.", backupDir, err)
	}
	selectedInfos, err := job.SelectSets(infos, req.Sets)
	if err != nil {
		return err
	}

	first := selectedInfos[0].Header
	logPath := naming.LogFileName(backupDir, first.Date, naming.BackupID(first.RunID))
	log := job.OpenLogger(cfg, backupDir, first.Date, naming.BackupID(first.RunID), out)
	warningCount := 0
	if log.IsConsoleOnly() {
		warningCount++
	}
	defer log.Close()

	preflight := buildVerifyPreflight(selectedInfos, infos)
	mode := config.AuthMode(first.KeySet.AuthMode)
	usesYubiKey := mode == config.AuthModePasswordYubiKey || mode == config.AuthModeYubiKey
	u.ShowReport(verifyPreflightReport(cfg, backupDir, preflight, usesYubiKey, mode == config.AuthModeYubiKey, yubikey.CheckConnected))
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

	masters, err := unlock.KeySets(u, selectedInfos, "Enter verification password: ", log)
	if err != nil {
		return err
	}
	defer masters.Zero()

	return runVerifyOperation(ctx, u, selectedInfos, infos, backupDir, logPath, masters, log, warningCount)
}

// runVerifyOperation performs the verification using already-unlocked keys.
// It takes no further user input, so tests and automated flows can drive it
// directly by supplying the master keys; u receives the progress and the
// summary. inventory is used to find the full backup of each selected
// differential.
func runVerifyOperation(ctx context.Context, u interact.UI, selected, inventory []catalog.SetInfo, backupDir, logPath string, masters unlock.MasterKeys, log *logging.Logger, warningCount int) error {
	out := u.Output()
	fmt.Fprintln(out)
	first := selected[0].Header
	log.Info("Verification started - ID: %s, date: %s", first.RunID, first.Date)
	log.Info("Verification selection:")
	for _, info := range selected {
		log.Info("  %s", info.Entry.String())
	}

	skipped, err := verifySelectedEntries(ctx, u, selected, inventory, backupDir, masters, log)
	if err != nil {
		if ctx.Err() != nil {
			log.Warn("Verification cancelled.")
			return job.Cancelled("Verification")
		}
		return err
	}
	if skipped > 0 {
		warningCount++
	}

	log.Info("Verification completed successfully.")
	u.ShowResult(interact.Result{Warnings: warningCount, LogPath: logPath})
	return nil
}

type verifyPreflightItem struct {
	Entry          naming.BackupEntry
	PartCount      int
	TotalSizeBytes int64
	// Base is the full backup a differential needs.
	Base *catalog.SetInfo
	Err  error
}

// buildVerifyPreflight checks the selected sets. Verifying a differential
// verifies the complete restore point, so its full backup is needed too.
func buildVerifyPreflight(selected, inventory []catalog.SetInfo) []verifyPreflightItem {
	items := make([]verifyPreflightItem, 0, len(selected))
	for _, info := range selected {
		item := verifyPreflightItem{
			Entry:          info.Entry,
			PartCount:      len(info.Parts),
			TotalSizeBytes: info.SizeBytes,
			Err:            info.Err,
		}
		if item.Err == nil && info.Entry.IsDiff() {
			base, err := catalog.BaseOf(inventory, info.Entry)
			if err != nil {
				item.Err = err
			} else {
				item.Base = base
				item.TotalSizeBytes += base.SizeBytes
			}
		}
		items = append(items, item)
	}
	return items
}

// verifyPreflightReport describes the verification: the selected backups
// (with the full backup a differential needs) and the issues that block it.
func verifyPreflightReport(
	cfg *config.Config,
	backupDir string,
	items []verifyPreflightItem,
	usesYubiKey, yubiKeyOnly bool,
	checkYubiKeyConnected func() error,
) interact.Report {
	var issues []interact.Issue
	rows := []interact.Row{interact.Heading("Backup selection"), interact.Item(interact.StatusNone, "Path: "+filepath.ToSlash(backupDir))}
	for _, item := range items {
		status := interact.StatusOK
		if item.Err != nil {
			status = interact.StatusError
			issues = append(issues, interact.Issue{Status: interact.StatusError, Text: item.Err.Error()})
		}
		var details []string
		if item.Base != nil {
			details = append(details, fmt.Sprintf("with full backup %s (parts: %d)", item.Base.Entry.String(), len(item.Base.Parts)))
		}
		rows = append(rows, interact.Item(status, fmt.Sprintf("%s (parts: %d)", item.Entry.String(), item.PartCount), details...))
	}
	rows = append(rows, job.AuthRows(config.AuthModeFromFactors(usesYubiKey, yubiKeyOnly).Label(), usesYubiKey, "verification", checkYubiKeyConnected)...)

	size := "unknown"
	if totalBytes := estimateVerifyBytes(items); totalBytes > 0 {
		size = fsx.FormatBytesBinary(uint64(totalBytes))
	}
	summary := []interact.Row{interact.Field("Backup size", size), interact.Field("Log level", strings.ToLower(cfg.LogLevel))}

	return interact.Report{Title: "Verification preflight", Sections: []interact.Section{{Rows: rows}, {Rows: summary}}, Issues: issues}
}

func estimateVerifyBytes(items []verifyPreflightItem) int64 {
	var total int64
	for _, item := range items {
		if item.Err == nil {
			total += item.TotalSizeBytes
		}
	}
	return total
}

func validateVerifyPreflight(items []verifyPreflightItem) error {
	return job.ValidatePreflightItems(
		items,
		func(item verifyPreflightItem) bool { return item.Err != nil },
		"Verification preflight failed: %d selected item(s) are incomplete or invalid. Remedy: Fix the [ERROR] entries above and start verification again.",
	)
}

// verifySelectedEntries verifies each selected set and returns the number of
// files missing from the restore points because they could not be read
// during backup.
func verifySelectedEntries(ctx context.Context, rep interact.ProgressReporter, selected, inventory []catalog.SetInfo, backupDir string, masters unlock.MasterKeys, log *logging.Logger) (int, error) {
	skipped := 0
	for _, info := range selected {
		var base *naming.BackupEntry
		if info.Entry.IsDiff() {
			baseInfo, err := catalog.BaseOf(inventory, info.Entry)
			if err != nil {
				return 0, err
			}
			base = &baseInfo.Entry
		}
		n, err := verifyEntry(ctx, rep, info.Entry, base, backupDir, masters[info.Header.KeySet.ID], log)
		if err != nil {
			return 0, fmt.Errorf("Failed to verify directory %q: %w", info.Entry.String(), err)
		}
		skipped += n
	}
	return skipped, nil
}

// verifyEntry verifies one restore point: a full backup, or a differential
// together with its full backup base.
func verifyEntry(ctx context.Context, rep interact.ProgressReporter, entry naming.BackupEntry, base *naming.BackupEntry, backupDir string, master []byte, log *logging.Logger) (int, error) {
	set, err := catalog.OpenSet(backupDir, entry)
	if err != nil {
		return 0, err
	}
	defer set.Close()
	parts := len(set.Paths)
	var baseSet *container.Set
	if base != nil {
		baseSet, err = catalog.OpenSet(backupDir, *base)
		if err != nil {
			return 0, fmt.Errorf("Full backup %s: %w", base.String(), err)
		}
		defer baseSet.Close()
		parts += len(baseSet.Paths)
	}

	log.Info("Processing backup directory: %s", entry.DirectoryName)
	var done atomic.Int64
	stopReport := job.TrackProgress(rep, interact.Progress{Step: "Verifying", Item: entry.DirectoryName, Total: restorepoint.SectionSize(set, baseSet)}, &done)
	m, err := restorepoint.Process(ctx, set, baseSet, master, "", true, log, &done)
	stopReport()
	if err != nil {
		return 0, err
	}
	log.Info("  Verified: %d file(s), %d directory(s) in %d part file(s) - [%s] successfully verified", m.Footer.Files, m.Footer.Dirs, parts, entry.DirectoryName)
	return restorepoint.ReportSkippedFiles(m, entry.DirectoryName, log), nil
}
