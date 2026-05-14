package verify

import (
	"RestoreSafe/internal/catalog"
	"RestoreSafe/internal/operation"
	"RestoreSafe/internal/security"
	"RestoreSafe/internal/util"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
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

	selected, selection, err := resolveVerifySelection(targetDir, index)
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
	defer log.Close()
	log.Info("Verification started - Selection: %q", selection)

	preflight := buildVerifyPreflight(selected, targetDir)
	printVerifyPreflightWithYubiKeyCheck(cfg, targetDir, preflight, requiresYubiKey, yubiKeyOnly, security.CheckYubiKeyConnected)
	if err := validateVerifyPreflight(preflight); err != nil {
		return err
	}

	confirmed, err := operation.PromptStartAction("verification")
	if err != nil {
		return err
	}
	if !confirmed {
		log.Info("Verification cancelled by user before start")
		fmt.Println("Verification cancelled.")
		return nil
	}

	password, err := operation.ReadPasswordWithRetry(targetDir, selected[0], "Enter verification password: ", log)
	if err != nil {
		return err
	}

	fmt.Println("Verification started.")
	log.Info("Verifying %d selected item(s)", len(selected))
	if err := verifySelectedEntries(selected, targetDir, password, log); err != nil {
		return err
	}

	log.Info("Verification completed successfully.")
	return nil
}

func resolveVerifySelection(targetDir string, index []util.BackupEntry) ([]util.BackupEntry, string, error) {
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

func printVerifyPreflightWithYubiKeyCheck(
	cfg *util.Config,
	targetDir string,
	items []verifyPreflightItem,
	requiresYubiKey, yubiKeyOnly bool,
	checkYubiKeyConnected func() error,
) {
	var issues []string

	fmt.Println()
	fmt.Println("-----------------------------------------")

	// Backup selection
	fmt.Println("Backup selection:")
	fmt.Printf("  Source folder: %s\n", filepath.ToSlash(targetDir))
	for _, item := range items {
		if item.Err != nil {
			fmt.Printf("  [ERROR] %s (parts: %d)\n", item.Entry.String(), item.PartCount)
			issues = append(issues, item.Err.Error())
		} else {
			fmt.Printf("  [OK] %s (parts: %d)\n", item.Entry.String(), item.PartCount)
		}
	}
	var totalBytes int64
	for _, item := range items {
		if item.Err == nil {
			totalBytes += item.TotalSizeBytes
		}
	}
	if totalBytes > 0 {
		fmt.Printf("  Used disk space (total): %s\n", util.FormatBytesBinary(uint64(totalBytes)))
	} else {
		fmt.Printf("  Used disk space (total): unknown\n")
	}

	// Authentication and Log level
	operation.PrintPreflightField(operation.PreflightFieldLabelWidth, "Authentication", operation.BackupAuthenticationLabel(requiresYubiKey, yubiKeyOnly))
	operation.PrintYubiKeyPreflightStatus(requiresYubiKey, "verification", checkYubiKeyConnected)
	operation.PrintPreflightField(operation.PreflightFieldLabelWidth, "Log level", strings.ToLower(cfg.LogLevel))

	// Print collected issues
	if len(issues) > 0 {
		fmt.Println()
		for _, issue := range issues {
			fmt.Printf("[ERROR] %s\n", issue)
		}
	}
}

func validateVerifyPreflight(items []verifyPreflightItem) error {
	return operation.ValidatePreflightItems(
		items,
		func(item verifyPreflightItem) bool { return item.Err != nil },
		"Verify preflight failed: %d selected item(s) are incomplete or invalid. Remedy: Fix the [ERROR] entries above and start verify again.",
	)
}

func verifySelectedEntries(selected []util.BackupEntry, targetDir string, password []byte, log *util.Logger) error {
	for _, entry := range selected {
		if err := verifyEntry(entry, targetDir, password, log); err != nil {
			return fmt.Errorf("Failed to verify folder %q: %w", entry.String(), err)
		}
		log.Info("Folder %q successfully verified", entry.FolderName)
	}
	return nil
}

func verifyEntry(entry util.BackupEntry, targetDir string, password []byte, log *util.Logger) error {
	parts, err := catalog.CollectParts(targetDir, entry)
	if err != nil {
		return err
	}
	if len(parts) == 0 {
		return fmt.Errorf("No part files found for %s. Remedy: Ensure all .enc files for this backup are in the same target_folder.", entry.String())
	}

	log.Info("Processing %d part file(s) for %s", len(parts), entry.String())

	err = operation.RunDecryptPipeline(
		parts,
		password,
		log,
		entry.FolderName,
		"verified",
		"Archive validation",
		util.ValidateTar,
		func(partIndex, partCount int) {
			fmt.Printf("  Verifying part %d/%d...\n", partIndex, partCount)
		},
	)
	if err != nil {
		return err
	}

	return nil
}
