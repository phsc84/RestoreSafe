package catalog

import (
	"RestoreSafe/internal/format/naming"
	"fmt"
	"sort"
	"strings"
	"time"
)

// BackupRunSummary groups the complete backup sets written by one backup run.
type BackupRunSummary struct {
	RunID   naming.BackupID
	Date    string
	Entries []naming.BackupEntry
	Created time.Time
}

// BackupRunSummaries groups complete sets by the run that wrote them, newest
// run first. Incomplete sets are not restore points and are left out.
func BackupRunSummaries(infos []SetInfo) []BackupRunSummary {
	byRun := make(map[string]*BackupRunSummary)
	for _, info := range infos {
		if !info.Complete() {
			continue
		}
		key := info.Header.RunID
		run := byRun[key]
		if run == nil {
			run = &BackupRunSummary{RunID: naming.BackupID(info.Header.RunID), Date: info.Entry.Date}
			byRun[key] = run
		}
		run.Entries = append(run.Entries, info.Entry)
		if c := info.Created(); c.After(run.Created) {
			run.Created = c
		}
	}

	runs := make([]BackupRunSummary, 0, len(byRun))
	for _, run := range byRun {
		sort.Slice(run.Entries, func(i, j int) bool {
			return run.Entries[i].DirectoryName < run.Entries[j].DirectoryName
		})
		runs = append(runs, *run)
	}
	sort.Slice(runs, func(i, j int) bool {
		if !runs[i].Created.Equal(runs[j].Created) {
			return runs[i].Created.After(runs[j].Created)
		}
		return string(runs[i].RunID) > string(runs[j].RunID)
	})
	return runs
}

// ResolveSelection maps user input to backup sets: a run ID selects every set
// written by that run; a full set name (e.g. Docs_ABC123_2026-09-01_FULL)
// selects that set.
func ResolveSelection(input string, runs []BackupRunSummary) ([]naming.BackupEntry, error) {
	input = strings.TrimSpace(input)
	upper := strings.ToUpper(input)

	if IsRawBackupID(upper) {
		for _, run := range runs {
			if string(run.RunID) == upper {
				return run.Entries, nil
			}
		}
	}
	for _, run := range runs {
		for _, e := range run.Entries {
			if strings.EqualFold(e.String(), input) {
				return []naming.BackupEntry{e}, nil
			}
		}
	}
	return nil, fmt.Errorf("Backup %q not found.", input)
}

// IsRawBackupID reports whether input has the form of a 6-character backup ID.
func IsRawBackupID(input string) bool {
	if len(input) != 6 {
		return false
	}
	for _, r := range input {
		if (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}
