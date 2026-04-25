// Package backup orchestrates the full backup workflow:
//  1. Prompt for password (and optionally YubiKey 2FA)
//  2. For each source folder: stream TAR → split → encrypt → write .enc parts
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
	// Resolve target folder (may be relative to exe dir).
	targetDir := util.ResolveDir(cfg.TargetFolder, exeDir)
	if err := os.MkdirAll(targetDir, 0o750); err != nil {
		return fmt.Errorf("Failed to create target folder: %w. Remedy: Check the path (prefer forward slashes in config.yaml, e.g. C:/Backups) and verify write permissions.", err)
	}

	sources := planBackupSources(cfg.SourceFolders, exeDir)

	// Determine backup run identifiers.
	id, err := util.NewBackupID()
	if err != nil {
		return err
	}
	date := util.DateString()

	// Set up logger.
	logPath := util.LogFileName(targetDir, date, id)
	log, err := util.NewLogger(logPath, cfg.LogLevel)
	if err != nil {
		return err
	}
	defer log.Close()

	log.Info("RestoreSafe backup started - ID: %s, date: %s", string(id), date)

	if err := validateSourceFolders(sources); err != nil {
		return err
	}

	// Plan local staging to mitigate same-volume read+write contention.
	firstValidSource := ""
	for _, src := range sources {
		if src.Err == nil && !src.Skip {
			firstValidSource = src.Resolved
			break
		}
	}
	stagingPlan := operation.PlanLocalStaging(firstValidSource, targetDir, os.TempDir())

	printBackupPreflightWithYubiKeyCheck(cfg, targetDir, sources, stagingPlan, security.CheckYubiKeyConnected)
	if err := validateTargetSpaceForBackup(targetDir, sources); err != nil {
		if strings.Contains(err.Error(), "Insufficient free space for backup:") {
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
		log.Info("Backup cancelled by user before start")
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

	// Optional YubiKey factor (2FA or sole factor in yubikey mode).
	var challengeHex string
	if cfg.UseYubiKey() {
		// Verify ykman is installed and a device is physically connected.
		if err := security.CheckYubiKeyConnected(); err != nil {
			return fmt.Errorf("YubiKey is required but no YubiKey was detected. Remedy: Connect the YubiKey and retry.")
		}
		fmt.Println("YubiKey connected. Please touch the YubiKey button.")
		var err error
		password, challengeHex, err = security.CombineWithPassword(password)
		if err != nil {
			return fmt.Errorf("YubiKey authentication failed: %w", err)
		}
		if cfg.IsYubiKeyOnly() {
			log.Info("YubiKey-only authentication successful. Challenge: %s", challengeHex)
		} else {
			log.Info("YubiKey-2FA successful. Challenge: %s", challengeHex)
		}
	}

	fmt.Println("Backup started.")
	log.Info("Backup started - %d source folders", runnableSourceCount(sources))
	warningCount := 0
	totalPartsCreated := 0
	processedFolders := make([]string, 0)

	// Determine actual working directory (staging or target).
	staging, err := operation.NewStagingScope(stagingPlan, "restoresafe-backup-stage-*", log)
	if err != nil {
		return err
	}
	if staging.Dir != "" {
		log.Info("Local staging enabled: backup will write to %s before finalizing to %s", filepath.ToSlash(staging.Dir), filepath.ToSlash(targetDir))
	}
	workingDir := staging.ActiveDir(targetDir)
	defer staging.Cleanup()

	// Back up each source folder.
	for _, source := range sources {
		if source.Warning != "" {
			log.Warn("Source folder warning: %s → %s", source.Resolved, source.Warning)
			warningCount++
		}
		if source.Skip {
			continue
		}

		srcAbs := source.Resolved
		folderName := source.BackupName
		if folderName == "" {
			folderName = util.FolderBaseName(srcAbs)
		}

		log.Info("Processing source folder: %s", srcAbs)
		log.Debug("Folder name in archive: %s", folderName)

		partCount, err := backupFolder(srcAbs, folderName, workingDir, date, id, password, cfg, log)
		if err != nil {
			return fmt.Errorf("Backup of %q failed: %w", srcAbs, err)
		}
		totalPartsCreated += partCount
		processedFolders = append(processedFolders, folderName)

		// Write YubiKey challenge file if needed.
		if cfg.UseYubiKey() && challengeHex != "" {
			challengeContent := challengeHex
			if cfg.IsYubiKeyOnly() {
				challengeContent = "NOPW:" + challengeHex
			}
			challengePath := util.ChallengeFileName(workingDir, folderName, date, id)
			if err := os.WriteFile(challengePath, []byte(challengeContent), 0o600); err != nil {
				return fmt.Errorf("Failed to write challenge file: %w. Remedy: Check write permissions in the target folder; for YubiKey backups, the .challenge file must be in the same folder as the .enc files.", err)
			}
			log.Debug("Challenge file written: %s", challengePath)
		}

		log.Info("Source folder %q successfully backed up", folderName)
	}

	// Copy results from staging to target if needed.
	if staging.Dir != "" {
		log.Info("Copying staged backup files from %s to %s", filepath.ToSlash(workingDir), filepath.ToSlash(targetDir))
		if err := copyBackupResults(workingDir, targetDir, log); err != nil {
			return fmt.Errorf("Failed to copy staged backup to target: %w. Remedy: Check target folder write permissions and free disk space.", err)
		}
		log.Info("Staged backup files copied to target")
	}

	if err := applyRetentionPolicy(targetDir, cfg.RetentionKeep, sources, log); err != nil {
		log.Warn("Retention cleanup failed: %v", err)
		warningCount++
	}

	log.Info("Backup completed successfully")
	printBackupCompletionSummary(processedFolders, totalPartsCreated, logPath, warningCount)
	fmt.Println("\nBackup completed.")
	return nil
}

// backupFolder streams folder → TAR → encrypt → split-writer.
func backupFolder(
	srcDir, folderName, targetDir, date string,
	id util.BackupID,
	password []byte,
	cfg *util.Config,
	log *util.Logger,
) (int, error) {
	sw, bw := newSplitOutput(targetDir, folderName, date, id, cfg.SplitSizeMB)
	sw.SetPartOpenedHook(func(seq int, path string) {
		log.Info("  Part %03d: %s", seq, filepath.Base(path))
	})
	pr, pw := io.Pipe()
	counters := &backupCounters{}

	progressDone := make(chan struct{})
	progressStopped := make(chan struct{})
	go func() {
		if cfg.IODiagnostics {
			operation.LogProgressUntilDone(log, folderName, "encrypted", &counters.inBytes, &counters.outBytes, &counters.outWriteCalls, progressDone)
		} else {
			<-progressDone
		}
		close(progressStopped)
	}()
	defer func() {
		close(progressDone)
		<-progressStopped
	}()

	tarErrCh := startTarProducer(log, srcDir, targetDir, pw)
	encErr := runEncryptStage(log, bw, pr, password, counters)
	tarErr := <-tarErrCh
	closeErr := closeSplitOutput(bw, sw)

	if encErr != nil {
		return 0, fmt.Errorf("Encryption failed: %w. Remedy: Check password/YubiKey and retry.", encErr)
	}
	if closeErr != nil {
		return 0, closeErr
	}
	if tarErr != nil {
		return 0, fmt.Errorf("Creating TAR failed: %w. Remedy: Check source-folder access and file permissions.", tarErr)
	}

	logPartSummary(sw, folderName, cfg.IODiagnostics, counters, log)
	return len(sw.Paths()), nil
}

func printBackupCompletionSummary(processedFolders []string, totalPartsCreated int, logPath string, warningCount int) {
	fmt.Println()
	fmt.Println("Backup summary")
	fmt.Println("--------------")
	fmt.Printf("Processed folders: %d\n", len(processedFolders))
	fmt.Printf("Parts created    : %d\n", totalPartsCreated)
	fmt.Printf("Log file         : %s\n", logPath)
	if warningCount > 0 {
		fmt.Printf("Warnings         : %d\n", warningCount)
	} else {
		fmt.Println("Warnings         : none")
	}
}
