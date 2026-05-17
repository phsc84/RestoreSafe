// Package restore orchestrates the full restore workflow:
//  1. List available backups in the target directory
//  2. Let the user choose which backup(s) to restore
//  3. Verify password (up to 3 attempts)
//  4. Decrypt and extract to the user-specified restore path
package restore

import (
	"RestoreSafe/internal/catalog"
	"RestoreSafe/internal/operation"
	"RestoreSafe/internal/security"
	"RestoreSafe/internal/util"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Run executes the full restore workflow.
func Run(cfg *util.Config, exeDir string) error {
	targetDir := util.ResolveDir(cfg.TargetDirectory, exeDir)

	// Enumerate backups.
	index, err := catalog.ScanBackups(targetDir)
	if err != nil {
		return fmt.Errorf("Failed to scan target directory %q: %w. Remedy: Check the target_directory path in config.yaml and ensure the directory exists and is readable.", targetDir, err)
	}
	if len(index) == 0 {
		fmt.Println("No backups found in target directory. Remedy: Check whether .enc files are in target_directory and whether the correct directory is configured.")
		return nil
	}

	selected, selection, err := resolveRestoreSelection(targetDir, index)
	if err != nil {
		if errors.Is(err, operation.ErrSelectionCancelled) {
			fmt.Println("Restore cancelled.")
			return nil
		}
		return err
	}
	warningCount := restoreSelectionWarningCount(selection, index)

	requiresYubiKey, yubiKeyOnly, err := catalog.BackupRunUsesYubiKey(targetDir, selected[0])
	if err != nil {
		return fmt.Errorf("Failed to inspect backup authentication: %w. Remedy: Check read permissions in the backup directory and verify .challenge filenames.", err)
	}

	logPath := util.LogFileName(targetDir, selected[0].Date, selected[0].ID)
	log := operation.OpenLogger(cfg, targetDir, selected[0])
	if log.IsConsoleOnly() {
		warningCount++
	}
	defer log.Close()

	restorePath, err := promptRestoreDestination(targetDir)
	if err != nil {
		if errors.Is(err, operation.ErrSelectionCancelled) {
			fmt.Println("Restore cancelled.")
			return nil
		}
		return err
	}

	stagingPlan := operation.PlanLocalStaging(targetDir, restorePath, os.TempDir())
	preflight := buildRestorePreflight(selected, targetDir, restorePath)
	printRestorePreflightWithYubiKeyCheck(os.Stdout, cfg, targetDir, restorePath, preflight, requiresYubiKey, yubiKeyOnly, stagingPlan, security.CheckYubiKeyConnected)
	if err := validateRestorePreflight(preflight); err != nil {
		return err
	}
	if err := validateRestoreTargetSpace(restorePath, preflight); err != nil {
		return err
	}
	if err := validateStagingSpace(stagingPlan, preflight); err != nil {
		return err
	}

	confirmed, err := operation.PromptStartAction("restore")
	if err != nil {
		return err
	}
	if !confirmed {
		log.InfoLogOnly("Restore cancelled by user before start")
		fmt.Println("Restore cancelled.")
		return nil
	}

	if stagingPlan.Enabled {
		log.InfoLogOnly("Local staging enabled: selected backup parts will be copied to temp storage at %s before restore", filepath.ToSlash(stagingPlan.ResolvedTempDir))
	}

	// Collect password (with retry).
	rep := selected[0]
	password, err := operation.ReadPasswordWithRetry(targetDir, rep, "Enter restore password: ", log)
	if err != nil {
		return err
	}
	defer func() { security.ZeroBytes(password) }()

	fmt.Println()
	log.Info("Restore started - ID: %s, date: %s, selection: %q", string(selected[0].ID), selected[0].Date, selection)
	log.Info("Restore destination: %s", restorePath)

	totalPartsProcessed, err := restoreSelectedEntries(selected, targetDir, restorePath, password, log, stagingPlan)
	if err != nil {
		return err
	}

	log.Info("Restore completed successfully.")
	printRestoreCompletionSummary(os.Stdout, selected, totalPartsProcessed, logPath, warningCount)
	fmt.Println("\nRestore completed successfully.")
	return nil
}

func resolveRestoreSelection(targetDir string, index []util.BackupEntry) ([]util.BackupEntry, string, error) {
	return operation.PromptBackupSelection("restore", targetDir, index)
}

func promptRestoreDestination(targetDir string) (string, error) {
	for {
		fmt.Printf("Enter restore destination:\n")
		fmt.Printf("  - Enter a dot (.) → restore in the backup directory itself [%s]\n", targetDir)
		fmt.Printf("  - Enter a specific path (e.g. C:\\Restore) → restore to this directory\n")
		fmt.Printf("  - Enter q → cancel\n")
		fmt.Println()

		restorePath, err := security.ReadLine("Restore destination: ")
		if err != nil {
			return "", err
		}
		fmt.Println()
		restorePath = strings.TrimSpace(restorePath)

		switch restorePath {
		case "":
			continue
		case "q":
			return "", operation.ErrSelectionCancelled
		case ".":
			return targetDir, nil
		}
		return restorePath, nil
	}
}

type restorePreflightItem struct {
	Entry          util.BackupEntry
	PartCount      int
	TotalSizeBytes int64
	OutputDir      string
	Err            error // parts-level error (inspection failure, no parts found)
	OutputDirErr   error // output directory error (already exists)
}

func buildRestorePreflight(selected []util.BackupEntry, targetDir, restorePath string) []restorePreflightItem {
	items := make([]restorePreflightItem, 0, len(selected))
	for _, entry := range selected {
		partCount, totalSizeBytes, err := catalog.InspectBackupParts(targetDir, entry)
		item := restorePreflightItem{
			Entry:          entry,
			PartCount:      partCount,
			TotalSizeBytes: totalSizeBytes,
			OutputDir:      filepath.Join(restorePath, entry.DirectoryName),
		}
		if err != nil {
			item.Err = err
		}
		if item.PartCount == 0 && item.Err == nil {
			item.Err = fmt.Errorf("No part files found. Remedy: Ensure all .enc parts of this backup are in the same target_directory.")
		}
		if _, err := os.Stat(item.OutputDir); err == nil {
			item.OutputDirErr = fmt.Errorf("Target directory already exists. Remedy: Choose a different restore destination or rename/delete the existing target directory.")
		}
		items = append(items, item)
	}
	return items
}

func printRestorePreflightWithYubiKeyCheck(
	w io.Writer,
	cfg *util.Config,
	targetDir, restorePath string,
	items []restorePreflightItem,
	requiresYubiKey, yubiKeyOnly bool,
	stagingPlan operation.LocalStagingPlan,
	checkYubiKeyConnected func() error,
) {
	var issues []string

	fmt.Fprintln(w)
	fmt.Fprintln(w, "Restore preflight")
	fmt.Fprintln(w, "-----------------")

	// Backup selection
	fmt.Fprintln(w, "Backup selection:")
	fmt.Fprintf(w, "  Path: %s\n", filepath.ToSlash(targetDir))
	for _, item := range items {
		if item.Err != nil {
			fmt.Fprintf(w, "  [ERROR] %s (parts: %d)\n", item.Entry.String(), item.PartCount)
			issues = append(issues, item.Err.Error())
		} else {
			fmt.Fprintf(w, "  [OK] %s (parts: %d)\n", item.Entry.String(), item.PartCount)
		}
	}
	estimatedRestoreBytes := estimateRestoreBytes(items)
	if estimatedRestoreBytes > 0 {
		fmt.Fprintf(w, "  Used disk space (total): %s\n", util.FormatBytesBinary(uint64(estimatedRestoreBytes)))
	} else {
		fmt.Fprintf(w, "  Used disk space (total): unknown\n")
	}

	// Restore destination
	fmt.Fprintln(w, "Restore destination:")
	destDisplay := displayRestoreOutputDir(restorePath)
	restoreFreeBytes, restoreFreeErr := queryRestoreTargetFreeBytes(restorePath)
	if restoreFreeErr != nil {
		fmt.Fprintf(w, "  [ERROR] %s\n", destDisplay)
		issues = append(issues, fmt.Sprintf("Cannot query free space for restore destination %s: %v", destDisplay, restoreFreeErr))
	} else {
		fmt.Fprintf(w, "  [OK] %s\n", destDisplay)
		fmt.Fprintf(w, "  Free disk space: %s\n", util.FormatBytesBinary(restoreFreeBytes))
		if util.IsSpaceInsufficient(estimatedRestoreBytes, restoreFreeBytes) {
			issues = append(issues, util.FormatInsufficientRestoreSpaceMessage(uint64(estimatedRestoreBytes), restoreFreeBytes))
		}
	}

	// Restored directory(s)
	fmt.Fprintln(w, "Restored directory(s):")
	for _, item := range items {
		displayDir := displayRestoreOutputDir(item.OutputDir)
		if item.OutputDirErr != nil {
			fmt.Fprintf(w, "  [ERROR] %s\n", displayDir)
			issues = append(issues, item.OutputDirErr.Error())
		} else {
			fmt.Fprintf(w, "  [OK] %s\n", displayDir)
		}
	}

	// Authentication and Log level
	operation.PrintField(w, operation.DefaultFieldLabelWidth, "Authentication", operation.BackupAuthenticationLabel(requiresYubiKey, yubiKeyOnly))
	operation.PrintYubiKeyPreflightStatus(w, requiresYubiKey, "restore", checkYubiKeyConnected)
	operation.PrintField(w, operation.DefaultFieldLabelWidth, "Log level", strings.ToLower(cfg.LogLevel))

	// Local staging block
	if stagingPlan.Enabled {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "Local staging via temp directory enabled, because backup directory and restore directory(s) share the same drive (%s).\n", util.VolumeDisplay(targetDir))
		fmt.Fprintln(w, "Temp directory:")
		tempDir := filepath.ToSlash(stagingPlan.ResolvedTempDir)
		tempFreeBytes, tempFreeErr := util.QueryFreeSpaceBytes(stagingPlan.ResolvedTempDir)
		if tempFreeErr != nil {
			fmt.Fprintf(w, "  [ERROR] %s\n", tempDir)
			issues = append(issues, fmt.Sprintf("Cannot query free space for temp directory: %v", tempFreeErr))
		} else {
			fmt.Fprintf(w, "  [OK] %s\n", tempDir)
			fmt.Fprintf(w, "  Free disk space: %s\n", util.FormatBytesBinary(tempFreeBytes))
			if estimatedRestoreBytes > 0 && uint64(estimatedRestoreBytes) > tempFreeBytes {
				issues = append(issues, fmt.Sprintf("Insufficient free space at temp directory for local staging: need %s, have %s. Remedy: Free up space in %s or point TEMP/TMP to a local drive with more space.", util.FormatBytesBinary(uint64(estimatedRestoreBytes)), util.FormatBytesBinary(tempFreeBytes), tempDir))
			}
		}
	} else if stagingPlan.SameVolume && util.IsNetworkVolume(targetDir) {
		issues = append(issues, fmt.Sprintf("[WARN] Backup directory and restore target are on the same drive/share (%s). This can cause long stalls on network/NAS storage. Local staging is unavailable because TEMP is on the same drive/share. Remedy: Prefer a different destination or point TEMP/TMP to a local drive.", util.VolumeDisplay(targetDir)))
	}

	// Print collected issues
	if len(issues) > 0 {
		fmt.Fprintln(w)
		for _, issue := range issues {
			if strings.HasPrefix(issue, "[WARN]") {
				fmt.Fprintln(w, issue)
			} else {
				fmt.Fprintf(w, "[ERROR] %s\n", issue)
			}
		}
	}
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

func restoreSelectedEntries(selected []util.BackupEntry, targetDir, restorePath string, password []byte, log *util.Logger, stagingPlan operation.LocalStagingPlan) (int, error) {
	totalPartsProcessed := 0
	for _, entry := range selected {
		var scope *operation.StagingScope
		if stagingPlan.Enabled {
			stagedDir, err := stageBackupEntryLocally(targetDir, entry, stagingPlan.ResolvedTempDir, log)
			if err != nil {
				return 0, fmt.Errorf("Local staging failed for %q: %w", entry.String(), err)
			}
			scope = operation.ActiveStagingScope(stagedDir, log)
		}

		partCount, err := restoreEntry(entry, scope.ActiveDir(targetDir), restorePath, password, log)
		scope.Cleanup()
		if err != nil {
			return 0, fmt.Errorf("Failed to restore directory %q: %w", entry.String(), err)
		}
		totalPartsProcessed += partCount
		log.Info("Directory %q successfully restored", entry.DirectoryName)
	}
	return totalPartsProcessed, nil
}

func stageBackupEntryLocally(targetDir string, entry util.BackupEntry, tempDir string, log *util.Logger) (string, error) {
	parts, err := catalog.CollectParts(targetDir, entry)
	if err != nil {
		return "", err
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("No part files found for %s. Remedy: Ensure all .enc files for this backup are in the same target_directory.", entry.String())
	}

	stageDir, err := operation.CreateStagingDir(tempDir, "restoresafe-restore-stage-*")
	if err != nil {
		return "", err
	}

	log.Info("Local staging started for %s: copying %d part file(s) to %s", entry.String(), len(parts), filepath.ToSlash(stageDir))

	for _, partPath := range parts {
		destinationPath := filepath.Join(stageDir, filepath.Base(partPath))
		if err := util.CopyFile(partPath, destinationPath); err != nil {
			operation.CleanupStagingDirDuring(stageDir, "error recovery", log)
			return "", err
		}
	}

	log.Info("Local staging completed for %s", entry.String())

	return stageDir, nil
}

// restoreEntry decrypts all parts of one backup entry and extracts to destDir.
func restoreEntry(entry util.BackupEntry, targetDir, destDir string, password []byte, log *util.Logger) (int, error) {
	parts, err := catalog.CollectParts(targetDir, entry)
	if err != nil {
		return 0, err
	}
	if len(parts) == 0 {
		return 0, fmt.Errorf("No part files found for %s. Remedy: Put all related .enc files into the same target_directory.", entry.String())
	}

	log.Info("Processing %d part file(s) for %s", len(parts), entry.String())

	// Verify target directory can be created before starting decryption.
	outDir := filepath.Join(destDir, entry.DirectoryName)
	if err := os.MkdirAll(outDir, 0o750); err != nil {
		return 0, fmt.Errorf("Failed to create target directory: %w. Remedy: Check write permissions and use a valid destination path.", err)
	}

	log.Info("Extracting to: %s", outDir)

	err = operation.RunDecryptPipeline(
		parts,
		password,
		log,
		entry.DirectoryName,
		"decrypted",
		"Extraction",
		func(r io.Reader) error { return util.ExtractTar(r, outDir) },
		nil,
	)
	if err != nil {
		return 0, err
	}

	return len(parts), nil
}

func restoreSelectionWarningCount(selection string, index []util.BackupEntry) int {
	normalized := strings.ToUpper(strings.TrimSpace(selection))
	if !catalog.IsRawBackupID(normalized) {
		return 0
	}

	_, _, allDates, found := catalog.ResolveSelectionForIDNewestDate(normalized, index)
	if !found {
		return 0
	}
	if len(allDates) > 1 {
		return 1
	}
	return 0
}

func printRestoreCompletionSummary(w io.Writer, selected []util.BackupEntry, totalPartsProcessed int, logPath string, warningCount int) {
	directoryNames := make([]string, 0, len(selected))
	for _, entry := range selected {
		directoryNames = append(directoryNames, entry.DirectoryName)
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "Restore summary")
	fmt.Fprintln(w, "---------------")
	const w21 = 21
	operation.PrintField(w, w21, "Processed directories", fmt.Sprintf("%d (%s)", len(selected), summarizeNames(directoryNames)))
	operation.PrintField(w, w21, "Parts processed", fmt.Sprintf("%d", totalPartsProcessed))
	operation.PrintField(w, w21, "Log file", logPath)
	if warningCount > 0 {
		operation.PrintField(w, w21, "Warnings", fmt.Sprintf("%d", warningCount))
	} else {
		operation.PrintField(w, w21, "Warnings", "none")
	}
}

func summarizeNames(names []string) string {
	if len(names) == 0 {
		return "none"
	}
	if len(names) <= 3 {
		return strings.Join(names, ", ")
	}
	return fmt.Sprintf("%s, %s, %s, +%d more", names[0], names[1], names[2], len(names)-3)
}
