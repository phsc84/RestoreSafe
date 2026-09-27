package ui

import (
	"RestoreSafe/internal/catalog"
	"RestoreSafe/internal/util"
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

func testRuns() []catalog.BackupRunSummary {
	return []catalog.BackupRunSummary{
		{
			RunID:   "ABC125",
			Date:    "2026-03-18",
			Created: time.Date(2026, 3, 18, 21, 12, 22, 0, time.UTC),
			Entries: []util.BackupEntry{
				{DirectoryName: "SourceDirectory1", ChainID: "ABC125", Date: "2026-03-18"},
				{DirectoryName: "SourceDirectory2", ChainID: "ABC125", Date: "2026-03-18"},
			},
		},
		{
			RunID:   "ABC123",
			Date:    "2026-03-18",
			Created: time.Date(2026, 3, 18, 20, 35, 3, 0, time.UTC),
			Entries: []util.BackupEntry{{DirectoryName: "SourceDirectory", ChainID: "ABC123", Date: "2026-03-18"}},
		},
	}
}

// scripted returns a console that answers line prompts with lines and writes
// its output to the returned buffer. Running out of lines is an error.
func scripted(lines ...string) (*Console, *bytes.Buffer) {
	var out bytes.Buffer
	c := &Console{Out: &out, ReadLine: func(string) (string, error) {
		if len(lines) == 0 {
			return "", errors.New("no more lines")
		}
		line := lines[0]
		lines = lines[1:]
		return line, nil
	}}
	return c, &out
}

func selectWith(inputs ...string) ([]util.BackupEntry, error, string) {
	c, out := scripted(inputs...)
	selected, err := c.SelectBackups("verify", testRuns())
	return selected, err, out.String()
}

func TestSelectBackupsCancelReturnsTypedError(t *testing.T) {
	_, err, _ := selectWith("q")
	if !errors.Is(err, ErrCancelled) {
		t.Fatalf("expected ErrCancelled, got %v", err)
	}
}

func TestPrintBackupSelectionPromptListsRunsInOrder(t *testing.T) {
	c, out := scripted()
	c.printBackupSelectionPrompt("restore", testRuns())
	output := out.String()

	first := "  - Backup ID: ABC125 / Timestamp (local): " + formatBackupRunTimestamp(testRuns()[0].Created)
	second := "  - Backup ID: ABC123 / Timestamp (local): " + formatBackupRunTimestamp(testRuns()[1].Created)
	if !strings.Contains(output, first) || !strings.Contains(output, second) || strings.Index(output, first) > strings.Index(output, second) {
		t.Fatalf("unexpected run listing: %q", output)
	}
	if !strings.Contains(output, "    - SourceDirectory1_ABC125_2026-03-18_FULL") {
		t.Fatalf("expected nested entry, got %q", output)
	}
}

func TestCompletedActionLabel(t *testing.T) {
	t.Parallel()

	if got := completedActionLabel("restore"); got != "restored" {
		t.Fatalf("expected restored, got %q", got)
	}
	if got := completedActionLabel("verify"); got != "verified" {
		t.Fatalf("expected verified, got %q", got)
	}
	if got := completedActionLabel("clean"); got != "cleaned" {
		t.Fatalf("expected cleaned fallback, got %q", got)
	}
}

func TestSelectBackupsNewestSelectsLatestRun(t *testing.T) {
	selected, err, _ := selectWith(".")
	if err != nil || len(selected) != 2 || selected[0].ChainID != "ABC125" {
		t.Fatalf("unexpected newest selection: %v, %v", selected, err)
	}
}

func TestSelectBackupsByNameSelectsMatchingEntry(t *testing.T) {
	selected, err, _ := selectWith("SourceDirectory_ABC123_2026-03-18_FULL")
	if err != nil || len(selected) != 1 || selected[0].DirectoryName != "SourceDirectory" {
		t.Fatalf("unexpected name selection: %v, %v", selected, err)
	}
}

func TestSelectBackupsByIDSelectsRunEntries(t *testing.T) {
	selected, err, _ := selectWith("abc125")
	if err != nil || len(selected) != 2 {
		t.Fatalf("unexpected ID selection: %v, %v", selected, err)
	}
}

func TestSelectBackupsEmptyInputPrintsRetryMessage(t *testing.T) {
	_, _, out := selectWith("", "q")
	if !strings.Contains(out, "Selection must not be empty.") {
		t.Fatalf("expected empty-selection message, got %q", out)
	}
}

func TestSelectBackupsUnknownNamePrintsError(t *testing.T) {
	_, _, out := selectWith("unknown-backup-name", "q")
	if !strings.Contains(out, "not found") {
		t.Fatalf("expected not-found message, got %q", out)
	}
}

func TestConfirmStartPrintsSingleBlankLineBeforeAndAfterPrompt(t *testing.T) {
	c, out := scripted("y")
	var prompts []string
	read := c.ReadLine
	c.ReadLine = func(prompt string) (string, error) {
		prompts = append(prompts, prompt)
		return read(prompt)
	}

	confirmed, err := c.ConfirmStart("verification")
	if err != nil || !confirmed {
		t.Fatalf("ConfirmStart = %v, %v; want true", confirmed, err)
	}
	if len(prompts) != 1 || prompts[0] != "Start verification now? [Y/n]: " {
		t.Fatalf("unexpected prompts: %q", prompts)
	}
	if got := out.String(); got != "\n\n" {
		t.Fatalf("expected exactly one empty line before and after prompt, got %q", got)
	}
}

func TestConfirmStartPrintsSingleBlankLineOnRetry(t *testing.T) {
	c, out := scripted("maybe", "n")

	confirmed, err := c.ConfirmStart("verification")
	if err != nil || confirmed {
		t.Fatalf("ConfirmStart = %v, %v; want false", confirmed, err)
	}
	expected := "\n\nPlease enter y (yes) or n (no).\n\n\n"
	if got := out.String(); got != expected {
		t.Fatalf("unexpected output.\nexpected: %q\n     got: %q", expected, got)
	}
}

func TestConfirmBackupStart(t *testing.T) {
	both := BackupStartOptions{OfferFull: true, OfferNewKeys: true}
	newKeys := BackupStartOptions{OfferNewKeys: true}
	const hintNewKeys = "Please enter y (yes), k (new keys), or n (no)."
	const hintBoth = "Please enter y (yes), f (full backup), k (new keys), or n (no)."
	cases := []struct {
		name    string
		opts    BackupStartOptions
		answers []string
		want    BackupStart
		hint    string
	}{
		{"new keys not offered asks yes/no", BackupStartOptions{}, []string{"y"}, BackupAsPlanned, ""},
		{"new keys not offered, no", BackupStartOptions{}, []string{"n"}, BackupCancel, ""},
		{"default starts as planned", both, []string{""}, BackupAsPlanned, ""},
		{"f makes full backups", both, []string{"f"}, BackupFull, ""},
		{"f not offered is invalid", newKeys, []string{"f", "y"}, BackupAsPlanned, hintNewKeys},
		{"invalid answer is repeated", newKeys, []string{"maybe", "k"}, BackupNewKeys, hintNewKeys},
		{"k creates new keys", both, []string{"K"}, BackupNewKeys, ""},
		{"n cancels", both, []string{"x", "no"}, BackupCancel, hintBoth},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, out := scripted(tc.answers...)
			got, err := c.ConfirmBackupStart(tc.opts)
			if err != nil || got != tc.want {
				t.Fatalf("ConfirmBackupStart = %v, %v; want %v", got, err, tc.want)
			}
			if tc.hint != "" && !strings.Contains(out.String(), tc.hint) {
				t.Fatalf("expected hint %q, got %q", tc.hint, out.String())
			}
		})
	}
}

