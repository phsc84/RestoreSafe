// Package interacttest provides Script, a text implementation of interact.UI
// for tests of the workflows.
package interacttest

import (
	"RestoreSafe/internal/format/catalog"
	"RestoreSafe/internal/format/naming"
	"RestoreSafe/internal/workflow/interact"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// Script is a text UI for tests: questions are text prompts whose answers
// come from ReadLine and ReadPassword, and output goes to Out. It reads
// nothing from the terminal.
type Script struct {
	// Out receives the output; nil means os.Stdout.
	Out io.Writer
	// ReadLine and ReadPassword answer the prompts, e.g. Answers("y"). A
	// prompt without an answer function is an error that names the prompt.
	ReadLine     func(prompt string) (string, error)
	ReadPassword func(prompt string) ([]byte, error)
}

var _ interact.UI = (*Script)(nil)

// stdout writes to the current os.Stdout, so output captured by swapping
// os.Stdout (as tests do) includes the script output.
type stdout struct{}

func (stdout) Write(p []byte) (int, error) { return os.Stdout.Write(p) }

// Output returns where the script writes its output.
func (s *Script) Output() io.Writer {
	if s.Out != nil {
		return s.Out
	}
	return stdout{}
}

func (s *Script) printf(format string, args ...any) {
	fmt.Fprintf(s.Output(), format, args...)
}

func (s *Script) println(args ...any) {
	fmt.Fprintln(s.Output(), args...)
}

func (s *Script) readLine(prompt string) (string, error) {
	if s.ReadLine != nil {
		return s.ReadLine(prompt)
	}
	return "", fmt.Errorf("no scripted answer for %q", prompt)
}

// Password answers a secret prompt.
func (s *Script) Password(prompt string) ([]byte, error) {
	if s.ReadPassword != nil {
		return s.ReadPassword(prompt)
	}
	return nil, fmt.Errorf("no scripted answer for %q", prompt)
}

// NewPassword reads a new password twice.
func (s *Script) NewPassword(prompt, confirmPrompt string) ([]byte, error) {
	return interact.ReadPasswordConfirmed(s.Password, prompt, confirmPrompt)
}

// SelectBackups lists the backups and reads a run ID, a set name, "." for
// the newest run, or "q" to cancel.
func (s *Script) SelectBackups(action string, runs []catalog.BackupRunSummary) ([]naming.BackupEntry, error) {
	for {
		s.printBackupSelectionPrompt(action, runs)

		selection, err := s.readLine("Selection: ")
		if err != nil {
			return nil, err
		}
		s.println()
		selection = strings.TrimSpace(selection)
		if selection == "" {
			s.println("Selection must not be empty.")
			s.println()
			continue
		}

		switch strings.ToLower(selection) {
		case "q":
			return nil, interact.ErrCancelled
		case ".":
			return runs[0].Entries, nil
		}

		selected, err := catalog.ResolveSelection(selection, runs)
		if err != nil {
			s.printf("%v Remedy: Check the ID or name in the list above.\n\n", err)
			continue
		}
		return selected, nil
	}
}

func (s *Script) printBackupSelectionPrompt(action string, runs []catalog.BackupRunSummary) {
	s.println("Available backups:")
	for _, run := range runs {
		s.printf("  - Backup ID: %s / Timestamp (local): %s\n", run.RunID, formatBackupRunTimestamp(run.Created))
		for _, entry := range run.Entries {
			if entry.IsDiff() {
				s.printf("    - %s (differential: full backup %s + changes)\n", entry.String(), entry.ChainID)
				continue
			}
			s.printf("    - %s\n", entry.String())
		}
	}
	s.println()

	completedAction := completedActionLabel(action)
	s.printf("Select backup(s) to %s:\n", action)
	s.printf("  - Enter a dot (.) → newest backup run [backup ID %s]\n", runs[0].RunID)
	s.printf("  - Enter backup ID only (e.g. ABC123) → all directories of this backup run will be %s\n", completedAction)
	s.printf("  - Enter specific backup (e.g. MyDirectory_ABC123_2024-01-15_FULL) → only this directory will be %s\n", completedAction)
	s.printf("  - Enter q → cancel\n")
	s.println()
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
func (s *Script) RestoreDestination(backupDir string) (string, error) {
	for {
		s.printf("Enter restore destination:\n")
		s.printf("  - Enter a dot (.) → restore in the backup directory itself [%s]\n", backupDir)
		s.printf("  - Enter a specific path (e.g. C:\\Restore) → restore to this directory\n")
		s.printf("  - Enter q → cancel\n")
		s.println()

		restorePath, err := s.readLine("Restore destination: ")
		if err != nil {
			return "", err
		}
		s.println()
		restorePath = strings.TrimSpace(restorePath)

		switch restorePath {
		case "":
			continue
		case "q":
			return "", interact.ErrCancelled
		case ".":
			return backupDir, nil
		}
		return restorePath, nil
	}
}

// ConfirmStart asks a yes/no question; yes is the default.
func (s *Script) ConfirmStart(action string) (bool, error) {
	for {
		s.println()
		answer, err := s.readLine(fmt.Sprintf("Start %s now? [Y/n]: ", action))
		s.println()
		if err != nil {
			return false, err
		}
		switch strings.ToLower(strings.TrimSpace(answer)) {
		case "", "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		default:
			s.println("Please enter y (yes) or n (no).")
		}
	}
}

// ConfirmBackupStart asks whether to start the backup. [F] makes every
// directory a full backup; [K] creates new keys, to change the password,
// replace a lost YubiKey, or get a new recovery code.
func (s *Script) ConfirmBackupStart(opts interact.BackupStartOptions) (interact.BackupStart, error) {
	if !opts.OfferNewKeys {
		ok, err := s.ConfirmStart("backup")
		if !ok || err != nil {
			return interact.BackupCancel, err
		}
		return interact.BackupAsPlanned, nil
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
		s.println()
		answer, err := s.readLine("Start backup now? " + options + ": ")
		s.println()
		if err != nil {
			return interact.BackupCancel, err
		}
		switch strings.ToLower(strings.TrimSpace(answer)) {
		case "", "y", "yes":
			return interact.BackupAsPlanned, nil
		case "f":
			if opts.OfferFull {
				s.println("Every source directory gets a full backup with the current keys.")
				return interact.BackupFull, nil
			}
		case "k":
			s.println("New keys will be created and every source directory gets a full backup. Passwords, YubiKey registrations, and recovery codes of the current keys will not open the new backups (they still open older backups).")
			return interact.BackupNewKeys, nil
		case "n", "no":
			return interact.BackupCancel, nil
		}
		s.printf("Please enter %s.\n", valid)
	}
}

// ChooseUnlockMethod asks for the regular credentials (default) or the
// recovery code.
func (s *Script) ChooseUnlockMethod(regular string) (bool, error) {
	for {
		answer, err := s.readLine(fmt.Sprintf("Unlock with [Y] %s (default) or [R] recovery code? [Y/r]: ", regular))
		if err != nil {
			return false, err
		}
		switch strings.ToLower(strings.TrimSpace(answer)) {
		case "", "y", "yes":
			return false, nil
		case "r":
			return true, nil
		default:
			s.println("Please enter y (password/YubiKey) or r (recovery code).")
		}
	}
}

// ShowRecoveryCode prints the recovery code with instructions for keeping it.
func (s *Script) ShowRecoveryCode(code string) {
	s.println()
	s.println("Your recovery code:")
	s.println()
	s.printf("    %s\n", code)
	s.println()
	s.println("  - This code alone restores every backup made with these keys, even without")
	s.println("    password or YubiKey. Treat it like the key to a safe.")
	s.println("  - Write it down on paper and store it in a safe place, never next to your backups.")
	s.println("  - It is shown only this once.")
	s.println()
}

// RetypeRecoveryCode reads the recovery code typed back by the user.
func (s *Script) RetypeRecoveryCode() (string, error) {
	return s.readLine("Type the recovery code to confirm you wrote it down: ")
}

// WaitForSpareYubiKey waits for Enter; "q" cancels.
func (s *Script) WaitForSpareYubiKey() (bool, error) {
	answer, err := s.readLine("Press Enter when the spare YubiKey is connected (q = cancel): ")
	if err != nil {
		return false, err
	}
	return !strings.EqualFold(strings.TrimSpace(answer), "q"), nil
}

// ShowReport prints the preflight summary.
func (s *Script) ShowReport(r interact.Report) {
	interact.WriteReport(s.Output(), r)
}

// Progress is ignored: the script shows the log lines of each step instead.
func (s *Script) Progress(interact.Progress) {}

// ShowResult prints the warning count (if any) and the log file path.
func (s *Script) ShowResult(r interact.Result) {
	if r.Warnings > 0 {
		s.printf("Warnings: %d\n", r.Warnings)
	}
	s.printf("\nLog file: %s\n", r.LogPath)
}

// Answers returns a ReadLine function that answers the prompts with lines,
// in order; once they are used up, every prompt is an error.
func Answers(lines ...string) func(prompt string) (string, error) {
	return func(prompt string) (string, error) {
		if len(lines) == 0 {
			return "", fmt.Errorf("no more scripted answers (prompt %q)", prompt)
		}
		line := lines[0]
		lines = lines[1:]
		return line, nil
	}
}
