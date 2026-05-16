package backup

import (
	"RestoreSafe/internal/operation"
	"RestoreSafe/internal/util"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

func printBackupPreflightWithYubiKeyCheck(
	w io.Writer,
	cfg *util.Config,
	targetDir string,
	sources []backupSource,
	stagingPlan operation.LocalStagingPlan,
	checkYubiKeyConnected func() error,
) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, "-----------------------------------------")
	estimatedBytes, estimateWarnings := estimateSelectedSourceBytes(sources)
	freeBytes, freeErr := util.QueryFreeSpaceBytes(targetDir)
	sameVolumeNetworkWarning := !stagingPlan.Enabled && stagingPlan.SameVolume && util.IsNetworkVolume(targetDir)

	fmt.Fprintln(w, "Source directory(s):")
	for _, src := range sources {
		baseName := util.DirectoryBaseName(src.Resolved)
		backupName := src.BackupName
		if backupName == "" {
			backupName = baseName
		}

		if src.Err != nil {
			fmt.Fprintf(w, "  [ERROR] %s\n", src.Resolved)
			if backupName != baseName {
				fmt.Fprintf(w, "          → backup name: %s\n", backupName)
			}
			fmt.Fprintf(w, "          → %v\n", src.Err)
			continue
		}
		if src.Warning != "" {
			fmt.Fprintf(w, "  [WARN] %s\n", src.Resolved)
			if backupName != baseName {
				fmt.Fprintf(w, "          → backup name: %s\n", backupName)
			}
			fmt.Fprintf(w, "          → %s\n", src.Warning)
		} else {
			fmt.Fprintf(w, "  [OK] %s\n", src.Resolved)
			if backupName != baseName {
				fmt.Fprintf(w, "          → backup name: %s\n", backupName)
			}
		}

		if sameVolumeNetworkWarning && !src.Skip && util.SameVolume(src.Resolved, targetDir) {
			fmt.Fprintf(w, "          → Source and target directories are on the same drive/share (%s). This can cause long stalls, especially on network/NAS storage. Local staging is unavailable because TEMP is on the same drive/share. Remedy: Prefer a different target drive/share or point TEMP/TMP to a local drive.\n", util.VolumeDisplay(targetDir))
		}
	}
	for _, warning := range estimateWarnings {
		fmt.Fprintf(w, "  [WARN] size estimate: %s\n", warning)
	}
	if estimatedBytes < 0 {
		estimatedBytes = 0
	}
	fmt.Fprintf(w, "  Needed disk space (total): %s\n", util.FormatBytesBinary(uint64(estimatedBytes)))

	fmt.Fprintln(w, "Backup directory:")
	fmt.Fprintf(w, "  [OK] %s\n", targetDir)
	if freeErr != nil {
		fmt.Fprintf(w, "  Free disk space: unknown (%v)\n", freeErr)
	} else {
		fmt.Fprintf(w, "  Free disk space: %s\n", util.FormatBytesBinary(freeBytes))
	}

	operation.PrintPreflightField(w, operation.PreflightFieldLabelWidth, "Split size", fmt.Sprintf("%d MB", cfg.SplitSizeMB))
	operation.PrintPreflightField(w, operation.PreflightFieldLabelWidth, "Retention keep", fmt.Sprintf("%d", cfg.RetentionKeep))
	operation.PrintPreflightField(w, operation.PreflightFieldLabelWidth, "KDF (Argon2id)", fmt.Sprintf("time=%d  memory=%d MB  threads=%d", cfg.Argon2.Time, cfg.Argon2.MemoryMB, cfg.Argon2.Threads))
	operation.PrintPreflightField(w, operation.PreflightFieldLabelWidth, "Authentication", cfg.AuthenticationMode.Label())
	operation.PrintYubiKeyPreflightStatus(w, cfg.UseYubiKey(), "backup", checkYubiKeyConnected)
	operation.PrintPreflightField(w, operation.PreflightFieldLabelWidth, "Log level", strings.ToLower(cfg.LogLevel))

	if stagingPlan.Enabled {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "Local staging via temp directory enabled, because source directory(s) and backup directory share the same drive (%s).\n", util.VolumeDisplay(targetDir))
		fmt.Fprintln(w, "Temp directory:")
		fmt.Fprintf(w, "  [OK] %s\n", filepath.ToSlash(stagingPlan.ResolvedTempDir))
		localFreeBytes, localFreeErr := util.QueryFreeSpaceBytes(stagingPlan.ResolvedTempDir)
		if localFreeErr != nil {
			fmt.Fprintf(w, "  Free disk space: unknown (%v)\n", localFreeErr)
		} else {
			fmt.Fprintf(w, "  Free disk space: %s\n", util.FormatBytesBinary(localFreeBytes))
		}
	}
}

func validateSourceDirectories(sources []backupSource) error {
	return operation.ValidatePreflightItems(
		sources,
		func(src backupSource) bool { return src.Err != nil },
		"Backup preflight failed: %d source directory(s) are invalid or inaccessible. Remedy: Fix the [ERROR] entries above and start backup again.",
	)
}

func validateTargetSpaceForBackup(targetDir string, sources []backupSource) error {
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

func validateStagingSpaceForBackup(stagingPlan operation.LocalStagingPlan, sources []backupSource) error {
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

func runnableSourceCount(sources []backupSource) int {
	count := 0
	for _, source := range sources {
		if source.Err != nil || source.Skip {
			continue
		}
		count++
	}
	return count
}

func estimateSelectedSourceBytes(sources []backupSource) (int64, []string) {
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
