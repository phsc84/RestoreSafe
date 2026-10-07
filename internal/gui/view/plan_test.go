package view

import (
	"RestoreSafe/internal/config"
	"RestoreSafe/internal/format/catalog"
	"RestoreSafe/internal/format/container"
	"RestoreSafe/internal/format/naming"
	"RestoreSafe/internal/problem"
	"RestoreSafe/internal/testutil/scenario"
	"RestoreSafe/internal/workflow/backup"
	"RestoreSafe/internal/workflow/interact"
	"RestoreSafe/internal/workflow/interact/interacttest"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

var planNow = time.Date(2026, 9, 30, 9, 0, 0, 0, time.Local)

func samplePlan() interact.BackupPlan {
	return interact.BackupPlan{
		BackupDir: `\\NAS\Backup`,
		Folders: []interact.FolderPlan{
			{Name: "Documents", Path: `C:\Docs`, Differential: true, DiffNumber: 4, BaseCreated: time.Date(2026, 9, 1, 20, 0, 0, 0, time.Local), EstimatedBytes: 200 << 20, AllBytes: 9 << 30},
			{Name: "Pictures", Path: `D:\Pics`, Reason: "last differential was 57% of the full backup (limit 50%)", EstimatedBytes: 54 << 30, AllBytes: 54 << 30},
			{Name: "Old", Path: `E:\Old`, Problem: "Source directory does not exist. Remedy: Connect the drive."},
			{Name: "Docs2", Path: `C:\Docs`, Skipped: true},
		},
		NeededBytes: 55 << 30, AllBytes: 63 << 30, FreeBytes: 370 << 30,
		Keys:        interact.KeyPlan{Created: time.Date(2026, 9, 1, 20, 0, 0, 0, time.Local), Password: true, YubiKeys: 1},
		VerifyAfter: true,
	}
}

func TestBackupPlanRowsAndLines(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{RetentionKeep: 3}
	v := BackupPlanOf(samplePlan(), nil, cfg, planNow)

	if v.Heading != `Back up 2 folders to \\NAS\Backup` || v.KeysNote != "" {
		t.Fatalf("heading %q, keys note %q", v.Heading, v.KeysNote)
	}
	docs, pics, old, dup := v.Rows[0], v.Rows[1], v.Rows[2], v.Rows[3]
	if docs.Badge == nil || docs.Badge.Text != "DIFF 4" || docs.Why != "Based on the full backup of 1 Sep" || docs.About != "200 MB" {
		t.Fatalf("differential row %+v", docs)
	}
	if pics.Badge == nil || pics.Badge.Text != "FULL" || !strings.HasPrefix(pics.Why, "Last differential was 57%") || pics.About != "54 GB" {
		t.Fatalf("full row %+v", pics)
	}
	if old.Badge != nil || old.Tone != ToneError || old.Why != "Source directory does not exist. Connect the drive." {
		t.Fatalf("problem row %+v", old)
	}
	if dup.Badge != nil || dup.Tone != ToneSecondary {
		t.Fatalf("duplicate row %+v", dup)
	}
	if v.Space.Text != "About 55 GB needed, up to 63 GB · 370 GB free" || v.Space.Tone != ToneSuccess {
		t.Fatalf("space %+v", v.Space)
	}
	if v.Unlock.Text != "One YubiKey touch, then your password" {
		t.Fatalf("unlock %q", v.Unlock.Text)
	}
	if v.Afterwards.Text != "Verify each new backup. Nothing is removed (keeps 3 chains per folder)." || v.RemovesLink != "" {
		t.Fatalf("afterwards %q", v.Afterwards.Text)
	}
	if v.Note == "" {
		t.Fatal("a differential needs the estimate note")
	}
	if v.Start == nil || v.Start.Enabled || v.Full != nil || v.NewKeys != nil || !v.Cancel.Enabled {
		t.Fatalf("before the question, Start waits: %+v %+v %+v", v.Start, v.Full, v.NewKeys)
	}
	checkWriting(t, v)
}

func TestBackupPlanButtons(t *testing.T) {
	t.Parallel()
	p := samplePlan()
	v := BackupPlanOf(p, &interact.BackupStartOptions{OfferFull: true, OfferNewKeys: true}, nil, planNow)
	if v.Start == nil || !v.Start.Enabled || v.Full == nil || v.Full.Action != ActionFullBackup || v.NewKeys == nil {
		t.Fatalf("automatic plan: %+v %+v %+v", v.Start, v.Full, v.NewKeys)
	}
	v = BackupPlanOf(p, &interact.BackupStartOptions{OfferNewKeys: true, OfferAutomatic: true}, nil, planNow)
	if v.Full == nil || v.Full.Action != ActionAutomaticPlan || v.Full.Text != "&Back to plan" {
		t.Fatalf("after Full backup instead: %+v", v.Full)
	}

	p.Issues = []interact.Issue{
		{Status: interact.StatusError, Code: interact.CodeSpaceInsufficient, Text: "Not enough free space.", Remedy: "Free up space."},
		{Status: interact.StatusWarn, Text: "A warning."},
	}
	v = BackupPlanOf(p, &interact.BackupStartOptions{Blocked: true, OfferFull: true}, nil, planNow)
	if v.Start != nil || v.Full != nil {
		t.Fatalf("a blocked plan has no Start: %+v %+v", v.Start, v.Full)
	}
	if len(v.Issues) != 2 || v.Issues[0].Tone != ToneError || v.Issues[0].Text != "Not enough free space. Free up space." || v.Issues[1].Tone != ToneWarning {
		t.Fatalf("issues %+v", v.Issues)
	}
	if v.Space.Tone != ToneError {
		t.Fatalf("space %+v", v.Space)
	}
}

func TestBackupPlanNewKeysAndRemovals(t *testing.T) {
	t.Parallel()
	p := samplePlan()
	p.Keys = interact.KeyPlan{New: true, NewKeysReason: "Recovery_code enabled in config.yaml", Password: true, YubiKeys: 2, RecoveryCode: true}
	p.VerifyAfter = false
	set := func(dir, chain string, diff int, created time.Time, size int64) catalog.SetInfo {
		e := naming.BackupEntry{DirectoryName: dir, ChainID: naming.BackupID(chain), Date: created.Format("2006-01-02"), DiffNumber: diff}
		info := catalog.SetInfo{Entry: e, SizeBytes: size, Header: &container.Header{CreatedUTC: created.UTC().Format(time.RFC3339)}}
		return info
	}
	jul := time.Date(2026, 7, 6, 20, 0, 0, 0, time.Local)
	p.Removes = []catalog.SetInfo{
		set("Pictures", "AAA001", 0, jul, 30<<30), set("Pictures", "AAA001", 1, jul, 6<<30), set("Pictures", "AAA001", 2, jul, 5<<30),
		set("Documents", "BBB002", 3, jul, 1<<30),
	}
	v := BackupPlanOf(p, nil, nil, planNow)
	if v.KeysNote != "New keys will be created: recovery_code enabled in config.yaml. Every folder gets a full backup." {
		t.Fatalf("keys note %q", v.KeysNote)
	}
	if v.Unlock.Text != "New password, register 2 YubiKeys (4 prompts), store a recovery code" {
		t.Fatalf("unlock %q", v.Unlock.Text)
	}
	if v.Afterwards.Text != "Remove 4 old backups (42 GB)." || v.RemovesLink == "" {
		t.Fatalf("afterwards %q", v.Afterwards.Text)
	}
	want := []string{"Pictures: full backup of 6 Jul and 2 differentials, 41 GB", "Documents: 1 differential, 1.0 GB"}
	if strings.Join(v.Removes, "|") != strings.Join(want, "|") {
		t.Fatalf("removes %q, want %q", v.Removes, want)
	}
	checkWriting(t, v)
}

func TestNewKeysConfirmNamesTheConfiguredLocks(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{AuthenticationMode: config.AuthModePasswordYubiKey, RecoveryCode: true}
	c := NewKeysConfirm(cfg)
	if !strings.Contains(c.Content, "locked with a new password, a new YubiKey registration and a new recovery code.") || c.Yes != "Create keys" {
		t.Fatalf("confirm %+v", c)
	}
	c = NewKeysConfirm(&config.Config{AuthenticationMode: config.AuthModePassword})
	if !strings.Contains(c.Content, "locked with a new password.") {
		t.Fatalf("password only: %q", c.Content)
	}
}

// planCapture records the backup plans the workflow shows.
type planCapture struct {
	*interacttest.Script
	plans []interact.BackupPlan
}

func (c *planCapture) ShowBackupPlan(p interact.BackupPlan) {
	c.plans = append(c.plans, p)
	c.Script.ShowBackupPlan(p)
}

// TestBackupPlanOfARealPlan words the plan the workflow builds for the
// scenarios, so the view reads what the workflow fills in.
func TestBackupPlanOfARealPlan(t *testing.T) {
	t.Parallel()
	for _, c := range []scenario.Condition{scenario.Protected, scenario.Empty, scenario.SourceMissing, scenario.NewKeysNeeded} {
		t.Run(string(c), func(t *testing.T) {
			t.Parallel()
			sc := scenario.Build(t, c)
			ui := &planCapture{Script: &interacttest.Script{ReadLine: interacttest.Answers("n")}}
			backup.Run(context.Background(), ui, sc.Config, "") //nolint:errcheck
			if len(ui.plans) == 0 {
				t.Fatal("no plan shown")
			}
			v := BackupPlanOf(ui.plans[0], &interact.BackupStartOptions{}, sc.Config, sc.Now)
			if len(v.Rows) == 0 || v.Unlock.Text == "" || v.Space.Text == "" || v.Afterwards.Text == "" {
				t.Fatalf("incomplete plan view: %+v", v)
			}
			for _, row := range v.Rows {
				if row.Badge == nil && row.Why == "" {
					t.Fatalf("row without type or reason: %+v", row)
				}
			}
			if (c == scenario.Empty || c == scenario.NewKeysNeeded) != (v.KeysNote != "") {
				t.Fatalf("keys note %q", v.KeysNote)
			}
			if c == scenario.SourceMissing && v.Start != nil {
				t.Fatal("a missing folder blocks the backup")
			}
			checkWriting(t, v)
		})
	}
}

func TestIssueTextEndsTheMessageBeforeTheRemedy(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ in, issue, first string }{
		{"Not enough space. Remedy: Free up space.", "Not enough space. Free up space.", "Not enough space."},
		{"yaml: line 3: bad escape Remedy: Check YAML syntax.", "yaml: line 3: bad escape. Check YAML syntax.", "yaml: line 3: bad escape."},
		{"No remedy here", "No remedy here", "No remedy here"},
	} {
		if got := errorText(errors.New(tc.in)); got != tc.issue {
			t.Errorf("errorText(%q) = %q, want %q", tc.in, got, tc.issue)
		}
		if got := firstSentences(problem.Split(errors.New(tc.in))); got != tc.first {
			t.Errorf("firstSentences(%q) = %q, want %q", tc.in, got, tc.first)
		}
	}
}

func TestSetDayWithoutHeader(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 7, 9, 0, 0, 0, time.Local)
	// An incomplete set has no header: its name gives the day, not 1 Jan 0001.
	info := catalog.SetInfo{Entry: naming.BackupEntry{DirectoryName: "Docs", ChainID: "ABC123", Date: "2026-10-06"}}
	if got := setDay(info, now); got != ShortDay(time.Date(2026, 10, 6, 0, 0, 0, 0, time.Local), now) {
		t.Fatalf("day %q", got)
	}
}
