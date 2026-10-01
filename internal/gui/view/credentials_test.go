package view

import (
	"RestoreSafe/internal/gui/flow"
	"RestoreSafe/internal/workflow/interact"
	"strings"
	"testing"
)

func TestUnlockDialog(t *testing.T) {
	t.Parallel()
	d := UnlockDialogOf(flow.Question{Prompt: "Enter backup password: "})
	if d.Title != "Unlock your backups" || d.Intro != "Enter the password of your backups." || d.Error != "" {
		t.Fatalf("first question %+v", d)
	}
	if len(d.Fields) != 1 || !d.Fields[0].Masked || d.OK != "Unlock" || d.Cancel == "" {
		t.Fatalf("fields %+v", d)
	}

	d = UnlockDialogOf(flow.Question{Prompt: "Enter backup password: ", Message: "Wrong password. 2 attempt(s) remaining.", Retry: true})
	if d.Error != "Wrong password. 2 attempts left." {
		t.Fatalf("retry %q", d.Error)
	}
	d = UnlockDialogOf(flow.Question{Prompt: "Enter backup password: ", Message: "Wrong password. 1 attempt(s) remaining.", Retry: true})
	if d.Error != "Wrong password. 1 attempt left." {
		t.Fatalf("last retry %q", d.Error)
	}

	d = UnlockDialogOf(flow.Question{Prompt: "Enter recovery code: "})
	if len(d.Fields) != 1 || d.Fields[0].Masked || d.Fields[0].Label != "Recovery code" {
		t.Fatalf("the recovery code is typed unmasked: %+v", d)
	}
	checkWriting(t, d)
}

func TestNewKeysDialogsCountTheSteps(t *testing.T) {
	t.Parallel()
	all := interact.KeyPlan{New: true, Password: true, YubiKeys: 2, RecoveryCode: true}
	q := flow.Question{Prompt: "Enter new backup password (at least 12 characters): "}
	d := NewPasswordDialogOf(q, all)
	if d.Title != "Create your keys · Step 1 of 4" || !strings.HasPrefix(d.Hint, "At least 12 characters.") || len(d.Fields) != 2 {
		t.Fatalf("password step %+v", d)
	}
	if d.Note != "Steps: password › register your YubiKey › register your spare YubiKey › recovery code" {
		t.Fatalf("steps %q", d.Note)
	}
	if s := SpareYubiKeyDialogOf(flow.Question{}, all); s.Title != "Create your keys · Step 3 of 4" || s.Error != "" {
		t.Fatalf("spare step %+v", s)
	}
	s := SpareYubiKeyDialogOf(flow.Question{Message: "This is YubiKey 1. Remove it and insert your spare YubiKey.", Retry: true}, all)
	if s.Error != "This is YubiKey 1. Remove it and insert your spare YubiKey." {
		t.Fatalf("spare retry %q", s.Error)
	}
	r := RecoveryCodeDialogOf("K7QF-9M2D-XW4P-HT6N-3JBV-R8LC", all)
	if r.Title != "Your recovery code · Step 4 of 4" || r.Cancel != "" || strings.Join(r.CodeLines, "|") != "K7QF-9M2D-XW4P|HT6N-3JBV-R8LC" {
		t.Fatalf("recovery code %+v", r)
	}
	retry := RetypeDialogOf(flow.Question{Message: "The code does not match the recovery code shown above. Remedy: Check your note. 2 attempt(s) remaining.", Retry: true}, all)
	if retry.Error != "The code does not match the recovery code shown above. Check your note. 2 attempts left." {
		t.Fatalf("retype retry %q", retry.Error)
	}

	only := interact.KeyPlan{New: true, Password: true}
	mismatch := flow.Question{Prompt: q.Prompt, Message: "Passwords do not match. Please try again.", Retry: true}
	if d := NewPasswordDialogOf(mismatch, only); d.Title != "Create your keys" || d.Note != "" || d.Error != "Passwords do not match." {
		t.Fatalf("password only %+v", d)
	}
	checkWriting(t, NewPasswordDialogOf(mismatch, all))
}

func TestCodeLines(t *testing.T) {
	t.Parallel()
	if got := CodeLines("ABC"); len(got) != 1 || got[0] != "ABC" {
		t.Fatalf("a code without groups stays on one line: %q", got)
	}
	if got := CodeLines("AAAAA-BBBBB-CCCCC-DDDDD-EEEEE"); strings.Join(got, "|") != "AAAAA-BBBBB-CCCCC|DDDDD-EEEEE" {
		t.Fatalf("odd group count: %q", got)
	}
}
