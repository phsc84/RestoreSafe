package view

import (
	"RestoreSafe/internal/gui/flow"
	"RestoreSafe/internal/workflow/interact"
	"fmt"
	"time"
)

// StepState is where a step of the trail is.
type StepState int

const (
	StepWaiting StepState = iota
	StepCurrent
	StepDone
)

// Step is one step of the trail (spec BR-2).
type Step struct {
	Text  string
	State StepState
}

// ProgressCard is the running operation (spec 6.2, figure 5.2).
type ProgressCard struct {
	Title string
	Steps []Step
	// Line is the current folder, or what happens while no folder is
	// worked on.
	Line string
	// Fraction is the bar, -1 for a marquee.
	Fraction float64
	// Bytes, Speed and Left are "" while unknown.
	Bytes, Speed, Left string
	Cancel             Button
	Log                Button
}

// ProgressCardOf words the running operation r.
func ProgressCardOf(r *flow.Run, now time.Time) ProgressCard {
	p := r.Progress
	c := ProgressCard{
		Title:    runTitle(r.Op),
		Steps:    stepsOf(r),
		Fraction: p.Fraction(),
		Cancel:   Button{Text: buttonCancel, Action: ActionCancel, Enabled: true},
		Log:      Button{Text: linkShowLog, Action: ActionShowLog, Enabled: true},
	}
	if r.Stage == flow.StagePlanning {
		c.Line, c.Fraction = progressPreparing, -1
		if r.Plan != nil {
			c.Line = progressCheckPlan
		}
		for i := range c.Steps {
			c.Steps[i].State = StepWaiting
		}
		return c
	}
	if r.Stage == flow.StageCancelling {
		c.Line, c.Fraction = progressCancelling, -1
		c.Cancel = Button{Text: buttonCancelling, Action: ActionCancel}
		return c
	}
	switch p.Phase {
	case interact.PhaseNone, interact.PhaseUnlocking:
		c.Line, c.Fraction = unlockingLine(r), -1
		return c
	case interact.PhaseBackingUp:
		c.Line = p.Item
		if f := planFolder(r, p.Item); f != nil {
			c.Line = fmt.Sprintf(progressFolder, p.Item, folderType(*f))
		}
	case interact.PhaseVerifying:
		c.Line = fmt.Sprintf(progressVerifying, p.Item)
	case interact.PhaseCleaningUp:
		c.Line, c.Fraction = progressCleaningUp, -1
		return c
	case interact.PhaseRestoring:
		c.Line = restoringLine(r, p.Item, now)
	default:
		c.Line = p.Item
	}
	if p.Total > 0 {
		c.Bytes = fmt.Sprintf(progressBytes, Size(p.Done), Size(p.Total))
	} else if p.Done > 0 {
		c.Bytes = Size(p.Done)
	}
	if rate := r.Speed.Rate(); rate > 0 {
		c.Speed = fmt.Sprintf(progressSpeed, Size(int64(rate)))
	}
	if left, ok := r.Speed.Left(p.Total, now); ok {
		c.Left = leftText(left)
	}
	return c
}

func runTitle(op flow.Op) string {
	switch op {
	case flow.OpRestore:
		return titleRestoring
	case flow.OpVerify:
		return titleVerifying
	}
	return titleBackingUp
}

// unlockingLine is the line while the keys are unlocked or created: the
// Windows Security prompt comes first in the YubiKey modes (spec CR-4).
func unlockingLine(r *flow.Run) string {
	if r.Plan == nil {
		return progressUnlocking
	}
	k := r.Plan.Keys
	switch {
	case k.New:
		return progressCreatingKeys
	case k.YubiKeys > 0:
		return progressSecurityPrompt
	}
	return progressUnlocking
}

// stepsOf is the trail of r: the steps before the current one are done.
func stepsOf(r *flow.Run) []Step {
	type step struct {
		phase interact.Phase
		text  string
	}
	unlock := stepUnlock
	if r.Plan != nil && r.Plan.Keys.New {
		unlock = stepCreateKeys
	}
	steps := []step{{interact.PhaseUnlocking, unlock}}
	switch r.Op {
	case flow.OpBackup:
		steps = append(steps, step{interact.PhaseBackingUp, stepBackUp})
		if r.Plan != nil && r.Plan.VerifyAfter {
			steps = append(steps, step{interact.PhaseVerifying, stepVerify})
		}
		steps = append(steps, step{interact.PhaseCleaningUp, stepCleanUp})
	case flow.OpVerify:
		steps = append(steps, step{interact.PhaseVerifying, stepVerify})
	case flow.OpRestore:
		steps = append(steps, step{interact.PhaseRestoring, stepRestore})
	}

	p := r.Progress
	phase := p.Phase
	if phase == interact.PhaseNone {
		phase = interact.PhaseUnlocking
	}
	current := 0
	for i, s := range steps {
		if s.phase == phase {
			current = i
		}
	}
	out := make([]Step, len(steps))
	for i, s := range steps {
		out[i] = Step{Text: s.text}
		switch {
		case i < current:
			out[i].State = StepDone
		case i == current:
			out[i].State = StepCurrent
			if p.Count > 0 && s.phase == phase {
				out[i].Text = fmt.Sprintf(stepCounted, s.text, p.Index, p.Count)
			}
		}
	}
	return out
}

func planFolder(r *flow.Run, name string) *interact.FolderPlan {
	if r.Plan == nil {
		return nil
	}
	for i := range r.Plan.Folders {
		if r.Plan.Folders[i].Name == name {
			return &r.Plan.Folders[i]
		}
	}
	return nil
}

