package engine

import (
	"RestoreSafe/internal/security"
	"RestoreSafe/internal/util"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"sync/atomic"
	"time"
)

// RunVerify verifies selected backup sets without restoring them to disk.
func RunVerify(cfg *util.Config, exeDir string) error {
	targetDir := resolveDir(cfg.TargetFolder, exeDir)

	index, err := scanBackups(targetDir)
	if err != nil {
		return fmt.Errorf("Failed to scan target folder %q: %w. Remedy: Check the target_folder path in config.yaml and ensure the folder is readable.", targetDir, err)
	}
	if len(index) == 0 {
		fmt.Println("No backups found in target folder. Remedy: Check whether .enc files are in target_folder and whether the correct folder is selected.")
		return nil
	}

	selected, selection, err := promptBackupSelection("verify", targetDir, index)
	if err != nil {
		return err
	}

	requiresYubiKey, err := backupRunUsesYubiKey(targetDir, selected[0])
	if err != nil {
		return fmt.Errorf("Failed to inspect backup authentication: %w. Remedy: Check read permissions in the backup folder and existing .challenge files.", err)
	}

	log := openOperationLogger(cfg, targetDir, selected[0])
	if log != nil {
		defer log.Close()
		log.Info("Verification started - Selection: %q", selection)
	}

	preflight := buildVerifyPreflight(selected, targetDir)
	printVerifyPreflight(targetDir, preflight, requiresYubiKey)
	if err := validateVerifyPreflight(preflight); err != nil {
		if log != nil {
			log.Error("%v", err)
		}
		return err
	}

	confirmed, err := promptStartAction("verification")
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

	password, err := readPasswordWithRetry(targetDir, selected[0], "Enter verification password: ", log)
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

	if err := verifySelectedEntries(selected, targetDir, password, log); err != nil {
		return err
	}

	if log != nil {
		log.Info("Verification completed successfully.")
	}
	fmt.Println("\nVerification completed successfully.")
	return nil
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
		partCount, totalSizeBytes, err := inspectBackupParts(targetDir, entry)
		items = append(items, verifyPreflightItem{
			Entry:          entry,
			PartCount:      partCount,
			TotalSizeBytes: totalSizeBytes,
			Err:            err,
		})
	}
	return items
}

func printVerifyPreflight(targetDir string, items []verifyPreflightItem, requiresYubiKey bool) {
	fmt.Println()
	fmt.Println("Verify preflight")
	fmt.Println("----------------")
	fmt.Printf("Backup folder   : %s\n", targetDir)
	fmt.Printf("Authentication  : %s\n", backupAuthenticationLabel(requiresYubiKey))
	fmt.Printf("Items selected  : %d\n", len(items))
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
			return fmt.Errorf("Failed to verify folder %q: %w. Remedy: Check .enc part completeness and password/YubiKey.", entry.String(), err)
		}
		if log != nil {
			log.Info("Folder %q successfully verified", entry.FolderName)
		}
	}
	return nil
}

func verifyEntry(entry util.BackupEntry, targetDir string, password []byte, log *util.Logger) error {
	parts := collectParts(targetDir, entry)
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
	go logVerifyProgress(log, entry.FolderName, &inBytes, &outBytes, &outWriteCalls, progressDone)
	defer close(progressDone)

	pr, pw := io.Pipe()
	decErrCh := make(chan error, 1)
	go func() {
		err := security.Decrypt(
			&countingWriter{w: pw, total: &outBytes, calls: &outWriteCalls},
			&countingReader{r: seqReader, total: &inBytes},
			password,
		)
		pw.CloseWithError(err) //nolint:errcheck
		decErrCh <- err
	}()

	validateErr := ValidateTar(pr)
	if validateErr != nil {
		pr.CloseWithError(validateErr) //nolint:errcheck
	}
	decErr := <-decErrCh

	if decErr != nil {
		if errors.Is(decErr, security.ErrWrongPassword) {
			return security.ErrWrongPassword
		}
		return fmt.Errorf("Decryption failed: %w. Remedy: Check the password; for YubiKey backups, the matching .challenge file must be in the same folder.", decErr)
	}
	if validateErr != nil {
		return fmt.Errorf("Archive validation failed: %w. Remedy: Check backup completeness and recreate the backup if needed.", validateErr)
	}

	return nil
}

func inspectBackupParts(targetDir string, entry util.BackupEntry) (int, int64, error) {
	entries, err := os.ReadDir(targetDir)
	if err != nil {
		return 0, 0, err
	}

	type partInfo struct {
		seq  int
		size int64
	}

	parts := make([]partInfo, 0)
	for _, dirEntry := range entries {
		parsedEntry, seq, ok := util.ParsePartFileName(dirEntry.Name())
		if !ok {
			continue
		}
		if parsedEntry != entry {
			continue
		}

		info, err := dirEntry.Info()
		if err != nil {
			return len(parts), 0, fmt.Errorf("Failed to inspect part file %q: %w. Remedy: Check file/folder permissions.", dirEntry.Name(), err)
		}
		parts = append(parts, partInfo{seq: seq, size: info.Size()})
	}

	if len(parts) == 0 {
		return 0, 0, fmt.Errorf("No part files found. Remedy: Ensure the .enc files are present in target_folder.")
	}

	sort.Slice(parts, func(i, j int) bool {
		return parts[i].seq < parts[j].seq
	})

	var totalSize int64
	for i, part := range parts {
		totalSize += part.size
		expectedSeq := i + 1
		if part.seq != expectedSeq {
			return len(parts), totalSize, fmt.Errorf("Missing part file %03d. Remedy: Restore the missing .enc part or create a new backup.", expectedSeq)
		}
	}

	return len(parts), totalSize, nil
}

func logVerifyProgress(log *util.Logger, folderName string, inBytes, outBytes, outWriteCalls *atomic.Int64, done <-chan struct{}) {
	if log == nil {
		<-done
		return
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			logStreamProgress(log, folderName, "verified", inBytes, outBytes, outWriteCalls, true)
			return
		case <-ticker.C:
			logStreamProgress(log, folderName, "verified", inBytes, outBytes, outWriteCalls, false)
		}
	}
}
