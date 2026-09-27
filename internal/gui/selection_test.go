package gui

import (
	"RestoreSafe/internal/format/catalog"
	"RestoreSafe/internal/format/naming"
	"strings"
	"testing"
	"time"
)

func selectionRuns() []catalog.BackupRunSummary {
	return []catalog.BackupRunSummary{
		{RunID: "RUN002", Created: time.Date(2026, 9, 20, 21, 0, 0, 0, time.Local), Entries: []naming.BackupEntry{
			{DirectoryName: "Docs", ChainID: "RUN001", Date: "2026-09-20", DiffNumber: 2},
			{DirectoryName: "Pics", ChainID: "RUN002", Date: "2026-09-20"},
		}},
		{RunID: "RUN001", Created: time.Date(2026, 9, 1, 8, 30, 0, 0, time.Local), Entries: []naming.BackupEntry{
			{DirectoryName: "Docs", ChainID: "RUN001", Date: "2026-09-01"},
		}},
	}
}

func TestSelectionLabels(t *testing.T) {
	t.Parallel()
	runs := selectionRuns()
	if got := runNodeLabel(runs[0]); got != "RUN002    2026-09-20 21:00    (2 backup sets)" {
		t.Errorf("run label %q", got)
	}
	if got := runNodeLabel(runs[1]); !strings.HasSuffix(got, "(1 backup set)") {
		t.Errorf("run label %q", got)
	}
	if got := setNodeLabel(runs[0].Entries[0]); got != "Docs    differential 002 of chain RUN001" {
		t.Errorf("differential label %q", got)
	}
	if got := setNodeLabel(runs[0].Entries[1]); got != "Pics    full backup" {
		t.Errorf("full label %q", got)
	}
}

func TestSelectionEntriesAndText(t *testing.T) {
	t.Parallel()
	runs := selectionRuns()
	cases := []struct {
		node    selectionNode
		action  string
		entries int
		text    string
	}{
		{selectionNode{0, -1}, "restore", 2, "Restores all 2 backup sets of run RUN002."},
		{selectionNode{1, -1}, "verify", 1, "Verifies the backup set of run RUN001: Docs_RUN001_2026-09-01_FULL."},
		{selectionNode{0, 0}, "restore", 1, "Restores only Docs_RUN001_2026-09-20_DIFF002. It needs the full backup of chain RUN001, which is read too."},
		{selectionNode{0, 1}, "verify", 1, "Verifies only Pics_RUN002_2026-09-20_FULL."},
		{selectionNode{5, -1}, "restore", 0, "Choose a backup run or a single backup set."},
		{selectionNode{0, 9}, "restore", 0, "Choose a backup run or a single backup set."},
	}
	for _, c := range cases {
		if got := len(selectionEntries(runs, c.node)); got != c.entries {
			t.Errorf("%+v: %d entries, want %d", c.node, got, c.entries)
		}
		if got := selectionText(c.action, runs, c.node); got != c.text {
			t.Errorf("%+v: text %q, want %q", c.node, got, c.text)
		}
	}
}
