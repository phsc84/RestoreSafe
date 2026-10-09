package view

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/phsc84/restoresafe/internal/config"
	"github.com/phsc84/restoresafe/internal/format/catalog"
	"github.com/phsc84/restoresafe/internal/format/naming"
	"github.com/phsc84/restoresafe/internal/gui/flow"
	"github.com/phsc84/restoresafe/internal/logging"
	"github.com/phsc84/restoresafe/internal/workflow/health"
	"github.com/phsc84/restoresafe/internal/workflow/interact"
)

// AllFolders is the filter value that shows every folder.
const AllFolders = ""

// FilterOption is an entry of the folder filter (GUI spec BK-3).
type FilterOption struct {
	Text string
	// Folder is the backup name it shows, AllFolders for all.
	Folder string
}

// Status is a status cell: text with a meaning and an icon.
type Status struct {
	Text  string
	Tone  Tone
	Glyph Glyph
}

// InfoLine is a line above the list: a problem, an information or the
// retention rule, with an optional action.
type InfoLine struct {
	Text   string
	Tone   Tone
	Glyph  Glyph
	Button *Button
	// Path is set when Text is one line that starts with a path: it is
	// shortened in the middle rather than wrapped.
	Path bool
}

// BackupRow is one backup set in the list (GUI spec BK-2).
type BackupRow struct {
	// Set is the set's name, which identifies the row.
	Set string
	// Tip describes the row in full: set name, path, created, full backup.
	Tip        string
	Folder     string
	Path       string
	Badge      Badge
	BasedOn    string
	BasedOnTip string
	Size       string
	Chain      string
	Status     Status
	// Usable is false for a set that can be neither restored nor verified;
	// Reason says why.
	Usable bool
	Reason string
}

// RunGroup is one backup run in the list (GUI spec BK-1).
type RunGroup struct {
	RunID  naming.BackupID
	Header string
	Tone   Tone
	Glyph  Glyph
	// Expanded is set for the newest run.
	Expanded bool
	Rows     []BackupRow
	// Hidden counts the sets of the run the folder filter hides.
	Hidden int
	// LogPath is the run's log, "" when there is none.
	LogPath string
	// When is the run's date in words, for the selection and the log title.
	When string
	// Placeholder is the text of the only row of a run without sets: a list
	// does not show an empty group.
	Placeholder string
}

// RestorePage is everything the Restore backup page shows (GUI spec 7).
type RestorePage struct {
	Title     string
	Filters   []FilterOption
	Filter    int // index of the selected filter
	Retention *InfoLine
	Lines     []InfoLine
	Columns   []string
	Groups    []RunGroup
	// Refresh takes a new snapshot of the machine (F5), as on Create backup.
	Refresh Button
}

// RestorePageOf computes the Restore backup page from the snapshot s, the
// configuration, the operation r (nil when none) and the folder filter.
func RestorePageOf(s *health.Snapshot, cfg *config.Config, r *flow.Run, folder string, now time.Time) RestorePage {
	p := RestorePage{Title: navRestore, Columns: backupColumns(), Refresh: Button{Text: buttonRefresh, Action: ActionCheckAgain}}
	if s == nil {
		return p
	}
	p.Refresh.Enabled = true
	p.Filters, p.Filter = filtersOf(s, folder)
	p.Retention = retentionLine(s, now)
	p.Lines = problemLines(s, cfg, now)
	p.Groups = groupsOf(s, r, folder, now)
	return p
}

func backupColumns() []string {
	return []string{columnFolder, columnType, columnBasedOn, columnSize, columnChain, columnStatus}
}

// filtersOf lists "All folders", the configured folders and the folders
// that only exist in the backup directory.
func filtersOf(s *health.Snapshot, selected string) ([]FilterOption, int) {
	opts := []FilterOption{{Text: filterAll, Folder: AllFolders}}
	known := map[string]bool{}
	for _, f := range s.Folders {
		if f.BackupName != "" && !known[f.BackupName] {
			known[f.BackupName] = true
			opts = append(opts, FilterOption{Text: f.BackupName, Folder: f.BackupName})
		}
	}
	var old []string
	for _, info := range s.Sets {
		name := info.Entry.DirectoryName
		if !known[name] {
			known[name] = true
			old = append(old, name)
		}
	}
	slices.Sort(old)
	for _, name := range old {
		opts = append(opts, FilterOption{Text: fmt.Sprintf(filterOld, name), Folder: name})
	}
	index := 0
	for i, o := range opts {
		if o.Folder == selected {
			index = i
		}
	}
	return opts, index
}

