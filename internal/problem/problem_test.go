package problem

import (
	"errors"
	"fmt"
	"io/fs"
	"testing"
)

func TestErrorTextIsMessageAndRemedy(t *testing.T) {
	t.Parallel()
	e := Errorf("Failed to read backup header: %w.", fs.ErrNotExist).WithRemedy("Check that the backup file is complete.")
	if got, want := e.Error(), "Failed to read backup header: file does not exist. Remedy: Check that the backup file is complete."; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
	if !errors.Is(e, fs.ErrNotExist) {
		t.Fatal("the cause must stay reachable for errors.Is")
	}
	if got := Errorf("Wrong recovery code.").Error(); got != "Wrong recovery code." {
		t.Fatalf("without remedy: %q", got)
	}
	if Errorf("No cause %d.", 1).Err != nil {
		t.Fatal("a message without %w has no cause")
	}
	if got, want := Errorf("Config file is written in YAML flow style.").WithRemedyOnOwnLine("Add them by hand.").Error(), "Config file is written in YAML flow style.\nRemedy: Add them by hand."; got != want {
		t.Fatalf("own line: %q, want %q", got, want)
	}
}

func TestSplit(t *testing.T) {
	t.Parallel()
	typed := Errorf("The full backup is missing.").WithRemedy("Restore its files.")
	for _, c := range []struct {
		name               string
		err                error
		wantText, wantRemd string
	}{
		{"nil", nil, "", ""},
		{"typed", typed, "The full backup is missing.", "Restore its files."},
		{"wrapped by a caller", fmt.Errorf("Restore failed: %w", typed), "Restore failed: The full backup is missing.", "Restore its files."},
		{"no remedy", errors.New("Wrong password."), "Wrong password.", ""},
		{"untyped text", errors.New("Disk full. Remedy: Free up space."), "Disk full.", "Free up space."},
		{"own line", Errorf("Invalid YAML.").WithRemedyOnOwnLine("Check the syntax."), "Invalid YAML.", "Check the syntax."},
		{"untyped own line", errors.New("Invalid YAML.\nRemedy: Check the syntax."), "Invalid YAML.", "Check the syntax."},
	} {
		text, remedy := Split(c.err)
		if text != c.wantText || remedy != c.wantRemd {
			t.Errorf("%s: Split = %q, %q; want %q, %q", c.name, text, remedy, c.wantText, c.wantRemd)
		}
	}
}
