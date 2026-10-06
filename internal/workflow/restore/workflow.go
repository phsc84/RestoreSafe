// Package restore orchestrates the full restore workflow:
//  1. List available backups in the backup directory
//  2. Let the user choose which backup(s) to restore
//  3. Unlock the keys (password and/or YubiKey, up to 3 password attempts)
//  4. Decrypt, extract, and check every file against its manifest hash
package restore

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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
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
		return errors.New("No restore destination chosen. Remedy: Choose the folder to restore into.")
	}
	restorePath := req.Destination

	infos, err := catalog.Inventory(backupDir)
	if err != nil {
		return fmt.Errorf("Failed to scan backup directory %q: %w. Remedy: Check the backup_directory path in config.yaml and ensure the directory exists and is readable.", backupDir, err)
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

	preflight := buildRestorePreflight(selectedInfos, infos, restorePath)
	usesYubiKey, yubiKeyOnly := authFactors(first.KeySet.AuthMode)
	details := restorePreflightReport(cfg, backupDir, restorePath, preflight, usesYubiKey, yubiKeyOnly, yubikey.CheckConnected)
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
	masters, err := unlock.KeySets(u, selectedInfos, "Enter restore password: ", log)
	if err != nil {
		return err
	}
	defer masters.Zero()

	return runRestoreOperation(ctx, u, selectedInfos, infos, backupDir, restorePath, logPath, masters, log, warningCount)
}

func authFactors(authMode int) (usesYubiKey, yubiKeyOnly bool) {
	mode := config.AuthMode(authMode)
	return mode == config.AuthModePasswordYubiKey || mode == config.AuthModeYubiKey, mode == config.AuthModeYubiKey
}

// runRestoreOperation performs the restore using already-unlocked keys. It
// takes no further user input, so tests and automated flows can drive it
// directly by supplying the master keys; u receives the progress and the
// summary. inventory is used to find the full backup of each selected
// differential.
func runRestoreOperation(ctx context.Context, u interact.UI, selected, inventory []catalog.SetInfo, backupDir, restorePath, logPath string, masters unlock.MasterKeys, log *logging.Logger, warningCount int) error {
	out := u.Output()
	fmt.Fprintln(out)
	first := selected[0].Header
	log.Info("Restore started - ID: %s, date: %s", first.RunID, first.Date)
	log.Info("Restore selection:")
	for _, info := range selected {
		log.Info("  %s", info.Entry.String())
	}

	unread, err := restoreSelectedEntries(ctx, u, selected, inventory, backupDir, restorePath, masters, log)
	if err != nil {
		if ctx.Err() != nil {
			log.Warn("Restore cancelled. Directories restored before cancelling are complete; a directory that was being restored is incomplete (see the warning above).")
			return job.Cancelled("Restore")
		}
		return err
	}
	if unread > 0 {
		warningCount++
	}

	log.Info("Restore completed successfully.")
	u.ShowResult(interact.Result{Warnings: warningCount, LogPath: logPath})
	return nil
}

type restorePreflightItem struct {
	Entry          naming.BackupEntry
	PartCount      int
	TotalSizeBytes int64
	OutputDir      string
	// Base is the full backup a differential needs.
	Base         *catalog.SetInfo
	Err          error // set-level error (missing base)
	OutputDirErr error // output directory error (already exists, invalid name)
	// OutputDirCode classifies OutputDirErr.
	OutputDirCode interact.Code
}

// buildRestorePreflight checks the selected sets. A differential also needs
// its chain's full backup (looked up in inventory); the size estimate covers
// both, because restore reads both.
func buildRestorePreflight(selected, inventory []catalog.SetInfo, restorePath string) []restorePreflightItem {
	items := make([]restorePreflightItem, 0, len(selected))
	for _, info := range selected {
		entry := info.Entry
		item := restorePreflightItem{
			Entry:          entry,
			PartCount:      len(info.Parts),
			TotalSizeBytes: info.SizeBytes,
			OutputDir:      filepath.Join(restorePath, entry.DirectoryName),
			Err:            info.Err,
		}
		if item.Err == nil && entry.IsDiff() {
			base, err := catalog.BaseOf(inventory, entry)
			if err != nil {
				item.Err = err
			} else {
				item.Base = base
				item.TotalSizeBytes += base.SizeBytes
			}
		}
		if nameErr := naming.ValidateBackupEntryName(entry.DirectoryName); nameErr != nil {
			item.OutputDirErr, item.OutputDirCode = nameErr, interact.CodeRestoreTargetInvalid
		} else if _, err := os.Stat(item.OutputDir); err == nil {
			item.OutputDirErr, item.OutputDirCode = fmt.Errorf("Restore directory already exists. Remedy: Choose a different restore destination or rename/delete the existing restore directory."), interact.CodeRestoreTargetExists
		}
		items = append(items, item)
	}
	return items
}

