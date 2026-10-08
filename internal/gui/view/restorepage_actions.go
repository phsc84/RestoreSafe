package view

import (
	"fmt"
	"strings"

	"github.com/phsc84/restoresafe/internal/format/naming"
)

// ActionBar is the selection in words and its actions (GUI spec BK-4).
type ActionBar struct {
	Text            string
	Restore, Verify Button
	// Sets are the selected sets that can be restored or verified.
	Sets []string
	// What names the selection in a question: "today, 09:12"; Whole is set
	// when Sets are every folder of that backup.
	What  string
	Whole bool
}

// SelectionOf words the selection: a set (set != "") or a whole run.
func SelectionOf(p RestorePage, runID naming.BackupID, set string) ActionBar {
	bar := ActionBar{
		Text:    selectionHint,
		Restore: Button{Text: buttonRestore, Action: ActionRestore},
		Verify:  Button{Text: buttonVerify, Action: ActionVerify},
	}
	for _, g := range p.Groups {
		if g.RunID != runID || (runID == "" && set == "") {
			continue
		}
		if set == "" {
			if len(g.Rows) == 0 {
				bar.Text = fmt.Sprintf(selectionLogOnly, capitalize(g.When))
				return bar
			}
			bar.Text = fmt.Sprintf(selectionRun, g.When, folderPhrase(len(g.Rows)))
			bar.What = g.When
			unusable := ""
			for _, row := range g.Rows {
				if row.Usable {
					bar.Sets = append(bar.Sets, row.Set)
				} else {
					unusable = row.Reason
				}
			}
			bar.Whole = unusable == ""
			return enable(bar, unusable)
		}
		for _, row := range g.Rows {
			if row.Set != set {
				continue
			}
			bar.Text = setWords(row, g.When)
			bar.What = bar.Text
			if row.Usable {
				bar.Sets = []string{row.Set}
			}
			return enable(bar, row.Reason)
		}
	}
	// Incomplete sets have no run.
	for _, g := range p.Groups {
		for _, row := range g.Rows {
			if row.Set == set && set != "" {
				bar.Text = setWords(row, row.Set)
				return enable(bar, row.Reason)
			}
		}
	}
	return bar
}

// enable enables the actions when sets are selected, with reason as the
// tooltip otherwise.
func enable(bar ActionBar, reason string) ActionBar {
	ok := len(bar.Sets) > 0
	bar.Restore.Enabled, bar.Verify.Enabled = ok, ok
	if !ok {
		bar.Restore.Reason, bar.Verify.Reason = reason, reason
		if reason != "" {
			bar.Text += " " + reason
		}
	}
	return bar
}

// setWords names a set as GUI spec 3.6 writes it: "Documents, differential 3
// of today".
func setWords(row BackupRow, when string) string {
	day := when
	if i := strings.Index(day, ","); i > 0 && !strings.Contains(day[:i], " ") {
		day = day[:i] // "today, 09:12" -> "today"
	}
	if row.Badge.Kind == BadgeDiff {
		return fmt.Sprintf(setDiffOf, row.Folder, strings.TrimPrefix(row.Badge.Text, "DIFF "), day)
	}
	return fmt.Sprintf(setFullOf, row.Folder, day)
}
