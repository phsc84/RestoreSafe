// Package backup orchestrates the full backup workflow:
//  1. Determine the keys: reuse the current key set or create new keys
//  2. For each source directory: stream TAR → encrypt → split → .enc parts
//     (written as .tmp and renamed once the set is complete)
//  3. Optionally re-read and verify the written sets (verify_after_backup)
//  4. Apply the retention policy and write a log file per backup run
package backup

import (
	"RestoreSafe/internal/catalog"
	"RestoreSafe/internal/container"
	"RestoreSafe/internal/operation"
	"RestoreSafe/internal/security"
	"RestoreSafe/internal/setio"
	"RestoreSafe/internal/ui"
	"RestoreSafe/internal/util"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"
)

// Run executes the full backup workflow, asking u for decisions and credentials.
func Run(u ui.UI, cfg *util.Config, exeDir string) error {
	out := u.Output()
	// Resolve backup directory (may be relative to exe dir).
	backupDir := util.ResolveDir(cfg.BackupDirectory, exeDir)
	if err := os.MkdirAll(backupDir, 0o750); err != nil {
		return fmt.Errorf("Failed to create backup directory: %w. Remedy: Check the path (prefer forward slashes in config.yaml, e.g. C:/Backups) and verify write permissions.", err)
	}

	lock, err := util.AcquireBackupLock(backupDir)
	if err != nil {
		return err
	}
	defer lock.Release()

	sources := resolveBackupSources(cfg.SourceDirectories, exeDir)

	infos, err := catalog.Inventory(backupDir)
	if err != nil {
		return fmt.Errorf("Failed to scan backup directory: %w. Remedy: Check read permissions in the backup directory.", err)
	}

	// Determine backup run identifiers.
	runID, err := newRunID(infos)
	if err != nil {
		return err
	}
	date := util.DateString()

	// Set up logger.
	logPath := util.LogFileName(backupDir, date, runID)
	log, err := util.NewLogger(logPath, cfg.LogLevel, out)
	if err != nil {
		return err
	}
	defer log.Close()

	removeLeftoverTempParts(backupDir, log)

	// Plan local staging to mitigate same-volume read+write contention.
	// Prefer a source that shares the backup volume so the plan correctly detects contention
	// when only some sources are on the same drive as the backup directory.
	stagingSourceDir := ""
	for _, src := range sources {
		if src.Err == nil && !src.Skip {
			if stagingSourceDir == "" {
				stagingSourceDir = src.Resolved
			}
			if util.SameVolume(src.Resolved, backupDir) {
				stagingSourceDir = src.Resolved
				break
			}
		}
	}
	stagingPlan := operation.PlanLocalStaging(stagingSourceDir, backupDir, os.TempDir())
	keys := planKeys(cfg, infos)
	plans := planBackupTypes(cfg, infos, sources, keys, false, time.Now())

	report := backupPreflightReport(cfg, backupDir, sources, stagingPlan, keys, plans, security.CheckYubiKeyConnected)
	issues, err := backupPreflightIssues(cfg, backupDir, sources, stagingPlan)
	report.Issues = issues
	u.ShowReport(report)
	if err != nil {
		return err
	}

	// [F] makes every directory a full backup (offered when a differential is
	// planned); [K] creates new keys (offered when existing keys would be
	// reused), to change the password, replace a lost YubiKey, or get a new
	// recovery code.
	choice, err := u.ConfirmBackupStart(ui.BackupStartOptions{OfferFull: anyDifferential(plans), OfferNewKeys: keys.Existing != nil})
	if err != nil {
		return err
	}
	switch choice {
	case ui.BackupCancel:
		log.InfoLogOnly("Backup cancelled by user before start")
		fmt.Fprintln(out, "Backup cancelled.")
		return nil
	case ui.BackupFull:
		plans = planBackupTypes(cfg, infos, sources, keys, true, time.Now())
	case ui.BackupNewKeys:
		keys = keyPlan{NewKeysReason: "New keys requested"}
		plans = planBackupTypes(cfg, infos, sources, keys, true, time.Now())
	}

	keySet, master, err := obtainKeys(u, cfg, keys, log)
	if err != nil {
		return err
	}
	defer security.ZeroBytes(master)

	return runBackupOperation(out, cfg, log, logPath, backupDir, sources, stagingPlan, date, runID, keySet, master, plans)
}

