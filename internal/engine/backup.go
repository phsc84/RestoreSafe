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
	"unicode"
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
	log.Info("Backup started – %d source folders", runnableSourceCount(sources))

	// Back up each source folder.
	for _, source := range sources {
		if source.Warning != "" {
			log.Warn("Source folder warning: %s -> %s", source.Resolved, source.Warning)
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

	if err := applyRetentionPolicy(targetDir, cfg.RetentionKeep, sources, log); err != nil {
		log.Warn("Retention cleanup failed: %v", err)
	}

	log.Info("Backup completed successfully")
	fmt.Printf("\nBackup completed. Log: %s\n", logPath)
	return nil
}

type sourceFolderStatus struct {
	Configured string
	Resolved   string
	BackupName string
	Warning    string
	Skip       bool
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
	markIdenticalSourceDuplicates(result)
	assignSourceBackupNames(result)
	return result
}

func markIdenticalSourceDuplicates(sources []sourceFolderStatus) {
	seenByPath := make(map[string]int)
	for i := range sources {
		if sources[i].Err != nil {
			continue
		}

		pathKey := normalizedSourcePathKey(sources[i].Resolved)
		if firstIndex, exists := seenByPath[pathKey]; exists {
			sources[i].Skip = true
			sources[i].Warning = fmt.Sprintf("identical duplicate of %s; this entry will be skipped", sources[firstIndex].Resolved)
			continue
		}

		seenByPath[pathKey] = i
	}
}

func normalizedSourcePathKey(path string) string {
	cleaned := filepath.Clean(path)
	cleaned = strings.ReplaceAll(cleaned, "/", "\\")
	return strings.ToLower(cleaned)
}

func assignSourceBackupNames(sources []sourceFolderStatus) {
	groupedIndices := make(map[string][]int)
	for i, source := range sources {
		if source.Err != nil || source.Skip {
			continue
		}
		baseName := util.FolderBaseName(source.Resolved)
		groupedIndices[baseName] = append(groupedIndices[baseName], i)
	}

	for baseName, indices := range groupedIndices {
		if len(indices) <= 1 {
			sources[indices[0]].BackupName = baseName
			continue
		}

		aliasOwners := make(map[string]int)
		for _, index := range indices {
			pathAlias := sourceAliasFromFullPath(sources[index].Resolved)
			backupName := fmt.Sprintf("%s__%s", baseName, pathAlias)
			sources[index].BackupName = backupName

			if ownerIndex, exists := aliasOwners[backupName]; exists {
				ownerPath := sources[ownerIndex].Resolved
				currentPath := sources[index].Resolved
				errText := fmt.Sprintf("backup name alias collision: %s and %s both resolve to %q; adjust one source path to avoid ambiguity", ownerPath, currentPath, backupName)
				sources[ownerIndex].Err = fmt.Errorf("%s", errText)
				sources[index].Err = fmt.Errorf("%s", errText)
				continue
			}

			aliasOwners[backupName] = index
		}
	}

	nameByPath := make(map[string]string)
	for i := range sources {
		if sources[i].Err != nil || sources[i].Skip {
			continue
		}
		if sources[i].BackupName == "" {
			sources[i].BackupName = util.FolderBaseName(sources[i].Resolved)
		}
		nameByPath[normalizedSourcePathKey(sources[i].Resolved)] = sources[i].BackupName
	}

	for i := range sources {
		if sources[i].BackupName != "" {
			continue
		}

		if sources[i].Skip {
			if name, exists := nameByPath[normalizedSourcePathKey(sources[i].Resolved)]; exists {
				sources[i].BackupName = name
				continue
			}
		}

		sources[i].BackupName = util.FolderBaseName(sources[i].Resolved)
	}
}

func sourceAliasFromFullPath(path string) string {
	parts := pathHintParts(path)
	return aliasFromParts(parts)
}

func pathHintParts(path string) []string {
	cleaned := filepath.Clean(path)
	volumeName := filepath.VolumeName(cleaned)
	volume := strings.TrimSuffix(volumeName, ":")

	withoutVolume := strings.TrimPrefix(cleaned, volumeName)
	withoutVolume = strings.TrimLeft(withoutVolume, "\\/")

	rawSegments := strings.FieldsFunc(withoutVolume, func(r rune) bool {
		return r == '\\' || r == '/'
	})
	if len(rawSegments) > 0 {
		rawSegments = rawSegments[:len(rawSegments)-1]
	}

	parts := make([]string, 0, len(rawSegments)+1)
	for _, segment := range rawSegments {
		if normalized := sanitizeAliasPart(segment); normalized != "" {
			parts = append(parts, normalized)
		}
	}

	if normalized := sanitizeAliasPart(volume); normalized != "" {
		parts = append(parts, normalized)
	}

	if len(parts) == 0 {
		return []string{"source"}
	}

	return parts
}

func aliasFromParts(parts []string) string {
	alias := strings.Join(parts, "-")
	if alias == "" {
		return "source"
	}
	return alias
}

func sanitizeAliasPart(part string) string {
	trimmed := strings.TrimSpace(part)
	if trimmed == "" {
		return ""
	}

	var b strings.Builder
	b.Grow(len(trimmed))
	lastWasDash := false

	for _, r := range trimmed {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastWasDash = false
		case r == '_':
			b.WriteRune(r)
			lastWasDash = false
		case unicode.IsSpace(r):
			b.WriteByte('~')
			lastWasDash = false
		case r == '-':
			if !lastWasDash {
				b.WriteByte('-')
				lastWasDash = true
			}
		default:
			if !lastWasDash {
				b.WriteByte('-')
				lastWasDash = true
			}
		}
	}

	cleaned := strings.Trim(b.String(), "-")
	return cleaned
}

func printBackupPreflight(cfg *util.Config, targetDir string, sources []sourceFolderStatus) {
	fmt.Println()
	fmt.Println("Backup preflight")
	fmt.Println("----------------")
	fmt.Printf("Target folder : %s\n", targetDir)
	fmt.Printf("Split size    : %d MB\n", cfg.SplitSizeMB)
	fmt.Printf("Retention keep: %d\n", cfg.RetentionKeep)
	fmt.Printf("YubiKey 2FA   : %s\n", yesNo(cfg.YubikeyEnable))
	fmt.Printf("Log level     : %s\n", strings.ToLower(cfg.LogLevel))
	fmt.Println("Source folders:")
	for _, src := range sources {
		baseName := util.FolderBaseName(src.Resolved)
		backupName := src.BackupName
		if backupName == "" {
			backupName = baseName
		}

		if src.Err != nil {
			fmt.Printf("  [ERROR] %s\n", src.Resolved)
			if backupName != baseName {
				fmt.Printf("          -> backup name: %s\n", backupName)
			}
			fmt.Printf("          -> %v\n", src.Err)
			continue
		}
		if src.Warning != "" {
			fmt.Printf("  [WARN]  %s\n", src.Resolved)
			if backupName != baseName {
				fmt.Printf("          -> backup name: %s\n", backupName)
			}
			fmt.Printf("          -> %s\n", src.Warning)
			continue
		}
		fmt.Printf("  [OK]    %s\n", src.Resolved)
		if backupName != baseName {
			fmt.Printf("          -> backup name: %s\n", backupName)
		}
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

func runnableSourceCount(sources []sourceFolderStatus) int {
	count := 0
	for _, source := range sources {
		if source.Err != nil || source.Skip {
			continue
		}
		count++
	}
	return count
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
