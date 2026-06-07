// Package backup orchestrates the full backup workflow:
//  1. Prompt for password (and optionally YubiKey 2FA)
//  2. For each source directory: stream TAR → split → encrypt → write .enc parts
//  3. Optionally re-read and verify the written parts (verify_after_backup)
//  4. Write a log file per backup run
package backup

import (
	"RestoreSafe/internal/catalog"
	"RestoreSafe/internal/operation"
	"RestoreSafe/internal/security"
	"RestoreSafe/internal/util"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
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

	// Determine backup run identifiers.
	id, err := util.NewBackupID()
	if err != nil {
		return err
	}
	date := util.DateString()

	// Set up logger.
	logPath := util.LogFileName(backupDir, date, id)
	log, err := util.NewLogger(logPath, cfg.LogLevel)
	if err != nil {
		return err
	}
	defer log.Close()

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

	printBackupPreflightWithYubiKeyCheck(os.Stdout, cfg, backupDir, sources, stagingPlan, security.CheckYubiKeyAvailability, security.CheckYubiKeyConnected)
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

	confirmed, err := operation.PromptStartAction("backup")
	if err != nil {
		return err
	}
	if !confirmed {
		log.InfoLogOnly("Backup cancelled by user before start")
		fmt.Println("Backup cancelled.")
		return nil
	}

	// Collect encryption credential(s), then run the backup with them as inputs.
	password, challengeJSON, err := collectBackupCredentials(cfg, log)
	if err != nil {
		return err
	}
	defer func() { security.ZeroBytes(password) }()

	return runBackupOperation(cfg, log, logPath, backupDir, sources, stagingPlan, date, id, password, challengeJSON)
}

// collectBackupCredentials gathers the encryption credential(s) for a backup run:
// the password (unless YubiKey-only) and, when a YubiKey factor is enabled, the
// combined key material plus the FIDO2 challenge JSON to persist alongside the parts.
// On every error path it zeroes any password material it has read.
func collectBackupCredentials(cfg *util.Config, log *util.Logger) (password []byte, challengeJSON string, err error) {
	if cfg.IsYubiKeyOnly() {
		fmt.Println("YubiKey-only mode: no password required.")
		password = []byte{}
	} else {
		password, err = security.ReadPasswordConfirmedWithPrompts("Enter backup password: ", "Re-enter backup password: ")
		if err != nil {
			return nil, "", err
		}
	}

	// Optional YubiKey factor (2FA or sole factor in yubikey mode).
	if !cfg.UseYubiKey() {
		return password, "", nil
	}

	if err := security.CheckYubiKeyConnected(); err != nil {
		security.ZeroBytes(password)
		return nil, "", security.ErrYubiKeyRequired
	}
	fmt.Println("YubiKey interaction:")
	fmt.Println("  1. Windows first asks for your YubiKey PIN to register the backup credential.")
	fmt.Println("  2. Windows asks again for your YubiKey PIN to derive the encryption key.")
	combined, chal, err := security.CombineWithPassword(password, cfg.IsYubiKeyOnly())
	security.ZeroBytes(password)
	if err != nil {
		return nil, "", fmt.Errorf("YubiKey authentication failed: %w", err)
	}
	if cfg.IsYubiKeyOnly() {
		log.InfoLogOnly("YubiKey-only authentication successful.")
	} else {
		log.InfoLogOnly("YubiKey-2FA successful.")
	}
	return combined, chal, nil
}

// runBackupOperation performs the backup using already-collected credentials. It
// takes no further input from the user, so it can be driven directly in tests and
// automated flows by supplying password (and challengeJSON for YubiKey runs).
func runBackupOperation(
	cfg *util.Config,
	log *util.Logger,
	logPath, backupDir string,
	sources []backupSource,
	stagingPlan operation.LocalStagingPlan,
	date string,
	id util.BackupID,
	password []byte,
	challengeJSON string,
) error {
	fmt.Println()
	n := runnableSourceCount(sources)
	dirWord := "directories"
	if n == 1 {
		dirWord = "directory"
	}
	log.Info("Backup started - ID: %s, date: %s, %d source %s", string(id), date, n, dirWord)
	warningCount := 0
	totalPartsCreated := 0
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

		argon2Params := security.Argon2Params{
			Time:     uint32(cfg.Argon2.Time),
			MemoryKB: uint32(cfg.Argon2.MemoryMB) * 1024,
			Threads:  uint8(cfg.Argon2.Threads),
		}
		partCount, err := backupDirectory(srcAbs, directoryName, workingDir, date, id, password, argon2Params, cfg, staging.Dir == "", log)
		if err != nil {
			return fmt.Errorf("Backup of %q failed: %w", srcAbs, err)
		}
		totalPartsCreated += partCount
		processedDirectories = append(processedDirectories, directoryName)
		directorySourcePaths[directoryName] = srcAbs

		// Write FIDO2 challenge file if needed.
		if cfg.UseYubiKey() && challengeJSON != "" {
			challengePath := util.ChallengeFileName(workingDir, directoryName, date, id)
			if err := os.WriteFile(challengePath, []byte(challengeJSON), 0o600); err != nil {
				return fmt.Errorf("Failed to write challenge file: %w. Remedy: Check write permissions in the backup directory; for YubiKey backups, the .challenge file must be in the same directory as the .enc files.", err)
			}
			log.Debug("Challenge file written: %s", challengePath)
		}
	}

	// Move results from staging to backup directory if needed.
	if staging.Dir != "" {
		if err := moveBackupResults(workingDir, backupDir, processedDirectories, directorySourcePaths, log); err != nil {
			return fmt.Errorf("Failed to move staged backup to backup directory: %w", err)
		}
	}

	// Optionally verify the freshly written parts before pruning old backups.
	verifyFailed := false
	if cfg.VerifyAfterBackup && len(processedDirectories) > 0 {
		failed := verifyBackupAfterWrite(backupDir, date, id, processedDirectories, password, log)
		if failed > 0 {
			verifyFailed = true
			warningCount += failed
		}
	}

	// Retention is skipped when verification failed so a verified older backup
	// set is never pruned in favour of an unverified new one.
	if verifyFailed {
		log.Warn("Cleanup old data skipped because post-backup verification failed; existing backup sets left untouched.")
	} else if err := applyRetentionPolicy(backupDir, cfg.RetentionKeep, sources, log); err != nil {
		log.Warn("  Cleanup old data failed: %v", err)
		warningCount++
	}

	staging.Cleanup()
	log.Info("Backup completed successfully")
	if warningCount > 0 {
		fmt.Printf("Warnings: %d\n", warningCount)
	}
	fmt.Printf("\nLog file: %s\n", logPath)
	return nil
}

