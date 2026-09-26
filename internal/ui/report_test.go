package ui

import (
	"strings"
	"testing"
)

func TestWriteReport(t *testing.T) {
	t.Parallel()
	r := Report{
		Title: "Test preflight",
		Sections: []Section{
			{Rows: []Row{
				Heading("Directories"),
				Item(StatusNone, "Path: C:/x"),
				Item(StatusOK, "Docs", "with full backup Docs_FULL"),
				Item(StatusError, "Pics"),
				Field("Authentication", "password only"),
				Field("Keys", "existing"),
			}},
			{Rows: []Row{
				Field("Needed space", "1 B"),
				Item(StatusWarn, "approaching the limit"),
				Field("Verify after backup", "enabled"),
			}},
			{Rows: []Row{Note("Local staging enabled."), Heading("Empty")}},
		},
		Issues: []Issue{{StatusError, "Pics is broken."}, {StatusWarn, "Slow drive."}},
	}
	want := `
Test preflight
--------------
Directories:
  Path: C:/x
  [OK] Docs
          → with full backup Docs_FULL
  [ERROR] Pics
Authentication: password only
Keys          : existing

Needed space       : 1 B
  [WARN] approaching the limit
Verify after backup: enabled

Local staging enabled.
Empty:

[ERROR] Pics is broken.
[WARN] Slow drive.
`
	var sb strings.Builder
	WriteReport(&sb, r)
	if got := sb.String(); got != want {
		t.Fatalf("unexpected report.\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestReportHasErrors(t *testing.T) {
	t.Parallel()
	if (Report{Issues: []Issue{{StatusWarn, "w"}}}).HasErrors() {
		t.Fatal("a warning must not block")
	}
	if !(Report{Issues: []Issue{{StatusWarn, "w"}, {StatusError, "e"}}}).HasErrors() {
		t.Fatal("an error must block")
	}
}
