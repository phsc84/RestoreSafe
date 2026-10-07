package health

import (
	"RestoreSafe/internal/config"
	"RestoreSafe/internal/format/catalog"
	"RestoreSafe/internal/format/container"
	"RestoreSafe/internal/format/naming"
	"RestoreSafe/internal/logging"
	"RestoreSafe/internal/workflow/interact"
	"RestoreSafe/internal/workflow/plan"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// State is the overall state of the backups (GUI spec 3.5). The frontend adds
// Running while an operation runs.
type State int

const (
	// StateEmpty: no complete backup exists yet.
	StateEmpty State = iota
	// StateProtected: every folder has a backup and nothing needs attention.
	StateProtected
	// StateWarning: a problem needs attention soon.
	StateWarning
	// StateError: a problem keeps backups from being made or restored.
	StateError
)

// Problem is a finding the user should know about. It carries a code and
// the facts that describe it; the frontend chooses the words.
type Problem struct {
	Code interact.Code
	// Status is StatusError, StatusWarn or StatusInfo.
	Status interact.Status
	// Folder is the backup name of the folder concerned, Path a path.
	Folder string
	Path   string
	// ChainID is the chain concerned (BASE_MISSING).
	ChainID naming.BackupID
	// Set is the backup set concerned (VERIFY_FAILED, INCOMPLETE_NEWEST,
	// SET_INCOMPLETE; BASE_MISSING: the full backup when it is there but
	// can't be used).
	Set naming.BackupEntry
	// Fault says why Set can't be used (SET_INCOMPLETE, BASE_MISSING), and
	// Parts are its missing parts (FaultMissingParts).
	Fault catalog.Fault
	Parts []int
	// Count counts what the problem is about: files, sets, days.
	Count int
	// Bytes is a size (SPACE_LOW: the space a full backup of all folders
	// needs).
	Bytes int64
	// Detail is the technical description with its remedy, for "Show
	// details".
	Detail string
}

// FolderStatus describes one configured source folder.
type FolderStatus struct {
	plan.Source
	// Newest is the newest complete backup set of the folder, nil without
	// one.
	Newest *catalog.SetInfo
	// Next is what the next backup does with the folder; nil when it cannot
	// be backed up or is a duplicate.
	Next *plan.Folder
}

// Storage describes the space in the backup directory; the sizes are 0
// when the space is unknown (Known false).
type Storage struct {
	Known                 bool
	TotalBytes, FreeBytes int64
	// BackupBytes is the size of all RestoreSafe 2 backup sets.
	BackupBytes int64
	// FullEstimate is the space a full backup of all folders needs: the
	// newest full backup of each folder.
	FullEstimate int64
}

// KeysSummary describes the keys of the newest backup.
type KeysSummary struct {
	Exists       bool
	Created      time.Time
	Methods      string
	SpareYubiKey bool
	RecoveryCode bool
	// YubiKeyConnected is nil when the configuration uses no YubiKey.
	YubiKeyConnected *bool
	// NewKeysReason is set when the next backup creates new keys.
	NewKeysReason string
}

// Snapshot is the state of the backups as the user interface shows it. It
// needs no password.
type Snapshot struct {
	State State
	// Problems are errors and warnings, the most urgent first.
	Problems []Problem
	// Notes are information: YubiKey not connected, new keys needed, 1.x
	// backups, leftovers, duplicate sources.
	Notes     []Problem
	Folders   []FolderStatus
	BackupDir string
	Runs      []catalog.BackupRunSummary
	// Sets are all backup sets, newest first, including incomplete ones.
	Sets []catalog.SetInfo
	// Facts are the facts of each run's log, by run ID.
	Facts map[naming.BackupID]logging.RunFacts
	// SetFacts and Verified are what the logs say about each backup set
	// (by set name): its backup and its newest verification.
	SetFacts map[string]logging.Fact
	Verified map[string]logging.Fact
	// Logs are the run logs in the backup directory, newest first.
	Logs    []RunLog
	Storage Storage
	Keys    KeysSummary
	// Retention lists what the next backup removes if it succeeds.
	Retention []catalog.SetInfo
	// Check is the health check, for "Check details" and for what it
	// blocks.
	Check   Result
	Checked time.Time
}

// Params are the inputs of a snapshot.
type Params struct {
	Config     *config.Config
	ExeDir     string
	ConfigPath string
	// Now is the time the snapshot is taken at.
	Now time.Time
}

// TakeSnapshot inspects the configuration, the folders and the backup
// directory and returns the snapshot. It may block on an unreachable drive
// or share; Checker limits how long a caller waits.
func TakeSnapshot(p Params) Snapshot {
	in := inspect(p.Config, p.ExeDir, p.ConfigPath)
	s := Snapshot{
		BackupDir: in.backupDir,
		Sets:      in.inventory.infos,
		Runs:      catalog.BackupRunSummaries(in.inventory.infos),
		Check:     buildResult(in.items),
		Checked:   p.Now,
	}
	s.readFacts()

	keys := plan.KeysFor(p.Config, s.Sets)
	next := plan.Folders(p.Config, s.Sets, in.sources, keys, false, p.Now)
	s.Folders = folderStatuses(in.sources, s.Sets, next)
	s.Storage = storage(in.backupDir, s.Sets, s.Folders)
	s.Keys = keysSummary(p.Config, s.Sets, keys, in.yubiKeyConnected)
	s.Retention = plan.RetentionPreview(p.Config, s.Sets, in.sources, next, p.Now)

	s.Problems, s.Notes = problems(p, in, &s)
	s.State = stateOf(s.Problems, len(s.Runs) > 0)
	return s
}

// stateOf is Empty without a backup, unless the backup directory or the
// configuration has an error: then it is unknown whether backups exist.
func stateOf(problems []Problem, anyBackup bool) State {
	switch {
	case !anyBackup && !blindingError(problems):
		return StateEmpty
	case hasStatus(problems, interact.StatusError):
		return StateError
	case hasStatus(problems, interact.StatusWarn):
		return StateWarning
	}
	return StateProtected
}

// blindingError reports an error that keeps the backups from being seen.
func blindingError(problems []Problem) bool {
	for _, p := range problems {
		switch p.Code {
		case interact.CodeBackupDirUnreachable, interact.CodeBackupDirNotWritable, interact.CodeConfigInvalid:
			if p.Status == interact.StatusError {
				return true
			}
		}
	}
	return false
}

func hasStatus(problems []Problem, status interact.Status) bool {
	for _, p := range problems {
		if p.Status == status {
			return true
		}
	}
	return false
}

// RunLog is the log file of a backup run. Verifications and restores of the
// run append to it.
type RunLog struct {
	RunID naming.BackupID
	Date  string
	Path  string
	// Modified is when the log was last written.
	Modified time.Time
}

// readFacts reads the facts of every run log in the backup directory,
// including the logs of runs that wrote no complete set (a failed or
// cancelled backup). A log that cannot be read adds no facts.
func (s *Snapshot) readFacts() {
	s.Facts = make(map[naming.BackupID]logging.RunFacts)
	s.SetFacts = make(map[string]logging.Fact)
	s.Verified = make(map[string]logging.Fact)
	s.Logs = nil
	entries, err := os.ReadDir(s.BackupDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		date, runID, ok := naming.ParseLogFileName(e.Name())
		if !ok || e.IsDir() {
			continue
		}
		path := filepath.Join(s.BackupDir, e.Name())
		l := RunLog{RunID: runID, Date: date, Path: path}
		if fi, err := e.Info(); err == nil {
			l.Modified = fi.ModTime()
		}
		s.Logs = append(s.Logs, l)
		facts, err := logging.ReadFacts(path)
		if err != nil {
			continue
		}
		s.Facts[runID] = facts
		for set, fact := range facts.Sets {
			s.SetFacts[set] = fact
		}
		for set, fact := range facts.Verify {
			if known, ok := s.Verified[set]; !ok || fact.Time.After(known.Time) {
				s.Verified[set] = fact
			}
		}
	}
	slices.SortFunc(s.Logs, func(a, b RunLog) int { return b.Modified.Compare(a.Modified) })
}

// LogOf returns the log of the run runID, or nil.
func (s *Snapshot) LogOf(runID naming.BackupID) *RunLog {
	for i := range s.Logs {
		if s.Logs[i].RunID == runID {
			return &s.Logs[i]
		}
	}
	return nil
}

func folderStatuses(sources []plan.Source, infos []catalog.SetInfo, next map[string]*plan.Folder) []FolderStatus {
	folders := make([]FolderStatus, 0, len(sources))
	for _, src := range sources {
		f := FolderStatus{Source: src}
		if src.Err == nil && !src.Skip {
			f.Next = next[src.BackupName]
		}
		for i := range infos {
			if infos[i].Entry.DirectoryName == src.BackupName && infos[i].Complete() {
				f.Newest = &infos[i]
				break
			}
		}
		folders = append(folders, f)
	}
	return folders
}

func storage(backupDir string, infos []catalog.SetInfo, folders []FolderStatus) Storage {
	var st Storage
	for _, info := range infos {
		st.BackupBytes += info.SizeBytes
	}
	for _, f := range folders {
		if f.Skip {
			continue
		}
		for _, info := range infos {
			if info.Entry.DirectoryName == f.BackupName && info.Complete() && !info.Entry.IsDiff() {
				st.FullEstimate += info.SizeBytes
				break
			}
		}
	}
	where := backupDir
	if _, err := os.Stat(where); err != nil {
		where = volumeRoot(backupDir)
	}
	if free, total, err := queryDiskSpace(where); err == nil {
		st.Known, st.FreeBytes, st.TotalBytes = true, int64(free), int64(total)
	}
	return st
}

func keysSummary(cfg *config.Config, infos []catalog.SetInfo, keys plan.Keys, connected *bool) KeysSummary {
	k := KeysSummary{YubiKeyConnected: connected}
	ks := catalog.CurrentKeySet(infos)
	if ks == nil {
		return k
	}
	k.Exists = true
	k.Created = ks.Created()
	k.Methods = ks.AuthMode.Label()
	k.SpareYubiKey = ks.YubiKeyCount() > 1
	k.RecoveryCode = ks.HasSlotType(container.SlotRecovery)
	if keys.Existing == nil {
		k.NewKeysReason = keys.NewKeysReason
	}
	return k
}
