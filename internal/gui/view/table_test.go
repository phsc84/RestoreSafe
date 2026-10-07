package view

import (
	"RestoreSafe/internal/config"
	"RestoreSafe/internal/testutil/scenario"
	"strings"
	"testing"
)

// checkTable checks that every row has one cell per column and exactly one
// column fills.
func checkTable(t *testing.T, tb Table, titles ...string) {
	t.Helper()
	if len(tb.Columns) != len(titles) {
		t.Fatalf("columns %+v, want %q", tb.Columns, titles)
	}
	fills := 0
	for i, c := range tb.Columns {
		if c.Title != titles[i] {
			t.Fatalf("column %d is %q, want %q", i, c.Title, titles[i])
		}
		if c.Fill {
			fills++
		}
	}
	if fills != 1 || tb.Name == "" {
		t.Fatalf("%d fill columns, name %q", fills, tb.Name)
	}
	for _, r := range tb.Rows {
		if len(r.Cells) != len(tb.Columns) {
			t.Fatalf("row %+v has %d cells for %d columns", r, len(r.Cells), len(tb.Columns))
		}
	}
}

// OV-3, BR-4
func TestFoldersTable(t *testing.T) {
	t.Parallel()
	o, _ := createPageOf(t, scenario.SourceMissing)
	tb := o.Folders.Table(nil)
	checkTable(t, tb, "Folder", "Last backup", "Next backup")
	docs, pics := tb.Rows[0], tb.Rows[1]
	if docs.Cells[2].Text != "DIFF" || !strings.Contains(docs.Tip, "Next backup DIFF: ") || !strings.HasPrefix(docs.Tip, o.Folders.Rows[0].Path) {
		t.Fatalf("folder row %+v", docs)
	}
	if pics.Cells[1].Text != "Can't be found" || pics.Cells[1].Tone != ToneError || pics.Cells[2].Text != "" {
		t.Fatalf("missing folder row %+v", pics)
	}

	running := o.Folders.Table(map[string]FolderProgress{"Docs": {Text: "Backing up, 29%", Badge: Badge{Kind: BadgeDiff, Text: "DIFF 2"}}})
	checkTable(t, running, "Folder", "Type", "Status")
	if r := running.Rows[0]; r.Cells[1].Badge == nil || r.Cells[1].Badge.Text != "DIFF 2" || r.Cells[2].Text != "Backing up, 29%" {
		t.Fatalf("running row %+v", r)
	}
	if r := running.Rows[1]; r.Cells[1].Badge != nil || r.Cells[2].Text != "Can't be found" {
		t.Fatalf("a folder the run leaves out %+v", r)
	}
	if running.Signature() == tb.Signature() {
		t.Fatal("the running table has other columns")
	}
}

// BP-2
func TestBackupPlanTable(t *testing.T) {
	t.Parallel()
	v := BackupPlanOf(samplePlan(), nil, &config.Config{}, planNow)
	tb := v.Table()
	checkTable(t, tb, "Folder", "Type", "Why", "About")
	if !tb.Columns[3].Right {
		t.Fatal("sizes are right-aligned")
	}
	docs, old := tb.Rows[0], tb.Rows[2]
	if docs.Cells[1].Badge == nil || docs.Cells[1].Badge.Text != "DIFF 4" || docs.Cells[2].Tone != ToneSecondary || docs.Cells[3].Text != "200 MB" {
		t.Fatalf("differential row %+v", docs)
	}
	if old.Cells[1].Badge != nil || old.Cells[2].Tone != ToneError || !strings.Contains(old.Tip, "Connect the drive") {
		t.Fatalf("problem row %+v", old)
	}
}

func TestSettingsFoldersTable(t *testing.T) {
	t.Parallel()
	p := settingsOf(t, scenario.SourceMissing, nil, false)
	tb := p.FoldersTable()
	checkTable(t, tb, "Folder", "Path", "Status")
	if len(tb.Rows) != len(p.Folders) || tb.Rows[0].Cells[1].Text != p.Folders[0].Path || tb.Rows[0].Tip != p.Folders[0].Path {
		t.Fatalf("rows %+v", tb.Rows)
	}
}
