package backup

import (
	"RestoreSafe/internal/config"
	"RestoreSafe/internal/format/archive"
	"RestoreSafe/internal/format/naming"
	"RestoreSafe/internal/fsx"
	"RestoreSafe/internal/workflow/interact"
	"RestoreSafe/internal/workflow/job"
	"RestoreSafe/internal/workflow/plan"
	"fmt"
	"strings"
	"time"
)

// backupPreflightReport describes what the backup will do: each source
// directory with its backup type, the backup directory, the keys, and the
// settings. The issues that block the backup are added by
// backupPreflightIssues.
func backupPreflightReport(
	cfg *config.Config,
	backupDir string,
	sources []plan.Source,
	keys plan.Keys,
	plans map[string]*plan.Folder,
	est spaceEstimate,
	checkYubiKeyConnected func() error,
) interact.Report {
	var maxPartCount int64
	splitSizeBytes := cfg.SplitSizeMB * 1024 * 1024
	for _, size := range est.sizes {
		if parts := estimatePartCountWithMargin(size, splitSizeBytes); parts > maxPartCount {
			maxPartCount = parts
		}
	}
	freeBytes, freeErr := fsx.QueryFreeSpaceBytes(backupDir)

	directories := []interact.Row{interact.Heading("Source directory(s)")}
	for _, src := range sources {
		baseName := naming.DirectoryBaseName(src.Resolved)
		backupName := src.BackupName
		if backupName == "" {
			backupName = baseName
		}
		var details []string
		if backupName != baseName {
			details = append(details, "backup name: "+backupName)
		}

		if src.Err != nil {
			directories = append(directories, interact.Item(interact.StatusError, src.Resolved, append(details, src.Err.Error())...))
			continue
		}
		status := interact.StatusOK
		if src.Warning != "" {
			status = interact.StatusWarn
			details = append(details, src.Warning)
		}
		if folder := plans[backupName]; folder != nil && !src.Skip {
			reasonLabel := "reason: "
			if folder.IsDiff() {
				reasonLabel = ""
			}
			details = append(details, fmt.Sprintf("%s backup (%s%s)", folder.Label(), reasonLabel, folder.Reason))
		}
		directories = append(directories, interact.Item(status, src.Resolved, details...))
	}
	for _, warning := range est.warnings {
		directories = append(directories, interact.Item(interact.StatusWarn, "size estimate: "+warning))
	}
	directories = append(directories, interact.Heading("Backup directory"), interact.Item(interact.StatusOK, backupDir))
	directories = append(directories, job.AuthRows(cfg.AuthenticationMode.Label(), cfg.UseYubiKey(), "backup", checkYubiKeyConnected)...)
	directories = append(directories, keyPlanRows(keys)...)

	settings := []interact.Row{interact.Field("Needed space", est.neededText())}
	if est.anyDiff {
		settings = append(settings, interact.Item(interact.StatusInfo, "The estimate leaves out files moved or renamed since the full backup; a differential stores them again."))
	}
	if freeErr != nil {
		settings = append(settings, interact.Field("Free space", fmt.Sprintf("unknown (%v)", freeErr)))
	} else {
		settings = append(settings, interact.Field("Free space", fsx.FormatBytesBinary(freeBytes)))
	}
	settings = append(settings, interact.Field("Split size", fmt.Sprintf("%d MB", cfg.SplitSizeMB)))
	if len(est.sizes) > 0 && splitSizeBytes > 0 {
		if advisory := partCountAdvisory(maxPartCount); advisory != "" {
			settings = append(settings, interact.Item(interact.StatusWarn, advisory))
		}
	}
	settings = append(settings, interact.Field("Retention", retentionSummary(cfg.RetentionKeep, cfg.Differential.RetentionKeepDifferentials)))
	verifyAfter := "disabled"
	if cfg.VerifyAfterBackup {
		verifyAfter = "enabled"
	}
	settings = append(settings, interact.Field("Verify after backup", verifyAfter))
	excludeInfo := "none"
	if n := len(cfg.Exclude); n > 0 {
		excludeInfo = strings.Join(cfg.Exclude, ", ")
	}
	settings = append(settings, interact.Field("Exclude", excludeInfo))
	unreadable := "abort backup (fail)"
	if cfg.SkipUnreadableFiles() {
		unreadable = "skip and warn (skip)"
	}
	settings = append(settings,
		interact.Field("Unreadable files", unreadable),
		interact.Field("KDF (Argon2id)", fmt.Sprintf("time=%d  memory=%d MB  threads=%d", cfg.Argon2.Time, cfg.Argon2.MemoryMB, cfg.Argon2.Threads)),
		interact.Field("Log level", strings.ToLower(cfg.LogLevel)),
	)

	return interact.Report{Title: "Backup preflight", Sections: []interact.Section{{Rows: directories}, {Rows: settings}}}
}

