package backup

import (
	"RestoreSafe/internal/operation"
	"RestoreSafe/internal/util"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

// backupPreflightLabelWidth is the label column width for the backup preflight
// summary fields. It is sized to the longest label so every colon lines up.
const backupPreflightLabelWidth = 19

func printBackupPreflightWithYubiKeyCheck(
	w io.Writer,
	cfg *util.Config,
	backupDir string,
	sources []backupSource,
	stagingPlan operation.LocalStagingPlan,
	checkYubiKeyAvailability func() error,
	checkYubiKeyConnected func() error,
) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Backup preflight")
	fmt.Fprintln(w, "----------------")
	sourceSizes, estimateWarnings := walkSourceSizes(sources)
	var estimatedBytes, maxPartCount int64
	splitSizeBytes := cfg.SplitSizeMB * 1024 * 1024
	for _, size := range sourceSizes {
		estimatedBytes += size
		if parts := estimatePartCountWithMargin(size, splitSizeBytes); parts > maxPartCount {
			maxPartCount = parts
		}
	}
	freeBytes, freeErr := util.QueryFreeSpaceBytes(backupDir)
	sameVolumeNetworkWarning := !stagingPlan.Enabled && stagingPlan.SameVolume && util.IsNetworkVolume(backupDir)

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

		if sameVolumeNetworkWarning && !src.Skip && util.SameVolume(src.Resolved, backupDir) {
			fmt.Fprintf(w, "          → Source and backup directories are on the same drive/share (%s). This can cause long stalls, especially on network/NAS storage. Local staging is unavailable because TEMP is on the same drive/share. Remedy: Prefer a different backup drive/share or point TEMP/TMP to a local drive.\n", util.VolumeDisplay(backupDir))
		}
	}
	for _, warning := range estimateWarnings {
		fmt.Fprintf(w, "  [WARN] size estimate: %s\n", warning)
	}

	fmt.Fprintln(w, "Backup directory:")
	fmt.Fprintf(w, "  [OK] %s\n", backupDir)

	operation.PrintAuthStatus(w, cfg.AuthenticationMode.Label(), cfg.UseYubiKey(), "backup", checkYubiKeyAvailability, checkYubiKeyConnected)

	fmt.Fprintln(w)
	if estimatedBytes < 0 {
		estimatedBytes = 0
	}
	operation.PrintField(w, backupPreflightLabelWidth, "Needed space", util.FormatBytesBinary(uint64(estimatedBytes)))
	if freeErr != nil {
		operation.PrintField(w, backupPreflightLabelWidth, "Free space", fmt.Sprintf("unknown (%v)", freeErr))
	} else {
		operation.PrintField(w, backupPreflightLabelWidth, "Free space", util.FormatBytesBinary(freeBytes))
	}
	operation.PrintField(w, backupPreflightLabelWidth, "Split size", fmt.Sprintf("%d MB", cfg.SplitSizeMB))
	if len(sourceSizes) > 0 && splitSizeBytes > 0 {
		if advisory := partCountAdvisory(maxPartCount); advisory != "" {
			fmt.Fprintf(w, "  [WARN] %s\n", advisory)
		}
	}
	operation.PrintField(w, backupPreflightLabelWidth, "Retention keep", fmt.Sprintf("%d", cfg.RetentionKeep))
	verifyAfter := "disabled"
	if cfg.VerifyAfterBackup {
		verifyAfter = "enabled"
	}
	operation.PrintField(w, backupPreflightLabelWidth, "Verify after backup", verifyAfter)
	operation.PrintField(w, backupPreflightLabelWidth, "KDF (Argon2id)", fmt.Sprintf("time=%d  memory=%d MB  threads=%d", cfg.Argon2.Time, cfg.Argon2.MemoryMB, cfg.Argon2.Threads))
	operation.PrintField(w, backupPreflightLabelWidth, "Log level", strings.ToLower(cfg.LogLevel))

	if stagingPlan.Enabled {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "Local staging via temp directory enabled, because source directory(s) and backup directory share the same drive (%s).\n", util.VolumeDisplay(backupDir))
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

