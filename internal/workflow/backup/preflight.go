package backup

import (
	"RestoreSafe/internal/config"
	"RestoreSafe/internal/format/archive"
	"RestoreSafe/internal/format/catalog"
	"RestoreSafe/internal/format/naming"
	"RestoreSafe/internal/fsx"
	"RestoreSafe/internal/problem"
	"RestoreSafe/internal/workflow/interact"
	"RestoreSafe/internal/workflow/job"
	"RestoreSafe/internal/workflow/plan"
	"errors"
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
// as report issues, together with the error of the first failed check.
// partErr is the result of validateBackupPartCount, which does not depend on
// the plan. A differential whose estimate fits, but that would not fit if it
// stored every file again, gets a warning instead of an error.
func backupPreflightIssues(backupDir string, sources []plan.Source, est spaceEstimate, partErr error) ([]interact.Issue, error) {
	var issues []interact.Issue
	var first error
	targetWarn, targetErr := validateTargetSpaceForBackup(backupDir, est)
	for _, check := range []struct {
		err  error
		code interact.Code
	}{
		{validateSourceDirectories(sources), sourceProblemCode(sources)},
		{targetErr, interact.CodeSpaceInsufficient},
		{partErr, interact.CodePartLimit},
	} {
		if check.err == nil {
			continue
		}
		if first == nil {
			first = check.err
		}
		issue := interact.IssueOf(interact.StatusError, check.code, check.err)
		issue.Text = strings.TrimPrefix(issue.Text, "Backup preflight failed: ")
		issues = append(issues, issue)
	}
	if targetWarn != "" {
		issues = append(issues, interact.IssueOf(interact.StatusWarn, interact.CodeSpaceEstimateOnly, errors.New(targetWarn)))
	}
	return issues, first
}

// sourceProblemCode is CodeSourceMissing when a source directory does not
// exist, CodeSourceInvalid for any other problem with the sources.
func sourceProblemCode(sources []plan.Source) interact.Code {
	for _, src := range sources {
		if src.Err != nil && job.SourceProblemCode(src.Err) == interact.CodeSourceMissing {
			return interact.CodeSourceMissing
		}
	}
	return interact.CodeSourceInvalid
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
			return problem.Errorf("Backup preflight failed: %q is approximately %s, which at a split size of %d MB would create about %d part files (incl. %d%% overhead margin) - exceeding the %d-part limit of the backup naming scheme.", source.Resolved, fsx.FormatBytesBinary(uint64(size)), cfg.SplitSizeMB, estimatedParts, partCountSafetyMarginPercent, naming.MaxPartSequence).WithRemedy(fmt.Sprintf("Increase split_size_mb in config.yaml so the backup fits within %d parts, or split the source into smaller backups.", naming.MaxPartSequence))
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
	// folders are full and likely per backup name.
	folders map[string]folderSize
	// warnings name sources that could not be measured.
	warnings []string
}

// folderSize is the measured size of one source directory.
type folderSize struct{ full, likely int64 }

// sourceSizes are the sizes of the source directories, measured once and
// reused for every plan the user looks at.
type sourceSizes struct {
	// names are the measured backup names, in source order.
	names []string
	// measures hold the size of all files and of those changed since the
	// base of the differential planned when they were measured.
	measures map[string]archive.SourceMeasure
	// diffs are the backup names whose Changed is relative to a base.
	diffs    map[string]bool
	warnings []string
}

// measureSources measures every source that will be backed up, once,
// following the exclude patterns. For a directory that gets a differential
// in plans, it also counts the files changed since the full backup.
func measureSources(cfg *config.Config, backupDir string, sources []plan.Source, plans map[string]*plan.Folder) sourceSizes {
	sizes := sourceSizes{measures: make(map[string]archive.SourceMeasure), diffs: make(map[string]bool)}
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
			sizes.diffs[name] = true
		}
		m, err := archive.MeasureSource(archive.BuildOptions{SourceDir: src.Resolved, ExcludeDirs: []string{backupDir}, Exclude: cfg.ExcludeMatcher}, since)
		if err != nil {
			sizes.warnings = append(sizes.warnings, fmt.Sprintf("%s (%v)", src.Resolved, err))
			continue
		}
		sizes.names = append(sizes.names, name)
		sizes.measures[name] = m
	}
	return sizes
}

