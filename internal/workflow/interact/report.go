package interact

import (
	"github.com/phsc84/restoresafe/internal/problem"
)

// Status is the state of a report item or issue.
type Status int

const (
	// StatusNone marks plain information without a state.
	StatusNone Status = iota
	StatusOK
	StatusInfo
	StatusWarn
	StatusError
)

func (s Status) String() string {
	switch s {
	case StatusOK:
		return "OK"
	case StatusInfo:
		return "INFO"
	case StatusWarn:
		return "WARN"
	case StatusError:
		return "ERROR"
	}
	return ""
}

// Report is the preflight summary of an operation: what it will do, and the
// issues that block it or deserve attention. It is shown before the user
// confirms the start.
type Report struct {
	Title    string
	Sections []Section
	// Issues are shown after the sections. An issue with StatusError blocks
	// the operation.
	Issues []Issue
}

// Section is a group of rows shown together; sections are visually separated.
type Section struct {
	Rows []Row
}

// RowKind says how a row is shown.
type RowKind int

const (
	// RowHeading introduces the items below it (e.g. "Source directory(s)").
	RowHeading RowKind = iota
	// RowItem is one checked thing (a directory, a backup set) with its status
	// and details.
	RowItem
	// RowField is a label/value pair of a summary.
	RowField
)

// Row is one line of a section with, for items, its details.
type Row struct {
	Kind RowKind
	// Label is the heading text or the field label.
	Label string
	// Text is the item text or the field value.
	Text string
	// Status and Details apply to items.
	Status  Status
	Details []string
}

// Issue is a problem found by the preflight.
type Issue struct {
	Status Status // StatusWarn or StatusError
	Code   Code
	// Text says what is wrong, Remedy what the user does about it (may be
	// empty).
	Text   string
	Remedy string
}

// IssueOf returns the issue of err, with its text and remedy.
func IssueOf(status Status, code Code, err error) Issue {
	text, remedy := problem.Split(err)
	return Issue{Status: status, Code: code, Text: text, Remedy: remedy}
}

// Full returns the issue as the console and the log show it: the text and
// its remedy.
func (i Issue) Full() string {
	return (&problem.Error{Msg: i.Text, Remedy: i.Remedy}).Error()
}

// hasErrors reports whether an issue blocks the operation.
func hasErrors(issues []Issue) bool {
	for _, issue := range issues {
		if issue.Status == StatusError {
			return true
		}
	}
	return false
}

// Heading returns a heading row.
func Heading(label string) Row { return Row{Kind: RowHeading, Label: label} }

// Item returns an item row.
func Item(status Status, text string, details ...string) Row {
	return Row{Kind: RowItem, Status: status, Text: text, Details: details}
}

// Field returns a label/value row.
func Field(label, value string) Row { return Row{Kind: RowField, Label: label, Text: value} }

// HasErrors reports whether an issue blocks the operation.
func (r Report) HasErrors() bool { return hasErrors(r.Issues) }