func folderType(f interact.FolderPlan) string {
	if f.Differential {
		return fmt.Sprintf(typeDifferentialN, f.DiffNumber)
	}
	return typeFullBackup
}

// leftText rounds the time left: "About 3 min left", "Less than a minute
// left" (spec BR-3).
func leftText(d time.Duration) string {
	if d < time.Minute {
		return leftUnderMinute
	}
	return fmt.Sprintf(leftAbout, Duration(d))
}

// FolderProgress is a folder's state in the running backup (spec BR-4).
type FolderProgress struct {
	Text  string
	Tone  Tone
	Glyph Glyph
	// Badge is the type the run gives the folder.
	Badge Badge
}

// RunFolders returns the state of each folder the backup r backs up, by
// backup name; nil for another operation.
func RunFolders(r *flow.Run) map[string]FolderProgress {
	if r == nil || r.Op != flow.OpBackup || r.Plan == nil {
		return nil
	}
	states := map[string]FolderProgress{}
	set := func(name, text string, tone Tone, glyph Glyph) {
		s, ok := states[name]
		if ok {
			s.Text, s.Tone, s.Glyph = text, tone, glyph
			states[name] = s
		}
	}
	for _, f := range r.Plan.Folders {
		if f.Problem == "" && !f.Skipped {
			states[f.Name] = FolderProgress{Text: folderWaiting, Tone: ToneSecondary, Badge: planBadge(f)}
		}
	}
	for _, f := range r.Finished {
		set(f.Name, fmt.Sprintf(folderDone, Size(f.Bytes)), ToneSuccess, GlyphCheck)
	}
	p := r.Progress
	if p.Phase != interact.PhaseBackingUp || p.Item == "" {
		return states
	}
	if s, ok := states[p.Item]; ok && s.Glyph == GlyphCheck {
		return states
	}
	switch {
	case r.Stage == flow.StageRunning:
		text := folderBackingUp
		if f := p.Fraction(); f >= 0 {
			text = fmt.Sprintf(folderBackingUpPct, int(f*100))
		}
		set(p.Item, text, ToneNeutral, GlyphNone)
	case r.Stage == flow.StageCancelling || r.Cancelled:
		set(p.Item, folderCancelled, ToneSecondary, GlyphNone)
	case r.Stage == flow.StageFinished && r.Err != nil:
		set(p.Item, folderFailed, ToneError, GlyphError)
	}
	return states
}

// planBadge is the badge of the type the plan gives f.
func planBadge(f interact.FolderPlan) Badge {
	if f.Differential {
		return Badge{Kind: BadgeDiff, Text: fmt.Sprintf(badgeDiff, f.DiffNumber), Name: fmt.Sprintf(badgeDiffName, f.DiffNumber)}
	}
	return Badge{Kind: BadgeFull, Text: badgeFull, Name: badgeFullName}
}

// RunActivity is the left part of the status bar while r runs, e.g.
// "Backing up Projects · 62%"; "" when r is not running.
func RunActivity(r *flow.Run) string {
	if r == nil {
		return ""
	}
	switch r.Stage {
	case flow.StagePlanning:
		return activityPlanning
	case flow.StageCancelling:
		return activityCancelling
	case flow.StageRunning:
	default:
		return ""
	}
	p := r.Progress
	switch p.Phase {
	case interact.PhaseBackingUp, interact.PhaseVerifying, interact.PhaseRestoring:
		text := fmt.Sprintf(activityItem, runTitle(r.Op), p.Item)
		if p.Phase == interact.PhaseVerifying && r.Op == flow.OpBackup {
			text = fmt.Sprintf(activityItem, titleVerifying, p.Item)
		}
		if f := p.Fraction(); f >= 0 {
			text += fmt.Sprintf(activityPercent, int(f*100))
		}
		return text
	case interact.PhaseCleaningUp:
		return progressCleaningUp
	}
	return progressUnlocking
}

// CancelConfirm asks before cancelling the running op (figure 6.3); when
// closing, before closing the window (spec 6.4).
func CancelConfirm(op flow.Op, closing bool) Confirm {
	c := Confirm{Instruction: cancelBackup, Content: cancelBackupContent, Yes: buttonCancelBackup, No: buttonKeepRunning}
	switch op {
	case flow.OpRestore:
		c = Confirm{Instruction: cancelRestore, Content: cancelRestoreContent, Yes: buttonCancelRestore, No: buttonKeepRestoring}
	case flow.OpVerify:
		c = Confirm{Instruction: cancelVerify, Content: cancelVerifyContent, Yes: buttonCancelVerify, No: buttonKeepRunning}
	}
	if closing {
		c.Instruction, c.Yes, c.No = closeInstruction, buttonCancelAndClose, buttonKeepRunning
	}
	return c
}

// restoringLine names the folder being restored and what is read for it:
// "Documents · differential 3, with its full backup of 1 Sep".
func restoringLine(r *flow.Run, folder string, now time.Time) string {
	if r.Restore == nil {
		return folder
	}
	for _, s := range r.Restore.Sets {
		if s.Set.DirectoryName != folder {
			continue
		}
		if !s.Set.IsDiff() {
			return fmt.Sprintf(progressRestoring, folder, restoringFull)
		}
		day := s.Base.Date
		if d, err := time.ParseInLocation("2006-01-02", s.Base.Date, time.Local); err == nil {
			day = ShortDay(d, now)
		}
		return fmt.Sprintf(progressRestoring, folder, fmt.Sprintf(restoringDiff, s.Set.DiffNumber, day))
	}
	return folder
}
