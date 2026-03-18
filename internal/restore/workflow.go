// Package restore orchestrates the full restore workflow:
//  1. List available backups in the target folder
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
	"sync/atomic"
)

// Run executes the full restore workflow.
func Run(cfg *util.Config, exeDir string) error {
	targetDir := util.ResolveDir(cfg.TargetFolder, exeDir)

	// Enumerate backups.
	index, err := catalog.ScanBackups(targetDir)
	if err != nil {
		return fmt.Errorf("Failed to scan target folder %q: %w. Remedy: Check the target_folder path in config.yaml and ensure the folder exists and is readable.", targetDir, err)
	}
	if len(index) == 0 {
		fmt.Println("No backups found in target folder. Remedy: Check whether .enc files are in target_folder and whether the correct folder is configured.")
		return nil
	}

	selected, selection, err := resolveRestoreSelection(cfg, targetDir, index)
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
		return fmt.Errorf("Failed to inspect backup authentication: %w. Remedy: Check read permissions in the backup folder and verify .challenge filenames.", err)
	}

	logPath := util.LogFileName(targetDir, selected[0].Date, selected[0].ID)
	log := operation.OpenLogger(cfg, targetDir, selected[0])
	if log == nil {
		warningCount++
	}
	if log != nil {
		defer log.Close()
	}

	if log != nil {
		log.Info("Restore started - Selection: %q", selection)
	}

	restorePath, err := promptRestoreDestination(targetDir)
	if err != nil {
		return err
	}

	stagingPlan := operation.PlanLocalStaging(targetDir, restorePath, os.TempDir())
	preflight := buildRestorePreflight(selected, targetDir, restorePath)
	printRestorePreflight(targetDir, restorePath, preflight, requiresYubiKey, yubiKeyOnly, stagingPlan)
	if err := validateRestorePreflight(preflight); err != nil {
		return err
	}

	if cfg.NonInteractive {
		if log != nil {
			log.Info("Non-interactive mode: start confirmation skipped")
		}
		fmt.Println("Starting restore automatically.")
	} else {
		confirmed, err := promptStartRestore()
		if err != nil {
			return err
		}
		if !confirmed {
			if log != nil {
				log.Info("Restore cancelled by user before start")
			}
			fmt.Println("Restore cancelled.")
			return nil
		}
	}

	if log != nil && stagingPlan.Enabled {
		log.Info("Local staging enabled: selected backup parts will be copied to temp storage at %s before restore", filepath.ToSlash(stagingPlan.TempDir))
	}

	// Collect password (with retry).
	rep := selected[0]
	password, err := operation.ReadPasswordWithRetry(targetDir, rep, "Enter restore password: ", log)
	if err != nil {
		return err
	}

	fmt.Println("Restore started.")
	if log != nil {
		log.Info("Restore destination: %s", restorePath)
	}

	totalPartsProcessed, err := restoreSelectedEntries(selected, targetDir, restorePath, password, log, stagingPlan)
	if err != nil {
		return err
	}

	if log != nil {
		log.Info("Restore completed successfully.")
	}
	printRestoreCompletionSummary(selected, totalPartsProcessed, logPath, warningCount)
	fmt.Println("\nRestore completed successfully.")
	return nil
}

func resolveRestoreSelection(cfg *util.Config, targetDir string, index []util.BackupEntry) ([]util.BackupEntry, string, error) {
	if cfg.NonInteractive {
		return catalog.ResolveNewestBackupRunSelection(targetDir, index)
	}
	return operation.PromptBackupSelection("restore", targetDir, index)
}

func promptRestoreDestination(targetDir string) (string, error) {
	fmt.Println()
	fmt.Println("Enter restore destination:")
	fmt.Println("  - Enter a specific path (e.g. C:\\Restore) → restore to this directory")
	fmt.Println("  - Enter a dot (.) → restore in the target folder itself")
	fmt.Println()

	restorePath, err := security.ReadLine("Restore destination: ")
	if err != nil {
		return "", err
	}
	restorePath = strings.TrimSpace(restorePath)

	if restorePath == "." {
		restorePath = targetDir
	}
	if restorePath == "" {
		return "", fmt.Errorf("Restore destination must not be empty. Remedy: Provide a destination folder (e.g. C:/Restore) or '.' for target_folder.")
	}
	return restorePath, nil
}

type restorePreflightItem struct {
	Entry     util.BackupEntry
	PartCount int
	OutputDir string
	Err       error
}

