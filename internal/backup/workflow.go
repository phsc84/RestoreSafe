// Package backup orchestrates the full backup workflow:
//  1. Prompt for password (and optionally YubiKey 2FA)
//  2. For each source folder: stream TAR → split → encrypt → write .enc parts
//  3. Write a log file per backup run
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
	"strings"
	"sync/atomic"
)

const splitWriteBufferSize = 32 * 1024 * 1024

// Run executes the full backup workflow.
func Run(cfg *util.Config, exeDir string) error {
	// Resolve target folder (may be relative to exe dir).
	targetDir := util.ResolveDir(cfg.TargetFolder, exeDir)
	if err := os.MkdirAll(targetDir, 0o750); err != nil {
		return fmt.Errorf("Failed to create target folder: %w. Remedy: Check the path (prefer forward slashes in config.yaml, e.g. C:/Backups) and verify write permissions.", err)
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

	printBackupPreflight(cfg, targetDir, sources, stagingPlan)

	if cfg.NonInteractive {
		log.Info("Unattended mode: start confirmation skipped")
		fmt.Println("Starting backup automatically.")
	} else {
		confirmed, err := promptStartBackup()
		if err != nil {
			return err
		}
		if !confirmed {
			log.Info("Backup cancelled by user before start")
			fmt.Println("Backup cancelled.")
			return nil
		}
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
		if cfg.IsYubiKeyOnly() {
			fmt.Println("Please touch the YubiKey button.")
		} else {
			fmt.Println("YubiKey detected. Please touch the YubiKey button.")
		}
		var err error
		password, challengeHex, err = security.CombineWithPassword(password)
		if err != nil {
			return fmt.Errorf("YubiKey authentication failed: %w. Remedy: Connect the YubiKey, touch it, and ensure ykchalresp is available on PATH.", err)
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
	workingDir := targetDir
	cleanup := func() {}
	if stagingPlan.Enabled {
		stagingDir, err := os.MkdirTemp(stagingPlan.ResolvedTempDir, "restoresafe-backup-stage-*")
		if err != nil {
			return fmt.Errorf("Failed to create local staging directory: %w. Remedy: Check TEMP/TMP write permissions and free disk space.", err)
		}
		log.Info("Local staging enabled: backup will write to %s before finalizing to %s", filepath.ToSlash(stagingDir), filepath.ToSlash(targetDir))
		workingDir = stagingDir
		cleanup = func() {
			if err := os.RemoveAll(stagingDir); err != nil {
				log.Warn("Failed to remove staging directory %s: %v", filepath.ToSlash(stagingDir), err)
			}
		}
	}

	// Back up each source folder.
	for _, source := range sources {
		if source.Warning != "" {
			log.Warn("Source folder warning: %s -> %s", source.Resolved, source.Warning)
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

		partCount, err := backupFolder(srcAbs, folderName, targetDir, date, id, password, cfg, log)
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
				cleanup()
				return fmt.Errorf("Failed to write challenge file: %w. Remedy: Check write permissions in the target folder; for YubiKey backups, the .challenge file must be in the same folder as the .enc files.", err)
			}
			log.Debug("Challenge file written: %s", challengePath)
		}

		log.Info("Source folder %q successfully backed up", folderName)
	}

	if err := applyRetentionPolicy(targetDir, cfg.RetentionKeep, sources, log); err != nil {
		log.Warn("Retention cleanup failed: %v", err)
		warningCount++
	}

	// Copy results from staging to target if needed.
	if stagingPlan.Enabled {
		log.Info("Copying staged backup files from %s to %s", filepath.ToSlash(workingDir), filepath.ToSlash(targetDir))
		if err := copyBackupResults(workingDir, targetDir, log); err != nil {
			cleanup()
			return fmt.Errorf("Failed to copy staged backup to target: %w. Remedy: Check target folder write permissions and free disk space.", err)
		}
		log.Info("Staged backup files copied to target")
	}
	cleanup()

	log.Info("Backup completed successfully")
	printBackupCompletionSummary(processedFolders, totalPartsCreated, logPath, warningCount)
	fmt.Println("\nBackup completed.")
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

// SourceFolderStatus is the exported form of source-folder preflight status.
type SourceFolderStatus = sourceFolderStatus

// InspectSourceFolders resolves and validates configured source folders.
func InspectSourceFolders(sourceFolders []string, exeDir string) []SourceFolderStatus {
	return inspectSourceFolders(sourceFolders, exeDir)
}

func inspectSourceFolders(sourceFolders []string, exeDir string) []sourceFolderStatus {
	result := make([]sourceFolderStatus, 0, len(sourceFolders))
	for _, src := range sourceFolders {
		resolved := util.ResolveDir(src, exeDir)
		status := sourceFolderStatus{Configured: src, Resolved: resolved}

		info, err := os.Stat(resolved)
		if err != nil {
			status.Err = fmt.Errorf("Not found or inaccessible: %w. Remedy: Check the path in config.yaml and use forward slashes on Windows (e.g. C:/Users/Name/Documents).", err)
			result = append(result, status)
			continue
		}
		if !info.IsDir() {
			status.Err = fmt.Errorf("Path is not a directory. Remedy: Provide a folder path, not a file path.")
			result = append(result, status)
			continue
		}
		if _, err := os.ReadDir(resolved); err != nil {
			status.Err = fmt.Errorf("Directory not readable: %w. Remedy: Check permissions and ensure this user can read the folder.", err)
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

	for _, r := range trimmed {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			for _, by := range []byte(string(r)) {
				b.WriteString(fmt.Sprintf("~%02X~", by))
			}
		}
	}

	if b.Len() == 0 {
		return "source"
	}

	return b.String()
}

func printBackupPreflight(cfg *util.Config, targetDir string, sources []sourceFolderStatus, stagingPlan operation.LocalStagingPlan) {
	fmt.Println()
	fmt.Println("Backup preflight")
	fmt.Println("----------------")
	fmt.Printf("Target folder   : %s\n", targetDir)
	fmt.Printf("Split size      : %d MB\n", cfg.SplitSizeMB)
	fmt.Printf("Retention keep  : %d\n", cfg.RetentionKeep)
	fmt.Printf("Authentication  : %s\n", backupAuthLabel(cfg))
	fmt.Printf("Log level       : %s\n", strings.ToLower(cfg.LogLevel))

	estimatedBytes, estimateWarnings := estimateSelectedSourceBytes(sources)
	if estimatedBytes > 0 {
		fmt.Printf("Est. source size: %s\n", util.FormatBytesBinary(uint64(estimatedBytes)))
	} else {
		fmt.Println("Est. source size: unknown")
	}
	for _, warning := range estimateWarnings {
		fmt.Printf("  [WARN] size estimate: %s\n", warning)
	}

	freeBytes, freeErr := util.QueryFreeSpaceBytes(targetDir)
	if freeErr != nil {
		fmt.Printf("Free space      : unknown (%v)\n", freeErr)
	} else {
		fmt.Printf("Free space      : %s\n", util.FormatBytesBinary(freeBytes))
		if estimatedBytes > 0 && uint64(estimatedBytes) > freeBytes {
			fmt.Println("  [WARN] estimated source size exceeds currently free space on target")
		}
	}

	if stagingPlan.Enabled {
		fmt.Printf("Local staging   : enabled via %s because source and target folders share the same drive/share (%s)\n", filepath.ToSlash(stagingPlan.ResolvedTempDir), util.VolumeDisplay(targetDir))
	}
	sameVolumeNetworkWarning := !stagingPlan.Enabled && stagingPlan.SameVolume && util.IsNetworkVolume(targetDir)

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
		} else {
			fmt.Printf("  [OK]    %s\n", src.Resolved)
			if backupName != baseName {
				fmt.Printf("          -> backup name: %s\n", backupName)
			}
		}

		if sameVolumeNetworkWarning && !src.Skip && util.SameVolume(src.Resolved, targetDir) {
			fmt.Printf("  [WARN]  Source folder warning: Source and target folders are on the same drive/share (%s). This can cause long stalls, especially on network/NAS storage. Local staging is unavailable because TEMP is on the same drive/share. Remedy: Prefer a different target drive/share or point TEMP/TMP to a local drive.\n", util.VolumeDisplay(targetDir))
		}
	}
	fmt.Println()
}

func countSourcesOnSameVolumeAsTarget(targetDir string, sources []sourceFolderStatus) int {
	count := 0
	for _, src := range sources {
		if src.Err != nil || src.Skip {
			continue
		}
		if util.SameVolume(src.Resolved, targetDir) {
			count++
		}
	}
	return count
}

func validateSourceFolders(sources []sourceFolderStatus) error {
	invalid := 0
	for _, src := range sources {
		if src.Err != nil {
			invalid++
		}
	}
	if invalid > 0 {
		return fmt.Errorf("Backup preflight failed: %d source folder(s) are invalid or inaccessible. Remedy: Fix the [ERROR] entries above and start backup again.", invalid)
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
			fmt.Println("Please enter Y (yes) or N (no). Remedy: Press Enter for yes or type n to cancel.")
		}
	}
}

func yesNo(v bool) string {
	if v {
		return "enabled"
	}
	return "disabled"
}

func backupAuthLabel(cfg *util.Config) string {
	switch cfg.AuthenticationMode {
	case util.AuthModeYubiKey:
		return "YubiKey only (no password)"
	case util.AuthModePasswordYubiKey:
		return "password + YubiKey"
	default:
		return "password only"
	}
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

func estimateSelectedSourceBytes(sources []sourceFolderStatus) (int64, []string) {
	var total int64
	warnings := make([]string, 0)

	for _, source := range sources {
		if source.Err != nil || source.Skip {
			continue
		}

		size, err := directorySizeBytes(source.Resolved)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s (%v)", source.Resolved, err))
			continue
		}
		total += size
	}

	return total, warnings
}

func directorySizeBytes(root string) (int64, error) {
	var total int64

	info, err := os.Stat(root)
	if err != nil {
		return 0, err
	}
	if !info.IsDir() {
		return 0, fmt.Errorf("Path is not a directory. Remedy: Use only directory paths in source_folders.")
	}

	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	if err != nil {
		return total, err
	}

	return total, nil
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
	pr, pw := io.Pipe()
	counters := &backupCounters{}

	progressDone := make(chan struct{})
	progressStopped := make(chan struct{})
	go func() {
		logBackupProgress(log, folderName, &counters.inBytes, &counters.outBytes, &counters.outWriteCalls, cfg.IODiagnostics, progressDone)
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
	log.Debug("Starting TAR creation for: %s", srcDir)
	go func() {
		err := operation.WriteTar(pw, srcDir, targetDir)
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

func logBackupProgress(log *util.Logger, folderName string, inBytes, outBytes, outWriteCalls *atomic.Int64, ioDiagnostics bool, done <-chan struct{}) {
	if !ioDiagnostics {
		<-done // Just wait for completion without logging
		return
	}

	operation.LogProgressUntilDone(log, folderName, "encrypted", inBytes, outBytes, outWriteCalls, done)
}

// copyBackupResults copies all encrypted part files and challenge files from staging directory to target directory.
func copyBackupResults(stagingDir, targetDir string, log *util.Logger) error {
	entries, err := os.ReadDir(stagingDir)
	if err != nil {
		return fmt.Errorf("Failed to list staging directory: %w", err)
	}

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

		if err := operation.CopyFile(srcPath, dstPath); err != nil {
			return fmt.Errorf("Failed to copy %s to target: %w", entry.Name(), err)
		}

		if log != nil {
			log.Debug("Copied staged file to target: %s", entry.Name())
		}
	}

	return nil
}
