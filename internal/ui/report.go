package ui

import (
	"fmt"
	"io"
	"strings"
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
	// RowNote is an explanatory sentence.
	RowNote
)

// Row is one line of a section with, for items, its details.
type Row struct {
	Kind RowKind
	// Label is the heading text or the field label.
	Label string
	// Text is the item text, the field value, or the note.
	Text string
	// Status and Details apply to items.
	Status  Status
	Details []string
}

// Issue is a problem found by the preflight.
type Issue struct {
	Status Status // StatusWarn or StatusError
	Text   string
}

// Heading returns a heading row.
func Heading(label string) Row { return Row{Kind: RowHeading, Label: label} }

// Item returns an item row.
func Item(status Status, text string, details ...string) Row {
	return Row{Kind: RowItem, Status: status, Text: text, Details: details}
}

// Field returns a label/value row.
func Field(label, value string) Row { return Row{Kind: RowField, Label: label, Text: value} }

// Note returns an explanatory row.
func Note(text string) Row { return Row{Kind: RowNote, Text: text} }

// HasErrors reports whether an issue blocks the operation.
func (r Report) HasErrors() bool {
	for _, issue := range r.Issues {
		if issue.Status == StatusError {
			return true
		}
	}
	return false
}

// minFieldLabelWidth is the narrowest label column of a section's fields, so
// short summaries line up with the "Authentication" field.
const minFieldLabelWidth = 14

// WriteReport writes r as console text: the title underlined, sections
// separated by blank lines, field labels aligned per section, and the issues
// at the end.
func WriteReport(w io.Writer, r Report) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, r.Title)
	fmt.Fprintln(w, strings.Repeat("-", len(r.Title)))
	for i, section := range r.Sections {
		if i > 0 {
			fmt.Fprintln(w)
		}
		width := minFieldLabelWidth
		for _, row := range section.Rows {
			if row.Kind == RowField && len(row.Label) > width {
				width = len(row.Label)
			}
		}
		for _, row := range section.Rows {
			writeRow(w, row, width)
		}
	}
	if len(r.Issues) > 0 {
		fmt.Fprintln(w)
		for _, issue := range r.Issues {
			fmt.Fprintf(w, "[%s] %s\n", issue.Status, issue.Text)
		}
	}
}

func writeRow(w io.Writer, row Row, labelWidth int) {
	switch row.Kind {
	case RowHeading:
		fmt.Fprintf(w, "%s:\n", row.Label)
	case RowField:
		fmt.Fprintf(w, "%-*s: %s\n", labelWidth, row.Label, row.Text)
	case RowNote:
		fmt.Fprintln(w, row.Text)
	case RowItem:
		if row.Status == StatusNone {
			fmt.Fprintf(w, "  %s\n", row.Text)
		} else {
			fmt.Fprintf(w, "  [%s] %s\n", row.Status, row.Text)
		}
		for _, detail := range row.Details {
			fmt.Fprintf(w, "          → %s\n", detail)
		}
	}
}