func buildRestorePreflight(selected []util.BackupEntry, targetDir, restorePath string) []restorePreflightItem {
	items := make([]restorePreflightItem, 0, len(selected))
	for _, entry := range selected {
		item := restorePreflightItem{
			Entry:     entry,
			PartCount: len(catalog.CollectParts(targetDir, entry)),
			OutputDir: filepath.Join(restorePath, entry.FolderName),
		}
		if item.PartCount == 0 {
			item.Err = fmt.Errorf("No part files found. Remedy: Ensure all .enc parts of this backup are in the same target_folder.")
		}
		if _, err := os.Stat(item.OutputDir); err == nil {
			item.Err = fmt.Errorf("Target directory already exists. Remedy: Choose a different restore destination or rename/delete the existing target directory.")
		}
		items = append(items, item)
	}
	return items
}

func printRestorePreflight(targetDir, restorePath string, items []restorePreflightItem, requiresYubiKey, yubiKeyOnly bool, stagingPlan operation.LocalStagingPlan) {
	displayBackupFolder := filepath.ToSlash(targetDir)
	displayRestoreTarget := filepath.ToSlash(restorePath)

	fmt.Println()
	fmt.Println("Restore preflight")
	fmt.Println("-----------------")
	fmt.Printf("Backup folder  : %s\n", displayBackupFolder)
	fmt.Printf("Restore target : %s\n", displayRestoreTarget)
	fmt.Printf("Authentication : %s\n", operation.BackupAuthenticationLabel(requiresYubiKey, yubiKeyOnly))
	fmt.Printf("Items selected : %d\n", len(items))
	if stagingPlan.Enabled {
		fmt.Printf("Local staging  : enabled via %s because backup folder and restore target share the same drive/share (%s)\n", filepath.ToSlash(stagingPlan.ResolvedTempDir), util.VolumeDisplay(targetDir))
	} else if stagingPlan.SameVolume {
		fmt.Printf("  [WARN] Backup folder and restore target are on the same drive/share (%s). This can cause long stalls, especially on network/NAS storage. Local staging is unavailable because TEMP is on the same drive/share. Remedy: Prefer a different destination or point TEMP/TMP to a local drive.\n", util.VolumeDisplay(targetDir))
	}
	fmt.Println("Selection:")
	for _, item := range items {
		if item.Err != nil {
			fmt.Printf("  [ERROR] %s (parts: %d)\n", item.Entry.String(), item.PartCount)
			fmt.Printf("          -> %v\n", item.Err)
			continue
		}
		fmt.Printf("  [OK]    %s (parts: %d)\n", item.Entry.String(), item.PartCount)
	}
	fmt.Println()
}

func validateRestorePreflight(items []restorePreflightItem) error {
	invalid := 0
	for _, item := range items {
		if item.Err != nil {
			invalid++
		}
	}
	if invalid > 0 {
		return fmt.Errorf("Restore preflight failed: %d selected item(s) are invalid. Remedy: Fix the [ERROR] entries above and start restore again.", invalid)
	}
	return nil
}

func promptStartRestore() (bool, error) {
	return operation.PromptStartAction("restore")
}

func restoreSelectedEntries(selected []util.BackupEntry, targetDir, restorePath string, password []byte, log *util.Logger, stagingPlan operation.LocalStagingPlan) (int, error) {
	totalPartsProcessed := 0
	for _, entry := range selected {
		readDir := targetDir
		cleanup := func() {}
		if stagingPlan.Enabled {
			stagedDir, err := stageBackupEntryLocally(targetDir, entry, stagingPlan.ResolvedTempDir, log)
			if err != nil {
				return 0, fmt.Errorf("Local staging failed for %q: %w", entry.String(), err)
			}
			readDir = stagedDir
			cleanup = func() {
				if err := os.RemoveAll(stagedDir); err != nil && log != nil {
					log.Warn("Failed to remove staging directory %s: %v", filepath.ToSlash(stagedDir), err)
				}
			}
		}

		partCount, err := restoreEntry(entry, readDir, restorePath, password, log)
		cleanup()
		if err != nil {
			return 0, fmt.Errorf("Failed to restore folder %q: %w", entry.String(), err)
		}
		totalPartsProcessed += partCount
		if log != nil {
			log.Info("Folder %q successfully restored", entry.FolderName)
		}
	}
	return totalPartsProcessed, nil
}