// keyPlanRows state whether the run reuses the existing keys or creates new
// ones, and what that means for the user.
func keyPlanRows(keys plan.Keys) []interact.Row {
	if keys.Existing != nil {
		return []interact.Row{interact.Field("Keys", "existing keys, "+keys.Existing.Summary())}
	}
	return []interact.Row{
		interact.Field("Keys", "new keys will be created"),
		interact.Item(interact.StatusInfo, keys.NewKeysReason+": new keys will be created and every source directory gets a full backup."),
		interact.Item(interact.StatusInfo, "Passwords, YubiKey registrations, and recovery codes of earlier keys do not open the new backups (they still open older backups)."),
	}
}

// backupPreflightIssues runs the checks that block a backup and returns them
// as report issues, together with the error of the first failed check. A
// differential whose estimate fits, but that would not fit if it stored
// every file again, gets a warning instead of an error.
func backupPreflightIssues(cfg *config.Config, backupDir string, sources []plan.Source, est spaceEstimate) ([]interact.Issue, error) {
	var issues []interact.Issue
	var first error
	targetWarn, targetErr := validateTargetSpaceForBackup(backupDir, est)
	for _, err := range []error{
		validateSourceDirectories(sources),
		targetErr,
		validateBackupPartCount(cfg, sources),
	} {
		if err == nil {
			continue
		}
		if first == nil {
			first = err
		}
		issues = append(issues, interact.Issue{Status: interact.StatusError, Text: strings.TrimPrefix(err.Error(), "Backup preflight failed: ")})
	}
	if targetWarn != "" {
		issues = append(issues, interact.Issue{Status: interact.StatusWarn, Text: targetWarn})
	}
	return issues, first
}

func validateSourceDirectories(sources []plan.Source) error {
	return job.ValidatePreflightItems(
		sources,
		func(src plan.Source) bool { return src.Err != nil },
		"Backup preflight failed: %d source directory(s) are invalid or inaccessible. Remedy: Fix the [ERROR] entries above and start backup again.",
	)
}

// spaceWarning is the warning for a differential that fits by its estimate
// but not if it stored every file again.
func spaceWarning(where string, est spaceEstimate, freeBytes uint64) string {
	return fmt.Sprintf("Enough free space %s for the files changed since the full backup (about %s), but not if the differential stores everything again (up to %s; free: %s). Moved or renamed files count as new. If the space runs out, the backup stops and removes the unfinished backup set.",
		where, fsx.FormatBytesBinary(uint64(est.likely)), fsx.FormatBytesBinary(uint64(est.full)), fsx.FormatBytesBinary(freeBytes))
}

// validateTargetSpaceForBackup checks the free space in the backup
// directory: an error when even the estimate does not fit, a warning when
// only the estimate fits.
func validateTargetSpaceForBackup(backupDir string, est spaceEstimate) (warning string, err error) {
	freeBytes, err := fsx.QueryFreeSpaceBytes(backupDir)
	if err != nil {
		return "", nil
	}
	if fsx.IsSpaceInsufficient(est.likely, freeBytes) {
		return "", fmt.Errorf("Backup preflight failed: %s", fsx.FormatInsufficientBackupSpaceMessage(uint64(est.likely), freeBytes))
	}
	if est.anyDiff && fsx.IsSpaceInsufficient(est.full, freeBytes) {
		return spaceWarning("in the backup directory", est, freeBytes), nil
	}
	return "", nil
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
	partCountWarnThreshold = naming.MaxPartSequence * 9 / 10
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
	if maxPartCount > naming.MaxPartSequence || maxPartCount < partCountWarnThreshold {
		return ""
	}
	return fmt.Sprintf(
		"Largest source is approaching the %d-part limit (≈ %d parts incl. %d%% overhead margin). Remedy: Increase split_size_mb in config.yaml to keep future backups within the limit.",
		naming.MaxPartSequence, maxPartCount, partCountSafetyMarginPercent,
	)
}

