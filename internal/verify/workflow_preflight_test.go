package verify

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateVerifyPreflight(t *testing.T) {
	t.Parallel()

	valid := []verifyPreflightItem{{}, {}}
	if err := validateVerifyPreflight(valid); err != nil {
		t.Fatalf("expected no error for valid verify preflight, got %v", err)
	}

	invalid := []verifyPreflightItem{{}, {Err: errors.New("broken")}}
	err := validateVerifyPreflight(invalid)
	if err == nil {
		t.Fatal("expected error for invalid verify preflight, got nil")
	}
	if !strings.Contains(err.Error(), "1 selected item") {
		t.Fatalf("unexpected verify preflight error: %v", err)
	}
}
