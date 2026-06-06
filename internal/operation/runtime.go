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

func OpenLogger(cfg *util.Config, backupDir string, rep util.BackupEntry) *util.Logger {
	logPath := util.LogFileName(backupDir, rep.Date, rep.ID)
	log, err := util.NewLogger(logPath, cfg.LogLevel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to open log file: %v. Remedy: Check write permissions in backup directory; operation continues without a log file.\n", err)
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
	backupDir string,
	rep util.BackupEntry,
	passwordPrompt string,
	log *util.Logger,
) ([]byte, error) {
	challengePath, requiresYubiKey, err := catalog.FindChallengeFileForRun(backupDir, rep.Date, rep.ID)
	if err != nil {
		return nil, err
	}

	// Determine whether this is a password-less YubiKey-only backup.
	yubiKeyOnly := false
	challengeJSON := ""
	if requiresYubiKey {
		yubiKeyOnly, err = catalog.IsChallengeFileYubiKeyOnly(challengePath)
		if err != nil {
			return nil, fmt.Errorf("YubiKey challenge file has invalid format: %w. Remedy: Ensure the matching .challenge file is unchanged and belongs to the same backup run as the .enc files.", err)
		}
		challengeJSON, err = readChallengeFile(challengePath)
		if err != nil {
			return nil, fmt.Errorf("YubiKey challenge file not found: %w. Remedy: Ensure the matching .challenge file is in the same directory as the .enc files.", err)
		}
	}

	// Derive the FIDO2 hmac-secret once, before the password retry loop. The
	// secret depends only on the credential ID and salt in the challenge file, so
	// it is identical for every attempt; deriving it once means a mistyped
	// password no longer forces another YubiKey PIN/touch on each retry.
	var fido2Secret []byte
	if requiresYubiKey {
		if err := security.CheckYubiKeyConnected(); err != nil {
			return nil, security.ErrYubiKeyRequired
		}
		fmt.Println("YubiKey connected. Follow the on-screen prompts to authenticate.")
		fido2Secret, err = security.DeriveFIDO2SecretForRestore(challengeJSON)
		if err != nil {
			return nil, fmt.Errorf("YubiKey authentication failed: %w", err)
		}
		defer security.ZeroBytes(fido2Secret)
		if yubiKeyOnly {
			log.InfoLogOnly("YubiKey-only authentication successful.")
		} else {
			log.InfoLogOnly("YubiKey-2FA successful.")
		}
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
			combined := security.CombinePasswordWithSecret(password, fido2Secret)
			security.ZeroBytes(password)
			password = combined
		}

		// Verify password by attempting a trial decrypt.
		parts, err := catalog.CollectParts(backupDir, rep)
		if err != nil {
			security.ZeroBytes(password)
			return nil, err
		}
		if len(parts) > 0 {
			if err := verifyPassword(parts[0], password); err == nil {
				return password, nil // caller is responsible for zeroing
			} else if errors.Is(err, security.ErrWrongPassword) {
				security.ZeroBytes(password)
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
				security.ZeroBytes(password)
				return nil, err
			}
		}

		// If no part file was found, accept the password and let the caller fail later.
		return password, nil // caller is responsible for zeroing
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
		return fmt.Errorf("Failed to open file: %w", err)
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

// readChallengeFile reads and validates the FIDO2 challenge JSON from a .challenge file.
func readChallengeFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	content := strings.TrimSpace(string(data))
	if content == "" {
		return "", fmt.Errorf("challenge file is empty. Remedy: The .challenge file may be corrupted or truncated; restore it from a backup of the backup directory.")
	}
	if err := security.ValidateChallengeJSON(content); err != nil {
		return "", fmt.Errorf("challenge file has invalid format: %w. Remedy: The .challenge file may be corrupted; restore it from a backup of the backup directory.", err)
	}
	return content, nil
}
