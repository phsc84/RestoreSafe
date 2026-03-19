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
