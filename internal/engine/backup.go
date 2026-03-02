// Package backup orchestrates the full backup workflow:
//  1. Prompt for password (and optionally YubiKey 2FA)
//  2. For each source folder: stream TAR → split → encrypt → write .enc parts
//  3. Write a log file per backup run
package engine

import (
	"RestoreSafe/internal/security"
	"RestoreSafe/internal/util"
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

const splitWriteBufferSize = 32 * 1024 * 1024

// Run executes the full backup workflow.
func RunBackup(cfg *util.Config, exeDir string) error {
	// Resolve target folder (may be relative to exe dir).
	targetDir := resolveDir(cfg.TargetFolder, exeDir)
	if err := os.MkdirAll(targetDir, 0o750); err != nil {
		return fmt.Errorf("Failed to create target folder: %w", err)
	}

	sources := inspectSourceFolders(cfg.SourceFolders, exeDir)

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

	log.Info("RestoreSafe backup started – ID: %s, date: %s", string(id), date)

	printBackupPreflight(cfg, targetDir, sources)
	if err := validateSourceFolders(sources); err != nil {
		log.Error("Preflight validation failed: %v", err)
		return err
	}

	confirmed, err := promptStartBackup()
	if err != nil {
		log.Error("Failed to read confirmation: %v", err)
		return err
	}
	if !confirmed {
		log.Info("Backup cancelled by user before start")
		fmt.Println("Backup cancelled.")
		return nil
	}

	// Collect password.
	password, err := security.ReadPasswordConfirmedWithPrompts("Enter backup password: ", "Re-enter backup password: ")
	if err != nil {
		log.Error("Password input failed: %v", err)
		return fmt.Errorf("Password input failed: %w", err)
	}

	// Optional YubiKey 2FA.
	var challengeHex string
	if cfg.YubikeyEnable {
		fmt.Println("YubiKey detected – please touch YubiKey button...")
		password, challengeHex, err = security.CombineWithPassword(password)
		if err != nil {
			log.Error("YubiKey authentication failed: %v", err)
			return fmt.Errorf("YubiKey authentication failed: %w", err)
		}
		log.Info("YubiKey-2FA successful. Challenge: %s", challengeHex)
	}

	fmt.Println("Backup started...")
	log.Info("Backup started – %d source folders", len(cfg.SourceFolders))

	// Back up each source folder.
	for _, source := range sources {
		srcAbs := source.Resolved
		folderName := util.FolderBaseName(srcAbs)

		log.Info("Processing source folder: %s", srcAbs)
		log.Debug("Folder name in archive: %s", folderName)

		if err := backupFolder(srcAbs, folderName, targetDir, date, id, password, cfg, log); err != nil {
			log.Error("Backup of %q failed: %v", srcAbs, err)
			return fmt.Errorf("Backup of %q failed: %w", srcAbs, err)
		}

		// Write YubiKey challenge file if needed.
		if cfg.YubikeyEnable && challengeHex != "" {
			challengePath := util.ChallengeFileName(targetDir, folderName, date, id)
			if err := os.WriteFile(challengePath, []byte(challengeHex), 0o600); err != nil {
				log.Error("Failed to write challenge file: %v", err)
				return fmt.Errorf("Failed to write challenge file: %w", err)
			}
			log.Debug("Challenge file written: %s", challengePath)
		}

		log.Info("Source folder %q successfully backed up", folderName)
	}

	log.Info("Backup completed successfully")
	fmt.Printf("\nBackup completed. Log: %s\n", logPath)
	return nil
}

type sourceFolderStatus struct {
	Configured string
	Resolved   string
	Err        error
}

func inspectSourceFolders(sourceFolders []string, exeDir string) []sourceFolderStatus {
	result := make([]sourceFolderStatus, 0, len(sourceFolders))
	for _, src := range sourceFolders {
		resolved := resolveDir(src, exeDir)
		status := sourceFolderStatus{Configured: src, Resolved: resolved}

		info, err := os.Stat(resolved)
		if err != nil {
			status.Err = fmt.Errorf("not found or inaccessible: %w", err)
			result = append(result, status)
			continue
		}
		if !info.IsDir() {
			status.Err = fmt.Errorf("path is not a directory")
			result = append(result, status)
			continue
		}
		if _, err := os.ReadDir(resolved); err != nil {
			status.Err = fmt.Errorf("directory not readable: %w", err)
		}

		result = append(result, status)
	}
	return result
}

func printBackupPreflight(cfg *util.Config, targetDir string, sources []sourceFolderStatus) {
	fmt.Println()
	fmt.Println("Backup preflight")
	fmt.Println("----------------")
	fmt.Printf("Target folder : %s\n", targetDir)
	fmt.Printf("Split size    : %d MB\n", cfg.SplitSizeMB)
	fmt.Printf("YubiKey 2FA   : %s\n", yesNo(cfg.YubikeyEnable))
	fmt.Printf("Log level     : %s\n", strings.ToLower(cfg.LogLevel))
	fmt.Println("Source folders:")
	for _, src := range sources {
		if src.Err != nil {
			fmt.Printf("  [ERROR] %s\n", src.Resolved)
			fmt.Printf("          -> %v\n", src.Err)
			continue
		}
		fmt.Printf("  [OK]    %s\n", src.Resolved)
	}
	fmt.Println()
}

func validateSourceFolders(sources []sourceFolderStatus) error {
	invalid := 0
	for _, src := range sources {
		if src.Err != nil {
			invalid++
		}
	}
	if invalid > 0 {
		return fmt.Errorf("Backup preflight failed: %d source folder(s) are invalid or inaccessible", invalid)
	}
	return nil
}

func promptStartBackup() (bool, error) {
	for {
		answer, err := security.ReadLine("Start backup now? [Y/n]: ")
		if err != nil {
			return false, err
		}
		switch strings.ToLower(strings.TrimSpace(answer)) {
		case "", "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		default:
			fmt.Println("Please enter Y (yes) or N (no).")
		}
	}
}

func yesNo(v bool) string {
	if v {
		return "enabled"
	}
	return "disabled"
}

// backupFolder streams folder → TAR → encrypt → split-writer.
func backupFolder(
	srcDir, folderName, targetDir, date string,
	id util.BackupID,
	password []byte,
	cfg *util.Config,
	log *util.Logger,
) error {
	sw, bw := newSplitOutput(targetDir, folderName, date, id, cfg.SplitSizeMB)
	pr, pw := io.Pipe()
	counters := &backupCounters{}

	progressDone := make(chan struct{})
	go logBackupProgress(log, folderName, &counters.inBytes, &counters.outBytes, &counters.outWriteCalls, cfg.IODiagnostics, progressDone)
	defer close(progressDone)

	tarErrCh := startTarProducer(log, srcDir, targetDir, pw)
	encErr := runEncryptStage(log, bw, pr, password, counters)
	tarErr := <-tarErrCh
	closeErr := closeSplitOutput(bw, sw)

	if encErr != nil {
		return fmt.Errorf("Encryption failed: %w", encErr)
	}
	if closeErr != nil {
		return closeErr
	}
	if tarErr != nil {
		return fmt.Errorf("Creating TAR failed: %w", tarErr)
	}

	logPartSummary(sw, folderName, cfg.IODiagnostics, counters, log)
	return nil
}

type backupCounters struct {
	inBytes       atomic.Int64
	outBytes      atomic.Int64
	outWriteCalls atomic.Int64
}

func newSplitOutput(targetDir, folderName, date string, id util.BackupID, splitSizeMB int64) (*util.Writer, *bufio.Writer) {
	splitSizeBytes := splitSizeMB * 1024 * 1024
	nameFunc := func(seq int) string {
		return util.PartFileName(targetDir, folderName, date, id, seq)
	}
	sw := util.NewWriter(nameFunc, splitSizeBytes)
	bw := bufio.NewWriterSize(sw, splitWriteBufferSize)
	return sw, bw
}

func startTarProducer(log *util.Logger, srcDir, targetDir string, pw *io.PipeWriter) <-chan error {
	tarErrCh := make(chan error, 1)
	go func() {
		log.Debug("Starting TAR creation for: %s", srcDir)
		err := WriteTar(pw, srcDir, targetDir)
		pw.CloseWithError(err) //nolint:errcheck
		tarErrCh <- err
	}()
	return tarErrCh
}

func runEncryptStage(log *util.Logger, bw *bufio.Writer, pr *io.PipeReader, password []byte, counters *backupCounters) error {
	log.Debug("Starting encryption...")
	err := security.Encrypt(
		&countingWriter{w: bw, total: &counters.outBytes, calls: &counters.outWriteCalls},
		&countingReader{r: pr, total: &counters.inBytes},
		password,
	)
	pr.Close() //nolint:errcheck
	return err
}

func closeSplitOutput(bw *bufio.Writer, sw *util.Writer) error {
	if err := bw.Flush(); err != nil {
		return fmt.Errorf("Flushing split buffer failed: %w", err)
	}
	if err := sw.Close(); err != nil {
		return fmt.Errorf("Closing split-writer failed: %w", err)
	}
	return nil
}

func logPartSummary(sw *util.Writer, folderName string, ioDiagnostics bool, counters *backupCounters, log *util.Logger) {
	parts := sw.Paths()
	log.Info("  Created: %d part file(s)", len(parts))
	if ioDiagnostics {
		stats := sw.Stats()
		avgEncryptWriteKB := 0.0
		calls := counters.outWriteCalls.Load()
		if calls > 0 {
			avgEncryptWriteKB = float64(counters.outBytes.Load()) / float64(calls) / 1024
		}
		avgFileWriteKB := 0.0
		if stats.FileWriteCalls > 0 {
			avgFileWriteKB = float64(stats.FileWriteBytes) / float64(stats.FileWriteCalls) / 1024
		}
		log.Debug("I/O diagnostics [%s]: encrypt writes=%d, avg encrypt write=%.2f KB", folderName, calls, avgEncryptWriteKB)
		log.Debug("I/O diagnostics [%s]: file writes=%d, avg file write=%.2f KB, parts opened=%d, parts closed=%d", folderName, stats.FileWriteCalls, avgFileWriteKB, stats.PartsOpened, stats.PartsClosed)
	}
	for i, p := range parts {
		fi, _ := os.Stat(p)
		size := int64(0)
		if fi != nil {
			size = fi.Size()
		}
		log.Debug("  Part %03d: %s (%.2f MB)", i+1, filepath.Base(p), float64(size)/(1024*1024))
	}
}

type countingWriter struct {
	w     io.Writer
	total *atomic.Int64
	calls *atomic.Int64
}

type countingReader struct {
	r     io.Reader
	total *atomic.Int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if n > 0 {
		c.total.Add(int64(n))
	}
	return n, err
}

func (c *countingWriter) Write(p []byte) (int, error) {
	if c.calls != nil {
		c.calls.Add(1)
	}
	n, err := c.w.Write(p)
	if n > 0 {
		c.total.Add(int64(n))
	}
	return n, err
}

func logBackupProgress(log *util.Logger, folderName string, inBytes, outBytes, outWriteCalls *atomic.Int64, ioDiagnostics bool, done <-chan struct{}) {
	if !ioDiagnostics {
		<-done // Just wait for completion without logging
		return
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			logStreamProgress(log, folderName, "encrypted", inBytes, outBytes, outWriteCalls, true)
			return
		case <-ticker.C:
			logStreamProgress(log, folderName, "encrypted", inBytes, outBytes, outWriteCalls, false)
		}
	}
}
