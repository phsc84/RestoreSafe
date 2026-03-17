package verify

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
	"sync/atomic"
)

// Run verifies selected backup sets without restoring them to disk.
func Run(cfg *util.Config, exeDir string) error {
	targetDir := util.ResolveDir(cfg.TargetFolder, exeDir)

	index, err := catalog.ScanBackups(targetDir)
	if err != nil {
		return fmt.Errorf("Failed to scan target folder %q: %w. Remedy: Check the target_folder path in config.yaml and ensure the folder is readable.", targetDir, err)
	}
	if len(index) == 0 {
		fmt.Println("No backups found in target folder. Remedy: Check whether .enc files are in target_folder and whether the correct folder is selected.")
		return nil
	}

	selected, selection, err := resolveVerifySelection(cfg, targetDir, index)
	if err != nil {
		if errors.Is(err, operation.ErrSelectionCancelled) {
			fmt.Println("Verification cancelled.")
			return nil
		}
		return err
	}

	requiresYubiKey, yubiKeyOnly, err := catalog.BackupRunUsesYubiKey(targetDir, selected[0])
	if err != nil {
		return fmt.Errorf("Failed to inspect backup authentication: %w. Remedy: Check read permissions in the backup folder and existing .challenge files.", err)
	}

	log := operation.OpenLogger(cfg, targetDir, selected[0])
	if log != nil {
		defer log.Close()
		log.Info("Verification started - Selection: %q", selection)
	}

	stagingPlan := operation.PlanLocalStaging(targetDir, targetDir, os.TempDir())
	preflight := buildVerifyPreflight(selected, targetDir)
	printVerifyPreflight(targetDir, preflight, requiresYubiKey, yubiKeyOnly, stagingPlan)
	if err := validateVerifyPreflight(preflight); err != nil {
		if log != nil {
			log.Error("%v", err)
		}
		return err
	}

	if cfg.NonInteractive {
		if log != nil {
			log.Info("Non-interactive mode: start confirmation skipped")
		}
		fmt.Println("Starting verification automatically.")
	} else {
		confirmed, err := operation.PromptStartAction("verification")
		if err != nil {
			if log != nil {
				log.Error("Failed to read confirmation: %v", err)
			}
			return err
		}
		if !confirmed {
			if log != nil {
				log.Info("Verification cancelled by user before start")
			}
			fmt.Println("Verification cancelled.")
			return nil
		}
	}

	password, err := operation.ReadPasswordWithRetry(targetDir, selected[0], "Enter verification password: ", log)
	if err != nil {
		if log != nil {
			log.Error("Password input failed: %v", err)
		}
		return err
	}

	fmt.Println("Verification started.")
	if log != nil {
		log.Info("Verifying %d selected item(s)", len(selected))
	}
	verifyDir := targetDir
	var cleanup = func() {}

	if stagingPlan.Enabled {
		stagedDir, err := operation.StageLocalDirectory(targetDir, targetDir, stagingPlan.ResolvedTempDir, log)
		if err != nil {
			if log != nil {
				log.Error("Local staging failed: %v", err)
			}
			return fmt.Errorf("Local staging failed: %w", err)
		}
		verifyDir = stagedDir
		cleanup = func() {
			if err := os.RemoveAll(stagedDir); err != nil && log != nil {
				log.Warn("Failed to remove staging directory %s: %v", filepath.ToSlash(stagedDir), err)
			}
		}
	}

	if err := verifySelectedEntries(selected, verifyDir, password, log); err != nil {
		cleanup()
		return err
	}
	cleanup()

	if log != nil {
		log.Info("Verification completed successfully.")
	} else {
		fmt.Println("\nVerification completed successfully.")
	}
	return nil
}

func resolveVerifySelection(cfg *util.Config, targetDir string, index []util.BackupEntry) ([]util.BackupEntry, string, error) {
	if cfg.NonInteractive {
		return catalog.ResolveNewestBackupRunSelection(targetDir, index)
	}
	return operation.PromptBackupSelection("verify", targetDir, index)
}

type verifyPreflightItem struct {
	Entry          util.BackupEntry
	PartCount      int
	TotalSizeBytes int64
	Err            error
}

