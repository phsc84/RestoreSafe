package gui

import (
	"RestoreSafe/internal/format/catalog"
	"RestoreSafe/internal/format/naming"
	"fmt"
)

// selectionNode is a node of the backup selection tree: a backup run, or one
// backup set of it.
type selectionNode struct {
	run   int // index in the runs
	entry int // index in the run's entries; -1 for the whole run
}

// runNodeLabel is the tree label of a backup run.
func runNodeLabel(run catalog.BackupRunSummary) string {
	sets := "backup sets"
	if len(run.Entries) == 1 {
		sets = "backup set"
	}
	return fmt.Sprintf("%s    %s    (%d %s)", run.RunID, run.Created.Local().Format("2006-01-02 15:04"), len(run.Entries), sets)
}

// setNodeLabel is the tree label of a backup set.
func setNodeLabel(e naming.BackupEntry) string {
	if e.IsDiff() {
		return fmt.Sprintf("%s    differential %03d of chain %s", e.DirectoryName, e.DiffNumber, e.ChainID)
	}
	return fmt.Sprintf("%s    full backup", e.DirectoryName)
}

// selectionEntries returns the backup sets a node selects.
func selectionEntries(runs []catalog.BackupRunSummary, n selectionNode) []naming.BackupEntry {
	if n.run < 0 || n.run >= len(runs) {
		return nil
	}
	run := runs[n.run]
	if n.entry < 0 {
		return run.Entries
	}
	if n.entry >= len(run.Entries) {
		return nil
	}
	return []naming.BackupEntry{run.Entries[n.entry]}
}

// selectionText explains what choosing a node does (action is "restore" or
// "verify"), including the full backup a differential needs.
func selectionText(action string, runs []catalog.BackupRunSummary, n selectionNode) string {
	entries := selectionEntries(runs, n)
	verb := "Restores"
	if action == "verify" {
		verb = "Verifies"
	}
	switch {
	case len(entries) == 0:
		return "Choose a backup run or a single backup set."
	case n.entry < 0:
		run := runs[n.run]
		if len(entries) == 1 {
			return fmt.Sprintf("%s the backup set of run %s: %s.", verb, run.RunID, entries[0].String())
		}
		return fmt.Sprintf("%s all %d backup sets of run %s.", verb, len(entries), run.RunID)
	}
	e := entries[0]
	if e.IsDiff() {
		return fmt.Sprintf("%s only %s. It needs the full backup of chain %s, which is read too.", verb, e.String(), e.ChainID)
	}
	return fmt.Sprintf("%s only %s.", verb, e.String())
}
