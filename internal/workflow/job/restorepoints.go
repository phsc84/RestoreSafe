package job

import (
	"fmt"
	"path/filepath"

	"github.com/phsc84/restoresafe/internal/format/catalog"
	"github.com/phsc84/restoresafe/internal/format/container"
	"github.com/phsc84/restoresafe/internal/format/naming"
	"github.com/phsc84/restoresafe/internal/logging"
	"github.com/phsc84/restoresafe/internal/problem"
	"github.com/phsc84/restoresafe/internal/workflow/interact"
)

// What restore and verify share about the restore points they read
// (refactoring 2.0 RF-30): checking the chosen sets, the "Backup selection"
// rows of their plans, and reading one restore point after the other.

// SelectionItem is a backup set chosen for a restore or verification.
type SelectionItem struct {
	Entry     naming.BackupEntry
	PartCount int
	// TotalSizeBytes is what the operation reads: the set and, for a
	// differential, its full backup.
	TotalSizeBytes int64
	// Base is the full backup a differential needs.
	Base *catalog.SetInfo
	// Err keeps the set from being used, e.g. its full backup is missing.
	Err error
}

// SelectionPreflight checks the selected sets. A differential needs its
// chain's full backup, looked up in inventory: a restore point is read
// whole, so its size covers both.
func SelectionPreflight(selected, inventory []catalog.SetInfo) []SelectionItem {
	items := make([]SelectionItem, 0, len(selected))
	for _, info := range selected {
		item := SelectionItem{Entry: info.Entry, PartCount: len(info.Parts), TotalSizeBytes: info.SizeBytes, Err: info.Err}
		if item.Err == nil && info.Entry.IsDiff() {
			base, err := catalog.BaseOf(inventory, info.Entry)
			if err != nil {
				item.Err = err
			} else {
				item.Base = base
				item.TotalSizeBytes += base.SizeBytes
			}
		}
		items = append(items, item)
	}
	return items
}

// SelectionRows returns the "Backup selection" rows of a plan's details:
// the backup directory and every set with its parts and full backup, and
// the issue of every set that can't be used.
func SelectionRows(backupDir string, items []SelectionItem) ([]interact.Row, []interact.Issue) {
	var issues []interact.Issue
	rows := []interact.Row{interact.Heading("Backup selection"), interact.Item(interact.StatusNone, "Path: "+filepath.ToSlash(backupDir))}
	for _, item := range items {
		status := interact.StatusOK
		if item.Err != nil {
			status = interact.StatusError
			issues = append(issues, interact.IssueOf(interact.StatusError, interact.CodeBaseMissing, item.Err))
		}
		var details []string
		if item.Base != nil {
			details = append(details, fmt.Sprintf("with full backup %s (parts: %d)", item.Base.Entry.String(), len(item.Base.Parts)))
		}
		rows = append(rows, interact.Item(status, fmt.Sprintf("%s (parts: %d)", item.Entry.String(), item.PartCount), details...))
	}
	return rows, issues
}

// SelectionBytes is what the operation reads of the sets that can be used.
func SelectionBytes(items []SelectionItem) int64 {
	var total int64
	for _, item := range items {
		if item.Err == nil {
			total += item.TotalSizeBytes
		}
	}
	return total
}

// SetPlan is the item as the plan shows it: the set, the full backup read
// with a differential, the size read, and what keeps it from being used.
func (item SelectionItem) SetPlan() interact.SetPlan {
	p := interact.SetPlan{Set: item.Entry, Bytes: item.TotalSizeBytes}
	if item.Base != nil {
		p.Base = item.Base.Entry
	}
	if item.Err != nil {
		p.Problem, p.Remedy = problem.Split(item.Err)
	}
	return p
}

// LogStart logs the start of a restore or verification ("Restore",
// "Verification") and the sets it reads.
func LogStart(log *logging.Logger, action string, selected []catalog.SetInfo) {
	first := selected[0].Header
	log.Info("%s started - ID: %s, date: %s", action, first.RunID, first.Date)
	log.Info("%s selection:", action)
	for _, info := range selected {
		log.Info("  %s", info.Entry.String())
	}
}

// EachRestorePoint calls fn for every selected set, numbered from 1, with
// the entry of the full backup a differential needs (nil for a full
// backup). It stops at the first error.
func EachRestorePoint(selected, inventory []catalog.SetInfo, fn func(n int, info catalog.SetInfo, base *naming.BackupEntry) error) error {
	for i, info := range selected {
		var base *naming.BackupEntry
		if info.Entry.IsDiff() {
			baseInfo, err := catalog.BaseOf(inventory, info.Entry)
			if err != nil {
				return err
			}
			base = &baseInfo.Entry
		}
		if err := fn(i+1, info, base); err != nil {
			return err
		}
	}
	return nil
}

// OpenRestorePoint opens the set entry of backupDir and, for a differential,
// its full backup base (nil for a full backup). closeAll closes both; parts
// is the number of part files read.
func OpenRestorePoint(backupDir string, entry naming.BackupEntry, base *naming.BackupEntry) (set, baseSet *container.Set, parts int, closeAll func(), err error) {
	set, err = catalog.OpenSet(backupDir, entry)
	if err != nil {
		return nil, nil, 0, nil, err
	}
	parts = len(set.Paths)
	if base != nil {
		baseSet, err = catalog.OpenSet(backupDir, *base)
		if err != nil {
			set.Close()
			return nil, nil, 0, nil, fmt.Errorf("Full backup %s: %w", base.String(), err)
		}
		parts += len(baseSet.Paths)
	}
	return set, baseSet, parts, func() {
		if baseSet != nil {
			baseSet.Close()
		}
		set.Close()
	}, nil
}
