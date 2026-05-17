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
	"strings"
)

// Run verifies selected backup sets without restoring them to disk.
func Run(cfg *util.Config, exeDir string) error {
	backupDir := util.ResolveDir(cfg.BackupDirectory, exeDir)

	index, err := catalog.ScanBackups(backupDir)
	if err != nil {
		return fmt.Errorf("Failed to scan backup directory %q: %w. Remedy: Check the backup_directory path in config.yaml and ensure the directory is readable.", backupDir, err)
	}
	if len(index) == 0 {
		fmt.Println("No backups found in backup directory. Remedy: Check whether .enc files are in the backup directory and whether the correct directory is selected.")
		return nil
	}

	selected, selection, err := resolveVerifySelection(backupDir, index)
	if err != nil {
		if errors.Is(err, operation.ErrSelectionCancelled) {
			fmt.Println("Verification cancelled.")
			return nil
		}
		return err
	}
	warningCount := verifySelectionWarningCount(selection, index)

	requiresYubiKey, yubiKeyOnly, err := catalog.BackupRunUsesYubiKey(backupDir, selected[0])
	if err != nil {
		return fmt.Errorf("Failed to inspect backup authentication: %w. Remedy: Check read permissions in the backup directory and existing .challenge files.", err)
	}

	logPath := util.LogFileName(backupDir, selected[0].Date, selected[0].ID)
	log := operation.OpenLogger(cfg, backupDir, selected[0])
	if log.IsConsoleOnly() {
		warningCount++
	}
	defer log.Close()

	preflight := buildVerifyPreflight(selected, backupDir)
	printVerifyPreflightWithYubiKeyCheck(os.Stdout, cfg, backupDir, preflight, requiresYubiKey, yubiKeyOnly, security.CheckYubiKeyConnected)
	if err := validateVerifyPreflight(preflight); err != nil {
		return err
	}

	confirmed, err := operation.PromptStartAction("verification")
	if err != nil {
		return err
	}
	if !confirmed {
		log.InfoLogOnly("Verification cancelled by user before start")
		fmt.Println("Verification cancelled.")
		return nil
	}

	password, err := operation.ReadPasswordWithRetry(backupDir, selected[0], "Enter verification password: ", log)
	if err != nil {
		return err
	}
	defer func() { security.ZeroBytes(password) }()

	fmt.Println()
	log.Info("Verification started - ID: %s, date: %s", string(selected[0].ID), selected[0].Date)
	log.Info("Verification selection:")
	for _, entry := range selected {
		log.Info("  %s", entry.String())
	}

	_, err = verifySelectedEntries(selected, backupDir, password, log)
	if err != nil {
		return err
	}

	log.Info("Verification completed successfully.")
	fmt.Printf("\nLog file: %s\n", logPath)
	if warningCount > 0 {
		fmt.Printf("Warnings: %d\n", warningCount)
	}
	return nil
}

func resolveVerifySelection(backupDir string, index []util.BackupEntry) ([]util.BackupEntry, string, error) {
	return operation.PromptBackupSelection("verify", backupDir, index)
}

type verifyPreflightItem struct {
	Entry          util.BackupEntry
	PartCount      int
	TotalSizeBytes int64
	Err            error
}

func buildVerifyPreflight(selected []util.BackupEntry, backupDir string) []verifyPreflightItem {
	items := make([]verifyPreflightItem, 0, len(selected))
	for _, entry := range selected {
		partCount, totalSizeBytes, err := catalog.InspectBackupParts(backupDir, entry)
		items = append(items, verifyPreflightItem{
			Entry:          entry,
			PartCount:      partCount,
			TotalSizeBytes: totalSizeBytes,
			Err:            err,
		})
	}
	return items
}

func printVerifyPreflightWithYubiKeyCheck(
	w io.Writer,
	cfg *util.Config,
	backupDir string,
	items []verifyPreflightItem,
	requiresYubiKey, yubiKeyOnly bool,
	checkYubiKeyConnected func() error,
) {
	var issues []string

	fmt.Fprintln(w)
	fmt.Fprintln(w, "Verification preflight")
	fmt.Fprintln(w, "----------------------")

	// Backup selection
	fmt.Fprintln(w, "Backup selection:")
	fmt.Fprintf(w, "  Path: %s\n", filepath.ToSlash(backupDir))
	for _, item := range items {
		if item.Err != nil {
			fmt.Fprintf(w, "  [ERROR] %s (parts: %d)\n", item.Entry.String(), item.PartCount)
			issues = append(issues, item.Err.Error())
		} else {
			fmt.Fprintf(w, "  [OK] %s (parts: %d)\n", item.Entry.String(), item.PartCount)
		}
	}
	totalBytes := estimateVerifyBytes(items)
	if totalBytes > 0 {
		fmt.Fprintf(w, "  Used disk space (total): %s\n", util.FormatBytesBinary(uint64(totalBytes)))
	} else {
		fmt.Fprintf(w, "  Used disk space (total): unknown\n")
	}

	// Authentication and Log level
	operation.PrintField(w, operation.DefaultFieldLabelWidth, "Authentication", operation.BackupAuthenticationLabel(requiresYubiKey, yubiKeyOnly))
	operation.PrintYubiKeyPreflightStatus(w, requiresYubiKey, "verification", checkYubiKeyConnected)
	operation.PrintField(w, operation.DefaultFieldLabelWidth, "Log level", strings.ToLower(cfg.LogLevel))

	// Print collected issues
	if len(issues) > 0 {
		fmt.Fprintln(w)
		for _, issue := range issues {
			fmt.Fprintf(w, "[ERROR] %s\n", issue)
		}
	}
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
	return operation.ValidatePreflightItems(
		items,
		func(item verifyPreflightItem) bool { return item.Err != nil },
		"Verification preflight failed: %d selected item(s) are incomplete or invalid. Remedy: Fix the [ERROR] entries above and start verification again.",
	)
}

func verifySelectedEntries(selected []util.BackupEntry, backupDir string, password []byte, log *util.Logger) (int, error) {
	totalPartsProcessed := 0
	for _, entry := range selected {
		partCount, err := verifyEntry(entry, backupDir, password, log)
		if err != nil {
			return 0, fmt.Errorf("Failed to verify directory %q: %w", entry.String(), err)
		}
		totalPartsProcessed += partCount
		log.Info("  Verified: %d part file(s) - [%s] successfully verified", partCount, entry.DirectoryName)
	}
	return totalPartsProcessed, nil
}

func verifyEntry(entry util.BackupEntry, backupDir string, password []byte, log *util.Logger) (int, error) {
	parts, err := catalog.CollectParts(backupDir, entry)
	if err != nil {
		return 0, err
	}
	if len(parts) == 0 {
		return 0, fmt.Errorf("No part files found for %s. Remedy: Ensure all .enc files for this backup are in the same backup directory.", entry.String())
	}

	log.Info("Processing backup directory: %s", entry.DirectoryName)

	err = operation.RunDecryptPipeline(
		parts,
		password,
		log,
		entry.DirectoryName,
		"verified",
		"Archive validation",
		util.ValidateTar,
		nil,
	)
	if err != nil {
		return 0, err
	}

	return len(parts), nil
}


func verifySelectionWarningCount(selection string, index []util.BackupEntry) int {
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