// newRunID generates a run ID that is not yet used as chain ID or run ID in
// the backup directory, so a chain ID always identifies one full backup.
func newRunID(infos []catalog.SetInfo) (util.BackupID, error) {
	used := make(map[string]bool)
	for _, info := range infos {
		used[string(info.Entry.ChainID)] = true
		if info.Header != nil {
			used[info.Header.RunID] = true
		}
	}
	for attempt := 0; attempt < 100; attempt++ {
		id, err := util.NewBackupID()
		if err != nil {
			return "", err
		}
		if !used[string(id)] {
			return id, nil
		}
	}
	return "", fmt.Errorf("Failed to generate a unique backup ID.")
}

// removeLeftoverTempParts deletes part files an interrupted backup left
// behind with the temporary suffix. It runs under the backup lock, so no other
// backup can be writing them.
func removeLeftoverTempParts(backupDir string, log *util.Logger) {
	names, err := catalog.ListTempParts(backupDir)
	if err != nil {
		log.Warn("Failed to look for leftovers of interrupted backups: %v", err)
		return
	}
	for _, name := range names {
		if err := os.Remove(filepath.Join(backupDir, name)); err != nil {
			log.Warn("Failed to remove leftover of an interrupted backup %s: %v", name, err)
			continue
		}
		log.Info("Removed leftover of an interrupted backup: %s", name)
	}
}

// runBackupOperation performs the backup using an already-unlocked key set.
// It takes no further input from the user, so it can be driven directly in
// tests and automated flows by supplying the key set and master key; out
// receives the summary. plans decides full or differential per directory;
// directories without a plan (or a nil map) get a full backup.
func runBackupOperation(
	out io.Writer,
	cfg *util.Config,
	log *util.Logger,
	logPath, backupDir string,
	sources []backupSource,
	stagingPlan operation.LocalStagingPlan,
	date string,
	runID util.BackupID,
	keySet *container.KeySet,
	master []byte,
	plans map[string]*dirPlan,
) error {
	fmt.Fprintln(out)
	n := runnableSourceCount(sources)
	dirWord := "directories"
	if n == 1 {
		dirWord = "directory"
	}
	log.Info("Backup started - ID: %s, date: %s, %d source %s", string(runID), date, n, dirWord)
	warningCount := 0
	var written []util.BackupEntry
	// retentionHold lists directories whose new backup misses unreadable
	// files; their older backups are kept because they may still have them.
	retentionHold := make(map[string]bool)
	processedDirectories := make([]string, 0)
	directorySourcePaths := make(map[string]string)

	// Determine actual working directory (staging or backup directory).
	staging, err := operation.NewStagingScope(stagingPlan, "restoresafe-backup-stage-*", log)
	if err != nil {
		return err
	}
	if staging.Dir != "" {
		log.InfoLogOnly("Local staging enabled: backup will write to %s before finalizing to %s", filepath.ToSlash(staging.Dir), filepath.ToSlash(backupDir))
	}
	workingDir := staging.ActiveDir(backupDir)
	defer staging.Cleanup()

	// Back up each source directory.
	for _, source := range sources {
		if source.Warning != "" {
			log.Warn("Source directory warning: %s → %s", source.Resolved, source.Warning)
			warningCount++
		}
		if source.Skip {
			continue
		}

		srcAbs := source.Resolved
		directoryName := source.BackupName
		if directoryName == "" {
			directoryName = util.DirectoryBaseName(srcAbs)
		}

		log.Info("Processing source directory: %s", srcAbs)
		log.Debug("Directory name in archive: %s", directoryName)

		entry := util.BackupEntry{DirectoryName: directoryName, ChainID: runID, Date: date}
		var base *setio.Base
		if plan := plans[directoryName]; plan.IsDiff() {
			loaded, err := loadBase(backupDir, plan.Base, keySet, master)
			if err != nil {
				log.Warn("  The full backup %s cannot be used as base (%v). A full backup is created instead.", plan.Base.Entry.String(), err)
				warningCount++
			} else {
				base = loaded
				entry = util.BackupEntry{DirectoryName: directoryName, ChainID: plan.Base.Entry.ChainID, Date: date, DiffNumber: plan.DiffNumber}
				log.Info("  Backup type: differential %03d of chain %s (%s)", plan.DiffNumber, plan.Base.Entry.ChainID, plan.Reason)
			}
		} else if plan != nil {
			log.Info("  Backup type: full (%s)", plan.Reason)
		}
		skipped, err := backupDirectory(srcAbs, entry, runID, base, workingDir, backupDir, keySet, master, cfg, staging.Dir == "", log)
		if err != nil {
			return fmt.Errorf("Backup of %q failed: %w", srcAbs, err)
		}
		if skipped > 0 {
			warningCount++
			retentionHold[directoryName] = true
		}
		written = append(written, entry)
		processedDirectories = append(processedDirectories, directoryName)
		directorySourcePaths[directoryName] = srcAbs
	}

	// Move results from staging to backup directory if needed.
	if staging.Dir != "" {
		if err := moveBackupResults(workingDir, backupDir, processedDirectories, directorySourcePaths, log); err != nil {
			return fmt.Errorf("Failed to move staged backup to backup directory: %w", err)
		}
	}

	// Optionally verify the freshly written sets before pruning old backups.
	verifyFailed := false
	if cfg.VerifyAfterBackup && len(written) > 0 {
		failed := verifyBackupAfterWrite(backupDir, written, master, log)
		if failed > 0 {
			verifyFailed = true
			warningCount += failed
		}
	}

	// Retention is skipped when verification failed so a verified older backup
	// set is never pruned in favour of an unverified new one.
	if verifyFailed {
		log.Warn("Cleanup old data skipped because post-backup verification failed; existing backup sets left untouched.")
	} else if err := applyRetentionPolicy(backupDir, cfg.RetentionKeep, cfg.Differential.RetentionKeepDifferentials, sources, retentionHold, log); err != nil {
		log.Warn("  Cleanup old data failed: %v", err)
		warningCount++
	}

	staging.Cleanup()
	if len(retentionHold) > 0 {
		log.Warn("Backup completed with warnings: some files could not be read and are not in this backup (see the warnings above).")
	} else {
		log.Info("Backup completed successfully")
	}
	if warningCount > 0 {
		fmt.Fprintf(out, "Warnings: %d\n", warningCount)
	}
	fmt.Fprintf(out, "\nLog file: %s\n", logPath)
	return nil
}

