package view

import (
	"RestoreSafe/internal/config"
	"RestoreSafe/internal/format/catalog"
	"RestoreSafe/internal/workflow/interact"
	"fmt"
	"strings"
	"time"
)

// PlanRow is one folder of the backup plan (spec BP-1).
type PlanRow struct {
	Name, Path string
	// Badge is the planned type; nil for a folder that is not backed up.
	Badge *Badge
	// Why explains the type, or says why the folder is not backed up.
	Why string
	// About is the size the run likely stores for the folder.
	About string
	Tone  Tone
	Glyph Glyph
}

// PlanLine is a labeled line under the plan's table (spec BP-2).
type PlanLine struct {
	Label, Text string
	Tone        Tone
	Glyph       Glyph
}

// IssueLine is a preflight issue (spec BP-5).
type IssueLine struct {
	Text  string
	Tone  Tone
	Glyph Glyph
}

// BackupPlanView is the backup plan dialog (spec 6.1).
type BackupPlanView struct {
	Title   string
	Heading string
	// KeysNote says why new keys are created, "" when the keys are reused.
	KeysNote string
	Rows     []PlanRow
	Space    PlanLine
	Unlock   PlanLine
	// Afterwards is verification and retention; Removes lists what retention
	// removes, shown by RemovesLink.
	Afterwards  PlanLine
	Removes     []string
	RemovesLink string
	// Note is about the size estimates, "" without a differential.
	Note    string
	Issues  []IssueLine
	Details Button
	// Start is nil when an issue blocks the backup; Full is "Full backup
	// instead" or "Back to plan"; Full and NewKeys are nil when not offered.
	Start, Full, NewKeys *Button
	Cancel               Button
}

// BackupPlanOf words the plan p. opts are the choices the workflow offers;
// nil while it has not asked yet, which disables Start.
func BackupPlanOf(p interact.BackupPlan, opts *interact.BackupStartOptions, cfg *config.Config, now time.Time) BackupPlanView {
	v := BackupPlanView{
		Title:   planTitle,
		Heading: fmt.Sprintf(planHeading, folderPhrase(backedUpCount(p)), Path(p.BackupDir)),
		Space:   spaceLine(p),
		Unlock:  PlanLine{Label: planUnlock, Text: unlockText(p.Keys)},
		Details: Button{Text: linkShowDetails, Action: ActionShowDetails, Enabled: true},
		Cancel:  Button{Text: buttonCancel, Action: ActionCancel, Enabled: true},
	}
	if p.Keys.New {
		v.KeysNote = fmt.Sprintf(planNewKeys, lowerFirst(p.Keys.NewKeysReason))
	}
	anyDiff := false
	for _, f := range p.Folders {
		v.Rows = append(v.Rows, planRow(f, now))
		anyDiff = anyDiff || (f.Differential && f.Problem == "" && !f.Skipped)
	}
	if anyDiff {
		v.Note = planEstimateNote
	}
	v.Afterwards, v.Removes = afterwards(p, cfg, now)
	if len(v.Removes) > 0 {
		v.RemovesLink = linkShowRemoved
	}
	for _, issue := range p.Issues {
		line := IssueLine{Text: issueText(issue.Text), Tone: ToneWarning, Glyph: GlyphWarning}
		if issue.Status == interact.StatusError {
			line.Tone, line.Glyph = ToneError, GlyphError
		}
		v.Issues = append(v.Issues, line)
	}

	if p.HasErrors() || (opts != nil && opts.Blocked) {
		return v
	}
	v.Start = &Button{Text: buttonStart, Action: ActionStartBackup, Enabled: opts != nil}
	if opts == nil {
		return v
	}
	switch {
	case opts.OfferAutomatic:
		v.Full = &Button{Text: buttonBackToPlan, Action: ActionAutomaticPlan, Enabled: true}
	case opts.OfferFull:
		v.Full = &Button{Text: buttonFullInstead, Action: ActionFullBackup, Enabled: true}
	}
	if opts.OfferNewKeys {
		v.NewKeys = &Button{Text: buttonNewKeys, Action: ActionNewKeys, Enabled: true}
	}
	return v
}

