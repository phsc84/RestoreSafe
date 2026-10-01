// Package backup orchestrates the full backup workflow:
//  1. Determine the keys: reuse the current key set or create new keys
//  2. For each source directory: stream TAR → encrypt → split → .enc parts
//     (written as .tmp and renamed once the set is complete)
//  3. Optionally re-read and verify the written sets (verify_after_backup)
//  4. Apply the retention policy and write a log file per backup run
package backup

import (
	"RestoreSafe/internal/config"
	"RestoreSafe/internal/format/archive"
	"RestoreSafe/internal/format/catalog"
	"RestoreSafe/internal/format/container"
	"RestoreSafe/internal/format/naming"
	"RestoreSafe/internal/format/setwriter"
	"RestoreSafe/internal/fsx"
	"RestoreSafe/internal/logging"
	"RestoreSafe/internal/security/cryptox"
	"RestoreSafe/internal/workflow/interact"
	"RestoreSafe/internal/workflow/job"
	"RestoreSafe/internal/workflow/plan"
	"RestoreSafe/internal/workflow/restorepoint"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
)

// Run executes the full backup workflow, asking u for decisions and credentials
// and reporting progress to it. Cancelling ctx stops the backup: sets
// completed before are kept, the interrupted one is removed, and the returned
// error matches context.Canceled.
func Run(ctx context.Context, u interact.UI, cfg *config.Config, exeDir string) error {
	out := u.Output()
	// Resolve backup directory (may be relative to exe dir).
	backupDir := fsx.ResolveDir(cfg.BackupDirectory, exeDir)
	if err := os.MkdirAll(backupDir, 0o750); err != nil {
		return fmt.Errorf("Failed to create backup directory: %w. Remedy: Check the path (prefer forward slashes in config.yaml, e.g. C:/Backups) and verify write permissions.", err)
	}

	lock, err := fsx.AcquireBackupLock(backupDir)
	if err != nil {
		return err
	}
	defer lock.Release()

	sources := plan.ResolveSources(cfg.SourceDirectories, exeDir)

	infos, err := catalog.Inventory(backupDir)
	if err != nil {
		return fmt.Errorf("Failed to scan backup directory: %w. Remedy: Check read permissions in the backup directory.", err)
	}

	// Determine backup run identifiers.
	runID, err := newRunID(infos)
	if err != nil {
		return err
	}
	date := naming.DateString()

	// Set up logger.
	logPath := naming.LogFileName(backupDir, date, runID)
	log, err := logging.NewLogger(logPath, cfg.LogLevel, out)
	if err != nil {
		return err
	}
	defer log.Close()

	removeLeftoverTempParts(backupDir, log)

	keys, plans, start, err := choosePlan(u, cfg, backupDir, sources, infos)
	if err != nil {
		return err
	}
	if !start {
		log.InfoLogOnly("Backup cancelled by user before start")
		fmt.Fprintln(out, "Backup cancelled.")
		return nil
	}

	keySet, master, err := obtainKeys(u, cfg, keys, log)
	if err != nil {
		return err
	}
	defer cryptox.ZeroBytes(master)

	return runBackupOperation(ctx, u, cfg, log, logPath, backupDir, sources, date, runID, keySet, master, plans)
}

