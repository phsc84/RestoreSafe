package interacttest

import (
	"fmt"
	"io"
	"strings"

	"github.com/phsc84/restoresafe/internal/workflow/interact"
)

// minFieldLabelWidth is the narrowest label column of a section's fields, so
// short summaries line up with the "Authentication" field.
const minFieldLabelWidth = 14

// WriteReport writes r as text: the title underlined, sections separated by
// blank lines, field labels aligned per section, and the issues at the end.
// Tests of the workflows compare reports in this form.
func WriteReport(w io.Writer, r interact.Report) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, r.Title)
	fmt.Fprintln(w, strings.Repeat("-", len(r.Title)))
	for i, section := range r.Sections {
		if i > 0 {
			fmt.Fprintln(w)
		}
		width := minFieldLabelWidth
		for _, row := range section.Rows {
			if row.Kind == interact.RowField && len(row.Label) > width {
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
			fmt.Fprintf(w, "[%s] %s\n", issue.Status, issue.Full())
		}
	}
}

func writeRow(w io.Writer, row interact.Row, labelWidth int) {
	switch row.Kind {
	case interact.RowHeading:
		fmt.Fprintf(w, "%s:\n", row.Label)
	case interact.RowField:
		fmt.Fprintf(w, "%-*s: %s\n", labelWidth, row.Label, row.Text)
	case interact.RowItem:
		if row.Status == interact.StatusNone {
			fmt.Fprintf(w, "  %s\n", row.Text)
		} else {
			fmt.Fprintf(w, "  [%s] %s\n", row.Status, row.Text)
		}
		for _, detail := range row.Details {
			fmt.Fprintf(w, "          → %s\n", detail)
		}
	}
}
