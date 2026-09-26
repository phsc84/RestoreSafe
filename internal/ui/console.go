package ui

import (
	"RestoreSafe/internal/catalog"
	"RestoreSafe/internal/security"
	"RestoreSafe/internal/util"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// Console is the terminal UI: questions are numbered text prompts answered on
// stdin, secrets are read without echo, and output goes to stdout.
type Console struct {
	// Out receives the output; nil means os.Stdout.
	Out io.Writer
	// ReadLine and ReadPassword read the answers; nil means the terminal
	// (security.ReadLine and security.ReadPassword). Tests script a session
	// by setting them.
	ReadLine     func(prompt string) (string, error)
	ReadPassword func(prompt string) ([]byte, error)
}

var _ UI = (*Console)(nil)

// stdout writes to the current os.Stdout, so output captured by swapping
// os.Stdout (as tests do) includes console output.
type stdout struct{}

func (stdout) Write(p []byte) (int, error) { return os.Stdout.Write(p) }

// Output returns the console output.
func (c *Console) Output() io.Writer {
	if c.Out != nil {
		return c.Out
	}
	return stdout{}
}

func (c *Console) printf(format string, args ...any) {
	fmt.Fprintf(c.Output(), format, args...)
}

func (c *Console) println(args ...any) {
	fmt.Fprintln(c.Output(), args...)
}

func (c *Console) readLine(prompt string) (string, error) {
	if c.ReadLine != nil {
		return c.ReadLine(prompt)
	}
	return security.ReadLine(prompt)
}

// Password reads a secret without echo.
func (c *Console) Password(prompt string) ([]byte, error) {
	if c.ReadPassword != nil {
		return c.ReadPassword(prompt)
	}
	return security.ReadPassword(prompt)
}

// NewPassword reads a new password twice.
func (c *Console) NewPassword(prompt, confirmPrompt string) ([]byte, error) {
	return security.ReadPasswordConfirmed(c.Password, prompt, confirmPrompt)
}

// SelectBackups lists the backups and reads a run ID, a set name, "." for
// the newest run, or "q" to cancel.
func (c *Console) SelectBackups(action string, runs []catalog.BackupRunSummary) ([]util.BackupEntry, error) {
	for {
		c.printBackupSelectionPrompt(action, runs)

		selection, err := c.readLine("Selection: ")
		if err != nil {
			return nil, err
		}
		c.println()
		selection = strings.TrimSpace(selection)
		if selection == "" {
			c.println("Selection must not be empty.")
			c.println()
			continue
		}

		switch strings.ToLower(selection) {
		case "q":
			return nil, ErrCancelled
		case ".":
			return runs[0].Entries, nil
		}

		selected, err := catalog.ResolveSelection(selection, runs)
		if err != nil {
			c.printf("%v Remedy: Check the ID or name in the list above.\n\n", err)
			continue
		}
		return selected, nil
	}
}

func (c *Console) printBackupSelectionPrompt(action string, runs []catalog.BackupRunSummary) {
	c.println("Available backups:")
	for _, run := range runs {
		c.printf("  - Backup ID: %s / Timestamp (local): %s\n", run.RunID, formatBackupRunTimestamp(run.Created))
		for _, entry := range run.Entries {
			if entry.IsDiff() {
				c.printf("    - %s (differential: full backup %s + changes)\n", entry.String(), entry.ChainID)
				continue
			}
			c.printf("    - %s\n", entry.String())
		}
	}
	c.println()

	completedAction := completedActionLabel(action)
	c.printf("Select backup(s) to %s:\n", action)
	c.printf("  - Enter a dot (.) → newest backup run [backup ID %s]\n", runs[0].RunID)
	c.printf("  - Enter backup ID only (e.g. ABC123) → all directories of this backup run will be %s\n", completedAction)
	c.printf("  - Enter specific backup (e.g. MyDirectory_ABC123_2024-01-15_FULL) → only this directory will be %s\n", completedAction)
	c.printf("  - Enter q → cancel\n")
	c.println()
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

func formatBackupRunTimestamp(ts time.Time) string {
	return ts.Local().Format("2006-01-02 15:04:05 MST")
}

// RestoreDestination reads a path, "." for the backup directory, or "q" to
// cancel.
func (c *Console) RestoreDestination(backupDir string) (string, error) {
	for {
		c.printf("Enter restore destination:\n")
		c.printf("  - Enter a dot (.) → restore in the backup directory itself [%s]\n", backupDir)
		c.printf("  - Enter a specific path (e.g. C:\\Restore) → restore to this directory\n")
		c.printf("  - Enter q → cancel\n")
		c.println()

		restorePath, err := c.readLine("Restore destination: ")
		if err != nil {
			return "", err
		}
		c.println()
		restorePath = strings.TrimSpace(restorePath)

		switch restorePath {
		case "":
			continue
		case "q":
			return "", ErrCancelled
		case ".":
			return backupDir, nil
		}
		return restorePath, nil
	}
}

// ConfirmStart asks a yes/no question; yes is the default.
func (c *Console) ConfirmStart(action string) (bool, error) {
	for {
		c.println()
		answer, err := c.readLine(fmt.Sprintf("Start %s now? [Y/n]: ", action))
		c.println()
		if err != nil {
			return false, err
		}
		switch strings.ToLower(strings.TrimSpace(answer)) {
		case "", "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		default:
			c.println("Please enter y (yes) or n (no).")
		}
	}
}

// ConfirmBackupStart asks whether to start the backup. [F] makes every
// directory a full backup; [K] creates new keys, to change the password,
// replace a lost YubiKey, or get a new recovery code.
func (c *Console) ConfirmBackupStart(opts BackupStartOptions) (BackupStart, error) {
	if !opts.OfferNewKeys {
		ok, err := c.ConfirmStart("backup")
		if !ok || err != nil {
			return BackupCancel, err
		}
		return BackupAsPlanned, nil
	}
	options := "[Y] yes"
	valid := "y (yes)"
	if opts.OfferFull {
		options += " / [F] full backup"
		valid += ", f (full backup)"
	}
	options += " / [K] new keys + full backup / [N] cancel"
	valid += ", k (new keys), or n (no)"
	for {
		c.println()
		answer, err := c.readLine("Start backup now? " + options + ": ")
		c.println()
		if err != nil {
			return BackupCancel, err
		}
		switch strings.ToLower(strings.TrimSpace(answer)) {
		case "", "y", "yes":
			return BackupAsPlanned, nil
		case "f":
			if opts.OfferFull {
				c.println("Every source directory gets a full backup with the current keys.")
				return BackupFull, nil
			}
		case "k":
			c.println("New keys will be created and every source directory gets a full backup. Passwords, YubiKey registrations, and recovery codes of the current keys will not open the new backups (they still open older backups).")
			return BackupNewKeys, nil
		case "n", "no":
			return BackupCancel, nil
		}
		c.printf("Please enter %s.\n", valid)
	}
}

// ChooseUnlockMethod asks for the regular credentials (default) or the
// recovery code.
func (c *Console) ChooseUnlockMethod(regular string) (bool, error) {
	for {
		answer, err := c.readLine(fmt.Sprintf("Unlock with [Y] %s (default) or [R] recovery code? [Y/r]: ", regular))
		if err != nil {
			return false, err
		}
		switch strings.ToLower(strings.TrimSpace(answer)) {
		case "", "y", "yes":
			return false, nil
		case "r":
			return true, nil
		default:
			c.println("Please enter y (password/YubiKey) or r (recovery code).")
		}
	}
}

// ShowRecoveryCode prints the recovery code with instructions for keeping it.
func (c *Console) ShowRecoveryCode(code string) {
	c.println()
	c.println("Your recovery code:")
	c.println()
	c.printf("    %s\n", code)
	c.println()
	c.println("  - This code alone restores every backup made with these keys, even without")
	c.println("    password or YubiKey. Treat it like the key to a safe.")
	c.println("  - Write it down on paper and store it in a safe place, never next to your backups.")
	c.println("  - It is shown only this once.")
	c.println()
}

// RetypeRecoveryCode reads the recovery code typed back by the user.
func (c *Console) RetypeRecoveryCode() (string, error) {
	return c.readLine("Type the recovery code to confirm you wrote it down: ")
}

// WaitForSpareYubiKey waits for Enter; "q" cancels.
func (c *Console) WaitForSpareYubiKey() (bool, error) {
	answer, err := c.readLine("Press Enter when the spare YubiKey is connected (q = cancel): ")
	if err != nil {
		return false, err
	}
	return !strings.EqualFold(strings.TrimSpace(answer), "q"), nil
}

// ShowReport prints the preflight summary.
func (c *Console) ShowReport(r Report) {
	WriteReport(c.Output(), r)
}

// Progress is ignored: the console shows the log lines of each step instead.
func (c *Console) Progress(Progress) {}
