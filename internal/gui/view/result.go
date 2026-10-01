package view

import (
	"RestoreSafe/internal/gui/flow"
	"RestoreSafe/internal/logging"
	"RestoreSafe/internal/workflow/interact"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// ResultCard is the end of an operation (spec 6.3, BR-7).
type ResultCard struct {
	Tone  Tone
	Glyph Glyph
	Title string
	// Lines say what happened per folder and whether retention ran.
	Lines []string
	// Detail is the workflow's message with its remedy, shown by Details;
	// Details is nil without one.
	Detail  string
	Details *Button
	// Log is nil when the run has no log file.
	Log  *Button
	Done Button
}

// ResultCardOf maps the end of the finished run r to its result card. It
// returns nil when the operation did not start (the user cancelled a
// question): the hero returns without a card (spec BP-6).
func ResultCardOf(r *flow.Run) *ResultCard {
	if r == nil || r.Stage != flow.StageFinished {
		return nil
	}
	name := opNoun(r.Op)
	err := r.Err
	c := &ResultCard{Done: Button{Text: buttonDone, Action: ActionDismiss, Enabled: true}}
	if r.LogPath != "" || (r.Result != nil && r.Result.LogPath != "") {
		c.Log = &Button{Text: linkShowLog, Action: ActionShowLog, Enabled: true}
	}
	switch {
	case err == nil && r.Result == nil, errors.Is(err, interact.ErrCancelled):
		// Cancelled in a question, or nothing to do.
		return nil
	case err == nil:
		finished(c, r, name)
	case errors.Is(err, context.Canceled) && !r.Started.IsZero():
		c.Tone, c.Glyph, c.Title = ToneNeutral, GlyphNone, fmt.Sprintf(resultCancelled, name)
		c.Lines = keptLines(r, true)
	case errors.Is(err, context.Canceled):
		return nil
	case r.Started.IsZero():
		// The plan blocked the start.
		c.Tone, c.Glyph, c.Title = ToneError, GlyphError, fmt.Sprintf(resultNotStarted, name)
		c.Lines = blockingIssues(r)
		if len(c.Lines) == 0 {
			c.Lines = []string{firstSentences(err.Error())}
		}
		c.Detail = err.Error()
		c.Details = &Button{Text: linkShowDetails, Action: ActionShowDetails, Enabled: true}
	default:
		c.Tone, c.Glyph, c.Title = ToneError, GlyphError, fmt.Sprintf(resultFailed, name)
		c.Lines = append([]string{firstSentences(err.Error())}, keptLines(r, false)...)
		c.Detail = err.Error()
		c.Details = &Button{Text: linkShowDetails, Action: ActionShowDetails, Enabled: true}
	}
	return c
}

func opNoun(op flow.Op) string {
	switch op {
	case flow.OpRestore:
		return nounRestore
	case flow.OpVerify:
		return nounVerification
	}
	return nounBackup
}

// finished words a run that ended without error.
func finished(c *ResultCard, r *flow.Run, name string) {
	warnings := r.Result.Warnings
	c.Tone, c.Glyph, c.Title = ToneSuccess, GlyphCheck, fmt.Sprintf(resultFinished, name)
	if warnings == 1 {
		c.Tone, c.Glyph, c.Title = ToneWarning, GlyphWarning, fmt.Sprintf(resultWarningOne, name)
	} else if warnings > 1 {
		c.Tone, c.Glyph, c.Title = ToneWarning, GlyphWarning, fmt.Sprintf(resultWarnings, name, warnings)
	}
	if r.Op != flow.OpBackup {
		c.Lines = []string{verifiedLine(r)}
		if warnings == 0 && r.Op == flow.OpVerify {
			c.Lines[0] += " " + resultEveryFile
		}
		if warnings > 0 {
			c.Lines = append(c.Lines, otherWarnings(warnings))
		}
		return
	}

	facts := r.Facts
	sets := orderedSets(r)
	summary := backupSummary(r, sets)
	explained := 0
	var warningLines []string
	for _, set := range sets {
		f := facts.Sets[set]
		if f.Skipped > 0 {
			folder := setFolder(set)
			text := fmt.Sprintf(resultSkipped, Count(f.Skipped), folder, folder)
			if f.Skipped == 1 {
				text = fmt.Sprintf(resultSkippedOne, folder, folder)
			}
			warningLines = append(warningLines, text)
			explained++
		}
		if v, ok := facts.Verify[set]; ok && v.Result == logging.ResultFailed {
			warningLines = append(warningLines, fmt.Sprintf(resultVerifyFailed, setFolder(set)))
			explained++
		}
	}
	if r.Plan != nil && r.Plan.VerifyAfter && len(sets) > 0 && allVerified(facts, sets) {
		summary += " " + resultVerified
	}
	if line := cleanupLine(facts.Cleanup); line != "" {
		summary += " " + line
	}
	c.Lines = append([]string{summary}, warningLines...)
	if rest := warnings - explained; rest > 0 {
		c.Lines = append(c.Lines, otherWarnings(rest))
	}
}

// orderedSets returns the sets the run wrote, in the order of the plan.
func orderedSets(r *flow.Run) []string {
	var sets []string
	for set := range r.Facts.Sets {
		sets = append(sets, set)
	}
	order := map[string]int{}
	if r.Plan != nil {
		for i, f := range r.Plan.Folders {
			order[f.Name] = i
		}
	}
	slices.SortFunc(sets, func(a, b string) int {
		if d := order[setFolder(a)] - order[setFolder(b)]; d != 0 {
			return d
		}
		return strings.Compare(a, b)
	})
	return sets
}

// backupSummary is "3 folders, 3.4 GB in 4 min."
func backupSummary(r *flow.Run, sets []string) string {
	n := len(sets)
	if n == 0 {
		n = len(r.Finished)
	}
	var bytes int64
	for _, set := range sets {
		bytes += r.Facts.Sets[set].Bytes
	}
	d := r.Ended.Sub(r.Started)
	if b := r.Facts.Backup; b != nil && b.Seconds > 0 {
		d = time.Duration(b.Seconds) * time.Second
	}
	if bytes > 0 {
		return fmt.Sprintf(resultSummarySize, folderPhrase(n), Size(bytes), Duration(d))
	}
	return fmt.Sprintf(resultSummary, folderPhrase(n), Duration(d))
}

func allVerified(facts logging.RunFacts, sets []string) bool {
	for _, set := range sets {
		if v, ok := facts.Verify[set]; !ok || v.Result != logging.ResultOK {
			return false
		}
	}
	return true
}

// cleanupLine says what retention removed, "" when it removed nothing or
// did not run.
func cleanupLine(f *logging.Fact) string {
	switch {
	case f == nil:
		return ""
	case f.Result == logging.ResultFailed:
		return resultCleanupFailed
	case f.Result == logging.ResultWarnings:
		return resultCleanupHeld
	case f.Removed == 1:
		return fmt.Sprintf(resultRemovedOne, Size(f.Bytes))
	case f.Removed > 1:
		return fmt.Sprintf(resultRemoved, f.Removed, Size(f.Bytes))
	}
	return ""
}

func verifiedLine(r *flow.Run) string {
	n := len(r.Facts.Verify)
	d := Duration(r.Ended.Sub(r.Started))
	if n == 1 {
		return fmt.Sprintf(resultVerifiedOne, d)
	}
	return fmt.Sprintf(resultVerifiedMany, n, d)
}

func otherWarnings(n int) string {
	if n == 1 {
		return resultOtherWarningOne
	}
	return fmt.Sprintf(resultOtherWarnings, n)
}

// keptLines says what a cancelled or failed backup kept: the folders backed
// up, and the unfinished one, which the workflow removed.
func keptLines(r *flow.Run, cancelled bool) []string {
	if r.Op != flow.OpBackup {
		if cancelled {
			return []string{resultNothingChanged}
		}
		return nil
	}
	var names []string
	for _, f := range r.Finished {
		names = append(names, f.Name)
	}
	var lines []string
	switch len(names) {
	case 0:
		lines = append(lines, resultNothingBackedUp)
	case 1:
		lines = append(lines, fmt.Sprintf(resultBackedUpOne, names[0]))
	default:
		lines = append(lines, fmt.Sprintf(resultBackedUpMany, joinAnd(names)))
	}
	p := r.Progress
	if p.Phase == interact.PhaseBackingUp && p.Item != "" && !slices.Contains(names, p.Item) {
		lines = append(lines, fmt.Sprintf(resultUnfinished, p.Item))
	}
	if cancelled || len(names) > 0 {
		lines = append(lines, resultNoneRemoved)
	}
	return lines
}

// blockingIssues are the plan's errors that kept the backup from starting.
func blockingIssues(r *flow.Run) []string {
	if r.Plan == nil {
		return nil
	}
	var lines []string
	for _, issue := range r.Plan.Issues {
		if issue.Status == interact.StatusError {
			lines = append(lines, firstSentences(issue.Text))
		}
	}
	return lines
}

// firstSentences is a workflow message without its remedy, which "Show
// details" shows.
func firstSentences(s string) string {
	if i := strings.Index(s, " Remedy: "); i >= 0 {
		s = s[:i]
	}
	return s
}

// setFolder returns the folder of a backup set name
// "<folder>_<chain>_<date>_<type>"; the folder may contain underscores.
func setFolder(set string) string {
	s := set
	for range 3 {
		i := strings.LastIndex(s, "_")
		if i < 0 {
			return set
		}
		s = s[:i]
	}
	return s
}
