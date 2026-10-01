package view

import (
	"RestoreSafe/internal/gui/flow"
	"RestoreSafe/internal/workflow/interact"
	"fmt"
	"regexp"
	"strings"
	"time"
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
	// CodeLines is a recovery code to write down, in lines of groups.
	CodeLines []string
	Note      string
	OK        string
	// Cancel is "" when the dialog cannot be cancelled.
	Cancel string
}

// UnlockDialogOf words the question for the password or the recovery code
// (figure 9.1). keys are the keys being unlocked, nil when unknown.
func UnlockDialogOf(q flow.Question, keys *interact.KeyPlan, now time.Time) CredentialDialog {
	d := CredentialDialog{Title: unlockTitle, OK: buttonUnlock, Cancel: buttonCancel, Error: retryError(q)}
	if strings.Contains(strings.ToLower(q.Prompt), "recovery code") {
		d.Intro = unlockRecoveryIntro
		d.Fields = []Field{{Label: fieldRecoveryCode}}
		return d
	}
	d.Intro = unlockIntro
	if keys != nil && !keys.Created.IsZero() {
		d.Intro = fmt.Sprintf(unlockIntroKeys, Day(keys.Created, now))
	}
	d.Fields = []Field{{Label: fieldPassword, Masked: true}}
	return d
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

// RecoveryCodeDialogOf shows the new recovery code once (figure 9.3).
func RecoveryCodeDialogOf(code string, keys interact.KeyPlan) CredentialDialog {
	return CredentialDialog{
		Title:     stepTitle(recoveryTitle, keys, keyStepRecovery),
		Intro:     recoveryIntro,
		CodeLines: CodeLines(code),
		Note:      recoveryNote,
		OK:        buttonWrittenDown,
	}
}

// RetypeDialogOf asks for the recovery code just shown (spec 13.3).
func RetypeDialogOf(q flow.Question, keys interact.KeyPlan) CredentialDialog {
	return CredentialDialog{
		Title:  stepTitle(recoveryTitle, keys, keyStepRecovery),
		Intro:  retypeIntro,
		Fields: []Field{{Label: fieldRecoveryCode}},
		Error:  retryError(q),
		OK:     buttonNext,
		Cancel: buttonCancel,
	}
}

// CodeLines splits a recovery code (groups separated by dashes) into two
// lines of equal group count, so it fits the dialog in a large font.
func CodeLines(code string) []string {
	groups := strings.Split(code, "-")
	if len(groups) < 2 {
		return []string{code}
	}
	half := (len(groups) + 1) / 2
	return []string{strings.Join(groups[:half], "-"), strings.Join(groups[half:], "-")}
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
