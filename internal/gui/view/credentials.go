package view

import (
	"fmt"
	"strings"

	"github.com/phsc84/restoresafe/internal/config"
	"github.com/phsc84/restoresafe/internal/workflow/interact"
)

// Field is an edit field of a credential dialog.
type Field struct {
	Label  string
	Masked bool
}

// CredentialDialog is one of the dialogs that unlock or create the keys
// (GUI spec 9).
type CredentialDialog struct {
	Title string
	Intro string
	// Hint follows the intro in secondary text.
	Hint   string
	Fields []Field
	// Error is under the fields: what was wrong with the last answer.
	Error string
	// Code is a recovery code to store, shown on one line with a Copy
	// button; nil for none. It is a secret: bytes, so that the caller can
	// zero it after the dialog closed.
	Code []byte
	Note string
	OK   string
	// Cancel is "" when the dialog cannot be cancelled.
	Cancel string
	// Link is an alternative answer, e.g. "Use your recovery code instead";
	// "" for none.
	Link string
}

// UnlockDialogOf words the question for the password or the recovery code
// (figure 9.1); with keys other than the operation's first, it names them by
// their backup and date (CR-1).
func UnlockDialogOf(q interact.SecretQuestion) CredentialDialog {
	d := CredentialDialog{Title: unlockTitle, OK: buttonUnlock, Cancel: buttonCancel, Error: unlockError(q.Attempt)}
	if q.Kind == interact.SecretRecoveryCode {
		d.Intro = unlockRecoveryIntro
		d.Fields = []Field{{Label: fieldRecoveryCode}}
		return d
	}
	d.Intro = unlockIntro
	d.Hint = otherKeysNotice(q.Keys, q.Attempt)
	d.Fields = []Field{{Label: fieldPassword, Masked: true}}
	return d
}

// UnlockChoiceOf words the unlock dialog of keys with a recovery code (CR-1):
// the regular way of the keys' mode, with the password field for
// password-only keys, and the link to the recovery code.
func UnlockChoiceOf(q interact.UnlockChoice) CredentialDialog {
	d := CredentialDialog{Title: unlockTitle, OK: buttonUnlock, Cancel: buttonCancel, Link: linkUseRecovery, Hint: otherKeysNotice(q.Keys, interact.Attempt{})}
	switch q.Mode {
	case config.AuthModeYubiKey:
		d.Intro = unlockWithYubiKey
	case config.AuthModePasswordYubiKey:
		d.Intro = unlockWithBoth
	default:
		d.Intro = unlockIntro
		d.Fields = []Field{{Label: fieldPassword, Masked: true}}
	}
	return d
}

// otherKeysNotice names the backup that needs keys other than the
// operation's first (CR-1); "" for the first keys and on a retry, which
// unlockError words.
func otherKeysNotice(k *interact.OtherKeys, a interact.Attempt) string {
	if k == nil || a.Retry() {
		return ""
	}
	return fmt.Sprintf(unlockOtherKeys, k.Set.String(), k.Created.Local().Format("2006-01-02"))
}

// NewPasswordDialogOf words the first step of creating keys (figure 9.2).
func NewPasswordDialogOf(q interact.NewPasswordQuestion, keys interact.KeyPlan) CredentialDialog {
	d := CredentialDialog{
		Title:  stepTitle(createKeysTitle, keys, keyStepPassword),
		Intro:  newPasswordIntro,
		Hint:   newPasswordHint,
		Fields: []Field{{Label: fieldPassword, Masked: true}, {Label: fieldConfirmPassword, Masked: true}},
		Error:  retryError(q.Attempt),
		Note:   stepsLine(keys),
		OK:     buttonNext,
		Cancel: buttonCancel,
	}
	if q.MinLength > 0 {
		d.Hint = fmt.Sprintf(newPasswordHintN, q.MinLength)
	}
	return d
}

// SpareYubiKeyDialogOf asks to connect the spare YubiKey (GUI spec CR-2).
func SpareYubiKeyDialogOf(q interact.SpareQuestion, keys interact.KeyPlan) CredentialDialog {
	return CredentialDialog{
		Title: stepTitle(createKeysTitle, keys, keyStepSpare),
		Intro: spareIntro,
		Hint:  spareHint,
		// Why it asks again, e.g. that this is YubiKey 1.
		Error:  retryError(q.Attempt),
		OK:     buttonContinue,
		Cancel: buttonCancel,
	}
}

// RecoveryCodeDialogOf shows the new recovery code once (figure 9.3), with a
// button that copies it, e.g. into a password manager. It has no Cancel
// button, but closing it cancels. It is not a numbered step: it asks
// nothing.
func RecoveryCodeDialogOf(code []byte, _ interact.KeyPlan) CredentialDialog {
	return CredentialDialog{
		Title: recoveryTitle,
		Intro: recoveryIntro,
		Code:  code,
		Note:  recoveryNote,
		OK:    buttonStored,
	}
}

// The steps of creating keys (GUI spec CR-2).
const (
	keyStepPassword = iota
	keyStepYubiKey
	keyStepSpare
	keyStepRecovery
)

// keySteps lists the steps the keys need, in order.
func keySteps(k interact.KeyPlan) []int {
	var steps []int
	if k.Password {
		steps = append(steps, keyStepPassword)
	}
	if k.YubiKeys >= 1 {
		steps = append(steps, keyStepYubiKey)
	}
	if k.YubiKeys >= 2 {
		steps = append(steps, keyStepSpare)
	}
	if k.RecoveryCode {
		steps = append(steps, keyStepRecovery)
	}
	return steps
}

// stepTitle is "<title> · Step n of N", or title alone for a single step.
// Only the steps that ask in a dialog of their own are numbered: YubiKey
// registration is the Windows Security prompt, and the recovery code
// dialog asks nothing.
func stepTitle(title string, k interact.KeyPlan, step int) string {
	var steps []int
	for _, s := range keySteps(k) {
		if s != keyStepYubiKey && s != keyStepRecovery {
			steps = append(steps, s)
		}
	}
	if len(steps) < 2 {
		return title
	}
	for i, s := range steps {
		if s == step {
			return fmt.Sprintf(stepOf, title, i+1, len(steps))
		}
	}
	return title
}

// stepsLine lists the steps: "Steps: password › register your YubiKey and
// spare › recovery code"; "" for a single step.
func stepsLine(k interact.KeyPlan) string {
	steps := keySteps(k)
	if len(steps) < 2 {
		return ""
	}
	var names []string
	for _, s := range steps {
		switch s {
		case keyStepPassword:
			names = append(names, stepNamePassword)
		case keyStepYubiKey:
			names = append(names, stepNameYubiKey)
		case keyStepSpare:
			names = append(names, stepNameSpare)
		case keyStepRecovery:
			names = append(names, stepNameRecovery)
		}
	}
	return stepsPrefix + strings.Join(names, " › ")
}

// retryError is why the last answer failed, e.g. "Passwords do not match.";
// "" for the first question.
func retryError(a interact.Attempt) string {
	if !a.Retry() || a.Failure == nil {
		return ""
	}
	return errorText(a.Failure)
}

// unlockError is why the last unlock failed with the attempts left, e.g.
// "Wrong password. 2 attempts left."; "" for the first question.
func unlockError(a interact.Attempt) string {
	msg := retryError(a)
	if msg == "" {
		return ""
	}
	if a.Left == 1 {
		return msg + " " + attemptLeftOne
	}
	return msg + " " + fmt.Sprintf(attemptsLeft, a.Left)
}
