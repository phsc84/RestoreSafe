package health

import (
	"fmt"
	"os"
	"time"

	"github.com/phsc84/restoresafe/internal/format/catalog"
	"github.com/phsc84/restoresafe/internal/format/naming"
	"github.com/phsc84/restoresafe/internal/logging"
	"github.com/phsc84/restoresafe/internal/workflow/interact"
	"github.com/phsc84/restoresafe/internal/workflow/job"
)

// problems returns the errors and warnings of the snapshot, the most urgent
// first (GUI spec 3.5), and the notes. Findings of the health check that have
// their own problem keep its detail text.
func problems(p Params, in inspection, s *Snapshot) (problems, notes []Problem) {
	fromItems := func(status interact.Status, codes ...interact.Code) []Problem {
		var out []Problem
		for _, item := range in.items {
			for _, code := range codes {
				if item.Code == code && item.Severity != healthOK {
					out = append(out, Problem{Code: code, Status: status, Detail: item.Detail})
				}
			}
		}
		return out
	}

	// Errors.
	problems = append(problems, fromItems(interact.StatusError, interact.CodeConfigInvalid)...)
	for _, item := range in.items {
		if item.Severity == healthError && (item.Code == interact.CodeBackupDirUnreachable || item.Code == interact.CodeBackupDirNotWritable) {
			problems = append(problems, Problem{Code: item.Code, Status: interact.StatusError, Path: in.backupDir, Detail: item.Detail})
		}
	}
	for _, f := range s.Folders {
		if f.Err != nil {
			problems = append(problems, Problem{Code: job.SourceProblemCode(f.Err), Status: interact.StatusError, Folder: f.BackupName, Path: f.Resolved, Detail: fmt.Sprintf("%s → %v", f.Resolved, f.Err)})
		}
	}
	bases := baseMissing(s.Sets)
	problems = append(problems, bases...)
	problems = append(problems, invalidSets(s.Sets, bases)...)
	problems = append(problems, verifyFailed(s.Sets, s.Verified)...)

	// Warnings.
	if overdue, ok := overdue(p, s.Sets); ok {
		problems = append(problems, overdue)
	}
	if len(s.Runs) > 0 {
		for _, f := range s.Folders {
			if f.Err == nil && !f.Skip && f.Newest == nil {
				problems = append(problems, Problem{Code: interact.CodeFolderNotBackedUp, Status: interact.StatusWarn, Folder: f.BackupName, Path: f.Resolved})
			}
		}
	}
	for _, f := range s.Folders {
		if f.Newest == nil || f.Skip {
			continue
		}
		if fact := s.SetFacts[f.Newest.Entry.String()]; fact.Unread() > 0 {
			problems = append(problems, Problem{Code: interact.CodeSkippedFiles, Status: interact.StatusWarn, Folder: f.BackupName, Set: f.Newest.Entry, Count: fact.Unread()})
		}
	}
	problems = append(problems, incompleteNewest(s.Folders, s.Sets)...)
	if st := s.Storage; st.Known && st.FullEstimate > st.FreeBytes {
		problems = append(problems, Problem{Code: interact.CodeSpaceLow, Status: interact.StatusWarn, Path: in.backupDir, Bytes: st.FullEstimate})
	}
	problems = append(problems, fromItems(interact.StatusWarn, interact.CodeArgon2Capped)...)
	for _, item := range in.items {
		if item.Severity == healthWarn && item.Code == interact.CodeBackupDirNotWritable {
			problems = append(problems, Problem{Code: item.Code, Status: interact.StatusWarn, Path: in.backupDir, Detail: item.Detail})
		}
	}

	notes = fromItems(interact.StatusInfo, interact.CodeSourceDuplicate, interact.CodeYubiKeyNotConnected, interact.CodeNewKeysNeeded)
	if n := len(in.inventory.legacy); n > 0 {
		notes = append(notes, Problem{Code: interact.CodeLegacyBackups, Status: interact.StatusInfo, Path: in.backupDir, Count: n, Detail: itemDetail(in.items, interact.CodeLegacyBackups)})
	}
	if n := len(in.inventory.temps); n > 0 {
		notes = append(notes, Problem{Code: interact.CodeLeftoverTempFiles, Status: interact.StatusInfo, Path: in.backupDir, Count: n, Detail: itemDetail(in.items, interact.CodeLeftoverTempFiles)})
	}
	return problems, notes
}

func itemDetail(items []healthItem, code interact.Code) string {
	for _, item := range items {
		if item.Code == code {
			return item.Detail
		}
	}
	return ""
}

