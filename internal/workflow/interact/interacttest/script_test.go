package interacttest

import (
	"RestoreSafe/internal/workflow/interact"
	"bytes"
	"errors"
	"strings"
	"testing"
)

func scripted(lines ...string) (*Script, *bytes.Buffer) {
	var out bytes.Buffer
	c := &Script{Out: &out, ReadLine: func(string) (string, error) {
		if len(lines) == 0 {
			return "", errors.New("no more lines")
		}
		line := lines[0]
		lines = lines[1:]
		return line, nil
	}}
	return c, &out
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
	both := interact.BackupStartOptions{OfferFull: true, OfferNewKeys: true}
	newKeys := interact.BackupStartOptions{OfferNewKeys: true}
	blockedFull := interact.BackupStartOptions{Blocked: true, OfferAutomatic: true}
	afterFull := interact.BackupStartOptions{OfferNewKeys: true, OfferAutomatic: true}
	const hintNewKeys = "Please enter y (yes), k (new keys), n (no)."
	const hintBoth = "Please enter y (yes), f (full backup), k (new keys), n (no)."
	const hintBlocked = "Please enter a (automatic plan), n (no)."
	cases := []struct {
		name    string
		opts    interact.BackupStartOptions
		answers []string
		want    interact.BackupStart
		hint    string
	}{
		{"new keys not offered asks yes/no", interact.BackupStartOptions{}, []string{"y"}, interact.BackupAsPlanned, ""},
		{"new keys not offered, no", interact.BackupStartOptions{}, []string{"n"}, interact.BackupCancel, ""},
		{"default starts as planned", both, []string{""}, interact.BackupAsPlanned, ""},
		{"f makes full backups", both, []string{"f"}, interact.BackupFull, ""},
		{"f not offered is invalid", newKeys, []string{"f", "y"}, interact.BackupAsPlanned, hintNewKeys},
		{"invalid answer is repeated", newKeys, []string{"maybe", "k"}, interact.BackupNewKeys, hintNewKeys},
		{"k creates new keys", both, []string{"K"}, interact.BackupNewKeys, ""},
		{"n cancels", both, []string{"x", "no"}, interact.BackupCancel, hintBoth},
		{"a returns to the automatic plan", afterFull, []string{"a"}, interact.BackupAutomatic, ""},
		{"a not offered is invalid", both, []string{"a", "y"}, interact.BackupAsPlanned, hintBoth},
		{"a blocked plan cannot start", blockedFull, []string{"y", "a"}, interact.BackupAutomatic, hintBlocked},
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

func TestWaitForSpareYubiKey(t *testing.T) {
	c, _ := scripted("", "Q")
	if ok, err := c.WaitForSpareYubiKey(interact.SpareQuestion{}); err != nil || !ok {
		t.Fatalf("Enter: got %v, %v; want true", ok, err)
	}
	if ok, err := c.WaitForSpareYubiKey(interact.SpareQuestion{}); err != nil || ok {
		t.Fatalf("q: got %v, %v; want false", ok, err)
	}
}

func TestShowResultPrintsWarningsAndLogFileLast(t *testing.T) {
	c, out := scripted()
	c.ShowResult(interact.Result{LogPath: "C:/Backups/run.log"})
	c.ShowResult(interact.Result{Warnings: 2, LogPath: "C:/Backups/run.log"})
	want := "\nLog file: C:/Backups/run.log\nWarnings: 2\n\nLog file: C:/Backups/run.log\n"
	if got := out.String(); got != want {
		t.Fatalf("unexpected output.\nwant: %q\n got: %q", want, got)
	}
}
