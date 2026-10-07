package view

import (
	"RestoreSafe/internal/config"
	"RestoreSafe/internal/format/catalog"
	"RestoreSafe/internal/format/naming"
	"RestoreSafe/internal/logging"
	"RestoreSafe/internal/workflow/health"
	"RestoreSafe/internal/workflow/interact"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Tone is the meaning of a color; the page maps it to the palette.
type Tone int

const (
	ToneNeutral Tone = iota
	ToneSuccess
	ToneWarning
	ToneError
	ToneInfo
	ToneSecondary
)

// Glyph names an icon; the page maps it to the icon font.
type Glyph int

const (
	GlyphNone Glyph = iota
	GlyphCheck
	GlyphWarning
	GlyphError
	GlyphInfo
	GlyphShield
	GlyphFolder
	GlyphDrive
	GlyphKey
)

// Action is what a button or link does; the page maps it to a handler.
type Action int

const (
	ActionNone Action = iota
	ActionBackUp
	ActionCheckAgain
	ActionShowInBackups
	ActionEditConfig
	ActionOpenSettings
	ActionRestore
	ActionVerify
	ActionOpenBackupDir
	ActionStartBackup
	ActionFullBackup
	ActionAutomaticPlan
	ActionNewKeys
	ActionCancel
	ActionShowDetails
	ActionShowLog
	ActionDismiss
	ActionStartRestore
	ActionStartVerify
	ActionOpenRestored
	ActionReload
	ActionAddMissing
)

// Button is a button or link of a view.
type Button struct {
	Text    string
	Action  Action
	Enabled bool
	// Reason says why a disabled button is disabled (tooltip).
	Reason string
}

// BadgeKind is the kind of a type badge.
type BadgeKind int

const (
	BadgeFull BadgeKind = iota
	BadgeDiff
)

// Badge is a type badge: FULL, DIFF 3.
type Badge struct {
	Kind BadgeKind
	Text string
	// Name is what screen readers announce.
	Name string
}

// Hero is the status at the top of the Create backup page (GUI spec OV-1).
type Hero struct {
	Tone  Tone
	Glyph Glyph
	Title string
	// Line carries the facts.
	Line string
	// Primary is the one fix action; Secondary is optional (errors only).
	Primary   Button
	Secondary *Button
}

// FolderRow is one folder of the Folders card (GUI spec OV-3).
type FolderRow struct {
	Name, Path string
	// Date is the date of the newest backup.
	Date string
	// DateTip is the exact date and time.
	DateTip string
	// Next is the type of the next backup, NextReason why.
	Next, NextReason string
	// Problem replaces date and type for a folder that cannot be backed up.
	Problem string
	Tone    Tone
}

// FoldersCard lists the configured folders (GUI spec OV-3, OV-5).
type FoldersCard struct {
	Title string
	Rows  []FolderRow
	// Note tells of a newest backup that failed or was cancelled.
	Note     string
	NoteTone Tone
}

// SegmentKind is the kind of a storage bar segment.
type SegmentKind int

const (
	SegmentBackups SegmentKind = iota
	SegmentOther
	SegmentFree
)

// Segment is a part of the storage bar.
type Segment struct {
	Kind     SegmentKind
	Fraction float64
	Text     string // legend
	Tip      string // the exact size
}

// StorageCard is the backup directory and its space (GUI spec OV-4).
type StorageCard struct {
	Title    string
	Path     string
	Used     string
	Segments []Segment
	Estimate string
}

// KeysCard describes the keys (GUI spec OV-6).
type KeysCard struct {
	Title   string
	Methods string
	Details string
	Note    string
	Tone    Tone
	// YubiKey is whether the YubiKey is connected, "" when none is used.
	YubiKey     string
	YubiKeyTone Tone
}

// CreatePage is everything the Create backup page shows.
type CreatePage struct {
	Title   string
	Hero    Hero
	Folders FoldersCard
	Storage StorageCard
	Keys    KeysCard
	// Refresh takes a new snapshot of the machine (F5).
	Refresh Button
}

// CreatePageOf computes the Create backup page from the snapshot s (nil while the
// first check runs), the configuration and the time.
func CreatePageOf(s *health.Snapshot, cfg *config.Config, now time.Time) CreatePage {
	if s == nil {
		return CreatePage{Title: navCreate, Hero: Hero{Tone: ToneNeutral, Glyph: GlyphShield, Title: heroChecking, Primary: Button{Text: buttonBackUp, Action: ActionBackUp}}, Refresh: Button{Text: buttonRefresh, Action: ActionCheckAgain}}
	}
	o := CreatePage{
		Title:   navCreate,
		Hero:    heroOf(s, cfg, now),
		Folders: foldersOf(s, now),
		Storage: storageOf(s),
		Keys:    keysOf(s),
		Refresh: Button{Text: buttonRefresh, Action: ActionCheckAgain, Enabled: true},
	}
	return o
}

// ReloadErrorHero is the hero while config.yaml could not be read again.
func ReloadErrorHero(err error) Hero {
	reason := errorText(err)
	return Hero{
		Tone: ToneError, Glyph: GlyphError,
		Title:   heroReloadTitle,
		Line:    fmt.Sprintf(heroReloadLine, reason),
		Primary: Button{Text: buttonEditConfig, Action: ActionEditConfig, Enabled: true},
	}
}

func heroOf(s *health.Snapshot, cfg *config.Config, now time.Time) Hero {
	backUp := Button{Text: buttonBackUp, Action: ActionBackUp, Enabled: !s.Check.BlocksBackup()}
	if !backUp.Enabled {
		backUp.Reason = reasonBlockedCheck
	}
	folders := folderCount(s)

	problems := backupProblems(s.Problems)
	switch {
	case s.State == health.StateEmpty:
		return Hero{Tone: ToneNeutral, Glyph: GlyphShield, Title: heroEmpty, Primary: backUp}
	case len(problems) == 0:
		line := folders
		if newest := newestRun(s); newest != nil {
			line = fmt.Sprintf(heroFacts, folders, When(newest.Created, now))
		}
		if len(s.Problems) > 0 {
			// Existing backups have a problem: not protected, but nothing to
			// fix here.
			return Hero{Tone: ToneNeutral, Glyph: GlyphShield, Title: heroReady, Line: line, Primary: backUp}
		}
		return Hero{Tone: ToneSuccess, Glyph: GlyphCheck, Title: heroProtected, Line: line, Primary: backUp}
	}

	p := problems[0]
	h := problemHero(p, s, cfg, now, backUp)
	if n := len(problems) - 1; n > 0 {
		h.Line += more(n)
	}
	if slices.ContainsFunc(problems, func(p health.Problem) bool { return p.Status == interact.StatusError }) {
		h.Tone, h.Glyph = ToneError, GlyphError
	} else {
		h.Tone, h.Glyph = ToneWarning, GlyphWarning
	}
	return h
}

// backupProblems leaves out the problems of existing backups that a new
// backup doesn't fix by itself and that don't keep it from running: Restore
// backup lists them (BK-6).
func backupProblems(problems []health.Problem) []health.Problem {
	var out []health.Problem
	for _, p := range problems {
		switch p.Code {
		case interact.CodeBaseMissing, interact.CodeSetIncomplete, interact.CodeIncompleteNewest:
			continue
		}
		out = append(out, p)
	}
	return out
}

// problemHero words the most urgent problem with its fix action.
func problemHero(p health.Problem, s *health.Snapshot, cfg *config.Config, now time.Time, backUp Button) Hero {
	checkAgain := Button{Text: buttonCheckAgain, Action: ActionCheckAgain, Enabled: true}
	editConfig := &Button{Text: buttonEditConfig, Action: ActionEditConfig, Enabled: true}
	showBackups := Button{Text: buttonShowBackups, Action: ActionShowInBackups, Enabled: true}
	switch p.Code {
	case interact.CodeConfigInvalid:
		return Hero{Title: heroConfigInvalid, Line: heroConfigLine, Primary: *editConfig}
	case interact.CodeBackupDirUnreachable:
		return Hero{Title: heroDirUnreachable, Line: fmt.Sprintf(heroDirUnreachLine, Path(p.Path)), Primary: checkAgain}
	case interact.CodeBackupDirNotWritable:
		return Hero{Title: heroDirNotWritable, Line: fmt.Sprintf(heroDirNotWritLine, Path(p.Path)), Primary: checkAgain}
	case interact.CodeSourceMissing:
		return Hero{Title: fmt.Sprintf(heroSourceMissing, Path(p.Path)), Line: heroSourceMissLine, Primary: checkAgain, Secondary: editConfig}
	case interact.CodeSourceInvalid:
		return Hero{Title: fmt.Sprintf(heroSourceInvalid, Path(p.Path)), Line: heroSourceInvLine, Primary: checkAgain, Secondary: editConfig}
	case interact.CodeBaseMissing:
		title, line := fmt.Sprintf(heroBaseMissing, p.Count, p.Folder), heroBaseMissingLine
		if p.Count == 1 {
			title, line = fmt.Sprintf(heroBaseMissingOne, p.Folder), heroBaseMissingLineOne
		}
		return Hero{Title: title, Line: fmt.Sprintf(line, p.ChainID, faultWords(p), p.ChainID), Primary: showBackups}
	case interact.CodeSetIncomplete:
		return setProblemHero(p, now, showBackups)
	case interact.CodeVerifyFailed:
		return Hero{Title: fmt.Sprintf(heroVerifyFailed, p.Folder), Line: heroVerifyLine, Primary: backUp}
	case interact.CodeOverdue:
		last := ""
		if newest := newestRun(s); newest != nil {
			last = When(newest.Created, now)
		}
		return Hero{Title: fmt.Sprintf(heroOverdue, p.Count), Line: fmt.Sprintf(heroOverdueLine, cfg.ReminderLimit(), last), Primary: backUp}
	case interact.CodeFolderNotBackedUp:
		return Hero{Title: fmt.Sprintf(heroNotBackedUp, p.Folder), Line: heroNotBackedUpLine, Primary: backUp}
	case interact.CodeSkippedFiles:
		title := fmt.Sprintf(heroSkipped, p.Count, p.Folder)
		if p.Count == 1 {
			title = fmt.Sprintf(heroSkippedOne, p.Folder)
		}
		return Hero{Title: title, Line: fmt.Sprintf(heroSkippedLine, p.Folder), Primary: backUp}
	case interact.CodeIncompleteNewest:
		return Hero{Title: fmt.Sprintf(heroIncomplete, p.Folder), Line: heroIncompleteLine, Primary: showBackups}
	case interact.CodeSpaceLow:
		return Hero{Title: heroSpaceLow, Line: fmt.Sprintf(heroSpaceLowLine, Size(p.Bytes), Size(s.Storage.FreeBytes)), Primary: showBackups}
	case interact.CodeArgon2Capped:
		return Hero{Title: heroArgon2, Line: heroArgon2Line, Primary: *editConfig}
	}
	return Hero{Title: string(p.Code), Line: p.Detail, Primary: checkAgain}
}

// setProblemHero words a set that can't be used (SET_INCOMPLETE): which set,
// why, and the remedy for that reason.
func setProblemHero(p health.Problem, now time.Time, primary Button) Hero {
	title, remedy := heroSetUnusable, remedyFromCopy
	switch p.Fault {
	case catalog.FaultRenamed:
		remedy = remedyNames
	case catalog.FaultUnreadable:
		title, remedy = heroSetUnreadable, remedyDrive
	}
	day := baseDay(p.Set, now)
	line := fmt.Sprintf(heroSetFullLine, day, p.Set.ChainID, faultWords(p), remedy)
	if p.Set.IsDiff() {
		line = fmt.Sprintf(heroSetDiffLine, p.Set.DiffNumber, day, p.Set.ChainID, faultWords(p), remedy)
	}
	return Hero{Title: fmt.Sprintf(title, p.Folder), Line: line, Primary: primary}
}

// faultWords says why the set of p can't be used ("is missing part 001").
// Without a set (BASE_MISSING), the full backup is missing.
func faultWords(p health.Problem) string {
	if p.Set == (naming.BackupEntry{}) {
		return faultGone
	}
	switch p.Fault {
	case catalog.FaultIncomplete:
		return faultIncomplete
	case catalog.FaultMissingParts:
		if len(p.Parts) == 1 {
			return fmt.Sprintf(faultPartMissing, p.Parts[0])
		}
		if len(p.Parts) > 1 {
			return fmt.Sprintf(faultPartsMissing, len(p.Parts), p.Parts[0], p.Parts[len(p.Parts)-1])
		}
	case catalog.FaultRenamed:
		return faultRenamed
	case catalog.FaultUnreadable:
		return faultUnreadable
	}
	return faultDamaged
}

func more(n int) string {
	if n == 1 {
		return heroMoreOne
	}
	return fmt.Sprintf(heroMore, n)
}

func folderCount(s *health.Snapshot) string {
	n := 0
	for _, f := range s.Folders {
		if !f.Skip {
			n++
		}
	}
	if n == 1 {
		return folderOne
	}
	return fmt.Sprintf(folderMany, n)
}

func newestRun(s *health.Snapshot) *catalog.BackupRunSummary {
	if len(s.Runs) == 0 {
		return nil
	}
	return &s.Runs[0]
}

func foldersOf(s *health.Snapshot, now time.Time) FoldersCard {
	card := FoldersCard{Title: cardFolders}
	card.Note, card.NoteTone = lastRunNote(s, now)
	for _, f := range s.Folders {
		row := FolderRow{Name: f.BackupName, Path: Path(f.Resolved)}
		switch {
		case f.Err != nil:
			row.Problem, row.Tone = folderInvalid, ToneError
			if p := folderProblem(s, f.BackupName); p != nil && p.Code == interact.CodeSourceMissing {
				row.Problem = folderMissing
			}
		case f.Skip:
			row.Problem, row.Tone = folderDuplicate, ToneSecondary
		default:
			if f.Newest != nil {
				row.Date = When(f.Newest.Created(), now)
				row.DateTip = Exact(f.Newest.Created())
			} else {
				row.Date, row.Tone = folderNoBackup, ToneWarning
			}
			if f.Next != nil {
				row.Next = typeOf(f.Next.IsDiff())
				row.NextReason = f.Next.Reason
			}
		}
		card.Rows = append(card.Rows, row)
	}
	return card
}

func folderProblem(s *health.Snapshot, folder string) *health.Problem {
	for i := range s.Problems {
		if s.Problems[i].Folder == folder {
			return &s.Problems[i]
		}
	}
	return nil
}

func typeOf(diff bool) string {
	if diff {
		return typeDiff
	}
	return typeFull
}

func badgeOf(e naming.BackupEntry) Badge {
	if e.IsDiff() {
		return Badge{Kind: BadgeDiff, Text: fmt.Sprintf(badgeDiff, e.DiffNumber), Name: fmt.Sprintf(badgeDiffName, e.DiffNumber)}
	}
	return Badge{Kind: BadgeFull, Text: badgeFull, Name: badgeFullName}
}

func storageOf(s *health.Snapshot) StorageCard {
	card := StorageCard{Title: cardStorage, Path: Path(s.BackupDir)}
	st := s.Storage
	if !st.Known || st.TotalBytes <= 0 {
		card.Used = storageUnknown
		return card
	}
	used := st.TotalBytes - st.FreeBytes
	other := max(used-st.BackupBytes, 0)
	total := float64(st.TotalBytes)
	card.Used = fmt.Sprintf(storageUsed, Size(used), Size(st.TotalBytes))
	card.Segments = []Segment{
		{Kind: SegmentBackups, Fraction: float64(st.BackupBytes) / total, Text: fmt.Sprintf(legendBackups, Size(st.BackupBytes)), Tip: ExactSize(st.BackupBytes)},
		{Kind: SegmentOther, Fraction: float64(other) / total, Text: fmt.Sprintf(legendOther, Size(other)), Tip: ExactSize(other)},
		{Kind: SegmentFree, Fraction: float64(st.FreeBytes) / total, Text: fmt.Sprintf(legendFree, Size(st.FreeBytes)), Tip: ExactSize(st.FreeBytes)},
	}
	if st.FullEstimate > 0 {
		card.Estimate = fmt.Sprintf(storageEstimate, Size(st.FullEstimate))
	}
	return card
}

// lastRunNote tells of the newest backup when it failed or was cancelled:
// the folders' dates don't show that.
func lastRunNote(s *health.Snapshot, now time.Time) (string, Tone) {
	run := newestRun(s)
	if run == nil {
		return laterRunNote(s, time.Time{}, now)
	}
	if facts, ok := s.Facts[run.RunID]; ok && facts.Backup != nil {
		switch facts.Backup.Result {
		case logging.ResultFailed:
			return fmt.Sprintf(laterFailed, When(run.Created, now)), ToneError
		case logging.ResultCancelled:
			return fmt.Sprintf(lastCancelled, When(run.Created, now)), ToneSecondary
		}
	}
	return laterRunNote(s, run.Created, now)
}

func keysOf(s *health.Snapshot) KeysCard {
	k := s.Keys
	card := KeysCard{Title: cardKeys}
	switch {
	case k.YubiKeyConnected != nil && *k.YubiKeyConnected:
		card.YubiKey, card.YubiKeyTone = yubiKeyConnected, ToneSecondary
	case k.YubiKeyConnected != nil:
		card.YubiKey, card.YubiKeyTone = yubiKeyMissing, ToneInfo
	}
	if !k.Exists {
		card.Note, card.Tone = keysNone, ToneInfo
		return card
	}
	card.Methods = capitalize(k.Methods)
	details := []string{fmt.Sprintf(keysCreated, ShortDay(k.Created, s.Checked))}
	if k.SpareYubiKey {
		details = append(details, keysSpare)
	}
	if k.RecoveryCode {
		details = append(details, keysRecovery)
	}
	card.Details = capitalize(strings.Join(details, " · "))
	if k.NewKeysReason != "" {
		card.Note, card.Tone = fmt.Sprintf(keysNewNeeded, k.NewKeysReason), ToneInfo
	}
	return card
}

// capitalize makes the first letter upper case ("today, 09:12" at the start
// of a line).
func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// laterRunNote tells of the newest backup when it is newer than the run
// with sets and failed or was cancelled before it wrote a set.
func laterRunNote(s *health.Snapshot, after time.Time, now time.Time) (string, Tone) {
	for _, l := range s.Logs {
		b := s.Facts[l.RunID].Backup
		if b == nil || !b.Time.After(after) {
			continue
		}
		switch b.Result {
		case logging.ResultFailed:
			return fmt.Sprintf(laterFailed, When(b.Time, now)), ToneError
		case logging.ResultCancelled:
			return fmt.Sprintf(laterCancelled, When(b.Time, now)), ToneSecondary
		}
	}
	return "", ToneNeutral
}

// Table is the Folders card as a table (GUI spec OV-3): each folder's newest
// backup and the type of the next. While a backup runs, states holds each
// folder's progress, and the table shows the type and state instead
// (BR-4).
func (c FoldersCard) Table(states map[string]FolderProgress) Table {
	if states != nil {
		t := Table{Name: c.Title, Columns: []Column{{Title: columnFolder, Width: 160}, {Title: columnType, Width: 80}, {Title: columnStatus, Fill: true}}}
		for _, r := range c.Rows {
			row := TableRow{Tip: r.Path, Cells: []TableCell{{Text: r.Name}}}
			if s, ok := states[r.Name]; ok {
				b := s.Badge
				row.Cells = append(row.Cells, TableCell{Badge: &b}, TableCell{Text: s.Text, Tone: s.Tone})
			} else {
				// Not part of this backup.
				row.Cells = append(row.Cells, TableCell{}, TableCell{Text: r.Problem, Tone: r.Tone})
			}
			t.Rows = append(t.Rows, row)
		}
		return t
	}
	t := Table{Name: c.Title, Columns: []Column{{Title: columnFolder, Fill: true}, {Title: columnLastBackup, Width: 140}, {Title: columnNextBackup, Width: 100}}}
	for _, r := range c.Rows {
		last := r.Date
		if r.Problem != "" {
			last = r.Problem
		}
		reason := ""
		if r.NextReason != "" {
			reason = fmt.Sprintf(tipNextBackup, r.Next, r.NextReason)
		}
		t.Rows = append(t.Rows, TableRow{
			Tip:   joinTip(r.Path, r.DateTip, reason),
			Cells: []TableCell{{Text: r.Name}, {Text: last, Tone: r.Tone}, {Text: r.Next, Tone: ToneSecondary}},
		})
	}
	return t
}
