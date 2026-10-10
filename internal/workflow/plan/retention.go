package plan

import (
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/phsc84/restoresafe/internal/config"
	"github.com/phsc84/restoresafe/internal/format/catalog"
	"github.com/phsc84/restoresafe/internal/format/container"
	"github.com/phsc84/restoresafe/internal/format/naming"
)

// UnreadableSetError reports a backup set whose metadata cannot be read
// for another reason than being incomplete. Retention removes nothing then,
// so no backup is deleted based on incomplete information.
type UnreadableSetError struct {
	Set catalog.SetInfo
}

func (e *UnreadableSetError) Error() string {
	return fmt.Sprintf("failed to inspect backup set %s (%v)", e.Set.Entry.String(), e.Set.Err)
}

// Retention returns, per backup name, the backup sets that retention
// removes: it keeps the newest keep chains (a full backup plus its
// differentials) of each source directory and removes older chains as a
// whole, so a full backup is never removed while a kept differential depends
// on it. Within each kept chain, only the newest keepDiffs differentials are
// kept (0 keeps all; the newest differential is always kept). Incomplete
// sets older than the newest complete set of their directory are removed
// too. Only the directories of sources without an error take part. With keep
// and keepDiffs both 0, nothing is removed. A set of these directories whose
// metadata cannot be read stops retention with an *UnreadableSetError.
func Retention(sources []Source, infos []catalog.SetInfo, keep, keepDiffs int) (map[string][]catalog.SetInfo, error) {
	if keep <= 0 && keepDiffs <= 0 {
		return nil, nil
	}
	directories := make(map[string]bool)
	for _, src := range sources {
		if src.Err == nil {
			directories[src.BackupName] = true
		}
	}
	for _, info := range infos {
		if directories[info.Entry.DirectoryName] && info.Err != nil && !catalog.IsIncomplete(info.Err) {
			return nil, &UnreadableSetError{Set: info}
		}
	}
	removed := make(map[string][]catalog.SetInfo)
	for directory := range directories {
		if sets := retentionCandidates(directory, infos, keep, keepDiffs); len(sets) > 0 {
			removed[directory] = sets
		}
	}
	return removed, nil
}

// RetentionPreview returns the backup sets that retention removes after the
// planned run, in the order of sources: it adds the planned sets (folders,
// created now) to the inventory and applies Retention to it. The preview
// assumes the run succeeds; retention is skipped when a new backup misses
// unreadable files or its verification fails, which only the run can tell.
func RetentionPreview(cfg *config.Config, infos []catalog.SetInfo, sources []Source, folders map[string]*Folder, now time.Time) []catalog.SetInfo {
	created := now.UTC().Format(time.RFC3339Nano)
	planned := make([]catalog.SetInfo, 0, len(folders)+len(infos))
	for _, src := range sources {
		folder, ok := folders[src.BackupName]
		if !ok || src.Skip {
			continue
		}
		entry := naming.BackupEntry{DirectoryName: src.BackupName}
		if folder.IsDiff() {
			entry.ChainID = folder.Base.Entry.ChainID
			entry.DiffNumber = folder.DiffNumber
		}
		planned = append(planned, catalog.SetInfo{Entry: entry, Header: &container.Header{CreatedUTC: created}})
	}
	planned = append(planned, infos...)

	removed, err := Retention(sources, planned, cfg.RetentionKeep, cfg.Differential.RetentionKeepDifferentials)
	if err != nil {
		return nil
	}
	var out []catalog.SetInfo
	seen := make(map[string]bool)
	for _, src := range sources {
		if src.Err != nil || seen[src.BackupName] {
			continue
		}
		seen[src.BackupName] = true
		out = append(out, removed[src.BackupName]...)
	}
	return out
}

// chain groups the backup sets of one directory that share a chain ID.
type chain struct {
	id          naming.BackupID
	fullCreated time.Time
	hasFull     bool
	sets        []catalog.SetInfo
}

// retentionCandidates returns the sets of directory that retention removes.
func retentionCandidates(directory string, infos []catalog.SetInfo, retentionKeep, keepDifferentials int) []catalog.SetInfo {
	chains := make(map[naming.BackupID]*chain)
	var incomplete []catalog.SetInfo
	var newestComplete time.Time
	for _, info := range infos {
		if info.Entry.DirectoryName != directory {
			continue
		}
		if !info.Complete() {
			incomplete = append(incomplete, info)
			continue
		}
		if c := info.Created(); c.After(newestComplete) {
			newestComplete = c
		}
		ch := chains[info.Entry.ChainID]
		if ch == nil {
			ch = &chain{id: info.Entry.ChainID}
			chains[info.Entry.ChainID] = ch
		}
		ch.sets = append(ch.sets, info)
		if !info.Entry.IsDiff() {
			ch.hasFull = true
			ch.fullCreated = info.Created()
		}
	}

	// Only chains with a complete full backup take part in retention; a chain
	// whose full backup is missing is reported by the health check instead.
	ordered := make([]*chain, 0, len(chains))
	for _, ch := range chains {
		if ch.hasFull {
			ordered = append(ordered, ch)
		}
	}
	sort.Slice(ordered, func(i, j int) bool {
		if !ordered[i].fullCreated.Equal(ordered[j].fullCreated) {
			return ordered[i].fullCreated.After(ordered[j].fullCreated)
		}
		return ordered[i].id > ordered[j].id
	})

	var out []catalog.SetInfo
	kept := ordered
	if retentionKeep > 0 && len(ordered) > retentionKeep {
		for _, ch := range ordered[retentionKeep:] {
			out = append(out, ch.sets...)
		}
		kept = ordered[:retentionKeep]
	}
	if keepDifferentials > 0 {
		for _, ch := range kept {
			out = append(out, surplusDifferentials(ch, keepDifferentials)...)
		}
	}
	for _, info := range incomplete {
		if !newestComplete.IsZero() && newestPartModTime(info.Parts).Before(newestComplete) {
			out = append(out, info)
		}
	}
	return out
}

// surplusDifferentials returns the differentials of ch beyond the newest keep
// ones. Differentials are independent of each other (each needs only the
// full backup), so older ones can be removed without affecting newer ones.
func surplusDifferentials(ch *chain, keep int) []catalog.SetInfo {
	var diffs []catalog.SetInfo
	for _, s := range ch.sets {
		if s.Entry.IsDiff() {
			diffs = append(diffs, s)
		}
	}
	if len(diffs) <= keep {
		return nil
	}
	sort.Slice(diffs, func(i, j int) bool {
		if diffs[i].Entry.DiffNumber != diffs[j].Entry.DiffNumber {
			return diffs[i].Entry.DiffNumber > diffs[j].Entry.DiffNumber
		}
		return diffs[i].Created().After(diffs[j].Created())
	})
	return diffs[keep:]
}

func newestPartModTime(parts []string) time.Time {
	var newest time.Time
	for _, p := range parts {
		if fi, err := os.Stat(p); err == nil && fi.ModTime().After(newest) {
			newest = fi.ModTime()
		}
	}
	return newest
}
