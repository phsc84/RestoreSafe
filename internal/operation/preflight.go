package operation

import (
	"fmt"
	"io"
	"strings"
)

// DefaultFieldLabelWidth is the standard label column width for summary/preflight fields.
const DefaultFieldLabelWidth = 14

// PrintField prints a left-aligned label/value pair with a fixed label column width.
func PrintField(w io.Writer, labelWidth int, label, value string) {
	fmt.Fprintf(w, "%-*s: %s\n", labelWidth, label, value)
}

// ValidatePreflightItems returns a formatted error when one or more items fail
// a caller-supplied validity check.
func ValidatePreflightItems[T any](items []T, hasError func(T) bool, failureTemplate string) error {
	invalid := 0
	for _, item := range items {
		if hasError(item) {
			invalid++
		}
	}
	if invalid > 0 {
		return fmt.Errorf(failureTemplate, invalid)
	}
	return nil
}

// PrintAuthStatus prints the Authentication field and YubiKey status lines.
// authLabel is the pre-formatted authentication mode string; action is the
// operation label ("backup", "restore", "verification").
func PrintAuthStatus(w io.Writer, authLabel string, requiresYubiKey bool, action string, checkYubiKeyAvailability func() error, checkYubiKeyConnected func() error) {
	PrintField(w, DefaultFieldLabelWidth, "Authentication", authLabel)
	PrintYubiKeyStatus(w, requiresYubiKey, action, checkYubiKeyAvailability, checkYubiKeyConnected)
}

// PrintPreflightIssues prints a blank line followed by each issue. Issues
// already prefixed with "[WARN]" are printed as-is; all others are wrapped
// with "[ERROR]". No output is produced when issues is empty.
func PrintPreflightIssues(w io.Writer, issues []string) {
	if len(issues) == 0 {
		return
	}
	fmt.Fprintln(w)
	for _, issue := range issues {
		if strings.HasPrefix(issue, "[WARN]") {
			fmt.Fprintln(w, issue)
		} else {
			fmt.Fprintf(w, "[ERROR] %s\n", issue)
		}
	}
}

// PrintYubiKeyStatus prints a YubiKey connection status line under
// the Authentication field. action is the operation label ("backup", "restore",
// "verification"). No output is produced when requiresYubiKey is false.
// checkYubiKeyAvailability is accepted but unused (kept for call-site compat).
func PrintYubiKeyStatus(w io.Writer, requiresYubiKey bool, action string, _ func() error, checkYubiKeyConnected func() error) {
	if !requiresYubiKey {
		return
	}
	if err := checkYubiKeyConnected(); err != nil {
		fmt.Fprintf(w, "  [WARN] YubiKey not connected. Remedy: Connect the YubiKey before starting %s.\n", action)
	} else {
		fmt.Fprintf(w, "  [OK] YubiKey connected. Keep it connected before starting %s.\n", action)
	}
}
