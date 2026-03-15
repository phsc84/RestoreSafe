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
		return fmt.Errorf("Failed to scan target folder %q: %w. Remedy: Check the target_folder path in config.yaml and ensure the folder exists and is readable.", targetDir, err)
	}
	if len(index) == 0 {
		fmt.Println("No backups found in target folder. Remedy: Check whether .enc files are in target_folder and whether the correct folder is configured.")
		return nil
	}

	selected, selection, err := promptRestoreSelection(targetDir, index)
	if err != nil {
		return err
	}
	warningCount := restoreSelectionWarningCount(selection, index)

	requiresYubiKey, err := backupRunUsesYubiKey(targetDir, selected[0])
	if err != nil {
		return fmt.Errorf("Failed to inspect backup authentication: %w. Remedy: Check read permissions in the backup folder and verify .challenge filenames.", err)
	}

	logPath := util.LogFileName(targetDir, selected[0].Date, selected[0].ID)
	log := openOperationLogger(cfg, targetDir, selected[0])
	if log == nil {
		warningCount++
	}
	if log != nil {
		defer log.Close()
	}

	if log != nil {
		log.Info("Restore started - Selection: %q", selection)
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

	fmt.Println("Restore started.")
	if log != nil {
		log.Info("Restore destination: %s", restorePath)
	}

	totalPartsProcessed, err := restoreSelectedEntries(selected, targetDir, restorePath, password, log)
	if err != nil {
		return err
	}

	if log != nil {
		log.Info("Restore completed successfully.")
	}
	printRestoreCompletionSummary(selected, totalPartsProcessed, logPath, warningCount)
	fmt.Println("\nRestore completed successfully.")
	return nil
}

func openOperationLogger(cfg *util.Config, targetDir string, rep util.BackupEntry) *util.Logger {
	logPath := util.LogFileName(targetDir, rep.Date, rep.ID)
	log, err := util.NewLogger(logPath, cfg.LogLevel)
	if err != nil {
		fmt.Printf("Warning: Failed to open log file: %v. Remedy: Check write permissions in target_folder; operation continues without a log file.\n", err)
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
		return "", fmt.Errorf("Restore destination must not be empty. Remedy: Provide a destination folder (e.g. C:/Restore) or '.' for target_folder.")
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
			item.Err = fmt.Errorf("No part files found. Remedy: Ensure all .enc parts of this backup are in the same target_folder.")
		}
		if _, err := os.Stat(item.OutputDir); err == nil {
			item.Err = fmt.Errorf("Target directory already exists. Remedy: Choose a different restore destination or rename/delete the existing target directory.")
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
		return fmt.Errorf("Restore preflight failed: %d selected item(s) are invalid. Remedy: Fix the [ERROR] entries above and start restore again.", invalid)
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
			fmt.Println("Please enter Y (yes) or N (no). Remedy: Press Enter for yes or type n to cancel.")
		}
	}
}

func restoreSelectedEntries(selected []util.BackupEntry, targetDir, restorePath string, password []byte, log *util.Logger) (int, error) {
	totalPartsProcessed := 0
	for _, entry := range selected {
		partCount, err := restoreEntry(entry, targetDir, restorePath, password, log)
		if err != nil {
			if log != nil {
				log.Error("Failed to restore folder %q: %v", entry.String(), err)
			}
			return 0, fmt.Errorf("Failed to restore folder %q: %w. Remedy: Check part files, password/YubiKey, and destination folder.", entry.String(), err)
		}
		totalPartsProcessed += partCount
		if log != nil {
			log.Info("Folder %q successfully restored", entry.FolderName)
		}
	}
	return totalPartsProcessed, nil
}

// restoreEntry decrypts all parts of one backup entry and extracts to destDir.
func restoreEntry(entry util.BackupEntry, targetDir, destDir string, password []byte, log *util.Logger) (int, error) {
	parts := collectParts(targetDir, entry)
	if len(parts) == 0 {
		errMsg := fmt.Sprintf("No part files found for %s", entry.String())
		return 0, fmt.Errorf("%s. Remedy: Put all related .enc files into the same target_folder.", errMsg)
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
		return 0, fmt.Errorf("%s. Remedy: Check write permissions and use a valid destination path.", errMsg)
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
			return 0, security.ErrWrongPassword
		}
		errMsg := fmt.Sprintf("Decryption failed: %v", decErr)
		return 0, fmt.Errorf("%s. Remedy: Check the password; for YubiKey backups, the matching .challenge file must be in the same folder as the .enc files.", errMsg)
	}
	if extractErr != nil {
		errMsg := fmt.Sprintf("Extraction failed: %v", extractErr)
		return 0, fmt.Errorf("%s. Remedy: Check backup completeness and use an empty destination folder.", errMsg)
	}

	return len(parts), nil
}

func restoreSelectionWarningCount(selection string, index []util.BackupEntry) int {
	normalized := strings.ToUpper(strings.TrimSpace(selection))
	if !isRawBackupID(normalized) {
		return 0
	}

	_, _, allDates, found := resolveSelectionForIDNewestDate(normalized, index)
	if !found {
		return 0
	}
	if len(allDates) > 1 {
		return 1
	}
	return 0
}

func printRestoreCompletionSummary(selected []util.BackupEntry, totalPartsProcessed int, logPath string, warningCount int) {
	folderNames := make([]string, 0, len(selected))
	for _, entry := range selected {
		folderNames = append(folderNames, entry.FolderName)
	}

	fmt.Println()
	fmt.Println("Restore summary")
	fmt.Println("---------------")
	fmt.Printf("Processed folders: %d (%s)\n", len(selected), summarizeNames(folderNames))
	fmt.Printf("Parts processed  : %d\n", totalPartsProcessed)
	fmt.Printf("Log file         : %s\n", logPath)
	if warningCount > 0 {
		fmt.Printf("Warnings         : %d\n", warningCount)
	} else {
		fmt.Println("Warnings         : none")
	}
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
				return nil, fmt.Errorf("YubiKey challenge file not found: %w. Remedy: Ensure the matching .challenge file is in the same folder as the .enc files.", err)
			}
			fmt.Println("YubiKey detected. Please touch the YubiKey button.")
			password, err = security.CombineWithPasswordForRestore(password, challengeHex)
			if err != nil {
				return nil, fmt.Errorf("YubiKey authentication failed: %w. Remedy: Connect the YubiKey, touch it, and verify slot 2 is configured correctly.", err)
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
						log.Warn("Wrong password or invalid second factor; attempt %d/%d", attempt, maxPasswordAttempts)
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
		return nil, fmt.Errorf("Too many failed authentication attempts. Application will now exit. Remedy: Restart and check password plus YubiKey setup (slot 2, touch).")
	}
	return nil, fmt.Errorf("Too many wrong password attempts. Application will now exit. Remedy: Restart and enter the correct backup password.")
}

// verifyPassword attempts to decrypt the first chunk of a part file to check the password.
// It reads at most one byte of plaintext to confirm authentication succeeded.
func verifyPassword(partPath string, password []byte) error {
	f, err := os.Open(partPath)
	if err != nil {
		return fmt.Errorf("Failed to open file: %w. Remedy: Check that the file exists and is readable.", err)
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
