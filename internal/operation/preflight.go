package operation

import "fmt"

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

// PreflightEntry is a single item in a preflight selection list.
type PreflightEntry struct {
	Label string
	Err   error
}

// PrintPreflightSelection prints the [OK] / [ERROR] list for a set of preflight entries.
func PrintPreflightSelection(entries []PreflightEntry) {
	for _, e := range entries {
		if e.Err != nil {
			fmt.Printf("  [ERROR] %s\n", e.Label)
			fmt.Printf("          -> %v\n", e.Err)
			continue
		}
		fmt.Printf("  [OK]    %s\n", e.Label)
	}
}
