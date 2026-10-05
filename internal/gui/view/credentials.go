package view

import (
	"RestoreSafe/internal/gui/flow"
	"RestoreSafe/internal/workflow/interact"
	"fmt"
	"regexp"
	"strings"
)

// Field is an edit field of a credential dialog.
type Field struct {
	Label  string
	Masked bool
}

// CredentialDialog is one of the dialogs that unlock or create the keys
// (spec 9).
type CredentialDialog struct {
	Title string
	Intro string
	// Hint follows the intro in secondary text.
	Hint   string
	Fields []Field
	// Error is under the fields: what was wrong with the last answer.
	Error string
	// Code is a recovery code to store, shown on one line.
	Code string
	// Copy is the text the Copy button puts on the clipboard; "" for no
	// Copy button.
	Copy string
	Note string
	OK   string
	// Cancel is "" when the dialog cannot be cancelled.
	Cancel string
	// Link is an alternative answer, e.g. "Use your recovery code instead";
	// "" for none.
	Link string
}

// UnlockDialogOf words the question for the password or the recovery code
// (figure 9.1). Naming the keys by their date (CR-1) matters only when a
// restore or verification spans several key sets; it comes with them.
func UnlockDialogOf(q flow.Question) CredentialDialog {
	d := CredentialDialog{Title: unlockTitle, OK: buttonUnlock, Cancel: buttonCancel, Error: retryError(q)}
	if strings.Contains(strings.ToLower(q.Prompt), "recovery code") {
		d.Intro = unlockRecoveryIntro
		d.Fields = []Field{{Label: fieldRecoveryCode}}
		return d
	}
	d.Intro = unlockIntro
	d.Hint = noticeOf(q)
	d.Fields = []Field{{Label: fieldPassword, Masked: true}}
	return d
}

// UnlockChoiceOf words the unlock dialog of keys with a recovery code (CR-1):
// the regular way, with the password field for password-only keys, and the
// link to the recovery code. regular names the keys' credentials as
// config.AuthMode.Label does.
func UnlockChoiceOf(q flow.Question, regular string) CredentialDialog {
	d := CredentialDialog{Title: unlockTitle, OK: buttonUnlock, Cancel: buttonCancel, Link: linkUseRecovery, Hint: noticeOf(q)}
	switch r := strings.ToLower(regular); {
	case strings.Contains(r, "yubikey only"):
		d.Intro = unlockWithYubiKey
	case strings.Contains(r, "yubikey"):
		d.Intro = unlockWithBoth
	default:
		d.Intro = unlockIntro
		d.Fields = []Field{{Label: fieldPassword, Masked: true}}
	}
	return d
}

// noticeOf is what the workflow said before a first question, e.g. that a
// backup uses older keys; "" on a retry, which retryError words.
func noticeOf(q flow.Question) string {
	if q.Retry {
		return ""
	}
	return issueText(q.Message)
}

// NewPasswordDialogOf words the first step of creating keys (figure 9.2).
func NewPasswordDialogOf(q flow.Question, keys interact.KeyPlan) CredentialDialog {
	d := CredentialDialog{
		Title:  stepTitle(createKeysTitle, keys, keyStepPassword),
		Intro:  newPasswordIntro,
		Hint:   newPasswordHint,
		Fields: []Field{{Label: fieldPassword, Masked: true}, {Label: fieldConfirmPassword, Masked: true}},
		Error:  retryError(q),
		Note:   stepsLine(keys),
		OK:     buttonNext,
		Cancel: buttonCancel,
	}
	if m := minLengthPattern.FindStringSubmatch(q.Prompt); m != nil {
		d.Hint = fmt.Sprintf(newPasswordHintN, m[1])
	}
	return d
}

var minLengthPattern = regexp.MustCompile(`at least (\d+) characters`)

// SpareYubiKeyDialogOf asks to connect the spare YubiKey (spec CR-2).
func SpareYubiKeyDialogOf(q flow.Question, keys interact.KeyPlan) CredentialDialog {
	return CredentialDialog{
		Title: stepTitle(createKeysTitle, keys, keyStepSpare),
		Intro: spareIntro,
		Hint:  spareHint,
		// The workflow says why it asks again, e.g. that this is YubiKey 1.
		Error:  retryError(q),
		OK:     buttonContinue,
		Cancel: buttonCancel,
	}
}

// RecoveryCodeDialogOf shows the new recovery code once (figure 9.3), with a
// button that copies it, e.g. into a password manager.
func RecoveryCodeDialogOf(code string, keys interact.KeyPlan) CredentialDialog {
	return CredentialDialog{
		Title: stepTitle(recoveryTitle, keys, keyStepRecovery),
		Intro: recoveryIntro,
		Code:  code,
		Copy:  code,
		Note:  recoveryNote,
		OK:    buttonStored,
	}
}

// The steps of creating keys (spec CR-2).
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
func stepTitle(title string, k interact.KeyPlan, step int) string {
	steps := keySteps(k)
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

var attemptsPattern = regexp.MustCompile(`(\d+) attempt\(s\) remaining\.$`)

// retryError rewords the workflow's message about the last answer, e.g.
// "Wrong password. 2 attempt(s) remaining." as "Wrong password. 2 attempts
// left."; "" for the first question.
func retryError(q flow.Question) string {
	if !q.Retry || q.Message == "" {
		return ""
	}
	msg := issueText(q.Message)
	msg = strings.TrimSuffix(msg, " Please try again.")
	if m := attemptsPattern.FindStringSubmatch(msg); m != nil {
		left := fmt.Sprintf(attemptsLeft, m[1])
		if m[1] == "1" {
			left = attemptLeftOne
		}
		msg = msg[:len(msg)-len(m[0])] + left
	}
	return msg
}
