package backup

import (
	"RestoreSafe/internal/operation"
	"RestoreSafe/internal/util"
	"fmt"
	"path/filepath"
	"strings"
)

const preflightFieldLabelWidth = 17

func printPreflightField(label, value string) {
	fmt.Printf("%-*s: %s\n", preflightFieldLabelWidth, label, value)
}

func printBackupPreflightWithYubiKeyCheck(
	cfg *util.Config,
	targetDir string,
	sources []backupSourcePlan,
	stagingPlan operation.LocalStagingPlan,
	checkYubiKeyConnected func() error,
) {
	fmt.Println()
	fmt.Println("Backup preflight")
	fmt.Println("----------------")
	estimatedBytes, estimateWarnings := estimateSelectedSourceBytes(sources)
	freeBytes, freeErr := util.QueryFreeSpaceBytes(targetDir)
	sameVolumeNetworkWarning := !stagingPlan.Enabled && stagingPlan.SameVolume && util.IsNetworkVolume(targetDir)

	fmt.Println("Source folder(s):")
	for _, src := range sources {
		baseName := util.FolderBaseName(src.Resolved)
		backupName := src.BackupName
		if backupName == "" {
			backupName = baseName
		}

		if src.Err != nil {
			fmt.Printf("  [ERROR] %s\n", src.Resolved)
			if backupName != baseName {
				fmt.Printf("          → backup name: %s\n", backupName)
			}
			fmt.Printf("          → %v\n", src.Err)
			continue
		}
		if src.Warning != "" {
			fmt.Printf("  [WARN] %s\n", src.Resolved)
			if backupName != baseName {
				fmt.Printf("          → backup name: %s\n", backupName)
			}
			fmt.Printf("          → %s\n", src.Warning)
		} else {
			fmt.Printf("  [OK] %s\n", src.Resolved)
			if backupName != baseName {
				fmt.Printf("          → backup name: %s\n", backupName)
			}
		}

		if sameVolumeNetworkWarning && !src.Skip && util.SameVolume(src.Resolved, targetDir) {
			fmt.Printf("          → Source and target folders are on the same drive/share (%s). This can cause long stalls, especially on network/NAS storage. Local staging is unavailable because TEMP is on the same drive/share. Remedy: Prefer a different target drive/share or point TEMP/TMP to a local drive.\n", util.VolumeDisplay(targetDir))
		}
	}
	for _, warning := range estimateWarnings {
		fmt.Printf("  [WARN] size estimate: %s\n", warning)
	}
	if estimatedBytes < 0 {
		estimatedBytes = 0
	}
	fmt.Printf("  [OK] Needed disk space (total): %s\n", util.FormatBytesBinary(uint64(estimatedBytes)))

	fmt.Println("Target folder:")
	fmt.Printf("  [OK] %s\n", targetDir)
	if freeErr != nil {
		fmt.Printf("  [WARN] Free disk space: unknown (%v)\n", freeErr)
	} else {
		fmt.Printf("  [OK] Free disk space: %s\n", util.FormatBytesBinary(freeBytes))
	}

	printPreflightField("Split size", fmt.Sprintf("%d MB", cfg.SplitSizeMB))
	printPreflightField("Retention keep", fmt.Sprintf("%d", cfg.RetentionKeep))
	printPreflightField("Authentication", cfg.AuthenticationMode.Label())
	if cfg.UseYubiKey() {
		status := "[OK]"
		msg := "YubiKey connected. Keep it connected now before starting backup."
		if err := checkYubiKeyConnected(); err != nil {
			status = "[WARN]"
			msg = "YubiKey authentication is enabled and no YubiKey is currently detected. Remedy: Connect the YubiKey now before starting backup."
		}
		fmt.Printf("  %s %s\n", status, msg)
	}
	printPreflightField("Log level", strings.ToLower(cfg.LogLevel))

	if stagingPlan.Enabled {
		printPreflightField("Local staging", fmt.Sprintf("enabled via %s because source and target folders share the same drive/share (%s)", filepath.ToSlash(stagingPlan.ResolvedTempDir), util.VolumeDisplay(targetDir)))

		localFreeBytes, localFreeErr := util.QueryFreeSpaceBytes(stagingPlan.ResolvedTempDir)
		if localFreeErr != nil {
			printPreflightField("Free space local", fmt.Sprintf("unknown (%v)", localFreeErr))
		} else {
			printPreflightField("Free space local", util.FormatBytesBinary(localFreeBytes))
		}
	}
}

func validateSourceFolders(sources []backupSourcePlan) error {
	return operation.ValidatePreflightItems(
		sources,
		func(src backupSourcePlan) bool { return src.Err != nil },
		"Backup preflight failed: %d source folder(s) are invalid or inaccessible. Remedy: Fix the [ERROR] entries above and start backup again.",
	)
}

func validateTargetSpaceForBackup(targetDir string, sources []backupSourcePlan) error {
	estimatedBytes, _ := estimateSelectedSourceBytes(sources)
	if estimatedBytes <= 0 {
		return nil
	}

	freeBytes, err := util.QueryFreeSpaceBytes(targetDir)
	if err != nil {
		return nil
	}

	if !isTargetSpaceInsufficient(estimatedBytes, freeBytes) {
		return nil
	}

	return fmt.Errorf(
		"Backup preflight failed: %s",
		util.FormatInsufficientBackupSpaceMessage(uint64(estimatedBytes), freeBytes),
	)
}

func isTargetSpaceInsufficient(estimatedBytes int64, freeBytes uint64) bool {
	return estimatedBytes > 0 && uint64(estimatedBytes) > freeBytes
}

func runnableSourceCount(sources []backupSourcePlan) int {
	count := 0
	for _, source := range sources {
		if source.Err != nil || source.Skip {
			continue
		}
		count++
	}
	return count
}

func estimateSelectedSourceBytes(sources []backupSourcePlan) (int64, []string) {
	var total int64
	warnings := make([]string, 0)

	for _, source := range sources {
		if source.Err != nil || source.Skip {
			continue
		}

		size, err := util.DirectorySizeBytes(source.Resolved)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s (%v)", source.Resolved, err))
			continue
		}
		total += size
	}

	return total, warnings
}