func validateTargetSpaceForBackup(backupDir string, sources []backupSource) error {
	estimatedBytes, _ := estimateSelectedSourceBytes(sources)
	if estimatedBytes <= 0 {
		return nil
	}

	freeBytes, err := util.QueryFreeSpaceBytes(backupDir)
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

const (
	// partCountSafetyMarginPercent inflates the raw source size before
	// estimating the part count. The encrypted stream carries the source data
	// plus TAR framing (a 512-byte header and padding per file) and a small
	// per-chunk encryption tag, so the on-disk size is always somewhat larger
	// than the raw directory size. The margin keeps backups whose estimate sits
	// just under the limit from silently crossing it at runtime.
	partCountSafetyMarginPercent = 5

	// partCountWarnThreshold is the estimated part count (with margin) at which
	// the preflight warns that a source is approaching the limit, so users can
	// raise split_size_mb before a growing backup eventually crosses it.
	partCountWarnThreshold = util.MaxPartSequence * 9 / 10
)

// estimatePartCount returns the number of fixed-size parts needed to hold
// sizeBytes (ceiling division). splitSizeBytes <= 0 yields 0.
func estimatePartCount(sizeBytes, splitSizeBytes int64) int64 {
	if splitSizeBytes <= 0 {
		return 0
	}
	return (sizeBytes + splitSizeBytes - 1) / splitSizeBytes
}

// estimatePartCountWithMargin estimates the part count after inflating the raw
// size by partCountSafetyMarginPercent to account for archive and encryption
// overhead.
func estimatePartCountWithMargin(sizeBytes, splitSizeBytes int64) int64 {
	withMargin := sizeBytes + sizeBytes*partCountSafetyMarginPercent/100
	return estimatePartCount(withMargin, splitSizeBytes)
}

// partCountAdvisory returns a preflight warning when the largest source is near
// (but not over) the part limit. Over-limit backups are hard-stopped by
// validateBackupPartCount with a clear error, so they return no advisory here.
func partCountAdvisory(maxPartCount int64) string {
	if maxPartCount > util.MaxPartSequence || maxPartCount < partCountWarnThreshold {
		return ""
	}
	return fmt.Sprintf(
		"Largest source is approaching the %d-part limit (≈ %d parts incl. %d%% overhead margin). Remedy: Increase split_size_mb in config.yaml to keep future backups within the limit.",
		util.MaxPartSequence, maxPartCount, partCountSafetyMarginPercent,
	)
}

// validateBackupPartCount rejects backups that would produce more part files
// than the naming scheme can represent (see util.MaxPartSequence). Each source
// directory is written as its own sequence of parts starting at 1, so the limit
// is checked per source. The estimate uses the source size and configured split
// size (plus a safety margin for overhead); both are known before the backup starts.
func validateBackupPartCount(cfg *util.Config, sources []backupSource) error {
	splitSizeBytes := cfg.SplitSizeMB * 1024 * 1024
	if splitSizeBytes <= 0 {
		return nil
	}

	for _, source := range sources {
		if source.Err != nil || source.Skip {
			continue
		}

		size, err := util.DirectorySizeBytes(source.Resolved)
		if err != nil {
			// Size could not be determined; the runtime path handles I/O errors.
			continue
		}

		estimatedParts := estimatePartCountWithMargin(size, splitSizeBytes)
		if estimatedParts > util.MaxPartSequence {
			return fmt.Errorf(
				"Backup preflight failed: %q is approximately %s, which at a split size of %d MB would create about %d part files (incl. %d%% overhead margin) - exceeding the %d-part limit of the backup naming scheme. Remedy: Increase split_size_mb in config.yaml so the backup fits within %d parts, or split the source into smaller backups.",
				source.Resolved,
				util.FormatBytesBinary(uint64(size)),
				cfg.SplitSizeMB,
				estimatedParts,
				partCountSafetyMarginPercent,
				util.MaxPartSequence,
				util.MaxPartSequence,
			)
		}
	}

	return nil
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

// walkSourceSizes measures each runnable source once, returning the per-source
// sizes and warnings for sources whose size could not be determined.
func walkSourceSizes(sources []backupSource) (sizes []int64, warnings []string) {
	sizes = make([]int64, 0)
	warnings = make([]string, 0)

	for _, source := range sources {
		if source.Err != nil || source.Skip {
			continue
		}

		size, err := util.DirectorySizeBytes(source.Resolved)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s (%v)", source.Resolved, err))
			continue
		}
		sizes = append(sizes, size)
	}

	return sizes, warnings
}

func estimateSelectedSourceBytes(sources []backupSource) (int64, []string) {
	sizes, warnings := walkSourceSizes(sources)
	var total int64
	for _, size := range sizes {
		total += size
	}
	return total, warnings
}