func buildVerifyPreflight(selected []util.BackupEntry, targetDir string) []verifyPreflightItem {
	items := make([]verifyPreflightItem, 0, len(selected))
	for _, entry := range selected {
		partCount, totalSizeBytes, err := catalog.InspectBackupParts(targetDir, entry)
		items = append(items, verifyPreflightItem{
			Entry:          entry,
			PartCount:      partCount,
			TotalSizeBytes: totalSizeBytes,
			Err:            err,
		})
	}
	return items
}

func printVerifyPreflight(targetDir string, items []verifyPreflightItem, requiresYubiKey, yubiKeyOnly bool, stagingPlan operation.LocalStagingPlan) {
	fmt.Println()
	fmt.Println("Verify preflight")
	fmt.Println("----------------")
	displayBackupFolder := filepath.ToSlash(targetDir)
	fmt.Printf("Backup folder   : %s\n", displayBackupFolder)
	fmt.Printf("Authentication  : %s\n", operation.BackupAuthenticationLabel(requiresYubiKey, yubiKeyOnly))
	fmt.Printf("Items selected  : %d\n", len(items))
	if stagingPlan.Enabled {
		fmt.Printf("Local staging   : enabled via %s because backup folder is on network storage (%s)\n", filepath.ToSlash(stagingPlan.ResolvedTempDir), util.VolumeDisplay(targetDir))
	}
	fmt.Println("Selection:")
	for _, item := range items {
		sizeMB := float64(item.TotalSizeBytes) / (1024 * 1024)
		if item.Err != nil {
			fmt.Printf("  [ERROR] %s (parts: %d, size: %.2f MB)\n", item.Entry.String(), item.PartCount, sizeMB)
			fmt.Printf("          -> %v\n", item.Err)
			continue
		}
		fmt.Printf("  [OK]    %s (parts: %d, size: %.2f MB)\n", item.Entry.String(), item.PartCount, sizeMB)
	}
	fmt.Println()
}

func validateVerifyPreflight(items []verifyPreflightItem) error {
	invalid := 0
	for _, item := range items {
		if item.Err != nil {
			invalid++
		}
	}
	if invalid > 0 {
		return fmt.Errorf("Verify preflight failed: %d selected item(s) are incomplete or invalid. Remedy: Fix the [ERROR] entries above and start verify again.", invalid)
	}
	return nil
}

func verifySelectedEntries(selected []util.BackupEntry, targetDir string, password []byte, log *util.Logger) error {
	for _, entry := range selected {
		if err := verifyEntry(entry, targetDir, password, log); err != nil {
			if log != nil {
				log.Error("Failed to verify folder %q: %v", entry.String(), err)
			}
			return fmt.Errorf("Failed to verify folder %q: %w", entry.String(), err)
		}
		if log != nil {
			log.Info("Folder %q successfully verified", entry.FolderName)
		}
	}
	return nil
}

func verifyEntry(entry util.BackupEntry, targetDir string, password []byte, log *util.Logger) error {
	parts := catalog.CollectParts(targetDir, entry)
	if len(parts) == 0 {
		return fmt.Errorf("No part files found for %s. Remedy: Ensure all .enc files for this backup are in the same target_folder.", entry.String())
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
		operation.LogProgressUntilDone(log, entry.FolderName, "verified", &inBytes, &outBytes, &outWriteCalls, progressDone)
		close(progressStopped)
	}()
	defer func() {
		close(progressDone)
		<-progressStopped
	}()

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

	validateErr := operation.ValidateTar(pr)
	if validateErr != nil {
		pr.CloseWithError(validateErr) //nolint:errcheck
	}
	decErr := <-decErrCh

	if decErr != nil {
		if errors.Is(decErr, security.ErrWrongPassword) {
			return fmt.Errorf("%w. Remedy: Check the password; for YubiKey backups, the matching .challenge file must be in the same folder.", security.ErrWrongPassword)
		}
		return fmt.Errorf("Decryption failed: %w", decErr)
	}
	if validateErr != nil {
		return fmt.Errorf("Archive validation failed: %w", validateErr)
	}

	return nil
}
