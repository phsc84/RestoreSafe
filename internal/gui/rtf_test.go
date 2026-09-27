package gui

import (
	"RestoreSafe/internal/workflow/interact"
	"strings"
	"testing"
)

func TestRTFEscape(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		`C:\Backups\{x}`: "C:\\\\Backups\\\\\\{x\\}",
		"a\nb":           `a\line b`,
		"→ ä":            `\u8594? \u228?`,
		"✔":              `\u10004?`,
		"\uFFFD":         `\u-3?`,
		"😀":              `\u-10179?\u-8704?`,
	}
	for in, want := range cases {
		if got := rtfEscape(in); got != want {
			t.Errorf("rtfEscape(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestReportRTF(t *testing.T) {
	t.Parallel()
	r := interact.Report{
		Title: "Backup preflight",
		Sections: []interact.Section{
			{Rows: []interact.Row{
				interact.Heading("Source directory(s)"),
				interact.Item(interact.StatusOK, `C:\Docs`, "Full backup"),
				interact.Item(interact.StatusError, "Pics"),
			}},
			{Rows: []interact.Row{interact.Field("Split size", "64 MB"), interact.Field("Verify after backup", "enabled"), interact.Note("Local staging enabled.")}},
		},
		Issues: []interact.Issue{{Status: interact.StatusWarn, Text: "Slow drive."}},
	}
	got := reportRTF(r, "Segoe UI", 9)

	if !strings.HasPrefix(got, `{\rtf1`) || !strings.HasSuffix(got, "}") {
		t.Fatalf("not an RTF document: %q", got)
	}
	if strings.Count(got, "{")-strings.Count(got, `\{`) != strings.Count(got, "}")-strings.Count(got, `\}`) {
		t.Fatalf("unbalanced groups: %q", got)
	}
	for _, want := range []string{
		`{\fonttbl{\f0\fswiss Segoe UI;}}`,
		`\fs18 `,                                 // 9 pt base size
		`{\b\fs24 Backup preflight}`,             // larger title
		`{\b Source directory(s)}`,               // bold heading
		"{\\cf1 \\u10004?}\\tab C:\\\\Docs\\par", // OK marker, escaped path
		`\u8594? Full backup`,                    // detail with arrow
		`{\cf4 \u10006?}\tab Pics\par`,           // error marker
		`\tab 64 MB\par`,                         // field value after the tab stop
		`\pard Local staging enabled.\par`,
		`{\cf3 \u9888?}\tab Slow drive.\par`, // issue with warning marker
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	// The field tab stop leaves room for the longest label of its section.
	if !strings.Contains(got, `\tx2510`) {
		t.Errorf("expected the field tab stop for \"Verify after backup\" at 2510 twips in\n%s", got)
	}
}