// restorePreflightReport describes the restore: the selected backups (with
// the full backup a differential needs), the destination, and the directories
// to be created, with the issues that block the restore.
func restorePreflightReport(
	cfg *config.Config,
	backupDir, restorePath string,
	items []restorePreflightItem,
	usesYubiKey, yubiKeyOnly bool,
	checkYubiKeyConnected func() error,
) interact.Report {
	var issues []interact.Issue
	addError := func(code interact.Code, text string) {
		issues = append(issues, interact.Issue{Status: interact.StatusError, Code: code, Text: text})
	}

	estimatedRestoreBytes := estimateRestoreBytes(items)
	destDisplay := displayRestoreOutputDir(restorePath)
	restoreFreeBytes, restoreFreeErr := queryRestoreTargetFreeBytes(restorePath)

	rows := []interact.Row{interact.Heading("Backup selection"), interact.Item(interact.StatusNone, "Path: "+filepath.ToSlash(backupDir))}
	for _, item := range items {
		status := interact.StatusOK
		if item.Err != nil {
			status = interact.StatusError
			addError(interact.CodeBaseMissing, item.Err.Error())
		}
		var details []string
		if item.Base != nil {
			details = append(details, fmt.Sprintf("with full backup %s (parts: %d)", item.Base.Entry.String(), len(item.Base.Parts)))
		}
		rows = append(rows, interact.Item(status, fmt.Sprintf("%s (parts: %d)", item.Entry.String(), item.PartCount), details...))
	}

	rows = append(rows, interact.Heading("Restore destination"))
	if restoreFreeErr != nil {
		rows = append(rows, interact.Item(interact.StatusError, destDisplay))
		addError(interact.CodeFreeSpaceUnknown, fmt.Sprintf("Cannot query free space for restore destination %s: %v", destDisplay, restoreFreeErr))
	} else {
		rows = append(rows, interact.Item(interact.StatusOK, destDisplay))
		if fsx.IsSpaceInsufficient(estimatedRestoreBytes, restoreFreeBytes) {
			addError(interact.CodeSpaceInsufficient, fsx.FormatInsufficientRestoreSpaceMessage(uint64(estimatedRestoreBytes), restoreFreeBytes))
		}
	}

	rows = append(rows, interact.Heading("Restored directory(s)"))
	for _, item := range items {
		status := interact.StatusOK
		if item.OutputDirErr != nil {
			status = interact.StatusError
			addError(item.OutputDirCode, item.OutputDirErr.Error())
		}
		rows = append(rows, interact.Item(status, displayRestoreOutputDir(item.OutputDir)))
	}
	rows = append(rows, job.AuthRows(config.AuthModeFromFactors(usesYubiKey, yubiKeyOnly).Label(), usesYubiKey, "restore", checkYubiKeyConnected)...)

	summary := []interact.Row{interact.Field("Backup size", "unknown")}
	if estimatedRestoreBytes > 0 {
		summary[0].Text = fsx.FormatBytesBinary(uint64(estimatedRestoreBytes))
	}
	if restoreFreeErr != nil {
		summary = append(summary, interact.Field("Free space", fmt.Sprintf("unknown (%v)", restoreFreeErr)))
	} else {
		summary = append(summary, interact.Field("Free space", fsx.FormatBytesBinary(restoreFreeBytes)))
	}
	summary = append(summary, interact.Field("Log level", strings.ToLower(cfg.LogLevel)))

	return interact.Report{Title: "Restore preflight", Sections: []interact.Section{{Rows: rows}, {Rows: summary}}, Issues: issues}
}

