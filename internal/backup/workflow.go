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
	"RestoreSafe/internal/util"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
)

// Run executes the full backup workflow.
func Run(cfg *util.Config, exeDir string) error {
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
	log, err := util.NewLogger(logPath, cfg.LogLevel)
	if err != nil {
		return err
	}
	defer log.Close()

	removeLeftoverTempParts(backupDir, log)

	if err := validateSourceDirectories(sources); err != nil {
		return err
	}

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

	printBackupPreflightWithYubiKeyCheck(os.Stdout, cfg, backupDir, sources, stagingPlan, keys, security.CheckYubiKeyAvailability, security.CheckYubiKeyConnected)
	if err := validateTargetSpaceForBackup(backupDir, sources); err != nil {
		if strings.Contains(err.Error(), "Insufficient free space for backup:") {
			fmt.Println()
			fmt.Printf("[ERROR] %s\n", strings.TrimPrefix(err.Error(), "Backup preflight failed: "))
		}
		return err
	}
	if err := validateStagingSpaceForBackup(stagingPlan, sources); err != nil {
		if strings.Contains(err.Error(), "Insufficient free space in temp directory") {
			fmt.Println()
			fmt.Printf("[ERROR] %s\n", strings.TrimPrefix(err.Error(), "Backup preflight failed: "))
		}
		return err
	}
	if err := validateBackupPartCount(cfg, sources); err != nil {
		fmt.Println()
		fmt.Printf("[ERROR] %s\n", strings.TrimPrefix(err.Error(), "Backup preflight failed: "))
		return err
	}

	confirmed, keys, err := promptBackupStart(keys)
	if err != nil {
		return err
	}
	if !confirmed {
		log.InfoLogOnly("Backup cancelled by user before start")
		fmt.Println("Backup cancelled.")
		return nil
	}

	keySet, master, err := obtainKeys(cfg, keys, log)
	if err != nil {
		return err
	}
	defer security.ZeroBytes(master)

	return runBackupOperation(cfg, log, logPath, backupDir, sources, stagingPlan, date, runID, keySet, master)
}

// promptBackupStart asks whether to start the backup. When existing keys
// would be reused, the user can choose [K] to create new keys instead (to
// change the password, replace a lost YubiKey, or get a new recovery code).
// It returns the key plan to use.
func promptBackupStart(keys keyPlan) (bool, keyPlan, error) {
	if keys.Existing == nil {
		ok, err := operation.PromptStartAction("backup")
		return ok, keys, err
	}
	for {
		fmt.Println()
		answer, err := readLineFn("Start backup now? [Y] yes / [K] new keys + full backup / [N] cancel: ")
		fmt.Println()
		if err != nil {
			return false, keys, err
		}
		switch strings.ToLower(strings.TrimSpace(answer)) {
		case "", "y", "yes":
			return true, keys, nil
		case "k":
			fmt.Println("New keys will be created. Passwords, YubiKey registrations, and recovery codes of the current keys will not open the new backups (they still open older backups).")
			return true, keyPlan{NewKeysReason: "New keys requested"}, nil
		case "n", "no":
			return false, keys, nil
		default:
			fmt.Println("Please enter y (yes), k (new keys), or n (no).")
		}
	}
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
// tests and automated flows by supplying the key set and master key.
func runBackupOperation(
	cfg *util.Config,
	log *util.Logger,
	logPath, backupDir string,
	sources []backupSource,
	stagingPlan operation.LocalStagingPlan,
	date string,
	runID util.BackupID,
	keySet *container.KeySet,
	master []byte,
) error {
	fmt.Println()
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
		skipped, err := backupDirectory(srcAbs, entry, runID, workingDir, backupDir, keySet, master, cfg, staging.Dir == "", log)
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
	} else if err := applyRetentionPolicy(backupDir, cfg.RetentionKeep, sources, retentionHold, log); err != nil {
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
		fmt.Printf("Warnings: %d\n", warningCount)
	}
	fmt.Printf("\nLog file: %s\n", logPath)
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
		m, err := operation.ProcessRestorePoint(set, master, "", true, log)
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

// backupDirectory writes one full backup set of srcDir into workingDir and
// returns the number of files and directories skipped as unreadable.
func backupDirectory(
	srcDir string,
	entry util.BackupEntry,
	runID util.BackupID,
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
	res, err := setio.WriteFullSet(setio.FullSetParams{
		SourceDir:      srcDir,
		ExcludeDirs:    []string{backupDir, workingDir},
		OutputDir:      workingDir,
		Entry:          entry,
		RunID:          runID,
		KeySet:         *keySet,
		Master:         master,
		SplitSizeBytes: cfg.SplitSizeMB * 1024 * 1024,
		SyncParts:      syncParts,
		Exclude:        cfg.ExcludeMatcher,
		SkipUnreadable: cfg.SkipUnreadableFiles(),
		OnSkip: func(rel, reason string) {
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
	if n := res.Stats.Excluded; n > 0 {
		log.Info("  Excluded by pattern: %d file(s)/directory(s)", n)
	}
	if n := res.Stats.Vanished; n > 0 {
		log.Info("  Deleted while the backup was running: %d file(s)/directory(s) (not in this backup)", n)
	}
	if n := res.Stats.Skipped; n > 0 {
		log.Warn("  [%s] %d file(s)/directory(s) could not be read and are not in this backup.", entry.DirectoryName, n)
	}
	return res.Stats.Skipped, nil
}