func TestRestoreDestination(t *testing.T) {
	c, _ := scripted("", " C:/Restore ")
	if got, err := c.RestoreDestination("D:/Backups"); err != nil || got != "C:/Restore" {
		t.Fatalf("RestoreDestination = %q, %v", got, err)
	}
	c, _ = scripted(".")
	if got, err := c.RestoreDestination("D:/Backups"); err != nil || got != "D:/Backups" {
		t.Fatalf("RestoreDestination(.) = %q, %v", got, err)
	}
	c, _ = scripted("q")
	if _, err := c.RestoreDestination("D:/Backups"); !errors.Is(err, ErrCancelled) {
		t.Fatalf("RestoreDestination(q) error = %v, want ErrCancelled", err)
	}
}

func TestWaitForSpareYubiKey(t *testing.T) {
	c, _ := scripted("", "Q")
	if ok, err := c.WaitForSpareYubiKey(); err != nil || !ok {
		t.Fatalf("Enter: got %v, %v; want true", ok, err)
	}
	if ok, err := c.WaitForSpareYubiKey(); err != nil || ok {
		t.Fatalf("q: got %v, %v; want false", ok, err)
	}
}

func TestShowResultPrintsWarningsAndLogFileLast(t *testing.T) {
	c, out := scripted()
	c.ShowResult(Result{LogPath: "C:/Backups/run.log"})
	c.ShowResult(Result{Warnings: 2, LogPath: "C:/Backups/run.log"})
	want := "\nLog file: C:/Backups/run.log\nWarnings: 2\n\nLog file: C:/Backups/run.log\n"
	if got := out.String(); got != want {
		t.Fatalf("unexpected output.\nwant: %q\n got: %q", want, got)
	}
}
