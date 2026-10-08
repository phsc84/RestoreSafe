// Package problem holds the errors RestoreSafe shows to the user: what
// happened, and what the user does about it (refactoring 2.0 RF-25).
//
// Error texts are sentences for the user on purpose, capitalised and with a
// full stop; staticcheck's ST1005 is turned off for them.
package problem

import (
	"errors"
	"fmt"
	"strings"
)

// The labels that join a message and its remedy in the text of an Error, as
// logs, the console and message boxes show it.
const (
	remedyLabel        = " Remedy: "
	remedyLabelOwnLine = "\nRemedy: "
)

// Error is an error meant for the user. Its text is Msg followed by
// " Remedy: " and Remedy, so logs keep their wording; a frontend shows the
// two parts on their own (Split).
type Error struct {
	// Msg says what happened, as one or more sentences.
	Msg string
	// Remedy says what the user does about it; empty when there is nothing
	// to do.
	Remedy string
	// OwnLine puts the remedy on a line of its own in the text, for the
	// errors that a message box shows (the configuration's).
	OwnLine bool
	// Err is the cause, for errors.Is and errors.As (may be nil).
	Err error
}

// Errorf returns an Error whose Msg is formatted as by fmt.Errorf; an
// operand of %w becomes its cause. Add the remedy with WithRemedy.
func Errorf(format string, args ...any) *Error {
	wrapped := fmt.Errorf(format, args...)
	e := &Error{Msg: wrapped.Error()}
	if errors.Unwrap(wrapped) != nil || isMulti(wrapped) {
		e.Err = wrapped
	}
	return e
}

// New returns an Error with the message msg, taken as it is (no format).
// Add the remedy with WithRemedy.
func New(msg string) *Error { return &Error{Msg: msg} }

// isMulti reports whether err wraps several errors (more than one %w).
func isMulti(err error) bool {
	_, ok := err.(interface{ Unwrap() []error })
	return ok
}

// WithRemedy sets what the user does about the problem and returns e.
func (e *Error) WithRemedy(remedy string) *Error {
	e.Remedy = remedy
	return e
}

// WithRemedyOnOwnLine sets the remedy and puts it on a line of its own in
// the text, and returns e.
func (e *Error) WithRemedyOnOwnLine(remedy string) *Error {
	e.Remedy, e.OwnLine = remedy, true
	return e
}

func (e *Error) Error() string {
	if e.Remedy == "" {
		return e.Msg
	}
	return e.Msg + e.label() + e.Remedy
}

func (e *Error) label() string {
	if e.OwnLine {
		return remedyLabelOwnLine
	}
	return remedyLabel
}

// Unwrap returns the cause.
func (e *Error) Unwrap() error { return e.Err }

// Split returns the text of err without its remedy, and the remedy. The
// remedy is that of the outermost Error in err's chain; the text keeps
// whatever callers added around it ("Restore failed: ..."). An error without
// a remedy returns its whole text and "".
func Split(err error) (text, remedy string) {
	if err == nil {
		return "", ""
	}
	full := err.Error()
	var e *Error
	if errors.As(err, &e) && e.Remedy != "" {
		if text, ok := strings.CutSuffix(full, e.label()+e.Remedy); ok {
			return text, e.Remedy
		}
	}
	return full, ""
}
