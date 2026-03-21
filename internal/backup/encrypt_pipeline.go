package backup

import (
	"RestoreSafe/internal/operation"
	"RestoreSafe/internal/security"
	"RestoreSafe/internal/util"
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
)

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
	bw := bufio.NewWriterSize(sw, util.SplitWriteBufferSize)
	return sw, bw
}

func startTarProducer(log *util.Logger, srcDir, targetDir string, pw *io.PipeWriter) <-chan error {
	tarErrCh := make(chan error, 1)
	log.Debug("Starting TAR creation for: %s", srcDir)
	go func() {
		err := util.WriteTar(pw, srcDir, targetDir)
		pw.CloseWithError(err) //nolint:errcheck
		tarErrCh <- err
	}()
	return tarErrCh
}

func runEncryptStage(log *util.Logger, bw *bufio.Writer, pr *io.PipeReader, password []byte, counters *backupCounters) error {
	log.Debug("Starting encryption...")
	err := security.Encrypt(
		&operation.CountingWriter{W: bw, Total: &counters.outBytes, Calls: &counters.outWriteCalls},
		&operation.CountingReader{R: pr, Total: &counters.inBytes},
		password,
	)
	pr.Close() //nolint:errcheck
	return err
}

func closeSplitOutput(bw *bufio.Writer, sw *util.Writer) error {
	if err := bw.Flush(); err != nil {
		return fmt.Errorf("Flushing split buffer failed: %w. Remedy: Check free disk space and write permissions in target_folder.", err)
	}
	if err := sw.Close(); err != nil {
		return fmt.Errorf("Closing split-writer failed: %w. Remedy: Check free disk space and write permissions in target_folder.", err)
	}
	return nil
}

func logPartSummary(sw *util.Writer, folderName string, ioDiagnostics bool, counters *backupCounters, log *util.Logger) {
	parts := sw.Paths()
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
		if !ioDiagnostics {
			continue
		}

		fi, err := os.Stat(p)
		if err != nil {
			log.Warn("Failed to inspect part file %s: %v", filepath.Base(p), err)
			continue
		}
		size := int64(0)
		if fi != nil {
			size = fi.Size()
		}
		log.Debug("  Part %03d size: %.2f MB", i+1, float64(size)/(1024*1024))
	}
	log.Info("  Created: %d part file(s)", len(parts))
}

// copyBackupResults copies all encrypted part files and challenge files from staging directory to target directory.
func copyBackupResults(stagingDir, targetDir string, log *util.Logger) error {
	entries, err := os.ReadDir(stagingDir)
	if err != nil {
		return fmt.Errorf("Failed to list staging directory: %w", err)
	}

	totalPartsByEntry := make(map[string]int)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".enc" {
			continue
		}
		if backupEntry, _, ok := util.ParsePartFileName(entry.Name()); ok {
			totalPartsByEntry[backupEntry.String()]++
		}
	}

	copiedPartsByEntry := make(map[string]int)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		srcPath := filepath.Join(stagingDir, entry.Name())
		dstPath := filepath.Join(targetDir, entry.Name())

		// Only copy .enc and .challenge files
		ext := filepath.Ext(entry.Name())
		if ext != ".enc" && ext != ".challenge" {
			continue
		}

		if err := util.CopyFile(srcPath, dstPath); err != nil {
			return fmt.Errorf("Failed to copy %s to target: %w", entry.Name(), err)
		}

		if log != nil {
			if ext == ".enc" {
				log.Info("  Copied: %s", entry.Name())
				if backupEntry, _, ok := util.ParsePartFileName(entry.Name()); ok {
					entryKey := backupEntry.String()
					copiedPartsByEntry[entryKey]++
					if copiedPartsByEntry[entryKey] == totalPartsByEntry[entryKey] {
						log.Info("  Source folder %q copy finished", backupEntry.FolderName)
					}
				}
			} else {
				log.Debug("Copied challenge file to target: %s", entry.Name())
			}
		}
	}

	return nil
}
