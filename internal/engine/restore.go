// Package restore orchestrates the full restore workflow:
//  1. List available backups in the target folder
//  2. Let the user choose which backup(s) to restore
//  3. Verify password (up to 3 attempts)
//  4. Decrypt and extract to the user-specified restore path
package engine

import (
	"RestoreSafe/internal/security"
	"RestoreSafe/internal/util"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

const maxPasswordAttempts = 3

// Run executes the full restore workflow.
func RunRestore(cfg *util.Config, exeDir string) error {
	targetDir := resolveDir(cfg.TargetFolder, exeDir)

	// Enumerate backups.
	index, err := scanBackups(targetDir)
	if err != nil {
		return fmt.Errorf("Failed to scan target folder %q: %w", targetDir, err)
	}
	if len(index) == 0 {
		fmt.Println("No backups found in target folder.")
		return nil
	}

	selected, selection, err := promptRestoreSelection(index)
	if err != nil {
		return err
	}

	requiresYubiKey, err := backupRunUsesYubiKey(targetDir, selected[0])
	if err != nil {
		return fmt.Errorf("Failed to inspect backup authentication: %w", err)
	}

	log := openOperationLogger(cfg, targetDir, selected[0])
	if log != nil {
		defer log.Close()
	}

	if log != nil {
		log.Info("Restore started – Selection: %q", selection)
	}

	restorePath, err := promptRestoreDestination(targetDir)
	if err != nil {
		if log != nil {
			log.Error("Failed to read restore destination: %v", err)
		}
		return err
	}

	preflight := buildRestorePreflight(selected, targetDir, restorePath)
	printRestorePreflight(targetDir, restorePath, preflight, requiresYubiKey)
	if err := validateRestorePreflight(preflight); err != nil {
		if log != nil {
			log.Error("%v", err)
		}
		return err
	}

	confirmed, err := promptStartRestore()
	if err != nil {
		if log != nil {
			log.Error("Failed to read confirmation: %v", err)
		}
		return err
	}
	if !confirmed {
		if log != nil {
			log.Info("Restore cancelled by user before start")
		}
		fmt.Println("Restore cancelled.")
		return nil
	}

	// Collect password (with retry).
	rep := selected[0]
	password, err := readPasswordWithRetry(targetDir, rep, "Enter restore password: ", log)
	if err != nil {
		if log != nil {
			log.Error("Password input failed: %v", err)
		}
		return err
	}

	fmt.Println("Restore started...")
	if log != nil {
		log.Info("Restore destination: %s", restorePath)
	}

	if err := restoreSelectedEntries(selected, targetDir, restorePath, password, log); err != nil {
		return err
	}

	if log != nil {
		log.Info("Restore completed successfully.")
	}
	fmt.Println("\nRestore completed successfully.")
	return nil
}

func promptRestoreSelection(index []util.BackupEntry) ([]util.BackupEntry, string, error) {
	return promptBackupSelection("restore", index)
}

func promptBackupSelection(action string, index []util.BackupEntry) ([]util.BackupEntry, string, error) {
	fmt.Println("Available backups:")
	entries := sortedEntries(index)
	for _, e := range entries {
		fmt.Printf("  %s\n", e.String())
	}
	fmt.Println()

	fmt.Println("Available backup IDs (with date):")
	for _, item := range sortedBackupIDDates(index) {
		fmt.Printf("  %s  ->  %s\n", item.Date, item.ID)
	}
	fmt.Println()

	fmt.Printf("Select backup(s) to %s:\n", action)
	completedAction := completedActionLabel(action)
	fmt.Printf("  - Enter backup ID only (e.g. ABC123) → all folders with this ID will be %s\n", completedAction)
	fmt.Printf("  - Enter specific backup (e.g. MyFolder_2024-01-15_ABC123) → only this folder will be %s\n", completedAction)
	fmt.Println()

	selection, err := security.ReadLine("Selection: ")
	if err != nil {
		return nil, "", err
	}
	selection = strings.TrimSpace(selection)
	normalized := strings.ToUpper(selection)

	if isRawBackupID(normalized) {
		selected, newestDate, allDates, found := resolveSelectionForIDNewestDate(normalized, index)
		if !found {
			return nil, "", fmt.Errorf("Backup %q not found.", normalized)
		}

		if len(allDates) > 1 {
			confirmed, err := confirmNewestIDSelection(normalized, newestDate, allDates)
			if err != nil {
				return nil, "", err
			}
			if !confirmed {
				return nil, "", fmt.Errorf("Selection cancelled.")
			}
		}

		return selected, normalized, nil
	}

	selected, err := resolveSelection(selection, index)
	if err != nil {
		return nil, "", err
	}
	return selected, selection, nil
}

type backupIDDate struct {
	Date string
	ID   string
}

func sortedBackupIDDates(index []util.BackupEntry) []backupIDDate {
	seen := make(map[string]bool)
	items := make([]backupIDDate, 0)
	for _, e := range index {
		item := backupIDDate{Date: e.Date, ID: string(e.ID)}
		key := item.Date + "|" + item.ID
		if seen[key] {
			continue
		}
		seen[key] = true
		items = append(items, item)
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].Date != items[j].Date {
			return items[i].Date > items[j].Date
		}
		return items[i].ID < items[j].ID
	})

	return items
}

