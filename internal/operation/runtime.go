package operation

import (
	"RestoreSafe/internal/catalog"
	"RestoreSafe/internal/security"
	"RestoreSafe/internal/util"
	"errors"
	"fmt"
	"os"
	"strings"
)

const maxPasswordAttempts = 3

var readLineFn = security.ReadLine

func OpenLogger(cfg *util.Config, targetDir string, rep util.BackupEntry) *util.Logger {
	logPath := util.LogFileName(targetDir, rep.Date, rep.ID)
	log, err := util.NewLogger(logPath, cfg.LogLevel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to open log file: %v. Remedy: Check write permissions in target_folder; operation continues without a log file.\n", err)
		return util.NewConsoleLogger(cfg.LogLevel)
	}
	return log
}

func PromptStartAction(action string) (bool, error) {
	for {
		fmt.Println()
		answer, err := readLineFn(fmt.Sprintf("Start %s now? [Y/n]: ", action))
		fmt.Println()
		if err != nil {
			return false, err
		}
		switch strings.ToLower(strings.TrimSpace(answer)) {
		case "", "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		default:
			fmt.Println("Please enter y (yes) or n (no).")
		}
	}
}

func BackupAuthenticationLabel(requiresYubiKey, yubiKeyOnly bool) string {
	switch {
	case yubiKeyOnly:
		return "YubiKey only (no password)"
	case requiresYubiKey:
		return "password + YubiKey"
	default:
		return "password only"
	}
}

func PasswordFailurePrefix(requiresYubiKey, yubiKeyOnly bool) string {
	switch {
	case yubiKeyOnly:
		return "Wrong YubiKey or corrupted file."
	case requiresYubiKey:
		return "Wrong password or invalid YubiKey response."
	default:
		return "Wrong password."
	}
}

// ReadPasswordWithRetry asks for the password up to maxPasswordAttempts times.
// It verifies the password by attempting to decrypt the first byte of the first part.
// In YubiKey-only mode (no password factor), the retry loop runs at most once since
// there is no password that can be corrected between attempts.
func ReadPasswordWithRetry(
	targetDir string,
	rep util.BackupEntry,
	passwordPrompt string,
	log *util.Logger,
) ([]byte, error) {
	challengePath, requiresYubiKey, err := catalog.FindChallengeFileForRun(targetDir, rep.Date, rep.ID)
	if err != nil {
		return nil, err
	}

	// Determine whether this is a password-less YubiKey-only backup.
	yubiKeyOnly := false
	if requiresYubiKey {
		yubiKeyOnly = catalog.IsChallengeFileYubiKeyOnly(challengePath)
	}

	for attempt := 1; attempt <= maxPasswordAttempts; attempt++ {
		var password []byte
		if yubiKeyOnly {
			// No password prompt in YubiKey-only mode.
			password = []byte{}
		} else {
			password, err = security.ReadPassword(passwordPrompt)
			if err != nil {
				return nil, err
			}
		}

		if requiresYubiKey {
			challengeHex, err := readChallengeFile(challengePath)
			if err != nil {
				return nil, fmt.Errorf("YubiKey challenge file not found: %w. Remedy: Ensure the matching .challenge file is in the same folder as the .enc files.", err)
			}
			// Verify ykman is installed and a device is physically connected.
			if err := security.CheckYubiKeyConnected(); err != nil {
				return nil, fmt.Errorf("YubiKey is required but no YubiKey was detected. Remedy: Connect the YubiKey and retry.")
			}
			fmt.Println("YubiKey connected. Please touch the YubiKey button.")
			password, err = security.CombineWithPasswordForRestore(password, challengeHex)
			if err != nil {
				return nil, fmt.Errorf("YubiKey authentication failed: %w", err)
			}
			if yubiKeyOnly {
				log.Info("YubiKey-only authentication successful. Challenge: %s", challengeHex)
			} else {
				log.Info("YubiKey-2FA successful. Challenge: %s", challengeHex)
			}
		}

		// Verify password by attempting a trial decrypt.
		parts, err := catalog.CollectParts(targetDir, rep)
		if err != nil {
			return nil, err
		}
		if len(parts) > 0 {
			if err := verifyPassword(parts[0], password); err == nil {
				return password, nil
			} else if errors.Is(err, security.ErrWrongPassword) {
				// In YubiKey-only mode there is no password to correct, so return immediately.
				if yubiKeyOnly {
					return nil, fmt.Errorf("YubiKey authentication failed: wrong key or corrupted file.")
				}
				remaining := maxPasswordAttempts - attempt
				if remaining > 0 {
					fmt.Printf("%s %d attempt(s) remaining.\n", PasswordFailurePrefix(requiresYubiKey, yubiKeyOnly), remaining)
					log.WarnLogOnly("Wrong password or invalid second factor; attempt %d/%d", attempt, maxPasswordAttempts)
				}
				continue
			} else {
				return nil, err
			}
		}

		// If no part file was found, accept the password and let the caller fail later.
		return password, nil
	}

	if yubiKeyOnly {
		return nil, fmt.Errorf("YubiKey authentication failed.")
	}
	if requiresYubiKey {
		return nil, fmt.Errorf("Too many failed authentication attempts.")
	}
	return nil, fmt.Errorf("Too many wrong password attempts.")
}

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
		// Decrypt finished without error (small file) - password is valid.
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

// readChallengeFile reads the challenge hex from a .challenge file.
// It strips the "NOPW:" prefix used by YubiKey-only backups so the caller
// always receives a plain hex string suitable for CombineWithPasswordForRestore.
func readChallengeFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	content := strings.TrimSpace(string(data))
	content = strings.TrimPrefix(content, "NOPW:")
	return content, nil
}