// retentionLine names what the next backup removes (GUI spec BK-7); nil
// when it removes nothing. The rule itself is on Settings.
func retentionLine(s *health.Snapshot, now time.Time) *InfoLine {
	if len(s.Retention) == 0 {
		return nil
	}
	var bytes int64
	for _, info := range s.Retention {
		bytes += info.SizeBytes
	}
	var groups []string
	for _, g := range removalGroups(s.Retention) {
		groups = append(groups, g.text(now))
	}
	text := fmt.Sprintf(nextRemoves, backupCount(len(s.Retention)), Size(bytes), strings.Join(groups, "; "))
	return &InfoLine{Text: text, Tone: ToneInfo, Glyph: GlyphInfo}
}

func chainCount(n int) string {
	if n == 1 {
		return chainOne
	}
	return fmt.Sprintf(chainMany, n)
}

// AddProblemHint adds a line to a successful result card when other backups
// have a problem (BR-7): the green check vouches only for what the run
// did. below is set on the page that lists the problems under the card.
func AddProblemHint(c *ResultCard, s *health.Snapshot, cfg *config.Config, now time.Time, below bool) {
	if c == nil || c.Tone != ToneSuccess || s == nil {
		return
	}
	for _, l := range problemLines(s, cfg, now) {
		if l.Tone == ToneError {
			hint := resultOtherProblem
			if below {
				hint = resultOtherProblemBelow
			}
			c.Lines = append(c.Lines, hint)
			return
		}
	}
}

// problemLines are the findings about the backups (GUI spec BK-6): errors and
// warnings with their remedy, and information.
func problemLines(s *health.Snapshot, cfg *config.Config, now time.Time) []InfoLine {
	var lines []InfoLine
	openDir := &Button{Text: buttonOpenBackupDir, Action: ActionOpenBackupDir, Enabled: true}
	for _, p := range s.Problems {
		switch p.Code {
		case interact.CodeBaseMissing, interact.CodeSetIncomplete, interact.CodeIncompleteNewest, interact.CodeVerifyFailed:
			h := problemHero(p, s, cfg, now, Button{})
			line := InfoLine{Text: h.Title + ". " + h.Line, Tone: ToneWarning, Glyph: GlyphWarning}
			if p.Status == interact.StatusError {
				line.Tone, line.Glyph = ToneError, GlyphError
			}
			if p.Code == interact.CodeBaseMissing {
				line.Button = openDir
			}
			lines = append(lines, line)
		}
	}
	for _, n := range s.Notes {
		switch n.Code {
		case interact.CodeLegacyBackups:
			lines = append(lines, InfoLine{Text: fmt.Sprintf(legacyLine, Count(n.Count)), Tone: ToneInfo, Glyph: GlyphInfo})
		case interact.CodeLeftoverTempFiles:
			lines = append(lines, InfoLine{Text: fmt.Sprintf(leftoverLine, Count(n.Count)), Tone: ToneInfo, Glyph: GlyphInfo})
		}
	}
	return lines
}