// validateBackupPartCount rejects backups that would produce more part files
// than the naming scheme can represent (see naming.MaxPartSequence). Each source
// directory is written as its own sequence of parts starting at 1, so the limit
// is checked per source. The estimate uses the source size and configured split
// size (plus a safety margin for overhead); both are known before the backup starts.
func validateBackupPartCount(cfg *config.Config, sources []plan.Source) error {
	splitSizeBytes := cfg.SplitSizeMB * 1024 * 1024
	if splitSizeBytes <= 0 {
		return nil
	}

	for _, source := range sources {
		if source.Err != nil || source.Skip {
			continue
		}

		size, err := fsx.DirectorySizeBytes(source.Resolved)
		if err != nil {
			// Size could not be determined; the runtime path handles I/O errors.
			continue
		}

		estimatedParts := estimatePartCountWithMargin(size, splitSizeBytes)
		if estimatedParts > naming.MaxPartSequence {
			return fmt.Errorf(
				"Backup preflight failed: %q is approximately %s, which at a split size of %d MB would create about %d part files (incl. %d%% overhead margin) - exceeding the %d-part limit of the backup naming scheme. Remedy: Increase split_size_mb in config.yaml so the backup fits within %d parts, or split the source into smaller backups.",
				source.Resolved,
				fsx.FormatBytesBinary(uint64(size)),
				cfg.SplitSizeMB,
				estimatedParts,
				partCountSafetyMarginPercent,
				naming.MaxPartSequence,
				naming.MaxPartSequence,
			)
		}
	}

	return nil
}

func runnableSourceCount(sources []plan.Source) int {
	count := 0
	for _, source := range sources {
		if source.Err != nil || source.Skip {
			continue
		}
		count++
	}
	return count
}

// spaceEstimate is the space a backup run needs, estimated from the sources
// before the password is asked (the list of files in a full backup is
// encrypted, so the exact changes of a differential are not known yet).
type spaceEstimate struct {
	// full is the size of all files: what the run stores if every
	// differential stored every file again.
	full int64
	// likely counts all files of full backups and, for differentials, the
	// files written or created since their full backup (see
	// archive.MeasureSource).
	likely int64
	// anyDiff is set when a directory gets a differential.
	anyDiff bool
	// sizes are the sizes of all files per measured source, for the part
	// count.
	sizes []int64
	// warnings name sources that could not be measured.
	warnings []string
}

// estimateBackupSpace measures every source that will be backed up, once,
// following the exclude patterns.
func estimateBackupSpace(cfg *config.Config, backupDir string, sources []plan.Source, plans map[string]*plan.Folder) spaceEstimate {
	var est spaceEstimate
	for _, src := range sources {
		if src.Err != nil || src.Skip {
			continue
		}
		name := src.BackupName
		if name == "" {
			name = naming.DirectoryBaseName(src.Resolved)
		}
		var since time.Time
		if folder := plans[name]; folder.IsDiff() {
			since = folder.Base.Created()
			est.anyDiff = true
		}
		m, err := archive.MeasureSource(archive.BuildOptions{SourceDir: src.Resolved, ExcludeDirs: []string{backupDir}, Exclude: cfg.ExcludeMatcher}, since)
		if err != nil {
			est.warnings = append(est.warnings, fmt.Sprintf("%s (%v)", src.Resolved, err))
			continue
		}
		est.full += m.Total
		est.likely += m.Changed
		est.sizes = append(est.sizes, m.Total)
	}
	return est
}

// neededText describes the needed space for the preflight.
func (est spaceEstimate) neededText() string {
	if !est.anyDiff {
		return fsx.FormatBytesBinary(uint64(est.full))
	}
	return fmt.Sprintf("about %s (files changed since the full backup); up to %s if everything is stored again",
		fsx.FormatBytesBinary(uint64(est.likely)), fsx.FormatBytesBinary(uint64(est.full)))
}

// checkSpaceForFullBackups checks the free space again when the user chose
// full backups over the planned differentials: the preflight only required
// the estimate of the changes to fit. Nothing is written before it passes.
func checkSpaceForFullBackups(backupDir string, est spaceEstimate) error {
	if !est.anyDiff {
		return nil
	}
	full := spaceEstimate{full: est.full, likely: est.full}
	if _, err := validateTargetSpaceForBackup(backupDir, full); err != nil {
		return fmt.Errorf("Full backup not started: %s", strings.TrimPrefix(err.Error(), "Backup preflight failed: "))
	}
	return nil
}
