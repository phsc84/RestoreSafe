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

// Pages of the restore wizard (GUI spec 8). The run to restore from is
// chosen on the Restore backup page (BK-4).
const (
	WizardFolders = iota
	WizardDestination
	WizardCheck
	WizardProgress
	WizardResult
)

// WizardSteps is the step indicator of page (RW-1): the pages before it
// are done, it is current.
func WizardSteps(page int) []Step {
	names := []string{stepFolders, stepDestination, stepCheck}
	steps := make([]Step, len(names))
	for i, n := range names {
		steps[i] = Step{Text: fmt.Sprintf(stepNumbered, i+1, n)}
		switch {
		case i < page:
			steps[i].State = StepDone
		case i == page:
			steps[i].State = StepCurrent
		}
	}
	return steps
}

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

// FoldersHeading is the heading of page 1, naming the restore point when
// (RW-4): "Which folders do you want back from today, 09:12?".
func FoldersHeading(when string) string {
	return fmt.Sprintf(foldersHeading, when)
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

// FolderChoice is a set of page 1 (RW-4).
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
		c.About = fmt.Sprintf(aboutSize, Size(c.Bytes))
		out = append(out, c)
	}
	return out
}

// UnrestorableNote names the folders of choices that can't be restored and
// why (RW-4); "" when all can.
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

// SelectionFooter words the checked folders: "2 folders · about 92 GB"
// (RW-2); "" when none is checked.
func SelectionFooter(choices []FolderChoice, checked map[naming.BackupEntry]bool) string {
	n := 0
	var bytes int64
	for _, c := range choices {
		if c.Enabled && checked[c.Set] {
			n++
			bytes += c.Bytes
		}
	}
	if n == 0 {
		return ""
	}
	return fmt.Sprintf(footerSelection, folderPhrase(n), Size(bytes))
}

// DestinationView is page 2 (RW-5).
type DestinationView struct {
	// Hint is shown instead of the checks while there is nothing to check.
	Hint string
	// Folders are the folders the restore creates, with their state; empty
	// while there is a hint.
	Folders Table
	// Remedy says what to do about folders that already exist.
	Remedy string
	Space  *InfoLine
	// Checking is set while the checks run.
	Checking bool
	Next     bool
}

// DestinationOf words the checks of dest: plan is the workflow's plan for
// it (nil while checking), err why there is none.
func DestinationOf(dest string, plan *interact.RestorePlan, err error, checking bool) DestinationView {
	switch {
	case strings.TrimSpace(dest) == "":
		return DestinationView{Hint: destHintEmpty}
	case !filepath.IsAbs(dest) || filepath.VolumeName(dest) == "":
		return DestinationView{Hint: destHintFullPath}
	case checking || (plan == nil && err == nil):
		return DestinationView{Hint: statusChecking, Checking: true}
	case err != nil:
		return DestinationView{Hint: issueText(err.Error())}
	}
	v := DestinationView{Next: !plan.HasErrors()}
	v.Folders = Table{Name: destCreates, Columns: []Column{{Title: columnFolder, Width: 140}, {Title: columnRestoredTo, Fill: true}, {Title: columnCheck, Width: 160}}}
	for _, s := range plan.Sets {
		dir := Path(s.OutputDir)
		check := TableCell{Text: destNew, Tone: ToneSuccess}
		switch {
		case s.OutputCode == interact.CodeRestoreTargetExists:
			check = TableCell{Text: destExists, Tone: ToneError}
			v.Remedy = destExistsRemedy
		case s.OutputProblem != "":
			check = TableCell{Text: fmt.Sprintf(destInvalid, issueText(s.OutputProblem)), Tone: ToneError}
		}
		v.Folders.Rows = append(v.Folders.Rows, TableRow{
			Tip:   joinTip(dir, check.Text),
			Cells: []TableCell{{Text: s.Set.DirectoryName}, {Text: dir}, check},
		})
	}
	v.Space = restoreSpace(*plan)
	return v
}

