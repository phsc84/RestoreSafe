// Package restore orchestrates the full restore workflow:
//  1. List available backups in the backup directory
//  2. Let the user choose which backup(s) to restore
//  3. Unlock the keys (password and/or YubiKey, up to 3 password attempts)
//  4. Decrypt, extract, and check every file against its manifest hash
package restore

import (
	"RestoreSafe/internal/catalog"
	"RestoreSafe/internal/container"
	"RestoreSafe/internal/operation"
	"RestoreSafe/internal/security"
	"RestoreSafe/internal/ui"
	"RestoreSafe/internal/util"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Run executes the full restore workflow, asking u for decisions and credentials.
func Run(u ui.UI, cfg *util.Config, exeDir string) error {
	out := u.Output()
	backupDir := util.ResolveDir(cfg.BackupDirectory, exeDir)

	// Enumerate backups.
	infos, err := catalog.Inventory(backupDir)
	if err != nil {
		return fmt.Errorf("Failed to scan backup directory %q: %w. Remedy: Check the backup_directory path in config.yaml and ensure the directory exists and is readable.", backupDir, err)
	}
	runs := catalog.BackupRunSummaries(infos)
	if len(runs) == 0 {
		fmt.Fprintln(out, "No complete backups found in backup directory. Remedy: Check whether .enc files are in the backup directory and whether the correct directory is configured.")
		return nil
	}

	selected, err := u.SelectBackups("restore", runs)
	if err != nil {
		if errors.Is(err, ui.ErrCancelled) {
			fmt.Fprintln(out, "Restore cancelled.")
			return nil
		}
		return err
	}
	selectedInfos := catalog.SelectInfos(infos, selected)

	first := selectedInfos[0].Header
	logPath := util.LogFileName(backupDir, first.Date, util.BackupID(first.RunID))
	log := operation.OpenLogger(cfg, backupDir, first.Date, util.BackupID(first.RunID), out)
	warningCount := 0
	if log.IsConsoleOnly() {
		warningCount++
	}
	defer log.Close()

	restorePath, err := u.RestoreDestination(backupDir)
	if err != nil {
		if errors.Is(err, ui.ErrCancelled) {
			fmt.Fprintln(out, "Restore cancelled.")
			return nil
		}
		return err
	}

	stagingPlan := operation.PlanLocalStaging(backupDir, restorePath, os.TempDir())
	preflight := buildRestorePreflight(selectedInfos, infos, restorePath)
	usesYubiKey, yubiKeyOnly := authFactors(first.KeySet.AuthMode)
	u.ShowReport(restorePreflightReport(cfg, backupDir, restorePath, preflight, usesYubiKey, yubiKeyOnly, stagingPlan, security.CheckYubiKeyConnected))
	if err := validateRestorePreflight(preflight); err != nil {
		return err
	}
	if err := validateRestoreTargetSpace(restorePath, preflight); err != nil {
		return err
	}
	if err := validateStagingSpace(stagingPlan, preflight); err != nil {
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

	if stagingPlan.Enabled {
		log.InfoLogOnly("Local staging enabled: selected backup parts will be copied to temp storage at %s before restore", filepath.ToSlash(stagingPlan.ResolvedTempDir))
	}

	masters, err := operation.UnlockKeySets(u, selectedInfos, "Enter restore password: ", log)
	if err != nil {
		return err
	}
	defer masters.Zero()

	return runRestoreOperation(out, selectedInfos, infos, backupDir, restorePath, logPath, masters, log, stagingPlan, warningCount)
}

func authFactors(authMode int) (usesYubiKey, yubiKeyOnly bool) {
	mode := util.AuthMode(authMode)
	return mode == util.AuthModePasswordYubiKey || mode == util.AuthModeYubiKey, mode == util.AuthModeYubiKey
}

// runRestoreOperation performs the restore using already-unlocked keys. It
// takes no further user input, so tests and automated flows can drive it
// directly by supplying the master keys. inventory is used to find the full
// backup of each selected differential.
func runRestoreOperation(out io.Writer, selected, inventory []catalog.SetInfo, backupDir, restorePath, logPath string, masters operation.MasterKeys, log *util.Logger, stagingPlan operation.LocalStagingPlan, warningCount int) error {
	fmt.Fprintln(out)
	first := selected[0].Header
	log.Info("Restore started - ID: %s, date: %s", first.RunID, first.Date)
	log.Info("Restore selection:")
	for _, info := range selected {
		log.Info("  %s", info.Entry.String())
	}

	skipped, err := restoreSelectedEntries(selected, inventory, backupDir, restorePath, masters, log, stagingPlan)
	if err != nil {
		return err
	}
	if skipped > 0 {
		warningCount++
	}

	log.Info("Restore completed successfully.")
	fmt.Fprintf(out, "\nLog file: %s\n", logPath)
	if warningCount > 0 {
		fmt.Fprintf(out, "Warnings: %d\n", warningCount)
	}
	return nil
}

type restorePreflightItem struct {
	Entry          util.BackupEntry
	PartCount      int
	TotalSizeBytes int64
	OutputDir      string
	// Base is the full backup a differential needs.
	Base         *catalog.SetInfo
	Err          error // set-level error (incomplete, missing base)
	OutputDirErr error // output directory error (already exists)
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
		if nameErr := util.ValidateBackupEntryName(entry.DirectoryName); nameErr != nil {
			item.OutputDirErr = nameErr
		} else if _, err := os.Stat(item.OutputDir); err == nil {
			item.OutputDirErr = fmt.Errorf("Restore directory already exists. Remedy: Choose a different restore destination or rename/delete the existing restore directory.")
		}
		items = append(items, item)
	}
	return items
}

// restorePreflightReport describes the restore: the selected backups (with
// the full backup a differential needs), the destination, the directories to
// be created, and local staging, with the issues that block the restore.
func restorePreflightReport(
	cfg *util.Config,
	backupDir, restorePath string,
	items []restorePreflightItem,
	usesYubiKey, yubiKeyOnly bool,
	stagingPlan operation.LocalStagingPlan,
	checkYubiKeyConnected func() error,
) ui.Report {
	var issues []ui.Issue
	addError := func(text string) { issues = append(issues, ui.Issue{Status: ui.StatusError, Text: text}) }

	estimatedRestoreBytes := estimateRestoreBytes(items)
	destDisplay := displayRestoreOutputDir(restorePath)
	restoreFreeBytes, restoreFreeErr := queryRestoreTargetFreeBytes(restorePath)

	rows := []ui.Row{ui.Heading("Backup selection"), ui.Item(ui.StatusNone, "Path: "+filepath.ToSlash(backupDir))}
	for _, item := range items {
		status := ui.StatusOK
		if item.Err != nil {
			status = ui.StatusError
			addError(item.Err.Error())
		}
		var details []string
		if item.Base != nil {
			details = append(details, fmt.Sprintf("with full backup %s (parts: %d)", item.Base.Entry.String(), len(item.Base.Parts)))
		}
		rows = append(rows, ui.Item(status, fmt.Sprintf("%s (parts: %d)", item.Entry.String(), item.PartCount), details...))
	}

	rows = append(rows, ui.Heading("Restore destination"))
	if restoreFreeErr != nil {
		rows = append(rows, ui.Item(ui.StatusError, destDisplay))
		addError(fmt.Sprintf("Cannot query free space for restore destination %s: %v", destDisplay, restoreFreeErr))
	} else {
		rows = append(rows, ui.Item(ui.StatusOK, destDisplay))
		if util.IsSpaceInsufficient(estimatedRestoreBytes, restoreFreeBytes) {
			addError(util.FormatInsufficientRestoreSpaceMessage(uint64(estimatedRestoreBytes), restoreFreeBytes))
		}
	}

	rows = append(rows, ui.Heading("Restored directory(s)"))
	for _, item := range items {
		status := ui.StatusOK
		if item.OutputDirErr != nil {
			status = ui.StatusError
			addError(item.OutputDirErr.Error())
		}
		rows = append(rows, ui.Item(status, displayRestoreOutputDir(item.OutputDir)))
	}
	rows = append(rows, operation.AuthRows(util.AuthModeFromFactors(usesYubiKey, yubiKeyOnly).Label(), usesYubiKey, "restore", checkYubiKeyConnected)...)

	summary := []ui.Row{ui.Field("Backup size", "unknown")}
	if estimatedRestoreBytes > 0 {
		summary[0].Text = util.FormatBytesBinary(uint64(estimatedRestoreBytes))
	}
	if restoreFreeErr != nil {
		summary = append(summary, ui.Field("Free space", fmt.Sprintf("unknown (%v)", restoreFreeErr)))
	} else {
		summary = append(summary, ui.Field("Free space", util.FormatBytesBinary(restoreFreeBytes)))
	}
	summary = append(summary, ui.Field("Log level", strings.ToLower(cfg.LogLevel)))

	report := ui.Report{Title: "Restore preflight", Sections: []ui.Section{{Rows: rows}, {Rows: summary}}}
	if stagingPlan.Enabled {
		tempDir := filepath.ToSlash(stagingPlan.ResolvedTempDir)
		staging := []ui.Row{
			ui.Note(fmt.Sprintf("Local staging via temp directory enabled, because backup directory and restore directory(s) share the same drive (%s).", util.VolumeDisplay(backupDir))),
			ui.Heading("Temp directory"),
		}
		tempFreeBytes, tempFreeErr := util.QueryFreeSpaceBytes(stagingPlan.ResolvedTempDir)
		if tempFreeErr != nil {
			staging = append(staging, ui.Item(ui.StatusError, tempDir))
			addError(fmt.Sprintf("Cannot query free space for temp directory: %v", tempFreeErr))
		} else {
			staging = append(staging, ui.Item(ui.StatusOK, tempDir), ui.Item(ui.StatusNone, "Free disk space: "+util.FormatBytesBinary(tempFreeBytes)))
			if estimatedRestoreBytes > 0 && uint64(estimatedRestoreBytes) > tempFreeBytes {
				addError(fmt.Sprintf("Insufficient free space at temp directory for local staging: need %s, have %s. Remedy: Free up space in %s or point TEMP/TMP to a local drive with more space.", util.FormatBytesBinary(uint64(estimatedRestoreBytes)), util.FormatBytesBinary(tempFreeBytes), tempDir))
			}
		}
		report.Sections = append(report.Sections, ui.Section{Rows: staging})
	} else if stagingPlan.SameVolume && util.IsNetworkVolume(backupDir) {
		issues = append(issues, ui.Issue{Status: ui.StatusWarn, Text: fmt.Sprintf("Backup directory and restore target are on the same drive/share (%s). This can cause long stalls on network/NAS storage. Local staging is unavailable because TEMP is on the same drive/share. Remedy: Prefer a different destination or point TEMP/TMP to a local drive.", util.VolumeDisplay(backupDir))})
	}
	report.Issues = issues
	return report
}

func displayRestoreOutputDir(outputDir string) string {
	absoluteOutputDir, err := filepath.Abs(outputDir)
	if err != nil {
		absoluteOutputDir = filepath.Clean(outputDir)
	}
	return filepath.ToSlash(absoluteOutputDir)
}

func validateRestorePreflight(items []restorePreflightItem) error {
	return operation.ValidatePreflightItems(
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

	if !util.IsSpaceInsufficient(estimatedRestoreBytes, restoreFreeBytes) {
		return nil
	}

	return fmt.Errorf("Restore preflight failed: %s", util.FormatInsufficientRestoreSpaceMessage(uint64(estimatedRestoreBytes), restoreFreeBytes))
}

func validateStagingSpace(stagingPlan operation.LocalStagingPlan, items []restorePreflightItem) error {
	if !stagingPlan.Enabled {
		return nil
	}
	estimatedBytes := estimateRestoreBytes(items)
	if estimatedBytes <= 0 {
		return nil
	}
	freeBytes, err := util.QueryFreeSpaceBytes(stagingPlan.ResolvedTempDir)
	if err != nil {
		// Fail-open: let the staging copy itself surface the error.
		return nil
	}
	if uint64(estimatedBytes) > freeBytes {
		return fmt.Errorf("Restore preflight failed: insufficient free space at temp directory for local staging: need %s, have %s. Remedy: Free up space in %s or point TEMP/TMP to a local drive with more space.",
			util.FormatBytesBinary(uint64(estimatedBytes)),
			util.FormatBytesBinary(freeBytes),
			filepath.ToSlash(stagingPlan.ResolvedTempDir))
	}
	return nil
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
				return util.QueryFreeSpaceBytes(probe)
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

	return util.QueryFreeSpaceBytes(restorePath)
}

// restoreSelectedEntries restores each selected set and returns the number of
// files that are missing from the restore points because they could not be
// read during backup.
func restoreSelectedEntries(selected, inventory []catalog.SetInfo, backupDir, restorePath string, masters operation.MasterKeys, log *util.Logger, stagingPlan operation.LocalStagingPlan) (int, error) {
	skipped := 0
	for _, info := range selected {
		entry := info.Entry
		var base *util.BackupEntry
		if entry.IsDiff() {
			baseInfo, err := catalog.BaseOf(inventory, entry)
			if err != nil {
				return 0, err
			}
			base = &baseInfo.Entry
		}

		var scope *operation.StagingScope
		if stagingPlan.Enabled {
			toStage := []util.BackupEntry{entry}
			if base != nil {
				toStage = append(toStage, *base)
			}
			stagedDir, err := stageBackupEntriesLocally(backupDir, toStage, stagingPlan.ResolvedTempDir, log)
			if err != nil {
				return 0, fmt.Errorf("Local staging failed for %q: %w", entry.String(), err)
			}
			scope = operation.ActiveStagingScope(stagedDir, log)
		}

		master := masters[info.Header.KeySet.ID]
		n, err := restoreEntry(entry, base, scope.ActiveDir(backupDir), restorePath, master, log)
		scope.Cleanup()
		if err != nil {
			return 0, fmt.Errorf("Failed to restore directory %q: %w", entry.String(), err)
		}
		skipped += n
	}
	return skipped, nil
}

// stageBackupEntriesLocally copies the parts of entries (a set and, for a
// differential, its full backup) into one new staging directory.
func stageBackupEntriesLocally(backupDir string, entries []util.BackupEntry, tempDir string, log *util.Logger) (string, error) {
	stageDir, err := operation.CreateStagingDir(tempDir, "restoresafe-restore-stage-*")
	if err != nil {
		return "", err
	}

	log.Info("Copy backup files to local staging directory.")
	log.Info("  From: %s", filepath.ToSlash(backupDir))
	log.Info("  To: %s", filepath.ToSlash(stageDir))

	for _, entry := range entries {
		parts, err := catalog.CollectParts(backupDir, entry)
		if err == nil && len(parts) == 0 {
			err = fmt.Errorf("No part files found for %s. Remedy: Ensure all .enc files for this backup are in the same backup directory.", entry.String())
		}
		if err != nil {
			operation.CleanupStagingDirDuring(stageDir, "error recovery", log)
			return "", err
		}
		log.Info("Copying backup files of %s", entry.String())
		for _, partPath := range parts {
			log.Info("  Copy: %s", filepath.Base(partPath))
			destinationPath := filepath.Join(stageDir, filepath.Base(partPath))
			if err := util.CopyFile(partPath, destinationPath); err != nil {
				operation.CleanupStagingDirDuring(stageDir, "error recovery", log)
				return "", err
			}
		}
		log.Info("  Copied: %d part file(s) - [%s] successfully copied", len(parts), entry.DirectoryName)
	}

	return stageDir, nil
}

// restoreEntry decrypts one backup set (for a differential together with its
// full backup base) and extracts it to destDir, checking every file against
// its manifest hash. It returns the number of files that are not in the
// restore point because they could not be read during backup.
func restoreEntry(entry util.BackupEntry, base *util.BackupEntry, backupDir, destDir string, master []byte, log *util.Logger) (int, error) {
	if err := util.ValidateBackupEntryName(entry.DirectoryName); err != nil {
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

	m, err := operation.ProcessRestorePoint(set, baseSet, master, outDir, false, log)
	if err != nil {
		log.Warn("  The restore of [%s] is INCOMPLETE: %s may contain only part of the backup.", entry.DirectoryName, filepath.ToSlash(outDir))
		return 0, err
	}
	parts := len(set.Paths)
	if baseSet != nil {
		parts += len(baseSet.Paths)
	}
	log.Info("  Restored: %d file(s), %d directory(s) from %d part file(s) - [%s] successfully restored and checked", m.Footer.Files, m.Footer.Dirs, parts, entry.DirectoryName)
	if n := m.Footer.Stale; n > 0 {
		log.Warn("  [%s] %d file(s) are restored in the older version of the full backup, because they could not be read when this differential was created.", entry.DirectoryName, n)
	}
	return operation.ReportSkippedFiles(m, entry.DirectoryName, log), nil
}
