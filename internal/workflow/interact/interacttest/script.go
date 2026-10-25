// Package interacttest provides Script, an implementation of interact.UI for
// tests of the workflows.
package interacttest

import (
	"fmt"

	"github.com/phsc84/restoresafe/internal/workflow/interact"
)

// Script is a UI for tests: it answers each question with the next answer
// of its kind and records what the workflow showed and asked. A question
// whose answers are used up is an error that names it.
type Script struct {
	// Starts answers ConfirmBackupStart and ConfirmStart, in order.
	// ConfirmStart starts on BackupAsPlanned and cancels on BackupCancel.
	Starts []interact.BackupStart
	// Recovery answers ChooseUnlockMethod, in order: true unlocks with the
	// recovery code.
	Recovery []bool
	// Refusals answers YubiKeyRefused, in order.
	Refusals []interact.YubiKeyRefusalAnswer
	// Secrets answers Password, one each, and NewPassword, two each: the
	// password and its confirmation.
	Secrets []string

	// Questions are the questions asked, in order: the action of
	// ConfirmStart, or the interact type ConfirmBackupStart (options),
	// ChooseUnlockMethod, YubiKeyRefused, Password or NewPassword got.
	Questions []any
	// BackupPlans, RestorePlans and VerifyPlans are the plans shown.
	BackupPlans  []interact.BackupPlan
	RestorePlans []interact.RestorePlan
	VerifyPlans  []interact.VerifyPlan
	// RecoveryCodes are the recovery codes shown.
	RecoveryCodes []string
	// Results are the results shown.
	Results []interact.Result
	// LogPath is the log file the workflow reported; LogFailures are the
	// problems with it the workflow reported.
	LogPath     string
	LogFailures []interact.LogFailure
	// LockWaits are the texts of WaitingForLock; OnLockWait, when set, is
	// called after each, e.g. to cancel the wait.
	LockWaits  []string
	OnLockWait func()
}

var _ interact.UI = (*Script)(nil)

// next takes the first of answers for the question q.
func next[T any](answers *[]T, q string) (T, error) {
	var zero T
	if len(*answers) == 0 {
		return zero, fmt.Errorf("no scripted answer for %s", q)
	}
	a := (*answers)[0]
	*answers = (*answers)[1:]
	return a, nil
}

func (s *Script) secret(q string) ([]byte, error) {
	secret, err := next(&s.Secrets, q)
	if err != nil {
		return nil, err
	}
	return []byte(secret), nil
}

// Password answers with the next secret.
func (s *Script) Password(q interact.SecretQuestion) ([]byte, error) {
	s.Questions = append(s.Questions, q)
	return s.secret("Password")
}

// NewPassword answers with the next two secrets, checked as the window
// checks the password and its confirmation.
func (s *Script) NewPassword(q interact.NewPasswordQuestion) ([]byte, error) {
	s.Questions = append(s.Questions, q)
	pw, err := s.secret("NewPassword")
	if err != nil {
		return nil, err
	}
	confirm, err := s.secret("NewPassword (confirmation)")
	if err != nil {
		return nil, err
	}
	return interact.CheckNewPassword(pw, confirm)
}

// ConfirmStart answers with the next start.
func (s *Script) ConfirmStart(action string) (bool, error) {
	s.Questions = append(s.Questions, action)
	start, err := next(&s.Starts, "ConfirmStart("+action+")")
	return start == interact.BackupAsPlanned, err
}

// ConfirmBackupStart answers with the next start; one that opts doesn't
// offer is an error.
func (s *Script) ConfirmBackupStart(opts interact.BackupStartOptions) (interact.BackupStart, error) {
	s.Questions = append(s.Questions, opts)
	start, err := next(&s.Starts, "ConfirmBackupStart")
	if err != nil {
		return interact.BackupCancel, err
	}
	offered := map[interact.BackupStart]bool{
		interact.BackupCancel:    true,
		interact.BackupAsPlanned: !opts.Blocked,
		interact.BackupFull:      opts.OfferFull,
		interact.BackupNewKeys:   opts.OfferNewKeys,
		interact.BackupAutomatic: opts.OfferAutomatic,
	}
	if !offered[start] {
		return interact.BackupCancel, fmt.Errorf("start %d is not offered: %+v", start, opts)
	}
	return start, nil
}

// ChooseUnlockMethod answers with the next unlock method.
func (s *Script) ChooseUnlockMethod(q interact.UnlockChoice) (bool, error) {
	s.Questions = append(s.Questions, q)
	return next(&s.Recovery, "ChooseUnlockMethod")
}

// YubiKeyRefused answers with the next refusal answer.
func (s *Script) YubiKeyRefused(q interact.YubiKeyRefusal) (interact.YubiKeyRefusalAnswer, error) {
	s.Questions = append(s.Questions, q)
	a, err := next(&s.Refusals, "YubiKeyRefused")
	if err != nil {
		return interact.RefusalCancel, err
	}
	return a, nil
}

// ShowRecoveryCode records the code.
func (s *Script) ShowRecoveryCode(code []byte) error {
	s.RecoveryCodes = append(s.RecoveryCodes, string(code))
	return nil
}

// ShowBackupPlan records the plan.
func (s *Script) ShowBackupPlan(p interact.BackupPlan) { s.BackupPlans = append(s.BackupPlans, p) }

// ShowRestorePlan records the plan.
func (s *Script) ShowRestorePlan(p interact.RestorePlan) { s.RestorePlans = append(s.RestorePlans, p) }

// ShowVerifyPlan records the plan.
func (s *Script) ShowVerifyPlan(p interact.VerifyPlan) { s.VerifyPlans = append(s.VerifyPlans, p) }

// Progress is ignored.
func (s *Script) Progress(interact.Progress) {}

// ShowResult records the result.
func (s *Script) ShowResult(r interact.Result) { s.Results = append(s.Results, r) }

// WaitingForLock records the text.
func (s *Script) WaitingForLock(text string) {
	s.LockWaits = append(s.LockWaits, text)
	if s.OnLockWait != nil {
		s.OnLockWait()
	}
}

// LogStarted records the log file.
func (s *Script) LogStarted(path string) { s.LogPath = path }

// LogFailed records a problem with the log file.
func (s *Script) LogFailed(f interact.LogFailure) { s.LogFailures = append(s.LogFailures, f) }

// Starting returns a Script that starts once and answers the secret
// questions with secrets.
func Starting(secrets ...string) *Script {
	return &Script{Starts: []interact.BackupStart{interact.BackupAsPlanned}, Secrets: secrets}
}