func planRow(f interact.FolderPlan, now time.Time) PlanRow {
	row := PlanRow{Name: f.Name, Path: Path(f.Path), Tone: ToneNeutral}
	switch {
	case f.Problem != "":
		row.Why, row.Tone, row.Glyph = issueText(f.Problem), ToneError, GlyphError
		return row
	case f.Skipped:
		row.Why, row.Tone = folderDuplicate, ToneSecondary
		return row
	case f.Differential:
		b := planBadge(f)
		row.Badge = &b
		row.Why = fmt.Sprintf(planWhyDiff, ShortDay(f.BaseCreated, now))
		row.About = Size(f.EstimatedBytes)
	default:
		b := planBadge(f)
		row.Badge = &b
		row.Why = capitalize(f.Reason)
		row.About = Size(f.AllBytes)
	}
	if f.Warning != "" {
		row.Why += ". " + capitalize(f.Warning)
		row.Tone, row.Glyph = ToneWarning, GlyphWarning
	}
	return row
}

// backedUpCount counts the folders the plan backs up.
func backedUpCount(p interact.BackupPlan) int {
	n := 0
	for _, f := range p.Folders {
		if f.Problem == "" && !f.Skipped {
			n++
		}
	}
	return n
}

func folderPhrase(n int) string {
	if n == 1 {
		return folderOne
	}
	return fmt.Sprintf(folderMany, n)
}

// spaceLine compares the space the run needs with the free space; the
// issues of the plan say whether it fits.
func spaceLine(p interact.BackupPlan) PlanLine {
	line := PlanLine{Label: planSpace, Tone: ToneSuccess, Glyph: GlyphCheck}
	needed := fmt.Sprintf(spaceNeeded, Size(p.NeededBytes))
	if p.AllBytes > p.NeededBytes {
		needed = fmt.Sprintf(spaceNeededUpTo, Size(p.NeededBytes), Size(p.AllBytes))
	}
	if p.FreeBytes < 0 {
		line.Text = needed + spaceFreeUnknown
	} else {
		line.Text = needed + fmt.Sprintf(spaceFree, Size(p.FreeBytes))
	}
	for _, issue := range p.Issues {
		switch issue.Code {
		case interact.CodeSpaceInsufficient, interact.CodeFreeSpaceUnknown:
			line.Tone, line.Glyph = ToneError, GlyphError
		case interact.CodeSpaceEstimateOnly:
			if line.Tone != ToneError {
				line.Tone, line.Glyph = ToneWarning, GlyphWarning
			}
		}
	}
	return line
}

// unlockText lists the prompts that follow Start, in their order.
func unlockText(k interact.KeyPlan) string {
	if k.New {
		var steps []string
		if k.Password {
			steps = append(steps, unlockNewPassword)
		}
		switch k.YubiKeys {
		case 1:
			steps = append(steps, unlockRegisterOne)
		case 2:
			steps = append(steps, unlockRegisterTwo)
		}
		if k.RecoveryCode {
			steps = append(steps, unlockRecoveryCode)
		}
		return capitalize(strings.Join(steps, ", "))
	}
	switch {
	case k.Password && k.YubiKeys > 0:
		return unlockTouchAndPassword
	case k.YubiKeys > 0:
		return unlockTouch
	}
	return unlockPassword
}