// newRunID generates a run ID that is not yet used as chain ID or run ID in
// the backup directory, so a chain ID always identifies one full backup.
func newRunID(infos []catalog.SetInfo) (naming.BackupID, error) {
	used := make(map[string]bool)
	for _, info := range infos {
		used[string(info.Entry.ChainID)] = true
		if info.Header != nil {
			used[info.Header.RunID] = true
		}
	}
	for attempt := 0; attempt < 100; attempt++ {
		id, err := naming.NewBackupID()
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
func removeLeftoverTempParts(backupDir string, log *logging.Logger) {
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
// tests and automated flows by supplying the key set and master key; u
// receives the progress and the summary. plans decides full or differential
// per directory; directories without a plan (or a nil map) get a full backup.
func runBackupOperation(
	ctx context.Context,
	u interact.UI,
	cfg *config.Config,
	log *logging.Logger,
	logPath, backupDir string,
	sources []plan.Source,
	date string,
	runID naming.BackupID,
	keySet *container.KeySet,
	master []byte,
	plans map[string]*plan.Folder,
) error {
	out := u.Output()
	fmt.Fprintln(out)
	n := runnableSourceCount(sources)
	dirWord := "directories"
	if n == 1 {
		dirWord = "directory"
	}
	log.Info("Backup started - ID: %s, date: %s, %d source %s", string(runID), date, n, dirWord)
	warningCount := 0
	var written []naming.BackupEntry
	// retentionHold lists directories whose new backup misses unreadable
	// files; their older backups are kept because they may still have them.
	retentionHold := make(map[string]bool)

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
			directoryName = naming.DirectoryBaseName(srcAbs)
		}

		log.Info("Processing source directory: %s", srcAbs)
		log.Debug("Directory name in archive: %s", directoryName)

		entry := naming.BackupEntry{DirectoryName: directoryName, ChainID: runID, Date: date}
		var base *setwriter.Base
		if folder := plans[directoryName]; folder.IsDiff() {
			loaded, err := loadBase(backupDir, folder.Base, keySet, master)
			if err != nil {
				log.Warn("  The full backup %s cannot be used as base (%v). A full backup is created instead.", folder.Base.Entry.String(), err)
				warningCount++
			} else {
				base = loaded
				entry = naming.BackupEntry{DirectoryName: directoryName, ChainID: folder.Base.Entry.ChainID, Date: date, DiffNumber: folder.DiffNumber}
				log.Info("  Backup type: differential %03d of chain %s (%s)", folder.DiffNumber, folder.Base.Entry.ChainID, folder.Reason)
			}
		} else if folder != nil {
			log.Info("  Backup type: full (%s)", folder.Reason)
		}
		skipped, err := backupDirectory(ctx, u, srcAbs, entry, runID, base, backupDir, keySet, master, cfg, log)
		if err != nil {
			return backupFailed(ctx, log, fmt.Errorf("Backup of %q failed: %w", srcAbs, err))
		}
		if skipped > 0 {
			warningCount++
			retentionHold[directoryName] = true
		}
		written = append(written, entry)
	}

	// Optionally verify the freshly written sets before pruning old backups.
	verifyFailed := false
	if cfg.VerifyAfterBackup && len(written) > 0 {
		failed, err := verifyBackupAfterWrite(ctx, u, backupDir, written, master, log)
		if err != nil {
			return backupFailed(ctx, log, err)
		}
		if failed > 0 {
			verifyFailed = true
			warningCount += failed
		}
	}

	// A cancelled run leaves the older backups alone.
	if err := ctx.Err(); err != nil {
		return backupFailed(ctx, log, err)
	}

	// Retention is skipped when verification failed so a verified older backup
	// set is never pruned in favour of an unverified new one.
	if verifyFailed {
		log.Warn("Cleanup old data skipped because post-backup verification failed; existing backup sets left untouched.")
	} else if err := applyRetentionPolicy(backupDir, cfg.RetentionKeep, cfg.Differential.RetentionKeepDifferentials, sources, retentionHold, log); err != nil {
		log.Warn("  Cleanup old data failed: %v", err)
		warningCount++
	}

	if len(retentionHold) > 0 {
		log.Warn("Backup completed with warnings: some files could not be read and are not in this backup (see the warnings above).")
	} else {
		log.Info("Backup completed successfully")
	}
	u.ShowResult(interact.Result{Warnings: warningCount, LogPath: logPath})
	return nil
}

// backupFailed returns err, or, when the user cancelled the backup, logs what
// was kept and returns the cancellation.
func backupFailed(ctx context.Context, log *logging.Logger, err error) error {
	if ctx.Err() == nil {
		return err
	}
	log.Warn("Backup cancelled. Backup sets completed before cancelling were kept, an interrupted one was removed, and old backups were not cleaned up.")
	return job.Cancelled("Backup")
}

// verifyKeptRemedy is appended to each post-backup verification failure so the
// reason and the "files kept / try a manual restore" guidance live on one line.
const verifyKeptRemedy = " The backup files were kept; try a manual restore/verify."

// verifyBackupAfterWrite re-reads the sets just written, decrypts them, and
// checks every file against its manifest hash. It reuses the unlocked master
// key, so it needs no additional password prompt or YubiKey touch. Failures
// are logged as warnings and the backup files are left in place; the number of
// sets that failed is returned so the caller can flag the run and skip
// retention. When ctx is cancelled, it stops with the context's error.
func verifyBackupAfterWrite(ctx context.Context, rep interact.ProgressReporter, backupDir string, entries []naming.BackupEntry, master []byte, log *logging.Logger) (int, error) {
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
		var done atomic.Int64
		stop := job.TrackProgress(rep, interact.Progress{Step: "Verifying", Item: entry.DirectoryName, Total: restorepoint.SectionSize(set, nil)}, &done)
		m, err := restorepoint.VerifyOwnData(ctx, set, master, log, &done)
		stop()
		parts := len(set.Paths)
		set.Close() //nolint:errcheck
		if ctxErr := ctx.Err(); ctxErr != nil {
			return failures, ctxErr
		}
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
	return failures, nil
}

// loadBase opens the full backup a differential is based on and decrypts and
// validates its manifest. The base must use the unlocked key set.
func loadBase(backupDir string, info *catalog.SetInfo, keySet *container.KeySet, master []byte) (*setwriter.Base, error) {
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
	return &setwriter.Base{Header: set.Header, Manifest: m, ManifestSHA256: sum}, nil
}

// backupDirectory writes one backup set of srcDir into backupDir: a
// differential of base, or a full backup when base is nil. It returns the
// number of files and directories skipped as unreadable.
func backupDirectory(
	ctx context.Context,
	rep interact.ProgressReporter,
	srcDir string,
	entry naming.BackupEntry,
	runID naming.BackupID,
	base *setwriter.Base,
	backupDir string,
	keySet *container.KeySet,
	master []byte,
	cfg *config.Config,
	log *logging.Logger,
) (int, error) {
	var inBytes, outBytes, outWriteCalls atomic.Int64
	var progressLog *logging.Logger
	if cfg.IODiagnostics {
		progressLog = log
	}
	stopProgress := job.StartProgressTracking(progressLog, entry.DirectoryName, "encrypted", &inBytes, &outBytes, &outWriteCalls)
	defer stopProgress()

	excludeDirs := []string{backupDir}
	var done atomic.Int64
	var total int64
	if rep != nil {
		total = archive.SourceSize(archive.BuildOptions{SourceDir: srcDir, ExcludeDirs: excludeDirs, Exclude: cfg.ExcludeMatcher})
	}
	stopReport := job.TrackProgress(rep, interact.Progress{Step: "Backing up", Item: entry.DirectoryName, Total: total}, &done)
	defer stopReport()

	log.Debug("Starting TAR creation and encryption for: %s", srcDir)
	res, err := setwriter.Write(setwriter.Params{
		SourceDir:      srcDir,
		ExcludeDirs:    excludeDirs,
		OutputDir:      backupDir,
		Entry:          entry,
		Base:           base,
		RunID:          runID,
		KeySet:         *keySet,
		Master:         master,
		SplitSizeBytes: cfg.SplitSizeMB * 1024 * 1024,
		SyncParts:      true,
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
		Counters: setwriter.Counters{In: &inBytes, Out: &outBytes, Calls: &outWriteCalls},
		Context:  ctx,
		Progress: &done,
	})
	if err != nil {
		return 0, err
	}

	logPartSummary(res.Parts, entry.DirectoryName, cfg.IODiagnostics, &outBytes, &outWriteCalls, log)
	log.Info("  Backed up: %d file(s), %d directory(s), %s", res.Manifest.Files, res.Manifest.Dirs, fsx.FormatBytesBinary(uint64(res.Manifest.TotalBytes)))
	if base != nil {
		log.Info("  Differential: %d new or changed file(s) stored (%s), %d unchanged file(s) in the full backup", res.Stats.Stored, fsx.FormatBytesBinary(uint64(res.Manifest.DataBytes)), res.Stats.Unchanged)
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