// baseMissing returns one problem per chain whose differentials have no
// complete full backup. When the full backup is there but can't be used, Set
// is it and Fault says why.
func baseMissing(infos []catalog.SetInfo) []Problem {
	fulls := completeFullChains(infos)
	var out []Problem
	index := make(map[string]int)
	for _, info := range infos {
		if !info.Complete() || !info.Entry.IsDiff() || fulls[info.Entry.ChainKey()] {
			continue
		}
		key := info.Entry.ChainKey()
		if i, seen := index[key]; seen {
			out[i].Count++
			continue
		}
		index[key] = len(out)
		p := Problem{
			Code: interact.CodeBaseMissing, Status: interact.StatusError,
			Folder: info.Entry.DirectoryName, ChainID: info.Entry.ChainID, Count: 1,
			Detail: baseMissingDetail(info),
		}
		if base := brokenFull(infos, key); base != nil {
			p.Set, p.Fault, p.Parts = base.Entry, catalog.FaultOf(base.Err), catalog.MissingParts(base.Err)
			p.Detail += fmt.Sprintf("\n%s → %v", base.Entry.String(), base.Err)
		}
		out = append(out, p)
	}
	return out
}

// brokenFull returns the full backup of the chain key that is there but
// can't be used, nil without one.
func brokenFull(infos []catalog.SetInfo, key string) *catalog.SetInfo {
	for i := range infos {
		if info := &infos[i]; !info.Entry.IsDiff() && info.Entry.ChainKey() == key && info.Err != nil {
			return info
		}
	}
	return nil
}

// invalidSets returns the sets that are broken, not just incomplete (e.g. a
// header that does not match the file name): they can never be used. A full
// backup that a BASE_MISSING problem names is left out: that problem says
// why it can't be used.
func invalidSets(infos []catalog.SetInfo, baseProblems []Problem) []Problem {
	named := make(map[string]bool)
	for _, p := range baseProblems {
		if p.Set != (naming.BackupEntry{}) {
			named[p.Set.String()] = true
		}
	}
	var out []Problem
	for _, info := range infos {
		if info.Err == nil || catalog.IsIncomplete(info.Err) || named[info.Entry.String()] {
			continue
		}
		out = append(out, Problem{
			Code: interact.CodeSetIncomplete, Status: interact.StatusError, Folder: info.Entry.DirectoryName, Set: info.Entry,
			Fault: catalog.FaultOf(info.Err), Parts: catalog.MissingParts(info.Err),
			Detail: fmt.Sprintf("%s → %v", info.Entry.String(), info.Err),
		})
	}
	return out
}

// verifyFailed returns the sets whose newest verification failed.
func verifyFailed(infos []catalog.SetInfo, verified map[string]logging.Fact) []Problem {
	var out []Problem
	for _, info := range infos {
		if fact, ok := verified[info.Entry.String()]; ok && fact.Result == logging.ResultFailed {
			out = append(out, Problem{Code: interact.CodeVerifyFailed, Status: interact.StatusError, Folder: info.Entry.DirectoryName, Set: info.Entry, Detail: fact.Error})
		}
	}
	return out
}

// overdue reports when the newest complete backup is older than the
// reminder limit; Count is its age in days.
func overdue(p Params, infos []catalog.SetInfo) (Problem, bool) {
	limit := p.Config.ReminderLimit()
	if limit <= 0 {
		return Problem{}, false
	}
	var newest time.Time
	for _, info := range infos {
		if c := info.Created(); info.Complete() && c.After(newest) {
			newest = c
		}
	}
	if newest.IsZero() {
		return Problem{}, false
	}
	days := int(p.Now.Sub(newest).Hours() / 24)
	if days < limit {
		return Problem{}, false
	}
	return Problem{Code: interact.CodeOverdue, Status: interact.StatusWarn, Count: days}, true
}

// incompleteNewest returns the incomplete sets that are newer than the
// newest complete set of their folder: probably a crash. Older incomplete
// sets are removed by the next backup's retention.
func incompleteNewest(folders []FolderStatus, infos []catalog.SetInfo) []Problem {
	var out []Problem
	for _, f := range folders {
		var since time.Time
		if f.Newest != nil {
			since = f.Newest.Created()
		}
		for _, info := range infos {
			if info.Entry.DirectoryName != f.BackupName || info.Err == nil || !catalog.IsIncomplete(info.Err) {
				continue
			}
			if newestModTime(info.Parts).After(since) {
				out = append(out, Problem{Code: interact.CodeIncompleteNewest, Status: interact.StatusWarn, Folder: f.BackupName, Set: info.Entry, Detail: fmt.Sprintf("%s → %v", info.Entry.String(), info.Err)})
			}
		}
	}
	return out
}

func newestModTime(parts []string) time.Time {
	var newest time.Time
	for _, part := range parts {
		if fi, err := os.Stat(part); err == nil && fi.ModTime().After(newest) {
			newest = fi.ModTime()
		}
	}
	return newest
}