func openOperationLogger(cfg *util.Config, targetDir string, rep util.BackupEntry) *util.Logger {
	logPath := util.LogFileName(targetDir, rep.Date, rep.ID)
	log, err := util.NewLogger(logPath, cfg.LogLevel)
	if err != nil {
		fmt.Printf("Warning: Failed to open log file: %v\n", err)
		return nil
	}
	return log
}

func promptRestoreDestination(targetDir string) (string, error) {
	fmt.Println("Enter restore destination:")
	fmt.Println("  - Enter a specific path (e.g. C:\\Restore) → restore to this directory")
	fmt.Println("  - Enter a dot (.) → restore in the target folder itself")
	fmt.Println()

	restorePath, err := security.ReadLine("Restore destination: ")
	if err != nil {
		return "", err
	}
	restorePath = strings.TrimSpace(restorePath)

	if restorePath == "." {
		restorePath = targetDir
	}
	if restorePath == "" {
		return "", fmt.Errorf("Restore destination must not be empty.")
	}
	return restorePath, nil
}

type restorePreflightItem struct {
	Entry     util.BackupEntry
	PartCount int
	OutputDir string
	Err       error
}

func buildRestorePreflight(selected []util.BackupEntry, targetDir, restorePath string) []restorePreflightItem {
	items := make([]restorePreflightItem, 0, len(selected))
	for _, entry := range selected {
		item := restorePreflightItem{
			Entry:     entry,
			PartCount: len(collectParts(targetDir, entry)),
			OutputDir: filepath.Join(restorePath, entry.FolderName),
		}
		if item.PartCount == 0 {
			item.Err = fmt.Errorf("No part files found")
		}
		if _, err := os.Stat(item.OutputDir); err == nil {
			item.Err = fmt.Errorf("Target directory already exists")
		}
		items = append(items, item)
	}
	return items
}

func printRestorePreflight(targetDir, restorePath string, items []restorePreflightItem, requiresYubiKey bool) {
	fmt.Println()
	fmt.Println("Restore preflight")
	fmt.Println("-----------------")
	fmt.Printf("Backup folder  : %s\n", targetDir)
	fmt.Printf("Restore target : %s\n", restorePath)
	fmt.Printf("Authentication : %s\n", backupAuthenticationLabel(requiresYubiKey))
	fmt.Printf("Items selected : %d\n", len(items))
	fmt.Println("Selection:")
	for _, item := range items {
		if item.Err != nil {
			fmt.Printf("  [ERROR] %s (parts: %d)\n", item.Entry.String(), item.PartCount)
			fmt.Printf("          -> %v\n", item.Err)
			continue
		}
		fmt.Printf("  [OK]    %s (parts: %d)\n", item.Entry.String(), item.PartCount)
	}
	fmt.Println()
}

