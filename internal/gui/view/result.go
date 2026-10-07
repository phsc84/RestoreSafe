package view

import (
	"RestoreSafe/internal/format/naming"
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

// ResultCard is the end of an operation (GUI spec 6.3, BR-7).
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
	Log *Button
	// Open opens the restored folders; nil for other operations.
	Open *Button
	Done Button
}

// ResultCardOf maps the end of the finished run r to its result card. It
// returns nil when the operation did not start (the user cancelled a
// question): the hero returns without a card (GUI spec BP-6).
func ResultCardOf(r *flow.Run) *ResultCard {
	if r == nil || r.Stage != flow.StageFinished {
		return nil
	}
	name := opNoun(r.Op)
	err := r.Err
	c := &ResultCard{Done: Button{Text: buttonDone, Action: ActionDismiss, Enabled: true}}
	if r.LogPath != "" || (r.Result != nil && r.Result.LogPath != "") {
		c.Log = &Button{Text: buttonShowRunLog, Action: ActionShowLog, Enabled: true}
	}
	if r.Op == flow.OpRestore && r.Restore != nil && !r.Started.IsZero() {
		c.Open = &Button{Text: buttonOpenFolder2, Action: ActionOpenRestored, Enabled: true}
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
		if r.Op == flow.OpRestore {
			c.Lines = restoreKeptLines(r)
		}
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
		if r.Op == flow.OpRestore {
			// A failed restore leaves a folder incomplete: red, never amber
			// (RW-8).
			c.Title = resultIncomplete
			c.Lines = append([]string{firstSentences(err.Error())}, restoreKeptLines(r)...)
			c.Lines = append(c.Lines, restoreVerifyHint)
		}
		if r.Op == flow.OpVerify && r.What != "" && verifyFoundDamage(r) {
			c.Title = fmt.Sprintf(resultDamaged, r.What)
		}
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

// finished words a run that ended without error. The title says what the
// user has now: "4 folders backed up", "2 folders restored", "The backup of
// today, 09:12 can be restored" (BR-7).
func finished(c *ResultCard, r *flow.Run, name string) {
	warnings := r.Result.Warnings
	c.Tone, c.Glyph = ToneSuccess, GlyphCheck
	if warnings > 0 {
		c.Tone, c.Glyph = ToneWarning, GlyphWarning
	}
	sets := orderedSets(r)
	c.Title = fmt.Sprintf(resultFinished, name)
	switch n := doneCount(r, sets); {
	case r.Op == flow.OpVerify && r.What != "" && r.Whole:
		c.Title = fmt.Sprintf(resultCanRestore, r.What)
	case r.Op == flow.OpVerify && r.What != "" && n > 0:
		// Only some folders of the backup were verifiable: the title names
		// them, not the whole backup.
		c.Title = fmt.Sprintf(resultCanRestoreSome, verifiedFolders(r, n), r.What)
	case r.Op == flow.OpVerify && n > 0:
		c.Title = fmt.Sprintf(resultCanRestoreFolders, folderPhrase(n))
	case r.Op == flow.OpRestore && n > 0:
		c.Title = fmt.Sprintf(resultRestored, folderPhrase(n))
	case r.Op == flow.OpBackup && n > 0:
		c.Title = fmt.Sprintf(resultBackedUp, folderPhrase(n))
	}
	switch {
	case warnings == 1:
		c.Title += resultWithWarningOne
	case warnings > 1:
		c.Title += fmt.Sprintf(resultWithWarnings, warnings)
	}
	if r.Op == flow.OpRestore {
		restoreFinished(c, r)
		return
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
	summary := backupSummary(r, sets)
	explained := 0
	var warningLines []string
	for _, set := range sets {
		if f := facts.Sets[set]; f.Unread() > 0 {
			folder := setFolder(set)
			var parts []string
			if f.Skipped > 0 {
				parts = append(parts, countPhrase(f.Skipped, resultSkippedOne, resultSkipped, folder))
			}
			if f.Stale > 0 {
				parts = append(parts, countPhrase(f.Stale, resultStaleOne, resultStale, folder))
			}
			parts = append(parts, fmt.Sprintf(resultOlderKept, folder))
			warningLines = append(warningLines, strings.Join(parts, " "))
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

// verifiedFolders names the n folders a verification read: "Pictures",
// "Documents and Pictures", or "4 folders".
func verifiedFolders(r *flow.Run, n int) string {
	if r.Verify == nil || n > 3 {
		return folderPhrase(n)
	}
	var names []string
	for _, s := range r.Verify.Sets {
		names = append(names, s.Set.DirectoryName)
	}
	return joinAnd(names)
}

// verifyFoundDamage reports whether the verification found a set damaged,
// rather than failing for another reason (a password, a drive).
func verifyFoundDamage(r *flow.Run) bool {
	if r.Verify == nil {
		return false
	}
	for _, s := range r.Verify.Sets {
		if v, ok := r.Facts.Verify[s.Set.String()]; ok && v.Result == logging.ResultFailed {
			return true
		}
	}
	return false
}

// doneCount is how many folders the run backed up, restored or verified.
func doneCount(r *flow.Run, sets []string) int {
	switch {
	case r.Op == flow.OpVerify && r.Verify != nil:
		return len(r.Verify.Sets)
	case r.Op == flow.OpVerify:
		return len(r.Facts.Verify)
	case r.Op == flow.OpRestore && r.Restore != nil:
		return len(r.Restore.Sets)
	case r.Op == flow.OpBackup && len(sets) > 0:
		return len(sets)
	}
	return len(r.Finished)
}

// backupSummary is "3.4 GB in 4 min."; the title has the folders.
func backupSummary(r *flow.Run, sets []string) string {
	var bytes int64
	for _, set := range sets {
		bytes += r.Facts.Sets[set].Bytes
	}
	d := r.Ended.Sub(r.Started)
	if b := r.Facts.Backup; b != nil && b.Seconds > 0 {
		d = time.Duration(b.Seconds) * time.Second
	}
	if bytes > 0 {
		return fmt.Sprintf(resultSummarySize, Size(bytes), Duration(d))
	}
	return fmt.Sprintf(resultSummary, Duration(d))
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
	// The run's log also holds earlier verifications: count this one's sets.
	n := len(r.Facts.Verify)
	if r.Verify != nil {
		n = len(r.Verify.Sets)
	}
	bases := false
	if r.Verify != nil {
		for _, s := range r.Verify.Sets {
			bases = bases || s.Base.DirectoryName != ""
		}
	}
	d := Duration(r.Ended.Sub(r.Started))
	switch {
	case n == 1 && bases:
		return fmt.Sprintf(resultCheckedBaseOne, d)
	case n == 1:
		return fmt.Sprintf(resultCheckedOne, d)
	case bases:
		return fmt.Sprintf(resultCheckedBases, folderPhrase(n), d)
	}
	return fmt.Sprintf(resultChecked, folderPhrase(n), d)
}

func otherWarnings(n int) string {
	if n == 1 {
		return resultOtherWarningOne
	}
	return fmt.Sprintf(resultOtherWarnings, n)
}

// countPhrase words n files of folder: one takes the folder, many the count
// and the folder.
func countPhrase(n int, one, many, folder string) string {
	if n == 1 {
		return fmt.Sprintf(one, folder)
	}
	return fmt.Sprintf(many, Count(n), folder)
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
		s = endSentence(s[:i])
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

// restoreFinished words a restore that ended without error (RW-8).
func restoreFinished(c *ResultCard, r *flow.Run) {
	bytes, dest := int64(0), ""
	explained := 0
	var unread []string
	if p := r.Restore; p != nil {
		bytes, dest = p.NeededBytes, Path(p.Destination)
		for _, s := range p.Sets {
			f, folder := r.Facts.Restored[s.Set.String()], s.Set.DirectoryName
			switch {
			case f.Skipped == 1:
				unread = append(unread, fmt.Sprintf(restoreSkippedOne, folder))
			case f.Skipped > 1:
				unread = append(unread, fmt.Sprintf(restoreSkipped, folder, Count(f.Skipped)))
			}
			full := baseDay(s.Base, r.Ended)
			switch {
			case f.Stale == 1:
				unread = append(unread, fmt.Sprintf(restoreStaleOne, folder, full))
			case f.Stale > 1:
				unread = append(unread, fmt.Sprintf(restoreStale, folder, Count(f.Stale), full))
			}
		}
		if len(unread) > 0 {
			explained = 1 // the workflow counts unread files as one warning
		}
	}
	line := fmt.Sprintf(restoredLine, Size(bytes), dest, Duration(r.Ended.Sub(r.Started))) + " " + restoreEveryFile
	c.Lines = append([]string{line}, unread...)
	if rest := r.Result.Warnings - explained; rest > 0 {
		c.Lines = append(c.Lines, otherWarnings(rest))
	}
}

// baseDay is the day of the full backup base, from its name ("1 Sep"); the
// name's date as is when it does not parse.
func baseDay(base naming.BackupEntry, now time.Time) string {
	t, err := time.ParseInLocation("2006-01-02", base.Date, time.Local)
	if err != nil {
		return base.Date
	}
	return ShortDay(t, now)
}

// restoreKeptLines says what a cancelled or failed restore left: the
// folders restored, the one that is incomplete, the ones not restored.
func restoreKeptLines(r *flow.Run) []string {
	done := map[string]bool{}
	var kept []string
	for _, f := range r.Finished {
		done[f.Name] = true
		kept = append(kept, f.Name)
	}
	current := ""
	if p := r.Progress; p.Phase == interact.PhaseRestoring && !done[p.Item] {
		current = p.Item
	}
	var lines, missing []string
	if len(kept) == 1 {
		lines = append(lines, fmt.Sprintf(restoreKept, kept[0]))
	} else if len(kept) > 1 {
		lines = append(lines, fmt.Sprintf(restoreKeptN, joinAnd(kept)))
	}
	if r.Restore != nil {
		for _, s := range r.Restore.Sets {
			name := s.Set.DirectoryName
			switch {
			case name == current:
				lines = append(lines, fmt.Sprintf(restoreIncompleteDir, Path(s.OutputDir)))
			case !done[name]:
				missing = append(missing, name)
			}
		}
	}
	if len(missing) == 1 {
		lines = append(lines, fmt.Sprintf(restoreNotRestored, missing[0]))
	} else if len(missing) > 1 {
		lines = append(lines, fmt.Sprintf(restoreNotRestoredN, joinAnd(missing)))
	}
	return lines
}
