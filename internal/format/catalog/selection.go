package catalog

import (
	"sort"
	"time"

	"github.com/phsc84/restoresafe/internal/format/naming"
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
