package job

import (
	"RestoreSafe/internal/workflow/interact"
	"errors"
	"strings"
	"testing"
)

func TestValidatePreflightItems_NoFailures(t *testing.T) {
	t.Parallel()

	items := []int{1, 2, 3}
	err := ValidatePreflightItems(items, func(v int) bool { return v < 0 }, "failed: %d item(s)", "")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidatePreflightItems_CountsFailures(t *testing.T) {
	t.Parallel()

	items := []int{1, -2, -3, 4}
	err := ValidatePreflightItems(items, func(v int) bool { return v < 0 }, "failed: %d item(s)", "")
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
	err := ValidatePreflightItems(items, func(s string) bool { return s == "bad" }, "failed: %d item(s)", "")
	if err != nil {
		t.Fatalf("expected no error for empty list, got %v", err)
	}
}

func TestAuthRows(t *testing.T) {
	t.Parallel()
	connected := func() error { return nil }
	disconnected := func() error { return errors.New("not connected") }

	rows := AuthRows("password only", false, "backup", disconnected)
	if len(rows) != 1 || rows[0].Kind != interact.RowField || rows[0].Label != "Authentication" || rows[0].Text != "password only" {
		t.Fatalf("without YubiKey expected only the field, got %+v", rows)
	}
	rows = AuthRows("YubiKey only", true, "backup", disconnected)
	if len(rows) != 2 || rows[1].Status != interact.StatusWarn || !strings.Contains(rows[1].Text, "before starting backup") {
		t.Fatalf("disconnected YubiKey expected a warning, got %+v", rows)
	}
	rows = AuthRows("YubiKey only", true, "restore", connected)
	if len(rows) != 2 || rows[1].Status != interact.StatusOK || !strings.Contains(rows[1].Text, "before starting restore") {
		t.Fatalf("connected YubiKey expected OK, got %+v", rows)
	}
}