// verifyKeptRemedy is appended to each post-backup verification failure so the
// reason and the "files kept / try a manual restore" guidance live on one line.
const verifyKeptRemedy = " The backup files were kept; try a manual restore/verify."

// verifyBackupAfterWrite re-reads the sets just written, decrypts them, and
// checks every file against its manifest hash. It reuses the unlocked master
// key, so it needs no additional password prompt or YubiKey touch. Failures
// are logged as warnings and the backup files are left in place; the number of
// sets that failed is returned so the caller can flag the run and skip
// retention.
func verifyBackupAfterWrite(backupDir string, entries []util.BackupEntry, master []byte, log *util.Logger) int {
	log.Info("Verifying backup integrity")

	failures := 0
	for _, entry := range entries {
		set, err := catalog.OpenSet(backupDir, entry)
		if err != nil {
			log.Warn("  Post-backup verification failed for [%s]: %v.%s", entry.DirectoryName, err, verifyKeptRemedy)
			failures++
			continue
		}
		// A differential's own data is checked; its full backup was
		// verified when it was written.
		m, err := operation.VerifyOwnData(set, master, log)
		parts := len(set.Paths)
		set.Close() //nolint:errcheck
		if err != nil {
			log.Warn("  Post-backup verification failed for [%s]: %v.%s", entry.DirectoryName, err, verifyKeptRemedy)
			failures++
			continue
		}
		log.Info("  Verified: %d part file(s), %d file(s) - [%s] successfully verified", parts, m.Footer.Files, entry.DirectoryName)
	}

	if failures == 0 {
		log.Info("  Post-backup verification successful")
	}
	return failures
}

