package verify

import (
	"RestoreSafe/internal/catalog"
	"RestoreSafe/internal/operation"
	"RestoreSafe/internal/security"
	"RestoreSafe/internal/util"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const preflightFieldLabelWidth = 15

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

	stagingPlan := operation.PlanLocalStaging(targetDir, targetDir, os.TempDir())
	preflight := buildVerifyPreflight(selected, targetDir)
	printVerifyPreflightWithYubiKeyCheck(cfg, targetDir, preflight, requiresYubiKey, yubiKeyOnly, stagingPlan, security.CheckYubiKeyConnected)
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
	var scope *operation.StagingScope
	if stagingPlan.Enabled {
		fmt.Println("Staging selected backups locally. This can take a moment on network storage.")
		stagedDir, err := stageSelectedVerifyParts(selected, targetDir, stagingPlan.ResolvedTempDir, log)
		if err != nil {
			return fmt.Errorf("Local staging failed: %w", err)
		}
		fmt.Println("Local staging completed.")
		scope = operation.ActiveStagingScope(stagedDir, log)
	}
	defer scope.Cleanup()

	verifyDir := scope.ActiveDir(targetDir)
	if err := verifySelectedEntries(selected, verifyDir, password, log); err != nil {
		return err
	}

	log.Info("Verification completed successfully.")
	return nil
}

func resolveVerifySelection(targetDir string, index []util.BackupEntry) ([]util.BackupEntry, string, error) {
	return operation.PromptBackupSelection("verify", targetDir, index)
}

type verifyPreflightItem struct {
	Entry     util.BackupEntry
	PartCount int
	Err       error
}

func buildVerifyPreflight(selected []util.BackupEntry, targetDir string) []verifyPreflightItem {
	items := make([]verifyPreflightItem, 0, len(selected))
	for _, entry := range selected {
		partCount, _, err := catalog.InspectBackupParts(targetDir, entry)
		items = append(items, verifyPreflightItem{
			Entry:     entry,
			PartCount: partCount,
			Err:       err,
		})
	}
	return items
}

func printVerifyPreflightWithYubiKeyCheck(
	cfg *util.Config,
	targetDir string,
	items []verifyPreflightItem,
	requiresYubiKey, yubiKeyOnly bool,
	stagingPlan operation.LocalStagingPlan,
	checkYubiKeyConnected func() error,
) {
	fmt.Println()
	fmt.Println("Verify preflight")
	fmt.Println("----------------")

	fmt.Println("Backup selection:")
	entries := make([]operation.PreflightEntry, len(items))
	for i, item := range items {
		entries[i] = operation.PreflightEntry{
			Label: fmt.Sprintf("%s (parts: %d)", item.Entry.String(), item.PartCount),
			Err:   item.Err,
		}
	}
	operation.PrintPreflightSelection(entries)
	if stagingPlan.Enabled {
		operation.PrintPreflightField(preflightFieldLabelWidth, "Local staging", fmt.Sprintf("enabled via %s because backup folder is on network storage (%s)", filepath.ToSlash(stagingPlan.ResolvedTempDir), util.VolumeDisplay(targetDir)))
	}

	operation.PrintPreflightField(preflightFieldLabelWidth, "Authentication", operation.BackupAuthenticationLabel(requiresYubiKey, yubiKeyOnly))
	if requiresYubiKey {
		status := "[OK]"
		msg := "YubiKey connected. Keep it connected now before starting verification."
		if err := checkYubiKeyConnected(); err != nil {
			status = "[WARN]"
			msg = "YubiKey authentication is enabled and no YubiKey is currently detected. Remedy: Connect the YubiKey now before starting verification."
		}
		fmt.Printf("  %s %s\n", status, msg)
	}

	operation.PrintPreflightField(preflightFieldLabelWidth, "Log level", strings.ToLower(cfg.LogLevel))
}

func validateVerifyPreflight(items []verifyPreflightItem) error {
	return operation.ValidatePreflightItems(
		items,
		func(item verifyPreflightItem) bool { return item.Err != nil },
		"Verify preflight failed: %d selected item(s) are incomplete or invalid. Remedy: Fix the [ERROR] entries above and start verify again.",
	)
}

func stageSelectedVerifyParts(selected []util.BackupEntry, sourceDir, tempDir string, log *util.Logger) (string, error) {
	stagingDir, err := operation.CreateStagingDir(tempDir, "verify-stage-*")
	if err != nil {
		return "", err
	}

	copied := 0
	seen := make(map[string]struct{})
	for _, entry := range selected {
		parts, err := catalog.CollectParts(sourceDir, entry)
		if err != nil {
			operation.CleanupStagingDirDuring(stagingDir, "error recovery", log)
			return "", err
		}
		if len(parts) == 0 {
			operation.CleanupStagingDirDuring(stagingDir, "error recovery", log)
			return "", fmt.Errorf("No part files found for %s. Remedy: Ensure all .enc files for this backup are in the same folder.", entry.String())
		}

		for _, partPath := range parts {
			if _, exists := seen[partPath]; exists {
				continue
			}
			seen[partPath] = struct{}{}

			destinationPath := filepath.Join(stagingDir, filepath.Base(partPath))
			if err := util.CopyFile(partPath, destinationPath); err != nil {
				operation.CleanupStagingDirDuring(stagingDir, "error recovery", log)
				if strings.Contains(err.Error(), "Remedy:") {
					return "", err
				}
				return "", fmt.Errorf("Failed to stage selected part %q: %w. Remedy: Check network availability, TEMP/TMP free space, and write permissions.", filepath.Base(partPath), err)
			}
			copied++
		}
	}

	log.Info("Local staging enabled: staged %d selected part file(s) to %s", copied, filepath.ToSlash(stagingDir))

	return stagingDir, nil
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
	)
	if err != nil {
		return err
	}

	return nil
}