func displayRestoreOutputDir(outputDir string) string {
	absoluteOutputDir, err := filepath.Abs(outputDir)
	if err != nil {
		absoluteOutputDir = filepath.Clean(outputDir)
	}
	return filepath.ToSlash(absoluteOutputDir)
}

func validateRestorePreflight(items []restorePreflightItem) error {
	return job.ValidatePreflightItems(
		items,
		func(item restorePreflightItem) bool { return item.Err != nil || item.OutputDirErr != nil },
		"Restore preflight failed: %d selected item(s) are invalid. Remedy: Fix the [ERROR] entries above and start restore again.",
	)
}

func validateRestoreTargetSpace(restorePath string, items []restorePreflightItem) error {
	estimatedRestoreBytes := estimateRestoreBytes(items)
	if estimatedRestoreBytes <= 0 {
		return nil
	}

	restoreFreeBytes, err := queryRestoreTargetFreeBytes(restorePath)
	if err != nil {
		return nil
	}

	if !fsx.IsSpaceInsufficient(estimatedRestoreBytes, restoreFreeBytes) {
		return nil
	}

	return fmt.Errorf("Restore preflight failed: %s", fsx.FormatInsufficientRestoreSpaceMessage(uint64(estimatedRestoreBytes), restoreFreeBytes))
}

func estimateRestoreBytes(items []restorePreflightItem) int64 {
	var total int64
	for _, item := range items {
		if item.Err != nil {
			continue
		}
		total += item.TotalSizeBytes
	}
	return total
}

func queryRestoreTargetFreeBytes(restorePath string) (uint64, error) {
	probe := filepath.Clean(restorePath)
	for {
		info, err := os.Stat(probe)
		if err == nil {
			if info.IsDir() {
				return fsx.QueryFreeSpaceBytes(probe)
			}
			probe = filepath.Dir(probe)
		} else {
			parent := filepath.Dir(probe)
			if parent == probe {
				break
			}
			probe = parent
		}
	}

	return fsx.QueryFreeSpaceBytes(restorePath)
}

// restoreSelectedEntries restores each selected set and returns the number of
// files that are missing from the restore points because they could not be
// read during backup. It stops when ctx is cancelled and reports its
// progress to rep.
func restoreSelectedEntries(ctx context.Context, rep interact.ProgressReporter, selected, inventory []catalog.SetInfo, backupDir, restorePath string, masters unlock.MasterKeys, log *logging.Logger) (int, error) {
	skipped := 0
	for i, info := range selected {
		entry := info.Entry
		var base *naming.BackupEntry
		if entry.IsDiff() {
			baseInfo, err := catalog.BaseOf(inventory, entry)
			if err != nil {
				return 0, err
			}
			base = &baseInfo.Entry
		}

		master := masters[info.Header.KeySet.ID]
		n, err := restoreEntry(ctx, job.Stamp(rep, interact.PhaseRestoring, i+1, len(selected)), entry, base, backupDir, restorePath, master, log)
		if err != nil {
			return 0, fmt.Errorf("Failed to restore directory %q: %w", entry.String(), err)
		}
		skipped += n
	}
	return skipped, nil
}