// restoreSpace compares the space a restore needs with the free space.
func restoreSpace(p interact.RestorePlan) *InfoLine {
	switch {
	case p.FreeBytes < 0:
		return &InfoLine{Text: spaceUnknownDest, Tone: ToneError, Glyph: GlyphError}
	case hasCode(p.Issues, interact.CodeSpaceInsufficient):
		return &InfoLine{Text: fmt.Sprintf(spaceTooLittle, Size(p.NeededBytes), Size(p.FreeBytes)), Tone: ToneError, Glyph: GlyphError}
	case float64(p.NeededBytes) > 0.9*float64(p.FreeBytes):
		return &InfoLine{Text: fmt.Sprintf(spaceTight, Size(p.NeededBytes), Size(p.FreeBytes)), Tone: ToneWarning, Glyph: GlyphWarning}
	}
	return &InfoLine{Text: fmt.Sprintf(spaceEnough, Size(p.NeededBytes), Size(p.FreeBytes)), Tone: ToneSuccess, Glyph: GlyphCheck}
}

func hasCode(issues []interact.Issue, code interact.Code) bool {
	for _, i := range issues {
		if i.Code == code {
			return true
		}
	}
	return false
}

// CheckView is page 3, the restore preflight (RW-6): a sentence with the
// run and the destination, the folders and what is read for each, then
// space and unlocking.
type CheckView struct {
	Heading string
	// Summary leads to Destination: "2 folders from the backup of today,
	// 09:12, each into a new folder in".
	Summary     string
	Destination string
	Folders     Table
	Space       InfoLine
	Unlock      InfoLine
	Note        string
	Issues      []IssueLine
	Details     Button
	// Restore is nil when an issue blocks the restore.
	Restore *Button
}

// RestoreCheckOf words the workflow's plan p of the run made when ("today,
// 09:12").
func RestoreCheckOf(p interact.RestorePlan, when string, now time.Time) CheckView {
	summary := checkSummaryMany
	if len(p.Sets) == 1 {
		summary = checkSummaryOne
	}
	v := CheckView{
		Heading:     checkHeading,
		Summary:     fmt.Sprintf(summary, folderPhrase(len(p.Sets)), when),
		Destination: Path(p.Destination),
		Folders:     Table{Name: checkFoldersName, Columns: []Column{{Title: columnFolder, Width: 140}, {Title: columnReadFrom, Fill: true}}},
		Space:       *restoreSpace(p),
		Unlock:      InfoLine{Text: unlockWords(p.Unlock), Glyph: GlyphKey},
		Note:        checkNote,
		Details:     Button{Text: linkShowDetails, Action: ActionShowDetails, Enabled: true},
	}
	for _, s := range p.Sets {
		read := setReadWords(s.SetPlan, now)
		v.Folders.Rows = append(v.Folders.Rows, TableRow{
			Tip:   joinTip(Path(s.OutputDir), read),
			Cells: []TableCell{{Text: s.Set.DirectoryName}, {Text: read}},
		})
	}
	for _, issue := range p.Issues {
		line := IssueLine{Text: issueText(issue.Text), Tone: ToneWarning, Glyph: GlyphWarning}
		if issue.Status == interact.StatusError {
			line.Tone, line.Glyph = ToneError, GlyphError
		}
		v.Issues = append(v.Issues, line)
	}
	if !p.HasErrors() {
		v.Restore = &Button{Text: buttonRestoreStart, Action: ActionStartRestore, Enabled: true}
	}
	return v
}

// setReadWords names what is read for a set: "Differential 3 + full backup
// of 1 Sep".
func setReadWords(s interact.SetPlan, now time.Time) string {
	if !s.Set.IsDiff() {
		return readFull
	}
	day := s.Base.Date
	if d, err := time.ParseInLocation("2006-01-02", s.Base.Date, time.Local); err == nil {
		day = ShortDay(d, now)
	}
	return fmt.Sprintf(readDiff, s.Set.DiffNumber, day)
}

// unlockWords names how the keys are unlocked: "Unlock with password +
// YubiKey, or recovery code".
func unlockWords(u interact.UnlockPlan) string {
	text := fmt.Sprintf(checkUnlock, u.Methods)
	if u.RecoveryCode {
		text += unlockOrRecovery
	}
	return text
}
