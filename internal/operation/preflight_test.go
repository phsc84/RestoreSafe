package operation

import (
	"strings"
	"testing"
)

func TestValidatePreflightItems_NoFailures(t *testing.T) {
	t.Parallel()

	items := []int{1, 2, 3}
	err := ValidatePreflightItems(items, func(v int) bool { return v < 0 }, "failed: %d item(s)")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidatePreflightItems_CountsFailures(t *testing.T) {
	t.Parallel()

	items := []int{1, -2, -3, 4}
	err := ValidatePreflightItems(items, func(v int) bool { return v < 0 }, "failed: %d item(s)")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed: 2 item(s)") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestValidatePreflightItems_EmptyInput(t *testing.T) {
	t.Parallel()

	var items []string
	err := ValidatePreflightItems(items, func(s string) bool { return s == "bad" }, "failed: %d item(s)")
	if err != nil {
		t.Fatalf("expected no error for empty list, got %v", err)
	}
}
