package view

import (
	"RestoreSafe/internal/workflow/interact"
	"fmt"
	"time"
)

// VerifyPlanView is the Verify window (figure 7.3), laid out and worded
// like the backup plan (6.1).
type VerifyPlanView struct {
	Heading string
	Folders Table
	Read    PlanLine
	Unlock  PlanLine
	Note    string
	Issues  []IssueLine
	Details Button
	// Start is nil when an issue blocks the verification.
	Start  *Button
	Cancel Button
}

// VerifyPlanOf words the plan the workflow made of the selection; what
// names it ("today, 09:12").
func VerifyPlanOf(p interact.VerifyPlan, what string, now time.Time) VerifyPlanView {
	v := VerifyPlanView{
		Heading: fmt.Sprintf(verifyHeading, folderPhrase(len(p.Sets)), what),
		Folders: Table{Name: PlanColumnFolder, Columns: []Column{
			{Title: PlanColumnFolder, Fill: true}, {Title: PlanColumnType, Width: 80}, {Title: PlanColumnAbout, Width: 80, Right: true},
		}},
		Read:    PlanLine{Label: verifyReadLabel, Text: fmt.Sprintf(verifyReadText, Size(p.Bytes))},
		Unlock:  PlanLine{Label: planUnlock, Text: restoreUnlockText(p.Unlock)},
		Note:    verifyNote(p, now),
		Details: Button{Text: linkShowDetails, Action: ActionShowDetails, Enabled: true},
		Cancel:  Button{Text: buttonCancel, Action: ActionCancel, Enabled: true},
	}
	for _, s := range p.Sets {
		b := badgeOf(s.Set)
		row := TableRow{Cells: []TableCell{{Text: s.Set.DirectoryName}, {Badge: &b}, {Text: Size(s.Bytes)}}}
		if s.Problem != "" {
			row.Cells[0].Tone, row.Cells[2] = ToneError, TableCell{Text: CheckNone}
			row.Tip = issueText(s.Problem, s.Remedy)
		}
		v.Folders.Rows = append(v.Folders.Rows, row)
	}
	for _, issue := range p.Issues {
		line := IssueLine{Text: issueText(issue.Text, issue.Remedy), Tone: ToneWarning, Glyph: GlyphWarning}
		if issue.Status == interact.StatusError {
			line.Tone, line.Glyph = ToneError, GlyphError
		}
		v.Issues = append(v.Issues, line)
	}
	if !p.HasErrors() {
		v.Start = &Button{Text: buttonStart, Action: ActionStartVerify, Enabled: true}
	}
	return v
}

// verifyNote says what a verification does, and which full backups it reads
// with the differentials.
func verifyNote(p interact.VerifyPlan, now time.Time) string {
	note := verifyChecks
	bases, days := map[string]bool{}, map[string]bool{}
	var baseDay time.Time
	for _, s := range p.Sets {
		if s.Base.DirectoryName != "" && !bases[s.Base.String()] {
			bases[s.Base.String()] = true
			days[s.Base.Date] = true
			if d, err := time.ParseInLocation("2006-01-02", s.Base.Date, time.Local); err == nil {
				baseDay = d
			}
		}
	}
	switch {
	case len(bases) == 1:
		note += " " + fmt.Sprintf(verifyReadsBase, ShortDay(baseDay, now))
	case len(days) == 1:
		note += " " + fmt.Sprintf(verifyReadsBasesOf, ShortDay(baseDay, now))
	case len(bases) > 1:
		note += " " + verifyReadsBases
	}
	return note
}
