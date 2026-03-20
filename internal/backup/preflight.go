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
	fmt.Println("Backup preflight")
	fmt.Println("----------------")
	fmt.Printf("Target folder   : %s\n", targetDir)
	fmt.Printf("Split size      : %d MB\n", cfg.SplitSizeMB)
	fmt.Printf("Retention keep  : %d\n", cfg.RetentionKeep)
	fmt.Printf("Authentication  : %s\n", cfg.AuthenticationMode.Label())
	if cfg.UseYubiKey() {
		status := "[OK]"
		msg := "YubiKey connected. Keep it connected now before starting backup."
		if err := checkYubiKeyConnected(); err != nil {
			status = "[WARN]"
			msg = "YubiKey authentication is enabled and no YubiKey is currently detected. Remedy: Connect the YubiKey now before starting backup."
		}
		fmt.Printf("  %s %s\n", status, msg)
	}
	fmt.Printf("Log level       : %s\n", strings.ToLower(cfg.LogLevel))

	estimatedBytes, estimateWarnings := estimateSelectedSourceBytes(sources)
	if estimatedBytes > 0 {
		fmt.Printf("Est. source size: %s\n", util.FormatBytesBinary(uint64(estimatedBytes)))
	} else {
		fmt.Println("Est. source size: unknown")
	}
	for _, warning := range estimateWarnings {
		fmt.Printf("  [WARN] size estimate: %s\n", warning)
	}

	freeBytes, freeErr := util.QueryFreeSpaceBytes(targetDir)
	if freeErr != nil {
		fmt.Printf("Free space      : unknown (%v)\n", freeErr)
	} else {
		fmt.Printf("Free space      : %s\n", util.FormatBytesBinary(freeBytes))
		if estimatedBytes > 0 && uint64(estimatedBytes) > freeBytes {
			fmt.Println("  [WARN] estimated source size exceeds currently free space on target")
		}
	}

	if stagingPlan.Enabled {
		fmt.Printf("Local staging   : enabled via %s because source and target folders share the same drive/share (%s)\n", filepath.ToSlash(stagingPlan.ResolvedTempDir), util.VolumeDisplay(targetDir))
	}
	sameVolumeNetworkWarning := !stagingPlan.Enabled && stagingPlan.SameVolume && util.IsNetworkVolume(targetDir)

	fmt.Println("Source folders:")
	for _, src := range sources {
		baseName := util.FolderBaseName(src.Resolved)
		backupName := src.BackupName
		if backupName == "" {
			backupName = baseName
		}

		if src.Err != nil {
			fmt.Printf("  [ERROR] %s\n", src.Resolved)
			if backupName != baseName {
				fmt.Printf("          -> backup name: %s\n", backupName)
			}
			fmt.Printf("          -> %v\n", src.Err)
			continue
		}
		if src.Warning != "" {
			fmt.Printf("  [WARN]  %s\n", src.Resolved)
			if backupName != baseName {
				fmt.Printf("          -> backup name: %s\n", backupName)
			}
			fmt.Printf("          -> %s\n", src.Warning)
		} else {
			fmt.Printf("  [OK]    %s\n", src.Resolved)
			if backupName != baseName {
				fmt.Printf("          -> backup name: %s\n", backupName)
			}
		}

		if sameVolumeNetworkWarning && !src.Skip && util.SameVolume(src.Resolved, targetDir) {
			fmt.Printf("  [WARN]  Source folder warning: Source and target folders are on the same drive/share (%s). This can cause long stalls, especially on network/NAS storage. Local staging is unavailable because TEMP is on the same drive/share. Remedy: Prefer a different target drive/share or point TEMP/TMP to a local drive.\n", util.VolumeDisplay(targetDir))
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