// afterwards words what follows the backup: verification and retention
// (spec BP-2, 11.3), and lists what retention removes.
func afterwards(p interact.BackupPlan, cfg *config.Config, now time.Time) (PlanLine, []string) {
	line := PlanLine{Label: planAfterwards}
	var parts []string
	if p.VerifyAfter {
		parts = append(parts, afterVerify)
	}
	groups := removalGroups(p.Removes)
	var total int64
	for _, info := range p.Removes {
		total += info.SizeBytes
	}
	switch {
	case len(groups) == 0:
		parts = append(parts, keptText(cfg))
	case p.VerifyAfter:
		parts = append(parts, fmt.Sprintf(afterRemoveIfOK, backupCount(len(p.Removes)), Size(total)))
	default:
		parts = append(parts, fmt.Sprintf(afterRemove, backupCount(len(p.Removes)), Size(total)))
	}
	line.Text = strings.Join(parts, " ")
	var lines []string
	for _, g := range groups {
		lines = append(lines, g.text(now))
	}
	return line, lines
}

func keptText(cfg *config.Config) string {
	if cfg == nil || (cfg.RetentionKeep <= 0 && cfg.Differential.RetentionKeepDifferentials <= 0) {
		return afterKeepAll
	}
	if cfg.RetentionKeep == 1 {
		return afterKeepOne
	}
	if cfg.RetentionKeep > 1 {
		return fmt.Sprintf(afterKeepChains, cfg.RetentionKeep)
	}
	return afterNothing
}

func backupCount(n int) string {
	if n == 1 {
		return oldBackupOne
	}
	return fmt.Sprintf(oldBackupMany, n)
}

// removal is the part of one chain that retention removes.
type removal struct {
	folder string
	full   *catalog.SetInfo
	diffs  int
	bytes  int64
}

// removalGroups groups the sets by folder and chain, in their order.
func removalGroups(infos []catalog.SetInfo) []*removal {
	var groups []*removal
	index := map[string]*removal{}
	for i := range infos {
		info := &infos[i]
		key := info.Entry.ChainKey()
		g := index[key]
		if g == nil {
			g = &removal{folder: info.Entry.DirectoryName}
			index[key] = g
			groups = append(groups, g)
		}
		if info.Entry.IsDiff() {
			g.diffs++
		} else {
			g.full = info
		}
		g.bytes += info.SizeBytes
	}
	return groups
}

func (g *removal) text(now time.Time) string {
	diffs := fmt.Sprintf(removeDiffs, g.diffs)
	if g.diffs == 1 {
		diffs = removeDiffOne
	}
	switch {
	case g.full != nil && g.diffs > 0:
		return fmt.Sprintf(removeFullAndDiffs, g.folder, ShortDay(g.full.Created(), now), diffs, Size(g.bytes))
	case g.full != nil:
		return fmt.Sprintf(removeFull, g.folder, ShortDay(g.full.Created(), now), Size(g.bytes))
	}
	return fmt.Sprintf(removeOnlyDiffs, g.folder, diffs, Size(g.bytes))
}

// issueText shows a workflow message with its remedy as plain sentences.
func issueText(s string) string {
	return strings.Replace(s, " Remedy: ", " ", 1)
}

// lowerFirst makes the first letter lower case, for a reason inside a
// sentence.
func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

// Confirm is a confirmation task dialog.
type Confirm struct {
	Instruction, Content string
	// Yes confirms; No is the default and keeps things as they are.
	Yes, No string
}

// NewKeysConfirm is the confirmation of "New keys + full backup…" (figure
// 6.2), for the configured unlock methods.
func NewKeysConfirm(cfg *config.Config) Confirm {
	var locks []string
	if !cfg.IsYubiKeyOnly() {
		locks = append(locks, lockPassword)
	}
	if cfg.UseYubiKey() {
		locks = append(locks, lockYubiKey)
	}
	if cfg.RecoveryCode {
		locks = append(locks, lockRecoveryCode)
	}
	return Confirm{
		Instruction: newKeysInstruction,
		Content:     fmt.Sprintf(newKeysContent, joinAnd(locks)),
		Yes:         newKeysYes,
		No:          newKeysNo,
	}
}

// joinAnd joins "a", "b" and "c" as "a, b and c".
func joinAnd(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}