// groupsOf builds one group per run, newest first: the runs that wrote
// sets, the runs whose log tells they failed or were cancelled without a
// set, and the incomplete sets by day.
func groupsOf(s *health.Snapshot, r *flow.Run, folder string, now time.Time) []RunGroup {
	type groupAt struct {
		at time.Time
		g  RunGroup
	}
	var all []groupAt
	keysOfRun := keySetsByRun(s)

	// Runs with complete sets, oldest first, to tell when the keys changed.
	runs := slices.Clone(s.Runs)
	slices.Reverse(runs)
	var previous time.Time
	for _, run := range runs {
		// The run created its keys when they are newer than the run before
		// it (for the oldest run left: created just before it).
		keysCreated := keysOfRun[run.RunID]
		newKeys := !keysCreated.IsZero() && keysCreated.After(previous) && !keysCreated.After(run.Created)
		if previous.IsZero() {
			newKeys = !keysCreated.IsZero() && run.Created.Sub(keysCreated) < time.Hour
		}
		previous = run.Created
		g := RunGroup{RunID: run.RunID, When: When(run.Created, now)}
		if l := s.LogOf(run.RunID); l != nil {
			g.LogPath = l.Path
		}
		var bytes int64
		for _, info := range s.Sets {
			if info.Complete() && naming.BackupID(info.Header.RunID) == run.RunID {
				bytes += info.SizeBytes
				if folder == AllFolders || info.Entry.DirectoryName == folder {
					g.Rows = append(g.Rows, rowOf(s, info, r, now))
				} else {
					g.Hidden++
				}
			}
		}
		if len(g.Rows) == 0 {
			continue
		}
		g.Header, g.Tone, g.Glyph = runHeader(g.When, len(run.Entries), bytes, s.Facts[run.RunID], newKeys)
		all = append(all, groupAt{run.Created, g})
	}

	// Runs that left only their log.
	withSets := map[naming.BackupID]bool{}
	for _, run := range s.Runs {
		withSets[run.RunID] = true
	}
	if folder == AllFolders {
		for _, l := range s.Logs {
			b := s.Facts[l.RunID].Backup
			if withSets[l.RunID] || b == nil || (b.Result != logging.ResultFailed && b.Result != logging.ResultCancelled) {
				continue
			}
			at := l.Modified
			if !b.Time.IsZero() {
				at = b.Time
			}
			g := RunGroup{RunID: l.RunID, LogPath: l.Path, When: When(at, now)}
			g.Placeholder = runNoSets
			if b.Result == logging.ResultFailed {
				g.Header, g.Tone, g.Glyph = fmt.Sprintf(runFailed, capitalize(g.When)), ToneError, GlyphError
			} else {
				g.Header, g.Tone, g.Glyph = fmt.Sprintf(runCancelled, capitalize(g.When)), ToneSecondary, GlyphNone
			}
			all = append(all, groupAt{at, g})
		}
	}

	// Incomplete sets, by day.
	byDay := map[string]*RunGroup{}
	for _, info := range s.Sets {
		if info.Complete() || (folder != AllFolders && info.Entry.DirectoryName != folder) {
			continue
		}
		g := byDay[info.Entry.Date]
		if g == nil {
			g = &RunGroup{Header: fmt.Sprintf(runIncomplete, info.Entry.Date), Tone: ToneError, Glyph: GlyphError}
			byDay[info.Entry.Date] = g
		}
		g.Rows = append(g.Rows, rowOf(s, info, r, now))
	}
	for day, g := range byDay {
		at, _ := time.ParseInLocation("2006-01-02", day, time.Local)
		all = append(all, groupAt{at, *g})
	}

	slices.SortStableFunc(all, func(a, b groupAt) int { return b.at.Compare(a.at) })
	groups := make([]RunGroup, len(all))
	for i, ga := range all {
		groups[i] = ga.g
	}
	if len(groups) > 0 {
		groups[0].Expanded = true
	}
	return groups
}

// keySetsByRun returns when the key set of each run was created.
func keySetsByRun(s *health.Snapshot) map[naming.BackupID]time.Time {
	keys := map[naming.BackupID]time.Time{}
	for _, info := range s.Sets {
		if info.Complete() {
			keys[naming.BackupID(info.Header.RunID)] = info.Header.KeySet.Created()
		}
	}
	return keys
}

// runHeader words a run: "Today, 09:12 · 3 folders · 3.4 GB · 1 warning".
func runHeader(when string, folders int, bytes int64, facts logging.RunFacts, newKeys bool) (string, Tone, Glyph) {
	parts := []string{capitalize(when), folderPhrase(folders), Size(bytes)}
	tone, glyph := ToneNeutral, GlyphNone
	if b := facts.Backup; b != nil {
		switch {
		case b.Result == logging.ResultCancelled:
			parts = append(parts, runPartCancelled)
			tone, glyph = ToneWarning, GlyphWarning
		case b.Result == logging.ResultFailed:
			parts = append(parts, runPartFailed)
			tone, glyph = ToneError, GlyphError
		case b.Warnings == 1:
			parts = append(parts, warningOne)
			tone, glyph = ToneWarning, GlyphWarning
		case b.Warnings > 1:
			parts = append(parts, fmt.Sprintf(warningMany, b.Warnings))
			tone, glyph = ToneWarning, GlyphWarning
		}
	}
	if newKeys {
		parts = append(parts, runPartNewKeys)
	}
	return strings.Join(parts, " · "), tone, glyph
}

