package operation

import (
	"RestoreSafe/internal/security"
	"RestoreSafe/internal/util"
	"fmt"
	"os"
	"strings"
)

var readLineFn = security.ReadLine

// OpenLogger opens the log file of the backup run date/runID in backupDir,
// falling back to a console-only logger when the file cannot be opened.
func OpenLogger(cfg *util.Config, backupDir, date string, runID util.BackupID) *util.Logger {
	logPath := util.LogFileName(backupDir, date, runID)
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
