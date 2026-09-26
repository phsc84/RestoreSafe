package operation

import (
	"RestoreSafe/internal/util"
	"fmt"
	"io"
)

// OpenLogger opens the log file of the backup run date/runID in backupDir,
// falling back to a console-only logger when the file cannot be opened.
// Messages are mirrored to console.
func OpenLogger(cfg *util.Config, backupDir, date string, runID util.BackupID, console io.Writer) *util.Logger {
	logPath := util.LogFileName(backupDir, date, runID)
	log, err := util.NewLogger(logPath, cfg.LogLevel, console)
	if err != nil {
		fmt.Fprintf(console, "Warning: Failed to open log file: %v. Remedy: Check write permissions in backup directory; operation continues without a log file.\n", err)
		return util.NewConsoleLogger(cfg.LogLevel, console)
	}
	return log
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
