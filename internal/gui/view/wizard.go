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

// Pages of the restore wizard (spec 8).
const (
	WizardWhen = iota
	WizardFolders
	WizardDestination
	WizardCheck
	WizardProgress
	WizardResult
)

// WizardSteps is the step indicator of page (RW-1): the pages before it
// are done, it is current.
func WizardSteps(page int) []Step {
	names := []string{stepWhen, stepFolders, stepDestination, stepCheck}
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

// RestorePoint is a backup run of page 1 (RW-3).
type RestorePoint struct {
	RunID   naming.BackupID
	When    string
	Folders string
	Size    string
	// Enabled is false when none of its sets can be restored; Reason says
	// why.
	Enabled bool
	Reason  string
}

// RestorePointsOf lists the runs to restore from, newest first.
func RestorePointsOf(s *health.Snapshot, now time.Time) []RestorePoint {
	if s == nil {
		return nil
	}
	var points []RestorePoint
	for _, run := range s.Runs {
		p := RestorePoint{RunID: run.RunID, When: capitalize(When(run.Created, now)), Reason: reasonNoneRestorable}
		var names []string
		var bytes int64
		for _, info := range runSets(s, run.RunID) {
			names = append(names, info.Entry.DirectoryName)
			bytes += info.SizeBytes
			if _, ok := baseOf(s, info); ok {
				p.Enabled, p.Reason = true, ""
			}
		}
		p.Folders = strings.Join(names, ", ")
		p.Size = Size(bytes)
		points = append(points, p)
	}
	return points
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

// FolderChoice is a set of page 2 (RW-4).
type FolderChoice struct {
	Set    naming.BackupEntry
	Folder string
	Badge  Badge
	// With is the full backup read with a differential: "+ FULL of 1 Sep".
	With  string
	About string
	Bytes int64
	// Enabled is false for a set that cannot be restored; Reason says why.
	Enabled bool
	Reason  string
}

// RestoreFoldersOf lists the sets of the run runID.
func RestoreFoldersOf(s *health.Snapshot, runID naming.BackupID, now time.Time) []FolderChoice {
	if s == nil {
		return nil
	}
	var out []FolderChoice
	for _, info := range runSets(s, runID) {
		c := FolderChoice{Set: info.Entry, Folder: info.Entry.DirectoryName, Badge: badgeOf(info.Entry), Bytes: info.SizeBytes, Enabled: true}
		base, ok := baseOf(s, info)
		switch {
		case !ok:
			c.Enabled, c.Reason = false, reasonBaseMissing
		case base != nil:
			c.With = fmt.Sprintf(withFullOf, ShortDay(base.Created(), now))
			c.Bytes += base.SizeBytes
		}
		c.About = fmt.Sprintf(aboutSize, Size(c.Bytes))
		out = append(out, c)
	}
	return out
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

// DestinationView is page 3 (RW-5).
type DestinationView struct {
	// Hint is shown instead of the checks while there is nothing to check.
	Hint string
	// Folders are the folders the restore creates, with their state.
	Folders []InfoLine
	Space   *InfoLine
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
	for _, s := range plan.Sets {
		dir := Path(s.OutputDir)
		switch {
		case s.OutputCode == interact.CodeRestoreTargetExists:
			v.Folders = append(v.Folders,
				InfoLine{Text: fmt.Sprintf(destExists, dir), Tone: ToneError, Glyph: GlyphError, Path: true},
				InfoLine{Text: destExistsRemedy, Tone: ToneSecondary})
		case s.OutputProblem != "":
			v.Folders = append(v.Folders, InfoLine{Text: fmt.Sprintf(destInvalid, dir, issueText(s.OutputProblem)), Tone: ToneError, Glyph: GlyphError})
		default:
			v.Folders = append(v.Folders, InfoLine{Text: dir, Tone: ToneSuccess, Glyph: GlyphCheck, Path: true})
		}
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

// CheckView is page 4, the restore preflight (RW-6).
type CheckView struct {
	Heading string
	Lines   []PlanLine
	Note    string
	Issues  []IssueLine
	Details Button
	// Restore is nil when an issue blocks the restore.
	Restore *Button
}

// RestoreCheckOf words the workflow's plan p of the run made when.
func RestoreCheckOf(p interact.RestorePlan, when string, now time.Time) CheckView {
	v := CheckView{Heading: checkHeading, Note: checkNote, Details: Button{Text: linkShowDetails, Action: ActionShowDetails, Enabled: true}}
	from := []string{when}
	var to []string
	for _, s := range p.Sets {
		from = append(from, setReadWords(s.SetPlan, now))
		to = append(to, fmt.Sprintf(checkToNew, Path(s.OutputDir)))
	}
	v.Lines = []PlanLine{
		{Label: checkFrom, Text: strings.Join(from, "\n")},
		{Label: checkTo, Paths: to},
		*spaceLine2(p),
		{Label: planUnlock, Text: unlockWords(p.Unlock)},
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

func spaceLine2(p interact.RestorePlan) *PlanLine {
	s := restoreSpace(p)
	return &PlanLine{Label: planSpace, Text: s.Text, Tone: s.Tone, Glyph: s.Glyph}
}

// setReadWords names what is read for a set: "Documents: differential 3 +
// full backup of 1 Sep".
func setReadWords(s interact.SetPlan, now time.Time) string {
	if !s.Set.IsDiff() {
		return fmt.Sprintf(readFull, s.Set.DirectoryName)
	}
	day := s.Base.Date
	if d, err := time.ParseInLocation("2006-01-02", s.Base.Date, time.Local); err == nil {
		day = ShortDay(d, now)
	}
	return fmt.Sprintf(readDiff, s.Set.DirectoryName, s.Set.DiffNumber, day)
}

// unlockWords names how the keys are unlocked: "Password and one YubiKey
// touch, or recovery code".
func unlockWords(u interact.UnlockPlan) string {
	text := capitalize(u.Methods)
	if u.RecoveryCode {
		text += unlockOrRecovery
	}
	return text
}
