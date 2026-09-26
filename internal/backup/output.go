package backup

import (
	"RestoreSafe/internal/operation"
	"RestoreSafe/internal/setio"
	"RestoreSafe/internal/util"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync/atomic"
)

func logPartSummary(parts []string, directoryName string, ioDiagnostics bool, outBytes, outWriteCalls *atomic.Int64, log *util.Logger) {
	if ioDiagnostics {
		avgEncryptWriteKB := 0.0
		if calls := outWriteCalls.Load(); calls > 0 {
			avgEncryptWriteKB = float64(outBytes.Load()) / float64(calls) / 1024
		}
		log.Debug("I/O diagnostics [%s]: encrypt writes=%d, avg encrypt write=%.2f KB", directoryName, outWriteCalls.Load(), avgEncryptWriteKB)
		for i, p := range parts {
			fi, err := os.Stat(p)
			if err != nil {
				log.Warn("Failed to inspect part file %s: %v", filepath.Base(p), err)
				continue
			}
			log.Debug("  Part %03d size: %.2f MB", i+1, float64(fi.Size())/(1024*1024))
		}
	}
	log.Info("  Created: %d part file(s) - [%s] successfully backed up", len(parts), directoryName)
}

type stagedFile struct{ name, src, dst string }

// moveBackupResults moves all encrypted part files from the staging directory
// to the backup directory. Each directory's parts are copied under the
// temporary suffix and renamed once all of them are in place, so an
// interrupted move never leaves a set that looks complete.
// directoryOrder specifies the directory names in processing order; if nil,
// directories are sorted alphabetically. directorySourcePaths maps directory
// name to original source path for display in log output.
func moveBackupResults(stagingDir, backupDir string, directoryOrder []string, directorySourcePaths map[string]string, log *util.Logger) error {
	entries, err := os.ReadDir(stagingDir)
	if err != nil {
		return fmt.Errorf("Failed to list staging directory: %w", err)
	}
	filesByDirectory := make(map[string][]stagedFile)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if backupEntry, _, ok := util.ParsePartFileName(name); ok {
			fn := backupEntry.DirectoryName
			filesByDirectory[fn] = append(filesByDirectory[fn], stagedFile{name, filepath.Join(stagingDir, name), filepath.Join(backupDir, name)})
		}
	}

	order := directoryOrder
	if len(order) == 0 {
		for fn := range filesByDirectory {
			order = append(order, fn)
		}
		sort.Strings(order)
	}

	if log != nil {
		log.Info("Move staged files to final backup directory.")
		log.Info("  From: %s", filepath.ToSlash(stagingDir))
		log.Info("  To: %s", filepath.ToSlash(backupDir))
	}

	for _, directoryName := range order {
		files := filesByDirectory[directoryName]
		if len(files) == 0 {
			continue
		}
		sort.Slice(files, func(i, j int) bool { return files[i].name < files[j].name })

		if log != nil {
			srcPath := directorySourcePaths[directoryName]
			if srcPath == "" {
				srcPath = directoryName
			}
			log.Info("Moving backup files of source directory: %s", filepath.ToSlash(srcPath))
		}

		if err := moveDirectoryFiles(log, directoryName, files); err != nil {
			return err
		}
	}

	return nil
}

// moveDirectoryFiles copies a single directory's staged part files to the
// backup directory under the temporary suffix, then finalizes them. Progress
// is logged with a deferred stop so the goroutine is always cleaned up.
func moveDirectoryFiles(log *util.Logger, directoryName string, files []stagedFile) error {
	var inBytes, outBytes, outWriteCalls atomic.Int64
	stopProgress := operation.StartProgressTracking(log, directoryName, "copied", &inBytes, &outBytes, &outWriteCalls)
	defer stopProgress()

	temps := make([]string, 0, len(files))
	for _, f := range files {
		if log != nil {
			log.Info("  Move: %s", f.name)
		}
		tmp := f.dst + util.TempSuffix
		if err := copyFileWithCounters(f.src, tmp, &inBytes, &outBytes, &outWriteCalls); err != nil {
			for _, t := range append(temps, tmp) {
				_ = os.Remove(t)
			}
			return err
		}
		temps = append(temps, tmp)
	}
	if _, err := setio.FinalizeParts(temps); err != nil {
		return err
	}

	if log != nil {
		log.Info("  Moved: %d part file(s) - [%s] successfully moved", len(files), directoryName)
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

	cr := &util.CountingReader{R: srcFile, Total: inBytes}
	cw := &util.CountingWriter{W: dstFile, Total: outBytes, Calls: outWriteCalls}

	if _, err := io.Copy(cw, cr); err != nil {
		return fmt.Errorf("Failed to copy %q: %w", src, err)
	}
	if err := dstFile.Sync(); err != nil {
		return fmt.Errorf("Failed to sync %q to disk: %w", dst, err)
	}
	return nil
}
