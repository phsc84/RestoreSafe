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
	"sort"
	"sync/atomic"
)

type backupCounters struct {
	inBytes       atomic.Int64
	outBytes      atomic.Int64
	outWriteCalls atomic.Int64
}

func newSplitOutput(targetDir, directoryName, date string, id util.BackupID, splitSizeMB int64) (*util.Writer, *bufio.Writer) {
	splitSizeBytes := splitSizeMB * 1024 * 1024
	nameFunc := func(seq int) string {
		return util.PartFileName(targetDir, directoryName, date, id, seq)
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

func runEncryptStage(log *util.Logger, bw *bufio.Writer, pr *io.PipeReader, password []byte, params security.Argon2Params, counters *backupCounters) error {
	log.Debug("Starting encryption...")
	err := security.Encrypt(
		&operation.CountingWriter{W: bw, Total: &counters.outBytes, Calls: &counters.outWriteCalls},
		&operation.CountingReader{R: pr, Total: &counters.inBytes},
		password,
		params,
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

func logPartSummary(sw *util.Writer, directoryName string, ioDiagnostics bool, counters *backupCounters, log *util.Logger) {
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
		log.Debug("I/O diagnostics [%s]: encrypt writes=%d, avg encrypt write=%.2f KB", directoryName, calls, avgEncryptWriteKB)
		log.Debug("I/O diagnostics [%s]: file writes=%d, avg file write=%.2f KB, parts opened=%d, parts closed=%d", directoryName, stats.FileWriteCalls, avgFileWriteKB, stats.PartsOpened, stats.PartsClosed)
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
		size := fi.Size()
		log.Debug("  Part %03d size: %.2f MB", i+1, float64(size)/(1024*1024))
	}
	log.Info("  Created: %d part file(s) - [%s] successfully backed up", len(parts), directoryName)
}

type stagedFile struct{ name, src, dst string }

// copyBackupResults copies all encrypted part files and challenge files from staging directory to target directory.
// directoryOrder specifies the directory names in processing order; if nil, directories are sorted alphabetically.
// directorySourcePaths maps directory name to original source path for display in log output.
func copyBackupResults(stagingDir, targetDir string, directoryOrder []string, directorySourcePaths map[string]string, log *util.Logger) error {
	entries, err := os.ReadDir(stagingDir)
	if err != nil {
		return fmt.Errorf("Failed to list staging directory: %w", err)
	}
	filesByDirectory := make(map[string][]stagedFile)
	var challengeFiles []stagedFile

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		srcPath := filepath.Join(stagingDir, name)
		dstPath := filepath.Join(targetDir, name)
		switch filepath.Ext(name) {
		case ".enc":
			if backupEntry, _, ok := util.ParsePartFileName(name); ok {
				fn := backupEntry.DirectoryName
				filesByDirectory[fn] = append(filesByDirectory[fn], stagedFile{name, srcPath, dstPath})
			}
		case ".challenge":
			challengeFiles = append(challengeFiles, stagedFile{name, srcPath, dstPath})
		}
	}

	order := directoryOrder
	if len(order) == 0 {
		for fn := range filesByDirectory {
			order = append(order, fn)
		}
		sort.Strings(order)
	}

	for _, directoryName := range order {
		files := filesByDirectory[directoryName]
		if len(files) == 0 {
			continue
		}

		if log != nil {
			srcPath := directorySourcePaths[directoryName]
			if srcPath == "" {
				srcPath = directoryName
			}
			log.Info("Copying backup files of source directory: %s", filepath.ToSlash(srcPath))
		}

		if err := copyDirectoryFiles(log, directoryName, files); err != nil {
			return err
		}
	}

	for _, f := range challengeFiles {
		if err := util.CopyFile(f.src, f.dst); err != nil {
			return fmt.Errorf("Failed to copy %s to target: %w", f.name, err)
		}
		if log != nil {
			log.Debug("Copied challenge file to target: %s", f.name)
		}
	}

	return nil
}

// copyDirectoryFiles copies a single directory's staged part files to the target,
// logging progress with a deferred stop so the goroutine is always cleaned up.
func copyDirectoryFiles(log *util.Logger, directoryName string, files []stagedFile) error {
	var inBytes, outBytes, outWriteCalls atomic.Int64
	stopProgress := operation.StartProgressTracking(log, directoryName, "copied", &inBytes, &outBytes, &outWriteCalls)
	defer stopProgress()

	for _, f := range files {
		if log != nil {
			log.Info("  Copy: %s", f.name)
		}
		if err := copyFileWithCounters(f.src, f.dst, &inBytes, &outBytes, &outWriteCalls); err != nil {
			return err
		}
	}

	if log != nil {
		log.Info("  Copied: %d part file(s) - [%s] successfully copied", len(files), directoryName)
	}
	return nil
}

// copyFileWithCounters copies src to dst while updating atomic counters for stall detection.
func copyFileWithCounters(src, dst string, inBytes, outBytes, outWriteCalls *atomic.Int64) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("Failed to open source file %q: %w", src, err)
	}
	defer srcFile.Close()

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("Failed to create destination file %q: %w", dst, err)
	}
	defer dstFile.Close()

	cr := &operation.CountingReader{R: srcFile, Total: inBytes}
	cw := &operation.CountingWriter{W: dstFile, Total: outBytes, Calls: outWriteCalls}

	if _, err := io.Copy(cw, cr); err != nil {
		return fmt.Errorf("Failed to copy %q: %w", src, err)
	}
	if err := dstFile.Sync(); err != nil {
		return fmt.Errorf("Failed to sync %q to disk: %w", dst, err)
	}
	return nil
}
