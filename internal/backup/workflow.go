// Package backup orchestrates the full backup workflow:
//  1. Prompt for password (and optionally YubiKey 2FA)
//  2. For each source directory: stream TAR → split → encrypt → write .enc parts
//  3. Write a log file per backup run
package backup

import (
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

	printBackupPreflightWithYubiKeyCheck(os.Stdout, cfg, backupDir, sources, stagingPlan, security.CheckYubiKeyConnected)
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

	confirmed, err := operation.PromptStartAction("backup")
	if err != nil {
		return err
	}
	if !confirmed {
		log.InfoLogOnly("Backup cancelled by user before start")
		fmt.Println("Backup cancelled.")
		return nil
	}

	// Collect password.
	var password []byte
	if cfg.IsYubiKeyOnly() {
		fmt.Println("YubiKey-only mode: no password required.")
		password = []byte{}
	} else {
		var err error
		password, err = security.ReadPasswordConfirmedWithPrompts("Enter backup password: ", "Re-enter backup password: ")
		if err != nil {
			return err
		}
	}
	defer func() { security.ZeroBytes(password) }()

	// Optional YubiKey factor (2FA or sole factor in yubikey mode).
	var challengeHex string
	if cfg.UseYubiKey() {
		// Verify ykman is installed and a device is physically connected.
		if err := security.CheckYubiKeyConnected(); err != nil {
			return security.ErrYubiKeyRequired
		}
		fmt.Println("YubiKey connected. Please touch the YubiKey button.")
		rawPassword := password
		combined, hex, err := security.CombineWithPassword(rawPassword)
		security.ZeroBytes(rawPassword)
		if err != nil {
			return fmt.Errorf("YubiKey authentication failed: %w", err)
		}
		password = combined
		challengeHex = hex
		if cfg.IsYubiKeyOnly() {
			log.Info("YubiKey-only authentication successful. Challenge: %s", challengeHex)
		} else {
			log.Info("YubiKey-2FA successful. Challenge: %s", challengeHex)
		}
	}

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
		partCount, err := backupDirectory(srcAbs, directoryName, workingDir, date, id, password, argon2Params, cfg, log)
		if err != nil {
			return fmt.Errorf("Backup of %q failed: %w", srcAbs, err)
		}
		totalPartsCreated += partCount
		processedDirectories = append(processedDirectories, directoryName)
		directorySourcePaths[directoryName] = srcAbs

		// Write YubiKey challenge file if needed.
		if cfg.UseYubiKey() && challengeHex != "" {
			challengeContent := challengeHex
			if cfg.IsYubiKeyOnly() {
				challengeContent = "NOPW:" + challengeHex
			}
			challengePath := util.ChallengeFileName(workingDir, directoryName, date, id)
			if err := os.WriteFile(challengePath, []byte(challengeContent), 0o600); err != nil {
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

	if err := applyRetentionPolicy(backupDir, cfg.RetentionKeep, sources, log); err != nil {
		log.Warn("Retention cleanup failed: %v", err)
		warningCount++
	}

	log.Info("Backup completed successfully")
	printBackupCompletionSummary(os.Stdout, processedDirectories, totalPartsCreated, logPath, warningCount)
	fmt.Println("\nBackup completed.")
	return nil
}

// backupDirectory streams directory → TAR → encrypt → split-writer.
func backupDirectory(
	srcDir, directoryName, backupDir, date string,
	id util.BackupID,
	password []byte,
	params security.Argon2Params,
	cfg *util.Config,
	log *util.Logger,
) (int, error) {
	sw, bw := newSplitOutput(backupDir, directoryName, date, id, cfg.SplitSizeMB)
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

func printBackupCompletionSummary(w io.Writer, processedDirectories []string, totalPartsCreated int, logPath string, warningCount int) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Backup summary")
	fmt.Fprintln(w, "--------------")
	const w21 = 21
	operation.PrintField(w, w21, "Processed directories", fmt.Sprintf("%d", len(processedDirectories)))
	operation.PrintField(w, w21, "Parts created", fmt.Sprintf("%d", totalPartsCreated))
	operation.PrintField(w, w21, "Log file", logPath)
	if warningCount > 0 {
		operation.PrintField(w, w21, "Warnings", fmt.Sprintf("%d", warningCount))
	} else {
		operation.PrintField(w, w21, "Warnings", "none")
	}
}
