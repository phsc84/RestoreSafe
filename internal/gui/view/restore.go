package view

import (
	"RestoreSafe/internal/format/catalog"
	"RestoreSafe/internal/format/naming"
	"RestoreSafe/internal/workflow/health"
	"RestoreSafe/internal/workflow/interact"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// RestorePointOf names when the run runID was made: "today, 09:12"; "" for
// an unknown run.
func RestorePointOf(s *health.Snapshot, runID naming.BackupID, now time.Time) string {
	if s == nil {
		return ""
	}
	for _, run := range s.Runs {
		if run.RunID == runID {
			return When(run.Created, now)
		}
	}
	return ""
}

// runSets returns the complete sets of a run, by folder name.
func runSets(s *health.Snapshot, runID naming.BackupID) []catalog.SetInfo {
	var sets []catalog.SetInfo
	for _, info := range s.Sets {
		if info.Complete() && naming.BackupID(info.Header.RunID) == runID {
			sets = append(sets, info)
		}
	}
	for i := 1; i < len(sets); i++ {
		for j := i; j > 0 && sets[j].Entry.DirectoryName < sets[j-1].Entry.DirectoryName; j-- {
			sets[j], sets[j-1] = sets[j-1], sets[j]
		}
	}
	return sets
}

// baseOf returns the full backup a set is restored with: none for a full
// backup (ok), the complete full of its chain for a differential (ok), or
// ok=false when that is missing.
func baseOf(s *health.Snapshot, info catalog.SetInfo) (*catalog.SetInfo, bool) {
	if !info.Entry.IsDiff() {
		return nil, true
	}
	for i := range s.Sets {
		b := &s.Sets[i]
		if b.Complete() && !b.Entry.IsDiff() && b.Entry.ChainKey() == info.Entry.ChainKey() {
			return b, true
		}
	}
	return nil, false
}

// FolderChoice is a set of the run, a row of the Restore window's table
// (RW-5).
type FolderChoice struct {
	Set    naming.BackupEntry
	Folder string
	Badge  Badge
	// About is the size read: for a differential with its full backup.
	About string
	Bytes int64
	// Enabled is false for a set that cannot be restored: its full backup
	// is missing (UnrestorableNote).
	Enabled bool
}

// RestoreFoldersOf lists the sets of the run runID.
func RestoreFoldersOf(s *health.Snapshot, runID naming.BackupID) []FolderChoice {
	if s == nil {
		return nil
	}
	var out []FolderChoice
	for _, info := range runSets(s, runID) {
		c := FolderChoice{Set: info.Entry, Folder: info.Entry.DirectoryName, Badge: badgeOf(info.Entry), Bytes: info.SizeBytes, Enabled: true}
		base, ok := baseOf(s, info)
		switch {
		case !ok:
			c.Enabled = false
		case base != nil:
			c.Bytes += base.SizeBytes
		}
		c.About = Size(c.Bytes)
		out = append(out, c)
	}
	return out
}

// UnrestorableNote names the folders of choices that can't be restored and
// why (RW-6); "" when all can.
func UnrestorableNote(choices []FolderChoice) string {
	var names []string
	for _, c := range choices {
		if !c.Enabled {
			names = append(names, c.Folder)
		}
	}
	switch len(names) {
	case 0:
		return ""
	case 1:
		return fmt.Sprintf(unrestorableOne, names[0])
	}
	return fmt.Sprintf(unrestorableMany, joinAnd(names))
}

// Chosen returns the checked sets of choices that can be restored.
func Chosen(choices []FolderChoice, checked map[naming.BackupEntry]bool) []naming.BackupEntry {
	var sets []naming.BackupEntry
	for _, c := range choices {
		if c.Enabled && checked[c.Set] {
			sets = append(sets, c.Set)
		}
	}
	return sets
}

// RestoreView is the Restore window's choose page below the destination
// (RW-2, RW-5, RW-6), worded like the backup plan (6.1).
type RestoreView struct {
	Heading string
	// Checks is the Check cell of each checked folder (RW-5) and Tips the
	// tooltip of its row; a folder without a check shows checkNone.
	Checks map[naming.BackupEntry]TableCell
	Tips   map[naming.BackupEntry]string
	// Hint replaces Space and Unlock while there is nothing to check.
	Hint     string
	HintTone Tone
	Checking bool
	Space    PlanLine
	Unlock   PlanLine
	Note     string
	Issues   []IssueLine
	Details  Button
	// Start is enabled when nothing blocks the restore.
	Start, Cancel Button
}

// CheckNone is the Check cell of a folder that isn't checked.
const CheckNone = "-"

// RestoreViewOf words the choices: the run's folders, which are checked,
// the run's date when, the destination dest and its check (plan, or err
// why there is none; checking while it runs).
func RestoreViewOf(choices []FolderChoice, checked map[naming.BackupEntry]bool, when, dest string, plan *interact.RestorePlan, err error, checking bool) RestoreView {
	n := len(Chosen(choices, checked))
	v := RestoreView{
		Heading: fmt.Sprintf(restoreHeadingNone, when),
		Note:    restoreNote,
		Details: Button{Text: linkShowDetails, Action: ActionShowDetails},
		Start:   Button{Text: buttonStart, Action: ActionStartRestore},
		Cancel:  Button{Text: buttonCancel, Action: ActionCancel, Enabled: true},
	}
	if n > 0 {
		v.Heading = fmt.Sprintf(restoreHeading, folderPhrase(n), when)
	}
	if note := UnrestorableNote(choices); note != "" {
		v.Issues = append(v.Issues, IssueLine{Text: note, Tone: ToneWarning, Glyph: GlyphWarning})
	}
	switch {
	case n == 0:
		v.Hint = hintChooseFolder
	case strings.TrimSpace(dest) == "":
		v.Hint = destHintEmpty
	case !filepath.IsAbs(dest) || filepath.VolumeName(dest) == "":
		v.Hint = destHintFullPath
	case checking || (plan == nil && err == nil):
		v.Hint, v.Checking = statusChecking, true
	case err != nil:
		v.Hint, v.HintTone = issueText(err.Error()), ToneError
	}
	if v.Hint != "" {
		if v.HintTone == ToneNeutral {
			v.HintTone = ToneSecondary
		}
		return v
	}

	v.Checks = map[naming.BackupEntry]TableCell{}
	v.Tips = map[naming.BackupEntry]string{}
	var exists []string
	for _, s := range plan.Sets {
		dir := Path(s.OutputDir)
		check := TableCell{Text: destNew, Tone: ToneSuccess}
		switch {
		case s.OutputCode == interact.CodeRestoreTargetExists:
			check = TableCell{Text: destExists, Tone: ToneError}
			exists = append(exists, s.Set.DirectoryName)
		case s.OutputProblem != "":
			check = TableCell{Text: destInvalid, Tone: ToneError}
			v.Issues = append(v.Issues, IssueLine{Text: fmt.Sprintf(destInvalidLine, s.Set.DirectoryName, issueText(s.OutputProblem)), Tone: ToneError, Glyph: GlyphError})
		}
		v.Checks[s.Set] = check
		v.Tips[s.Set] = joinTip(dir, check.Text)
	}
	switch len(exists) {
	case 0:
	case 1:
		v.Issues = append(v.Issues, IssueLine{Text: fmt.Sprintf(destExistsOne, exists[0]), Tone: ToneError, Glyph: GlyphError})
	default:
		v.Issues = append(v.Issues, IssueLine{Text: fmt.Sprintf(destExistsMany, joinAnd(exists)), Tone: ToneError, Glyph: GlyphError})
	}
	v.Space = restoreSpaceLine(*plan)
	v.Unlock = PlanLine{Label: planUnlock, Text: restoreUnlockText(plan.Unlock)}
	for _, issue := range plan.Issues {
		switch issue.Code {
		case interact.CodeRestoreTargetExists, interact.CodeRestoreTargetInvalid, interact.CodeSpaceInsufficient, interact.CodeFreeSpaceUnknown:
			continue // shown in the Check column, its lines and the Space line
		}
		line := IssueLine{Text: issueText(issue.Text), Tone: ToneWarning, Glyph: GlyphWarning}
		if issue.Status == interact.StatusError {
			line.Tone, line.Glyph = ToneError, GlyphError
		}
		v.Issues = append(v.Issues, line)
	}
	v.Details.Enabled = true
	v.Start.Enabled = !plan.HasErrors()
	return v
}

// restoreSpaceLine compares the space a restore needs with the free space,
// worded as the backup plan's Space line (BP-2).
func restoreSpaceLine(p interact.RestorePlan) PlanLine {
	line := PlanLine{Label: planSpace, Tone: ToneSuccess, Glyph: GlyphCheck}
	line.Text = fmt.Sprintf(spaceNeeded, Size(p.NeededBytes))
	switch {
	case p.FreeBytes < 0:
		line.Text += spaceFreeUnknown
		line.Tone, line.Glyph = ToneError, GlyphError
		return line
	case hasCode(p.Issues, interact.CodeSpaceInsufficient):
		line.Tone, line.Glyph = ToneError, GlyphError
	case float64(p.NeededBytes) > 0.9*float64(p.FreeBytes):
		line.Tone, line.Glyph = ToneWarning, GlyphWarning
	}
	line.Text += fmt.Sprintf(spaceFree, Size(p.FreeBytes))
	return line
}

func hasCode(issues []interact.Issue, code interact.Code) bool {
	for _, i := range issues {
		if i.Code == code {
			return true
		}
	}
	return false
}

// restoreUnlockText lists the prompts that follow Start, worded as the
// backup plan's Unlock line (BP-2): "One YubiKey touch, then your
// password, or recovery code".
func restoreUnlockText(u interact.UnlockPlan) string {
	text := unlockPassword
	switch {
	case u.Password && u.YubiKey:
		text = unlockTouchAndPassword
	case u.YubiKey:
		text = unlockTouch
	}
	if u.RecoveryCode {
		text += unlockOrRecovery
	}
	return text
}