// restoreEntry decrypts one backup set (for a differential together with its
// full backup base) and extracts it to destDir, checking every file against
// its manifest hash. It returns the number of files that could not be read
// during backup: missing from the restore point, or restored in an older
// version (stale). A restore fact records both counts.
func restoreEntry(ctx context.Context, rep interact.ProgressReporter, entry naming.BackupEntry, base *naming.BackupEntry, backupDir, destDir string, master []byte, log *logging.Logger) (int, error) {
	if err := naming.ValidateBackupEntryName(entry.DirectoryName); err != nil {
		return 0, err
	}
	set, err := catalog.OpenSet(backupDir, entry)
	if err != nil {
		return 0, err
	}
	defer set.Close()
	var baseSet *container.Set
	if base != nil {
		baseSet, err = catalog.OpenSet(backupDir, *base)
		if err != nil {
			return 0, fmt.Errorf("Full backup %s: %w", base.String(), err)
		}
		defer baseSet.Close()
	}

	log.Info("Processing backup directory: %s", entry.DirectoryName)

	// Verify restore directory can be created before starting decryption.
	// Ensure the parent exists, then create the entry's directory atomically:
	// os.Mkdir fails with os.ErrExist if it already exists, re-enforcing the
	// preflight invariant against a TOCTOU race or two entries resolving to the
	// same DirectoryName. os.MkdirAll would silently merge into an existing tree.
	outDir := filepath.Join(destDir, entry.DirectoryName)
	if err := os.MkdirAll(destDir, 0o750); err != nil {
		return 0, fmt.Errorf("Failed to create restore directory: %w. Remedy: Check write permissions and use a valid destination path.", err)
	}
	if err := os.Mkdir(outDir, 0o750); err != nil {
		if errors.Is(err, os.ErrExist) {
			return 0, fmt.Errorf("Restore directory already exists: %s. Remedy: Choose a different restore destination or rename/delete the existing restore directory.", filepath.ToSlash(outDir))
		}
		return 0, fmt.Errorf("Failed to create restore directory: %w. Remedy: Check write permissions and use a valid destination path.", err)
	}

	var done atomic.Int64
	stopReport := job.TrackProgress(rep, interact.Progress{Step: "Restoring", Item: entry.DirectoryName, Total: restorepoint.SectionSize(set, baseSet)}, &done)
	m, err := restorepoint.Process(ctx, set, baseSet, master, outDir, false, log, &done)
	stopReport()
	if err != nil {
		log.Warn("  The restore of [%s] is INCOMPLETE: %s may contain only part of the backup.", entry.DirectoryName, filepath.ToSlash(outDir))
		return 0, err
	}
	parts := len(set.Paths)
	if baseSet != nil {
		parts += len(baseSet.Paths)
	}
	log.Info("  Restored: %d file(s), %d directory(s) from %d part file(s) - [%s] successfully restored and checked", m.Footer.Files, m.Footer.Dirs, parts, entry.DirectoryName)
	stale := restorepoint.ReportStaleFiles(m, entry.DirectoryName, log)
	skipped := restorepoint.ReportSkippedFiles(m, entry.DirectoryName, log)
	if skipped+stale > 0 {
		log.Fact(logging.Fact{Kind: logging.FactRestore, Result: logging.ResultWarnings, Set: entry.String(), Skipped: skipped, Stale: stale})
	}
	return skipped + stale, nil
}

// restorePlan describes the restore for the user, from the values the
// details report was built from.
func restorePlan(items []restorePreflightItem, restorePath string, ks *container.KeySet, details interact.Report) interact.RestorePlan {
	p := interact.RestorePlan{
		Destination: restorePath,
		NeededBytes: estimateRestoreBytes(items),
		FreeBytes:   -1,
		Unlock:      job.UnlockPlan(ks),
		Issues:      details.Issues,
		Details:     details,
	}
	if free, err := queryRestoreTargetFreeBytes(restorePath); err == nil {
		p.FreeBytes = int64(free)
	}
	for _, item := range items {
		sp := interact.RestoreSetPlan{SetPlan: job.SetPlan(item.Entry, item.Base, item.TotalSizeBytes, item.Err), OutputDir: item.OutputDir}
		if item.OutputDirErr != nil {
			sp.OutputProblem, sp.OutputCode = item.OutputDirErr.Error(), item.OutputDirCode
		}
		p.Sets = append(p.Sets, sp)
	}
	return p
}

// PlanDestination is the plan of restoring sets into destination, without
// asking anything: the same checks Run makes before it asks to start (the
// folders to create, the space). infos is the inventory of backupDir. The
// restore wizard checks the destination with it while the user types.
func PlanDestination(cfg *config.Config, backupDir string, infos []catalog.SetInfo, sets []naming.BackupEntry, destination string) (interact.RestorePlan, error) {
	if strings.TrimSpace(destination) == "" {
		return interact.RestorePlan{}, errors.New("No restore destination chosen. Remedy: Choose the folder to restore into.")
	}
	selected, err := job.SelectSets(infos, sets)
	if err != nil {
		return interact.RestorePlan{}, err
	}
	items := buildRestorePreflight(selected, infos, destination)
	first := selected[0].Header
	usesYubiKey, yubiKeyOnly := authFactors(first.KeySet.AuthMode)
	noCheck := func() error { return nil }
	details := restorePreflightReport(cfg, backupDir, destination, items, usesYubiKey, yubiKeyOnly, noCheck)
	return restorePlan(items, destination, &first.KeySet, details), nil
}
