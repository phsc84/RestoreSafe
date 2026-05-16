package operation

import (
	"fmt"
	"io"
)

// DefaultFieldLabelWidth is the standard label column width for summary/preflight fields.
const DefaultFieldLabelWidth = 14

// PrintField prints a left-aligned label/value pair with a fixed label column width.
func PrintField(w io.Writer, labelWidth int, label, value string) {
	fmt.Fprintf(w, "%-*s: %s\n", labelWidth, label, value)
}