// verifyBackupAfterWrite re-reads the part files just written for each processed
// directory, decrypts them, and confirms the decrypted stream is a readable TAR.
// It reuses the in-memory credential the backup already derived (the combined
// password + YubiKey hmac-secret in YubiKey modes), so it needs no additional
// password prompt or YubiKey touch. Failures are logged as warnings and the
// backup files are left in place; the number of directories that failed is
// returned so the caller can flag the run and skip retention.
// verifyKeptRemedy is appended to each post-backup verification failure so the
// reason and the "files kept / try a manual restore" guidance live on one line.
const verifyKeptRemedy = " The backup files were kept; try a manual restore/verify."

func verifyBackupAfterWrite(backupDir, date string, id util.BackupID, directories []string, password []byte, log *util.Logger) int {
	log.Info("Verifying backup integrity")

	failures := 0
	for _, directoryName := range directories {
		entry := util.BackupEntry{DirectoryName: directoryName, Date: date, ID: id}
		parts, err := catalog.CollectParts(backupDir, entry)
		if err != nil {
			log.Warn("  Post-backup verification failed for [%s]: %v.%s", directoryName, err, verifyKeptRemedy)
			failures++
			continue
		}
		if len(parts) == 0 {
			log.Warn("  Post-backup verification failed for [%s]: no part files found in the backup directory.%s", directoryName, verifyKeptRemedy)
			failures++
			continue
		}

		err = operation.RunDecryptPipeline(parts, password, log, directoryName, "verified", "Archive validation", util.ValidateTar, nil)
		if err != nil {
			log.Warn("  Post-backup verification failed for [%s]: %v.%s", directoryName, err, verifyKeptRemedy)
			failures++
			continue
		}
		log.Info("  Verified: %d part file(s) - [%s] successfully verified", len(parts), directoryName)
	}

	if failures == 0 {
		log.Info("  Post-backup verification successful")
	}
	return failures
}

// backupDirectory streams directory → TAR → encrypt → split-writer.
func backupDirectory(
	srcDir, directoryName, backupDir, date string,
	id util.BackupID,
	password []byte,
	params security.Argon2Params,
	cfg *util.Config,
	syncParts bool,
	log *util.Logger,
) (int, error) {
	sw, bw := newSplitOutput(backupDir, directoryName, date, id, cfg.SplitSizeMB, syncParts)
	sw.SetPartOpenedHook(func(seq int, path string) {
		log.Info("  Part %03d: %s", seq, filepath.Base(path))
	})
	pr, pw := io.Pipe()
	counters := &backupCounters{}

	var progressLog *util.Logger
	if cfg.IODiagnostics {
		progressLog = log
	}
	stopProgress := operation.StartProgressTracking(progressLog, directoryName, "encrypted", &counters.inBytes, &counters.outBytes, &counters.outWriteCalls)
	defer stopProgress()

	tarErrCh := startTarProducer(log, srcDir, backupDir, pw)
	encErr := runEncryptStage(log, bw, pr, password, params, counters)
	tarErr := <-tarErrCh
	closeErr := closeSplitOutput(bw, sw)

	if encErr != nil {
		return 0, fmt.Errorf("Encryption failed: %w. Remedy: Check password/YubiKey and retry.", encErr)
	}
	if closeErr != nil {
		return 0, closeErr
	}
	if tarErr != nil {
		return 0, fmt.Errorf("Creating TAR failed: %w. Remedy: Check source-directory access and file permissions.", tarErr)
	}

	logPartSummary(sw, directoryName, cfg.IODiagnostics, counters, log)
	return len(sw.Paths()), nil
}
