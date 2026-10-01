package view

import (
	"RestoreSafe/internal/config"
	"RestoreSafe/internal/format/catalog"
	"RestoreSafe/internal/format/naming"
	"RestoreSafe/internal/workflow/health"
	"RestoreSafe/internal/workflow/interact"
	"fmt"
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
	ActionCheckDetails
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

// Hero is the status at the top of the Overview (spec OV-1).
type Hero struct {
	Tone  Tone
	Glyph Glyph
	Title string
	// Line carries the facts; Link follows it ("Check details").
	Line string
	Link Button
	// Primary is the one fix action; Secondary is optional (errors only).
	Primary   Button
	Secondary *Button
}

// FolderRow is one folder of the Folders card (spec OV-3).
type FolderRow struct {
	Name, Path string
	// Date is the date of the newest backup; Badge its type.
	Date  string
	Badge *Badge
	// Next is the type of the next backup, NextReason why.
	Next, NextReason string
	// Problem replaces date and type for a folder that cannot be backed up.
	Problem string
	Tone    Tone
}

// FoldersCard lists the configured folders.
type FoldersCard struct {
	Title string
	Link  Button
	Rows  []FolderRow
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
}

// StorageCard is the backup directory and its space (spec OV-4).
type StorageCard struct {
	Path     string
	Used     string
	Segments []Segment
	Estimate string
}

// SetRow is one backup set of the Last backup card.
type SetRow struct {
	Name  string
	Badge Badge
	Based string
	Tone  Tone
}

// LastBackupCard is the newest backup run (spec OV-5).
type LastBackupCard struct {
	Title string
	Link  Button
	Tone  Tone
	Glyph Glyph
	Line  string
	Rows  []SetRow
}

// KeysCard describes the keys (spec OV-6).
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

// Overview is everything the Overview page shows.
type Overview struct {
	Hero       Hero
	Folders    FoldersCard
	Storage    StorageCard
	LastBackup LastBackupCard
	Keys       KeysCard
	// Status is the right part of the status bar.
	Status string
}

// OverviewOf computes the Overview from the snapshot s (nil while the
// first check runs), the configuration and the time.
func OverviewOf(s *health.Snapshot, cfg *config.Config, now time.Time) Overview {
	if s == nil {
		return Overview{Hero: Hero{Tone: ToneNeutral, Glyph: GlyphShield, Title: heroChecking, Primary: Button{Text: buttonBackUp, Action: ActionBackUp}}}
	}
	o := Overview{
		Hero:       heroOf(s, cfg, now),
		Folders:    foldersOf(s, now),
		Storage:    storageOf(s),
		LastBackup: lastBackupOf(s, now),
		Keys:       keysOf(s),
	}
	if s.Storage.Known {
		o.Status = fmt.Sprintf(statusFreeSpace, Size(s.Storage.FreeBytes))
	}
	return o
}

func heroOf(s *health.Snapshot, cfg *config.Config, now time.Time) Hero {
	backUp := Button{Text: buttonBackUp, Action: ActionBackUp, Enabled: !s.Check.BlocksBackup()}
	if !backUp.Enabled {
		backUp.Reason = reasonBlockedCheck
	}
	details := Button{Text: linkCheckDetails, Action: ActionCheckDetails, Enabled: true}
	folders := folderCount(s)

	switch s.State {
	case health.StateEmpty:
		line := fmt.Sprintf(heroEmptyLine, folders, s.BackupDir)
		if s.Keys.Exists {
			line = fmt.Sprintf(heroEmptyLineKeys, folders, s.BackupDir)
		}
		if len(s.Problems) > 0 {
			line += more(len(s.Problems))
		}
		return Hero{Tone: ToneNeutral, Glyph: GlyphShield, Title: heroEmpty, Line: line, Link: details, Primary: backUp}
	case health.StateProtected:
		line := folders
		if newest := newestRun(s); newest != nil {
			line = fmt.Sprintf(heroFacts, folders, When(newest.Created, now))
		}
		return Hero{Tone: ToneSuccess, Glyph: GlyphCheck, Title: heroProtected, Line: line, Link: details, Primary: backUp}
	}

	p := s.Problems[0]
	h := problemHero(p, s, cfg, now, backUp)
	h.Link = details
	if n := len(s.Problems) - 1; n > 0 {
		h.Line += more(n)
	}
	if s.State == health.StateError {
		h.Tone, h.Glyph = ToneError, GlyphError
	} else {
		h.Tone, h.Glyph = ToneWarning, GlyphWarning
	}
	return h
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
		return Hero{Title: heroDirUnreachable, Line: fmt.Sprintf(heroDirUnreachLine, p.Path), Primary: checkAgain}
	case interact.CodeBackupDirNotWritable:
		return Hero{Title: heroDirNotWritable, Line: fmt.Sprintf(heroDirNotWritLine, p.Path), Primary: checkAgain}
	case interact.CodeSourceMissing:
		return Hero{Title: fmt.Sprintf(heroSourceMissing, p.Path), Line: heroSourceMissLine, Primary: checkAgain, Secondary: editConfig}
	case interact.CodeSourceInvalid:
		return Hero{Title: fmt.Sprintf(heroSourceInvalid, p.Path), Line: heroSourceInvLine, Primary: checkAgain, Secondary: editConfig}
	case interact.CodeBaseMissing:
		title := fmt.Sprintf(heroBaseMissing, p.Count, p.Folder)
		if p.Count == 1 {
			title = fmt.Sprintf(heroBaseMissingOne, p.Folder)
		}
		return Hero{Title: title, Line: fmt.Sprintf(heroBaseMissingLine, p.ChainID, p.ChainID), Primary: showBackups}
	case interact.CodeSetIncomplete:
		return Hero{Title: fmt.Sprintf(heroSetDamaged, p.Folder), Line: heroSetDamagedLine, Primary: showBackups}
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
	card := FoldersCard{Title: fmt.Sprintf(cardFolders, len(s.Folders)), Link: Button{Text: linkDetails, Action: ActionOpenSettings, Enabled: true}}
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
				b := badgeOf(f.Newest.Entry)
				row.Badge = &b
			} else {
				row.Date, row.Tone = folderNoBackup, ToneWarning
			}
			if f.Next != nil {
				row.Next = fmt.Sprintf(folderNext, typeOf(f.Next.IsDiff()))
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
	card := StorageCard{Path: Path(s.BackupDir)}
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
		{Kind: SegmentBackups, Fraction: float64(st.BackupBytes) / total, Text: fmt.Sprintf(legendBackups, Size(st.BackupBytes))},
		{Kind: SegmentOther, Fraction: float64(other) / total, Text: fmt.Sprintf(legendOther, Size(other))},
		{Kind: SegmentFree, Fraction: float64(st.FreeBytes) / total, Text: fmt.Sprintf(legendFree, Size(st.FreeBytes))},
	}
	if st.FullEstimate > 0 {
		card.Estimate = fmt.Sprintf(storageEstimate, Size(st.FullEstimate))
	}
	return card
}

