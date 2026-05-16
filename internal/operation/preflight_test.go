package operation

import (
	"errors"
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

func TestPrintYubiKeyPreflightStatusSkipsWhenNotRequired(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	PrintYubiKeyPreflightStatus(&sb, false, "backup", func() error { return nil })
	if sb.String() != "" {
		t.Fatalf("expected no output when YubiKey not required, got: %q", sb.String())
	}
}

func TestPrintYubiKeyPreflightStatusPrintsWarnWhenDisconnected(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	PrintYubiKeyPreflightStatus(&sb, true, "backup", func() error { return errors.New("not connected") })
	if !strings.Contains(sb.String(), "[WARN]") {
		t.Fatalf("expected [WARN] in output, got: %q", sb.String())
	}
}

func TestPrintYubiKeyPreflightStatusPrintsOKWhenConnected(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	PrintYubiKeyPreflightStatus(&sb, true, "backup", func() error { return nil })
	if !strings.Contains(sb.String(), "[OK]") {
		t.Fatalf("expected [OK] in output, got: %q", sb.String())
	}
}
