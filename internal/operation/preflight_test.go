package operation

import (
	"RestoreSafe/internal/testutil"
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

func TestPrintPreflightFieldFormatsAlignedOutput(t *testing.T) {
	// NOT parallel — testutil.CaptureStdout replaces os.Stdout globally.
	output := testutil.CaptureStdout(t, func() {
		PrintPreflightField(14, "Log level", "info")
	})
	if !strings.Contains(output, "Log level") {
		t.Fatalf("expected label in output, got: %q", output)
	}
	if !strings.Contains(output, "info") {
		t.Fatalf("expected value in output, got: %q", output)
	}
}

func TestPrintYubiKeyPreflightStatusSkipsWhenNotRequired(t *testing.T) {
	// NOT parallel — testutil.CaptureStdout replaces os.Stdout globally.
	output := testutil.CaptureStdout(t, func() {
		PrintYubiKeyPreflightStatus(false, "backup", func() error { return nil })
	})
	if output != "" {
		t.Fatalf("expected no output when YubiKey not required, got: %q", output)
	}
}

func TestPrintYubiKeyPreflightStatusPrintsWarnWhenDisconnected(t *testing.T) {
	// NOT parallel — testutil.CaptureStdout replaces os.Stdout globally.
	output := testutil.CaptureStdout(t, func() {
		PrintYubiKeyPreflightStatus(true, "backup", func() error { return errors.New("not connected") })
	})
	if !strings.Contains(output, "[WARN]") {
		t.Fatalf("expected [WARN] in output, got: %q", output)
	}
}

func TestPrintYubiKeyPreflightStatusPrintsOKWhenConnected(t *testing.T) {
	// NOT parallel — testutil.CaptureStdout replaces os.Stdout globally.
	output := testutil.CaptureStdout(t, func() {
		PrintYubiKeyPreflightStatus(true, "backup", func() error { return nil })
	})
	if !strings.Contains(output, "[OK]") {
		t.Fatalf("expected [OK] in output, got: %q", output)
	}
}