// loadBase opens the full backup a differential is based on and decrypts and
// validates its manifest. The base must use the unlocked key set.
func loadBase(backupDir string, info *catalog.SetInfo, keySet *container.KeySet, master []byte) (*setio.Base, error) {
	if info.Header.KeySet.ID != keySet.ID {
		return nil, fmt.Errorf("it uses different keys")
	}
	set, err := catalog.OpenSet(backupDir, info.Entry)
	if err != nil {
		return nil, err
	}
	defer set.Close()
	keys, err := set.SectionKeys(master)
	if err != nil {
		return nil, err
	}
	defer keys.Zero()
	m, sum, err := set.ReadManifest(keys)
	if err != nil {
		return nil, err
	}
	return &setio.Base{Header: set.Header, Manifest: m, ManifestSHA256: sum}, nil
}

// backupDirectory writes one backup set of srcDir into workingDir: a
// differential of base, or a full backup when base is nil. It returns the
// number of files and directories skipped as unreadable.
func backupDirectory(
	srcDir string,
	entry util.BackupEntry,
	runID util.BackupID,
	base *setio.Base,
	workingDir, backupDir string,
	keySet *container.KeySet,
	master []byte,
	cfg *util.Config,
	syncParts bool,
	log *util.Logger,
) (int, error) {
	var inBytes, outBytes, outWriteCalls atomic.Int64
	var progressLog *util.Logger
	if cfg.IODiagnostics {
		progressLog = log
	}
	stopProgress := operation.StartProgressTracking(progressLog, entry.DirectoryName, "encrypted", &inBytes, &outBytes, &outWriteCalls)
	defer stopProgress()

	log.Debug("Starting TAR creation and encryption for: %s", srcDir)
	res, err := setio.WriteSet(setio.SetParams{
		SourceDir:      srcDir,
		ExcludeDirs:    []string{backupDir, workingDir},
		OutputDir:      workingDir,
		Entry:          entry,
		Base:           base,
		RunID:          runID,
		KeySet:         *keySet,
		Master:         master,
		SplitSizeBytes: cfg.SplitSizeMB * 1024 * 1024,
		SyncParts:      syncParts,
		Exclude:        cfg.ExcludeMatcher,
		SkipUnreadable: cfg.SkipUnreadableFiles(),
		OnSkip: func(rel, reason string, stale bool) {
			if stale {
				log.Warn("  Older version kept (could not be read): %s → %s", rel, reason)
				return
			}
			log.Warn("  Skipped (could not be read): %s → %s", rel, reason)
		},
		OnPartOpened: func(seq int, path string) {
			log.Info("  Part %03d: %s", seq, filepath.Base(path))
		},
		Counters: setio.Counters{In: &inBytes, Out: &outBytes, Calls: &outWriteCalls},
	})
	if err != nil {
		return 0, err
	}

	logPartSummary(res.Parts, entry.DirectoryName, cfg.IODiagnostics, &outBytes, &outWriteCalls, log)
	log.Info("  Backed up: %d file(s), %d directory(s), %s", res.Manifest.Files, res.Manifest.Dirs, util.FormatBytesBinary(uint64(res.Manifest.TotalBytes)))
	if base != nil {
		log.Info("  Differential: %d new or changed file(s) stored (%s), %d unchanged file(s) in the full backup", res.Stats.Stored, util.FormatBytesBinary(uint64(res.Manifest.DataBytes)), res.Stats.Unchanged)
	}
	if n := res.Stats.Excluded; n > 0 {
		log.Info("  Excluded by pattern: %d file(s)/directory(s)", n)
	}
	if n := res.Stats.Vanished; n > 0 {
		log.Info("  Deleted while the backup was running: %d file(s)/directory(s) (not in this backup)", n)
	}
	if n := res.Stats.Skipped; n > 0 {
		log.Warn("  [%s] %d file(s)/directory(s) could not be read and are not in this backup.", entry.DirectoryName, n)
	}
	if n := res.Stats.Stale; n > 0 {
		log.Warn("  [%s] %d file(s) could not be read; this backup contains their older version from the full backup.", entry.DirectoryName, n)
	}
	return res.Stats.Skipped + res.Stats.Stale, nil
}
