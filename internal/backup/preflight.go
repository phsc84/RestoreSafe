package backup

import (
	"RestoreSafe/internal/operation"
	"RestoreSafe/internal/ui"
	"RestoreSafe/internal/util"
	"fmt"
	"path/filepath"
	"strings"
)

// backupPreflightReport describes what the backup will do: each source
// directory with its backup type, the backup directory, the keys, the
// settings, and local staging. The issues that block the backup are added by
// backupPreflightIssues.
func backupPreflightReport(
	cfg *util.Config,
	backupDir string,
	sources []backupSource,
	stagingPlan operation.LocalStagingPlan,
	keys keyPlan,
	plans map[string]*dirPlan,
	checkYubiKeyConnected func() error,
) ui.Report {
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

	directories := []ui.Row{ui.Heading("Source directory(s)")}
	for _, src := range sources {
		baseName := util.DirectoryBaseName(src.Resolved)
		backupName := src.BackupName
		if backupName == "" {
			backupName = baseName
		}
		var details []string
		if backupName != baseName {
			details = append(details, "backup name: "+backupName)
		}

		if src.Err != nil {
			directories = append(directories, ui.Item(ui.StatusError, src.Resolved, append(details, src.Err.Error())...))
			continue
		}
		status := ui.StatusOK
		if src.Warning != "" {
			status = ui.StatusWarn
			details = append(details, src.Warning)
		}
		if plan := plans[backupName]; plan != nil && !src.Skip {
			reasonLabel := "reason: "
			if plan.IsDiff() {
				reasonLabel = ""
			}
			details = append(details, fmt.Sprintf("%s backup (%s%s)", plan.Label(), reasonLabel, plan.Reason))
		}
		if sameVolumeNetworkWarning && !src.Skip && util.SameVolume(src.Resolved, backupDir) {
			details = append(details, fmt.Sprintf("Source and backup directories are on the same drive/share (%s). This can cause long stalls, especially on network/NAS storage. Local staging is unavailable because TEMP is on the same drive/share. Remedy: Prefer a different backup drive/share or point TEMP/TMP to a local drive.", util.VolumeDisplay(backupDir)))
		}
		directories = append(directories, ui.Item(status, src.Resolved, details...))
	}
	for _, warning := range estimateWarnings {
		directories = append(directories, ui.Item(ui.StatusWarn, "size estimate: "+warning))
	}
	directories = append(directories, ui.Heading("Backup directory"), ui.Item(ui.StatusOK, backupDir))
	directories = append(directories, operation.AuthRows(cfg.AuthenticationMode.Label(), cfg.UseYubiKey(), "backup", checkYubiKeyConnected)...)
	directories = append(directories, keyPlanRows(keys)...)

	if estimatedBytes < 0 {
		estimatedBytes = 0
	}
	settings := []ui.Row{ui.Field("Needed space", util.FormatBytesBinary(uint64(estimatedBytes)))}
	if freeErr != nil {
		settings = append(settings, ui.Field("Free space", fmt.Sprintf("unknown (%v)", freeErr)))
	} else {
		settings = append(settings, ui.Field("Free space", util.FormatBytesBinary(freeBytes)))
	}
	settings = append(settings, ui.Field("Split size", fmt.Sprintf("%d MB", cfg.SplitSizeMB)))
	if len(sourceSizes) > 0 && splitSizeBytes > 0 {
		if advisory := partCountAdvisory(maxPartCount); advisory != "" {
			settings = append(settings, ui.Item(ui.StatusWarn, advisory))
		}
	}
	settings = append(settings, ui.Field("Retention", retentionSummary(cfg.RetentionKeep, cfg.Differential.RetentionKeepDifferentials)))
	verifyAfter := "disabled"
	if cfg.VerifyAfterBackup {
		verifyAfter = "enabled"
	}
	settings = append(settings, ui.Field("Verify after backup", verifyAfter))
	excludeInfo := "none"
	if n := len(cfg.Exclude); n > 0 {
		excludeInfo = strings.Join(cfg.Exclude, ", ")
	}
	settings = append(settings, ui.Field("Exclude", excludeInfo))
	unreadable := "abort backup (fail)"
	if cfg.SkipUnreadableFiles() {
		unreadable = "skip and warn (skip)"
	}
	settings = append(settings,
		ui.Field("Unreadable files", unreadable),
		ui.Field("KDF (Argon2id)", fmt.Sprintf("time=%d  memory=%d MB  threads=%d", cfg.Argon2.Time, cfg.Argon2.MemoryMB, cfg.Argon2.Threads)),
		ui.Field("Log level", strings.ToLower(cfg.LogLevel)),
	)

	report := ui.Report{Title: "Backup preflight", Sections: []ui.Section{{Rows: directories}, {Rows: settings}}}
	if stagingPlan.Enabled {
		staging := []ui.Row{
			ui.Note(fmt.Sprintf("Local staging via temp directory enabled, because source directory(s) and backup directory share the same drive (%s).", util.VolumeDisplay(backupDir))),
			ui.Heading("Temp directory"),
			ui.Item(ui.StatusOK, filepath.ToSlash(stagingPlan.ResolvedTempDir)),
		}
		if localFreeBytes, err := util.QueryFreeSpaceBytes(stagingPlan.ResolvedTempDir); err != nil {
			staging = append(staging, ui.Item(ui.StatusNone, fmt.Sprintf("Free disk space: unknown (%v)", err)))
		} else {
			staging = append(staging, ui.Item(ui.StatusNone, "Free disk space: "+util.FormatBytesBinary(localFreeBytes)))
		}
		report.Sections = append(report.Sections, ui.Section{Rows: staging})
	}
	return report
}

// keyPlanRows state whether the run reuses the existing keys or creates new
// ones, and what that means for the user.
func keyPlanRows(keys keyPlan) []ui.Row {
	if keys.Existing != nil {
		return []ui.Row{ui.Field("Keys", "existing keys, "+keys.Existing.Summary())}
	}
	return []ui.Row{
		ui.Field("Keys", "new keys will be created"),
		ui.Item(ui.StatusInfo, keys.NewKeysReason+": new keys will be created and every source directory gets a full backup."),
		ui.Item(ui.StatusInfo, "Passwords, YubiKey registrations, and recovery codes of earlier keys do not open the new backups (they still open older backups)."),
	}
}

// backupPreflightIssues runs the checks that block a backup and returns them
// as report issues, together with the error of the first failed check.
func backupPreflightIssues(cfg *util.Config, backupDir string, sources []backupSource, stagingPlan operation.LocalStagingPlan) ([]ui.Issue, error) {
	var issues []ui.Issue
	var first error
	for _, err := range []error{
		validateSourceDirectories(sources),
		validateTargetSpaceForBackup(backupDir, sources),
		validateStagingSpaceForBackup(stagingPlan, sources),
		validateBackupPartCount(cfg, sources),
	} {
		if err == nil {
			continue
		}
		if first == nil {
			first = err
		}
		issues = append(issues, ui.Issue{Status: ui.StatusError, Text: strings.TrimPrefix(err.Error(), "Backup preflight failed: ")})
	}
	return issues, first
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