func lastBackupOf(s *health.Snapshot, now time.Time) LastBackupCard {
	card := LastBackupCard{Title: cardLastBackup, Link: Button{Text: linkShowInBackups, Action: ActionShowInBackups, Enabled: true}}
	run := newestRun(s)
	if run == nil {
		card.Line, card.Tone, card.Glyph = lastBackupNone, ToneSecondary, GlyphNone
		card.Link.Enabled = false
		return card
	}
	var size int64
	for _, info := range s.Sets {
		if info.Complete() && naming.BackupID(info.Header.RunID) == run.RunID {
			size += info.SizeBytes
		}
	}
	folders := folderOne
	if n := len(run.Entries); n != 1 {
		folders = fmt.Sprintf(folderMany, n)
	}
	card.Line = fmt.Sprintf(lastBackupLine, capitalize(When(run.Created, now)), folders, Size(size))
	card.Tone, card.Glyph = ToneSuccess, GlyphCheck
	if facts, ok := s.Facts[run.RunID]; ok && facts.Backup != nil {
		if facts.Backup.Seconds > 0 {
			card.Line += fmt.Sprintf(lastBackupDuration, Duration(time.Duration(facts.Backup.Seconds)*time.Second))
		}
		if facts.Backup.Warnings > 0 {
			card.Tone, card.Glyph = ToneWarning, GlyphWarning
		}
	}
	for _, e := range run.Entries {
		row := SetRow{Name: e.DirectoryName, Badge: badgeOf(e), Based: newChain}
		if e.IsDiff() {
			row.Based = basedOnMissing
			row.Tone = ToneError
			for _, info := range s.Sets {
				if info.Complete() && !info.Entry.IsDiff() && info.Entry.ChainKey() == e.ChainKey() {
					row.Based, row.Tone = fmt.Sprintf(basedOnFull, ShortDay(info.Created(), now)), ToneSecondary
				}
			}
		}
		card.Rows = append(card.Rows, row)
	}
	return card
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
		details = append([]string{keysSpare}, details...)
	}
	if k.RecoveryCode {
		details = append([]string{keysRecovery}, details...)
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