func validateRestorePreflight(items []restorePreflightItem) error {
	invalid := 0
	for _, item := range items {
		if item.Err != nil {
			invalid++
		}
	}
	if invalid > 0 {
		return fmt.Errorf("Restore preflight failed: %d selected item(s) are invalid", invalid)
	}
	return nil
}

func promptStartRestore() (bool, error) {
	return promptStartAction("restore")
}

func promptStartAction(action string) (bool, error) {
	for {
		answer, err := security.ReadLine(fmt.Sprintf("Start %s now? [Y/n]: ", action))
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

func restoreSelectedEntries(selected []util.BackupEntry, targetDir, restorePath string, password []byte, log *util.Logger) error {
	for _, entry := range selected {
		if err := restoreEntry(entry, targetDir, restorePath, password, log); err != nil {
			if log != nil {
				log.Error("Failed to restore folder %q: %v", entry.String(), err)
			}
			return fmt.Errorf("Failed to restore folder %q: %w", entry.String(), err)
		}
		if log != nil {
			log.Info("Folder %q successfully restored", entry.FolderName)
		}
	}
	return nil
}

// restoreEntry decrypts all parts of one backup entry and extracts to destDir.
func restoreEntry(entry util.BackupEntry, targetDir, destDir string, password []byte, log *util.Logger) error {
	parts := collectParts(targetDir, entry)
	if len(parts) == 0 {
		errMsg := fmt.Sprintf("No part files found for %s", entry.String())
		return fmt.Errorf("%s", errMsg)
	}

	if log != nil {
		log.Info("Processing %d part file(s) for %s", len(parts), entry.String())
	}

	seqReader := util.NewSequentialReader(parts)
	defer seqReader.Close()

	var inBytes atomic.Int64
	var outBytes atomic.Int64
	var outWriteCalls atomic.Int64
	progressDone := make(chan struct{})
	go logRestoreProgress(log, entry.FolderName, &inBytes, &outBytes, &outWriteCalls, progressDone)
	defer close(progressDone)

	// Verify target directory can be created before starting decryption.
	outDir := filepath.Join(destDir, entry.FolderName)
	if err := os.MkdirAll(outDir, 0o750); err != nil {
		errMsg := fmt.Sprintf("Failed to create target directory: %v", err)
		return fmt.Errorf("%s", errMsg)
	}

	if log != nil {
		log.Info("Extracting to: %s", outDir)
	}

	// Pipe: decrypt → TAR extract.
	pr, pw := io.Pipe()

	decErrCh := make(chan error, 1)
	go func() {
		err := security.Decrypt(
			&countingWriter{w: pw, total: &outBytes, calls: &outWriteCalls},
			&countingReader{r: seqReader, total: &inBytes},
			password,
		)
		pw.CloseWithError(err) //nolint:errcheck
		decErrCh <- err
	}()

	extractErr := ExtractTar(pr, outDir)
	if extractErr != nil {
		pr.CloseWithError(extractErr) //nolint:errcheck
	}
	// Note: Do NOT close pr here. The decrypt goroutine will close pw,
	// signaling EOF to the reader. Closing pr prematurely causes "write on closed pipe".
	decErr := <-decErrCh

	if decErr != nil {
		if errors.Is(decErr, security.ErrWrongPassword) {
			return security.ErrWrongPassword
		}
		errMsg := fmt.Sprintf("Decryption failed: %v", decErr)
		return fmt.Errorf("%s", errMsg)
	}
	if extractErr != nil {
		errMsg := fmt.Sprintf("Extraction failed: %v", extractErr)
		return fmt.Errorf("%s", errMsg)
	}

	return nil
}

func logRestoreProgress(log *util.Logger, folderName string, inBytes, outBytes, outWriteCalls *atomic.Int64, done <-chan struct{}) {
	if log == nil {
		<-done // Just wait for completion without logging
		return
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			logStreamProgress(log, folderName, "decrypted", inBytes, outBytes, outWriteCalls, true)
			return
		case <-ticker.C:
			logStreamProgress(log, folderName, "decrypted", inBytes, outBytes, outWriteCalls, false)
		}
	}
}

// readPasswordWithRetry asks for the password up to maxPasswordAttempts times.
// It verifies the password by attempting to decrypt the first byte of the first part.
func readPasswordWithRetry(
	targetDir string,
	rep util.BackupEntry,
	passwordPrompt string,
	log *util.Logger,
) ([]byte, error) {
	challengePath, requiresYubiKey, err := findChallengeFileForRun(targetDir, rep.Date, rep.ID)
	if err != nil {
		return nil, err
	}

	for attempt := 1; attempt <= maxPasswordAttempts; attempt++ {
		password, err := security.ReadPassword(passwordPrompt)
		if err != nil {
			return nil, err
		}

		if requiresYubiKey {
			challengeHex, err := readChallengeFile(challengePath)
			if err != nil {
				return nil, fmt.Errorf("YubiKey challenge file not found: %w", err)
			}
			fmt.Println("YubiKey detected – please touch YubiKey button...")
			password, err = security.CombineWithPasswordForRestore(password, challengeHex)
			if err != nil {
				return nil, fmt.Errorf("YubiKey authentication failed: %w", err)
			}
			if log != nil {
				log.Info("YubiKey-2FA successful. Challenge: %s", challengeHex)
			}
		}

		// Verify password by attempting a trial decrypt.
		parts := collectParts(targetDir, rep)
		if len(parts) > 0 {
			if err := verifyPassword(parts[0], password); err == nil {
				return password, nil
			} else if errors.Is(err, security.ErrWrongPassword) {
				remaining := maxPasswordAttempts - attempt
				if remaining > 0 {
					fmt.Printf("%s %d attempt(s) remaining.\n", passwordFailurePrefix(requiresYubiKey), remaining)
					if log != nil {
						log.Warn("Wrong password or invalid second factor – attempt %d/%d", attempt, maxPasswordAttempts)
					}
				}
				continue
			} else {
				return nil, err
			}
		}

		// If no part file was found, accept the password and let the caller fail later.
		return password, nil
	}

	if requiresYubiKey {
		return nil, fmt.Errorf("Too many failed authentication attempts – application will now exit.")
	}
	return nil, fmt.Errorf("Too many wrong password attempts – application will now exit.")
}

// verifyPassword attempts to decrypt the first chunk of a part file to check the password.
// It reads at most one byte of plaintext to confirm authentication succeeded.
func verifyPassword(partPath string, password []byte) error {
	f, err := os.Open(partPath)
	if err != nil {
		return fmt.Errorf("Failed to open file: %w", err)
	}
	defer f.Close()

	// Use a small writer that accepts the first write and then returns a
	// sentinel error to stop `Decrypt`. This avoids races with pipes and
	// lets us detect a successful authentication quickly.
	var errVerifyStop = errors.New("verify-stop")

	err = security.Decrypt(&verifyWriter{errVerifyStop: errVerifyStop}, f, password)
	if err == nil {
		// Decrypt finished without error (small file) — password is valid.
		return nil
	}
	if errors.Is(err, errVerifyStop) {
		// Our sentinel error indicates we stopped after successful auth.
		return nil
	}
	return err
}

// scanBackups walks targetDir and builds an index of all backup entries.
func scanBackups(targetDir string) ([]util.BackupEntry, error) {
	entries, err := os.ReadDir(targetDir)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	var result []util.BackupEntry

	for _, de := range entries {
		if de.IsDir() {
			continue
		}
		entry, _, ok := util.ParsePartFileName(de.Name())
		if !ok {
			continue
		}
		key := entry.String()
		if !seen[key] {
			seen[key] = true
			result = append(result, entry)
		}
	}

	return result, nil
}

// collectParts returns the sorted part file paths for an entry.
func collectParts(targetDir string, entry util.BackupEntry) []string {
	des, err := os.ReadDir(targetDir)
	if err != nil {
		return nil
	}

	type seqPath struct {
		seq  int
		path string
	}
	var parts []seqPath

	for _, de := range des {
		e, seq, ok := util.ParsePartFileName(de.Name())
		if !ok {
			continue
		}
		if e.FolderName != entry.FolderName || e.Date != entry.Date || e.ID != entry.ID {
			continue
		}
		parts = append(parts, seqPath{seq, filepath.Join(targetDir, de.Name())})
	}

	sort.Slice(parts, func(i, j int) bool { return parts[i].seq < parts[j].seq })

	paths := make([]string, len(parts))
	for i, p := range parts {
		paths[i] = p.path
	}
	return paths
}

// resolveSelection maps the user's input to one or more BackupEntry values.
func resolveSelection(input string, index []util.BackupEntry) ([]util.BackupEntry, error) {
	input = strings.TrimSpace(strings.ToUpper(input))

	if isRawBackupID(input) {
		matched, _, _, found := resolveSelectionForIDNewestDate(input, index)
		if found {
			return matched, nil
		}
	}

	// Try exact match on full name (case-insensitive).
	for _, e := range index {
		if strings.EqualFold(e.String(), input) {
			return []util.BackupEntry{e}, nil
		}
	}

	return nil, fmt.Errorf("Backup %q not found.", input)
}

func resolveSelectionForIDNewestDate(id string, index []util.BackupEntry) ([]util.BackupEntry, string, []string, bool) {
	matchedByDate := make(map[string][]util.BackupEntry)
	for _, entry := range index {
		if string(entry.ID) != id {
			continue
		}
		matchedByDate[entry.Date] = append(matchedByDate[entry.Date], entry)
	}
	if len(matchedByDate) == 0 {
		return nil, "", nil, false
	}

	allDates := make([]string, 0, len(matchedByDate))
	for date := range matchedByDate {
		allDates = append(allDates, date)
	}
	sort.Slice(allDates, func(i, j int) bool {
		return allDates[i] > allDates[j]
	})

	newestDate := allDates[0]
	return matchedByDate[newestDate], newestDate, allDates, true
}

func isRawBackupID(input string) bool {
	if len(input) != 6 {
		return false
	}
	for _, r := range input {
		if (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

func confirmNewestIDSelection(id, newestDate string, allDates []string) (bool, error) {
	fmt.Printf("Backup ID %s exists on multiple dates: %s\n", id, strings.Join(allDates, ", "))
	for {
		answer, err := security.ReadLine(fmt.Sprintf("Continue with newest date %s only? [Y/n]: ", newestDate))
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

// sortedEntries returns index sorted by date desc, then folder name.
func sortedEntries(index []util.BackupEntry) []util.BackupEntry {
	sorted := make([]util.BackupEntry, len(index))
	copy(sorted, index)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Date != sorted[j].Date {
			return sorted[i].Date > sorted[j].Date
		}
		return sorted[i].FolderName < sorted[j].FolderName
	})
	return sorted
}

type verifyWriter struct {
	done          bool
	errVerifyStop error
}

func (vw *verifyWriter) Write(p []byte) (int, error) {
	if vw.done {
		return 0, vw.errVerifyStop
	}
	vw.done = true
	// Indicate we consumed the data.
	return len(p), nil
}

func readChallengeFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func backupRunUsesYubiKey(targetDir string, entry util.BackupEntry) (bool, error) {
	_, found, err := findChallengeFileForRun(targetDir, entry.Date, entry.ID)
	return found, err
}

func findChallengeFileForRun(targetDir, date string, id util.BackupID) (string, bool, error) {
	entries, err := os.ReadDir(targetDir)
	if err != nil {
		return "", false, err
	}

	suffix := fmt.Sprintf("_%s_%s.challenge", date, string(id))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasSuffix(entry.Name(), suffix) {
			return filepath.Join(targetDir, entry.Name()), true, nil
		}
	}

	return "", false, nil
}

func backupAuthenticationLabel(requiresYubiKey bool) string {
	if requiresYubiKey {
		return "password + YubiKey (detected)"
	}
	return "password only"
}

func passwordFailurePrefix(requiresYubiKey bool) string {
	if requiresYubiKey {
		return "Wrong password or invalid YubiKey response."
	}
	return "Wrong password."
}

func completedActionLabel(action string) string {
	switch action {
	case "restore":
		return "restored"
	case "verify":
		return "verified"
	default:
		return action + "ed"
	}
}