// rowOf words one backup set (GUI spec BK-2).
func rowOf(s *health.Snapshot, info catalog.SetInfo, r *flow.Run, now time.Time) BackupRow {
	e := info.Entry
	set := e.String()
	row := BackupRow{
		Set:     set,
		Folder:  e.DirectoryName,
		Badge:   badgeOf(e),
		BasedOn: "-",
		Size:    Size(info.SizeBytes),
		Chain:   string(e.ChainID),
		Usable:  true,
		Status:  Status{Text: statusComplete, Tone: ToneSuccess, Glyph: GlyphCheck},
	}
	for _, f := range s.Folders {
		if f.BackupName == e.DirectoryName {
			row.Path = Path(f.Resolved)
		}
	}
	baseFound := true
	if e.IsDiff() {
		baseFound = false
		for _, b := range s.Sets {
			if b.Complete() && !b.Entry.IsDiff() && b.Entry.ChainKey() == e.ChainKey() {
				row.BasedOn = fmt.Sprintf(basedOnFullShort, ShortDay(b.Created(), now))
				row.BasedOnTip = b.Entry.String()
				baseFound = true
			}
		}
		if !baseFound {
			row.BasedOn = basedOnNone
		}
	}
	verified, hasVerify := s.Verified[set]
	switch {
	case !info.Complete():
		row.Status = Status{Text: statusIncomplete, Tone: ToneError, Glyph: GlyphError}
		row.Usable, row.Reason = false, reasonIncomplete
	case !baseFound:
		row.Status = Status{Text: statusBaseMissing, Tone: ToneError, Glyph: GlyphError}
		row.Usable, row.Reason = false, reasonBaseMissing
	case hasVerify && verified.Result == logging.ResultFailed:
		row.Status = Status{Text: statusDamaged, Tone: ToneError, Glyph: GlyphError}
	case s.SetFacts[set].Unread() == 1:
		row.Status = Status{Text: statusSkippedOne, Tone: ToneWarning, Glyph: GlyphWarning}
	case s.SetFacts[set].Unread() > 1:
		row.Status = Status{Text: fmt.Sprintf(statusSkipped, Count(s.SetFacts[set].Unread())), Tone: ToneWarning, Glyph: GlyphWarning}
	case hasVerify:
		row.Status = Status{Text: fmt.Sprintf(statusVerified, When(verified.Time, now)), Tone: ToneSuccess, Glyph: GlyphCheck}
	}
	if st, ok := verifying(r, e); ok {
		row.Status = st
	}
	tip := []string{set}
	if row.Path != "" {
		tip = append(tip, row.Path)
	}
	if info.Complete() {
		tip = append(tip, fmt.Sprintf(tipCreated, Exact(info.Created())))
	}
	if row.BasedOnTip != "" {
		tip = append(tip, fmt.Sprintf(tipBasedOn, row.BasedOnTip))
	}
	tip = append(tip, ExactSize(info.SizeBytes))
	row.Tip = strings.Join(tip, "\n")
	return row
}

// verifying returns the status of e while the verification r reads it.
func verifying(r *flow.Run, e naming.BackupEntry) (Status, bool) {
	if r == nil || r.Op != flow.OpVerify || r.Verify == nil || r.Stage != flow.StageRunning {
		return Status{}, false
	}
	p := r.Progress
	if p.Phase != interact.PhaseVerifying || p.Index < 1 || p.Index > len(r.Verify.Sets) {
		return Status{}, false
	}
	if r.Verify.Sets[p.Index-1].Set != e {
		return Status{}, false
	}
	text := statusVerifying
	if f := p.Fraction(); f >= 0 {
		text = fmt.Sprintf(statusVerifyingPct, int(f*100))
	}
	return Status{Text: text, Tone: ToneNeutral}, true
}

// RunLogLink is the link in a run's header that opens its log (GUI spec
// BK-5).
const RunLogLink = linkRunLog

// Menu are the items of the list's context menu (GUI spec BK-4).
type Menu struct {
	Restore, Verify, ShowLog, CopyName, OpenFolder string
}

// RestoreMenu returns the context menu of the list.
func RestoreMenu() Menu {
	return Menu{Restore: menuRestoreText, Verify: menuVerifyText, ShowLog: menuShowLog, CopyName: menuCopyName, OpenFolder: menuOpenFolder}
}
