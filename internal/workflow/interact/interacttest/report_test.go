package interacttest

import (
	"strings"
	"testing"

	"github.com/phsc84/restoresafe/internal/workflow/interact"
)

func TestWriteReport(t *testing.T) {
	t.Parallel()
	r := interact.Report{
		Title: "Test preflight",
		Sections: []interact.Section{
			{Rows: []interact.Row{
				interact.Heading("Directories"),
				interact.Item(interact.StatusNone, "Path: C:/x"),
				interact.Item(interact.StatusOK, "Docs", "with full backup Docs_FULL"),
				interact.Item(interact.StatusError, "Pics"),
				interact.Field("Authentication", "password only"),
				interact.Field("Keys", "existing"),
			}},
			{Rows: []interact.Row{
				interact.Field("Needed space", "1 B"),
				interact.Item(interact.StatusWarn, "approaching the limit"),
				interact.Field("Verify after backup", "enabled"),
			}},
			{Rows: []interact.Row{interact.Heading("Empty")}},
		},
		Issues: []interact.Issue{{Status: interact.StatusError, Text: "Pics is broken."}, {Status: interact.StatusWarn, Text: "Slow drive."}},
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
