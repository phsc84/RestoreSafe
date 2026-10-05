// Package interacttest provides Script, a text implementation of interact.UI
// for tests of the workflows.
package interacttest

import (
	"RestoreSafe/internal/workflow/interact"
	"fmt"
	"io"
	"os"
	"strings"
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
	// LogPath is the log file the workflow reported.
	LogPath string
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
// replace a lost YubiKey, or get a new recovery code; [A] returns to the
// automatic plan. A plan with errors offers no start.
func (s *Script) ConfirmBackupStart(opts interact.BackupStartOptions) (interact.BackupStart, error) {
	if !opts.Blocked && !opts.OfferFull && !opts.OfferNewKeys && !opts.OfferAutomatic {
		ok, err := s.ConfirmStart("backup")
		if !ok || err != nil {
			return interact.BackupCancel, err
		}
		return interact.BackupAsPlanned, nil
	}
	var options, valid []string
	if !opts.Blocked {
		options, valid = append(options, "[Y] yes"), append(valid, "y (yes)")
	}
	if opts.OfferFull {
		options, valid = append(options, "[F] full backup"), append(valid, "f (full backup)")
	}
	if opts.OfferNewKeys {
		options, valid = append(options, "[K] new keys + full backup"), append(valid, "k (new keys)")
	}
	if opts.OfferAutomatic {
		options, valid = append(options, "[A] automatic plan"), append(valid, "a (automatic plan)")
	}
	options, valid = append(options, "[N] cancel"), append(valid, "n (no)")
	for {
		s.println()
		answer, err := s.readLine("Start backup now? " + strings.Join(options, " / ") + ": ")
		s.println()
		if err != nil {
			return interact.BackupCancel, err
		}
		switch strings.ToLower(strings.TrimSpace(answer)) {
		case "", "y", "yes":
			if !opts.Blocked {
				return interact.BackupAsPlanned, nil
			}
		case "f":
			if opts.OfferFull {
				s.println("Every source directory gets a full backup with the current keys.")
				return interact.BackupFull, nil
			}
		case "k":
			if opts.OfferNewKeys {
				s.println("New keys will be created and every source directory gets a full backup. Passwords, YubiKey registrations, and recovery codes of the current keys will not open the new backups (they still open older backups).")
				return interact.BackupNewKeys, nil
			}
		case "a":
			if opts.OfferAutomatic {
				return interact.BackupAutomatic, nil
			}
		case "n", "no":
			return interact.BackupCancel, nil
		}
		s.printf("Please enter %s.\n", strings.Join(valid, ", "))
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
func (s *Script) ShowRecoveryCode(code string) error {
	s.println()
	s.println("Your recovery code:")
	s.println()
	s.printf("    %s\n", code)
	s.println()
	s.println("  - This code alone restores every backup made with these keys, even without")
	s.println("    password or YubiKey. Treat it like the key to a safe.")
	s.println("  - Store it in your password manager or on paper in a safe place, never next to your backups.")
	s.println("  - It is shown only this once.")
	s.println()
	return nil
}

// WaitForSpareYubiKey waits for Enter; "q" cancels.
func (s *Script) WaitForSpareYubiKey() (bool, error) {
	answer, err := s.readLine("Press Enter when the spare YubiKey is connected (q = cancel): ")
	if err != nil {
		return false, err
	}
	return !strings.EqualFold(strings.TrimSpace(answer), "q"), nil
}

// ShowBackupPlan prints the preflight summary of the plan.
func (s *Script) ShowBackupPlan(p interact.BackupPlan) { interact.WriteReport(s.Output(), p.Details) }

// ShowRestorePlan prints the preflight summary of the plan.
func (s *Script) ShowRestorePlan(p interact.RestorePlan) { interact.WriteReport(s.Output(), p.Details) }

// ShowVerifyPlan prints the preflight summary of the plan.
func (s *Script) ShowVerifyPlan(p interact.VerifyPlan) { interact.WriteReport(s.Output(), p.Details) }

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

// LogStarted records the log file; the script prints it with the result.
func (s *Script) LogStarted(path string) { s.LogPath = path }