// estimate returns the space needed by plans: a differential is expected to
// store its changed files, a full backup all files.
func (s sourceSizes) estimate(plans map[string]*plan.Folder) spaceEstimate {
	est := spaceEstimate{folders: make(map[string]folderSize), warnings: s.warnings}
	for _, name := range s.names {
		m := s.measures[name]
		size := folderSize{full: m.Total, likely: m.Total}
		if plans[name].IsDiff() && s.diffs[name] {
			size.likely = m.Changed
			est.anyDiff = true
		}
		est.full += size.full
		est.likely += size.likely
		est.sizes = append(est.sizes, m.Total)
		est.folders[name] = size
	}
	return est
}

// estimateBackupSpace measures the sources and estimates the space plans
// need.
func estimateBackupSpace(cfg *config.Config, backupDir string, sources []plan.Source, plans map[string]*plan.Folder) spaceEstimate {
	return measureSources(cfg, backupDir, sources, plans).estimate(plans)
}

// neededText describes the needed space for the preflight.
func (est spaceEstimate) neededText() string {
	if !est.anyDiff {
		return fsx.FormatBytesBinary(uint64(est.full))
	}
	return fmt.Sprintf("about %s (files changed since the full backup); up to %s if everything is stored again",
		fsx.FormatBytesBinary(uint64(est.likely)), fsx.FormatBytesBinary(uint64(est.full)))
}

// backupPlan describes the plan for the user: what happens to each source
// directory, the space, the keys, and what retention removes afterwards. It
// uses the values the details report was built from.
func backupPlan(cfg *config.Config, backupDir string, sources []plan.Source, keys plan.Keys, folders map[string]*plan.Folder, est spaceEstimate, removes []catalog.SetInfo, fullRequested bool, details interact.Report) interact.BackupPlan {
	p := interact.BackupPlan{
		BackupDir:     backupDir,
		NeededBytes:   est.likely,
		AllBytes:      est.full,
		FreeBytes:     -1,
		Keys:          keyPlanFor(cfg, keys),
		VerifyAfter:   cfg.VerifyAfterBackup,
		Removes:       removes,
		FullRequested: fullRequested,
		Issues:        details.Issues,
		Details:       details,
	}
	if free, err := fsx.QueryFreeSpaceBytes(backupDir); err == nil {
		p.FreeBytes = int64(free)
	}
	for _, src := range sources {
		fp := interact.FolderPlan{Name: src.BackupName, Path: src.Resolved, Skipped: src.Skip, Warning: src.Warning}
		if src.Err != nil {
			fp.Problem = src.Err.Error()
		} else if folder := folders[src.BackupName]; folder != nil && !src.Skip {
			fp.Reason = folder.Reason
			if folder.IsDiff() {
				fp.Differential = true
				fp.DiffNumber = folder.DiffNumber
				fp.Base = folder.Base.Entry
				fp.BaseCreated = folder.Base.Created()
			}
			size := est.folders[src.BackupName]
			fp.EstimatedBytes, fp.AllBytes = size.likely, size.full
		}
		p.Folders = append(p.Folders, fp)
	}
	return p
}

// keyPlanFor says which keys lock the new backups and which prompts unlock
// or create them.
func keyPlanFor(cfg *config.Config, keys plan.Keys) interact.KeyPlan {
	if ks := keys.Existing; ks != nil {
		mode := ks.AuthMode
		kp := interact.KeyPlan{Created: ks.Created(), Summary: ks.Summary(), Password: mode != config.AuthModeYubiKey}
		if mode != config.AuthModePassword {
			kp.YubiKeys = 1
		}
		return kp
	}
	kp := interact.KeyPlan{New: true, NewKeysReason: keys.NewKeysReason, Password: !cfg.IsYubiKeyOnly(), RecoveryCode: cfg.RecoveryCode}
	if cfg.UseYubiKey() {
		kp.YubiKeys = 1
		if cfg.YubiKeySpare {
			kp.YubiKeys = 2
		}
	}
	return kp
}