func stageBackupEntryLocally(targetDir string, entry util.BackupEntry, tempDir string, log *util.Logger) (string, error) {
	parts := catalog.CollectParts(targetDir, entry)
	if len(parts) == 0 {
		return "", fmt.Errorf("No part files found for %s. Remedy: Ensure all .enc files for this backup are in the same target_folder.", entry.String())
	}

	stageDir, err := os.MkdirTemp(tempDir, "restoresafe-restore-stage-*")
	if err != nil {
		return "", fmt.Errorf("Failed to create local staging directory: %w. Remedy: Check TEMP/TMP write permissions and free disk space.", err)
	}

	if log != nil {
		log.Info("Local staging started for %s: copying %d part file(s) to %s", entry.String(), len(parts), filepath.ToSlash(stageDir))
	}

	for _, partPath := range parts {
		destinationPath := filepath.Join(stageDir, filepath.Base(partPath))
		if err := operation.CopyFile(partPath, destinationPath); err != nil {
			_ = os.RemoveAll(stageDir)
			return "", err
		}
	}

	if log != nil {
		log.Info("Local staging completed for %s", entry.String())
	}

	return stageDir, nil
}

// restoreEntry decrypts all parts of one backup entry and extracts to destDir.
func restoreEntry(entry util.BackupEntry, targetDir, destDir string, password []byte, log *util.Logger) (int, error) {
	parts := catalog.CollectParts(targetDir, entry)
	if len(parts) == 0 {
		errMsg := fmt.Sprintf("No part files found for %s", entry.String())
		return 0, fmt.Errorf("%s. Remedy: Put all related .enc files into the same target_folder.", errMsg)
	}

	if log != nil {
		log.Info("Processing %d part file(s) for %s", len(parts), entry.String())
	}

	seqReader := util.NewSequentialReader(parts)
	defer seqReader.Close()

	var inBytes atomic.Int64
	var outBytes atomic.Int64
	var outWriteCalls atomic.Int64
	progressDone := make(chan struct{})
	progressStopped := make(chan struct{})
	go func() {
		logRestoreProgress(log, entry.FolderName, &inBytes, &outBytes, &outWriteCalls, progressDone)
		close(progressStopped)
	}()
	defer func() {
		close(progressDone)
		<-progressStopped
	}()

	// Verify target directory can be created before starting decryption.
	outDir := filepath.Join(destDir, entry.FolderName)
	if err := os.MkdirAll(outDir, 0o750); err != nil {
		errMsg := fmt.Sprintf("Failed to create target directory: %v", err)
		return 0, fmt.Errorf("%s. Remedy: Check write permissions and use a valid destination path.", errMsg)
	}

	if log != nil {
		log.Info("Extracting to: %s", outDir)
	}

	// Pipe: decrypt → TAR extract.
	pr, pw := io.Pipe()

	decErrCh := make(chan error, 1)
	go func() {
		err := security.Decrypt(
			&operation.CountingWriter{W: pw, Total: &outBytes, Calls: &outWriteCalls},
			&operation.CountingReader{R: seqReader, Total: &inBytes},
			password,
		)
		pw.CloseWithError(err) //nolint:errcheck
		decErrCh <- err
	}()

	extractErr := operation.ExtractTar(pr, outDir)
	if extractErr != nil {
		pr.CloseWithError(extractErr) //nolint:errcheck
	}
	// Note: Do NOT close pr here. The decrypt goroutine will close pw,
	// signaling EOF to the reader. Closing pr prematurely causes "write on closed pipe".
	decErr := <-decErrCh

	if decErr != nil {
		if errors.Is(decErr, security.ErrWrongPassword) {
			return 0, fmt.Errorf("%w. Remedy: Check the password; for YubiKey backups, the matching .challenge file must be in the same folder as the .enc files.", security.ErrWrongPassword)
		}
		return 0, fmt.Errorf("Decryption failed: %w", decErr)
	}
	if extractErr != nil {
		return 0, fmt.Errorf("Extraction failed: %w", extractErr)
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

func printRestoreCompletionSummary(selected []util.BackupEntry, totalPartsProcessed int, logPath string, warningCount int) {
	folderNames := make([]string, 0, len(selected))
	for _, entry := range selected {
		folderNames = append(folderNames, entry.FolderName)
	}

	fmt.Println()
	fmt.Println("Restore summary")
	fmt.Println("---------------")
	fmt.Printf("Processed folders: %d (%s)\n", len(selected), summarizeNames(folderNames))
	fmt.Printf("Parts processed  : %d\n", totalPartsProcessed)
	fmt.Printf("Log file         : %s\n", logPath)
	if warningCount > 0 {
		fmt.Printf("Warnings         : %d\n", warningCount)
	} else {
		fmt.Println("Warnings         : none")
	}
}

func logRestoreProgress(log *util.Logger, folderName string, inBytes, outBytes, outWriteCalls *atomic.Int64, done <-chan struct{}) {
	operation.LogProgressUntilDone(log, folderName, "decrypted", inBytes, outBytes, outWriteCalls, done)
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
