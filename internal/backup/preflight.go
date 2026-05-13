package backup

import (
	"RestoreSafe/internal/operation"
	"RestoreSafe/internal/util"
	"fmt"
	"path/filepath"
	"strings"
)

func printBackupPreflightWithYubiKeyCheck(
	cfg *util.Config,
	targetDir string,
	sources []backupSourcePlan,
	stagingPlan operation.LocalStagingPlan,
	checkYubiKeyConnected func() error,
) {
	fmt.Println()
	fmt.Println("-----------------------------------------")
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
	fmt.Printf("  Needed disk space (total): %s\n", util.FormatBytesBinary(uint64(estimatedBytes)))

	fmt.Println("Backup folder:")
	fmt.Printf("  [OK] %s\n", targetDir)
	if freeErr != nil {
		fmt.Printf("  Free disk space: unknown (%v)\n", freeErr)
	} else {
		fmt.Printf("  Free disk space: %s\n", util.FormatBytesBinary(freeBytes))
	}

	operation.PrintPreflightField(operation.PreflightFieldLabelWidth, "Split size", fmt.Sprintf("%d MB", cfg.SplitSizeMB))
	operation.PrintPreflightField(operation.PreflightFieldLabelWidth, "Retention keep", fmt.Sprintf("%d", cfg.RetentionKeep))
	operation.PrintPreflightField(operation.PreflightFieldLabelWidth, "KDF (Argon2id)", fmt.Sprintf("time=%d  memory=%d MB  threads=%d", cfg.Argon2.Time, cfg.Argon2.MemoryMB, cfg.Argon2.Threads))
	operation.PrintPreflightField(operation.PreflightFieldLabelWidth, "Authentication", cfg.AuthenticationMode.Label())
	operation.PrintYubiKeyPreflightStatus(cfg.UseYubiKey(), "backup", checkYubiKeyConnected)
	operation.PrintPreflightField(operation.PreflightFieldLabelWidth, "Log level", strings.ToLower(cfg.LogLevel))

	if stagingPlan.Enabled {
		fmt.Println()
		fmt.Printf("Local staging enabled, because source and target folders share the same drive/share (%s).\n", util.VolumeDisplay(targetDir))
		fmt.Println("Temp directory:")
		fmt.Printf("  [OK] %s\n", filepath.ToSlash(stagingPlan.ResolvedTempDir))
		localFreeBytes, localFreeErr := util.QueryFreeSpaceBytes(stagingPlan.ResolvedTempDir)
		if localFreeErr != nil {
			fmt.Printf("  Free disk space: unknown (%v)\n", localFreeErr)
		} else {
			fmt.Printf("  Free disk space: %s\n", util.FormatBytesBinary(localFreeBytes))
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

	if !util.IsSpaceInsufficient(estimatedBytes, freeBytes) {
		return nil
	}

	return fmt.Errorf(
		"Backup preflight failed: %s",
		util.FormatInsufficientBackupSpaceMessage(uint64(estimatedBytes), freeBytes),
	)
}

func validateStagingSpaceForBackup(stagingPlan operation.LocalStagingPlan, sources []backupSourcePlan) error {
	if !stagingPlan.Enabled {
		return nil
	}
	estimatedBytes, _ := estimateSelectedSourceBytes(sources)
	if estimatedBytes <= 0 {
		return nil
	}
	freeBytes, err := util.QueryFreeSpaceBytes(stagingPlan.ResolvedTempDir)
	if err != nil {
		return nil
	}
	if uint64(estimatedBytes) <= freeBytes {
		return nil
	}
	return fmt.Errorf(
		"Backup preflight failed: Insufficient free space in temp directory for local staging: needed %s, available %s. Remedy: Free disk space in %s or set TEMP/TMP to a different drive.",
		util.FormatBytesBinary(uint64(estimatedBytes)),
		util.FormatBytesBinary(freeBytes),
		filepath.ToSlash(stagingPlan.ResolvedTempDir),
	)
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
