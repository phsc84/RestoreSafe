package view

import (
	"RestoreSafe/internal/testutil/scenario"
	"RestoreSafe/internal/workflow/health"
	"RestoreSafe/internal/workflow/interact"
	"reflect"
	"strings"
	"testing"
)

func overviewOf(t *testing.T, c scenario.Condition) (Overview, *health.Snapshot) {
	t.Helper()
	sc := scenario.Build(t, c)
	s := health.TakeSnapshot(health.Params{Config: sc.Config, ConfigPath: sc.ConfigPath, Now: sc.Now})
	return OverviewOf(&s, sc.Config, sc.Now), &s
}

// TestOverviewHeroOfEveryScenario covers spec OV-1 and figure 5.3.
func TestOverviewHeroOfEveryScenario(t *testing.T) {
	t.Parallel()
	cases := []struct {
		condition scenario.Condition
		tone      Tone
		title     string // a part of the title
		primary   Action
		secondary Action
	}{
		{scenario.Protected, ToneSuccess, "Your folders are protected", ActionBackUp, ActionNone},
		{scenario.Empty, ToneNeutral, "Create your first backup", ActionBackUp, ActionNone},
		{scenario.Overdue, ToneWarning, "days old", ActionBackUp, ActionNone},
		{scenario.SkippedFiles, ToneWarning, "2 files in Docs weren't backed up", ActionBackUp, ActionNone},
		{scenario.BaseMissing, ToneError, "A backup of Docs can't be restored", ActionShowInBackups, ActionNone},
		{scenario.SourceMissing, ToneError, "can't be found", ActionCheckAgain, ActionEditConfig},
		{scenario.BackupDirUnreachable, ToneError, "isn't reachable", ActionCheckAgain, ActionNone},
		{scenario.IncompleteNewest, ToneWarning, "unfinished backup of Docs", ActionShowInBackups, ActionNone},
		{scenario.VerifyFailed, ToneError, "A backup of Docs is damaged", ActionBackUp, ActionNone},
		{scenario.Argon2Capped, ToneWarning, "capped", ActionEditConfig, ActionNone},
		{scenario.NewKeysNeeded, ToneSuccess, "Your folders are protected", ActionBackUp, ActionNone},
		{scenario.Legacy1x, ToneSuccess, "Your folders are protected", ActionBackUp, ActionNone},
		{scenario.LeftoverTmp, ToneSuccess, "Your folders are protected", ActionBackUp, ActionNone},
		{scenario.FolderNotBackedUp, ToneWarning, "Music has no backup yet", ActionBackUp, ActionNone},
	}
	if len(cases) != len(scenario.All) {
		t.Fatalf("%d cases for %d scenarios", len(cases), len(scenario.All))
	}
	for _, tc := range cases {
		t.Run(string(tc.condition), func(t *testing.T) {
			t.Parallel()
			o, _ := overviewOf(t, tc.condition)
			h := o.Hero
			if h.Tone != tc.tone || !strings.Contains(h.Title, tc.title) || h.Primary.Action != tc.primary {
				t.Fatalf("hero %+v, want tone %d, title with %q, primary %d", h, tc.tone, tc.title, tc.primary)
			}
			if (h.Secondary == nil) != (tc.secondary == ActionNone) || (h.Secondary != nil && h.Secondary.Action != tc.secondary) {
				t.Fatalf("secondary %+v, want %d", h.Secondary, tc.secondary)
			}
			if h.Line == "" && tc.condition != scenario.Empty {
				t.Fatalf("the hero needs its facts: %+v", h)
			}
			checkWriting(t, o)
		})
	}
}

func TestOverviewCardsWhenProtected(t *testing.T) {
	t.Parallel()
	o, s := overviewOf(t, scenario.Protected)

	if o.Title != "Create backup" || o.Folders.Title != "Folders to back up" || len(o.Folders.Rows) != 2 || o.Folders.Note != "" {
		t.Fatalf("folders: %+v", o.Folders)
	}
	for _, row := range o.Folders.Rows {
		if !strings.HasPrefix(row.Date, "today, ") || row.DateTip == "" || row.Next != "DIFF" || row.NextReason == "" || row.Problem != "" {
			t.Fatalf("folder row: %+v", row)
		}
	}

	st := o.Storage
	if st.Path != s.BackupDir || !strings.Contains(st.Used, " of ") || len(st.Segments) != 3 || !strings.HasPrefix(st.Estimate, "A full backup of all folders needs about ") {
		t.Fatalf("storage: %+v", st)
	}
	sum := 0.0
	for _, seg := range st.Segments {
		sum += seg.Fraction
	}
	if sum < 0.99 || sum > 1.01 {
		t.Fatalf("the segments must fill the bar: %.3f", sum)
	}

	k := o.Keys
	if k.Methods != "Password only" || !strings.HasPrefix(k.Details, "Created ") || k.Note != "" {
		t.Fatalf("keys: %+v", k)
	}
}

func TestOverviewCardsShowProblems(t *testing.T) {
	t.Parallel()
	o, _ := overviewOf(t, scenario.SourceMissing)
	if row := o.Folders.Rows[1]; row.Name != "Pics" || row.Problem != "Can't be found" || row.Tone != ToneError || row.Date != "" {
		t.Fatalf("missing folder row: %+v", row)
	}

	o, _ = overviewOf(t, scenario.BaseMissing)
	diff := o.Folders.Rows[0]
	if diff.Date == "" || diff.Next != "FULL" {
		t.Fatalf("a folder whose newest backup is a differential: %+v", diff)
	}

	o, _ = overviewOf(t, scenario.Empty)
	if o.Folders.Rows[0].Date != "no backup yet" || o.Folders.Rows[0].Tone != ToneWarning || o.Folders.Note != "" || o.Keys.Note != "Your first backup creates your keys." {
		t.Fatalf("empty: %+v %+v", o.Folders, o.Keys)
	}
	if o.Hero.Line != "" {
		t.Fatalf("empty hero: %+v", o.Hero)
	}

	o, _ = overviewOf(t, scenario.NewKeysNeeded)
	if !strings.HasPrefix(o.Keys.Note, "Your next backup creates new keys: ") || o.Keys.Tone != ToneInfo {
		t.Fatalf("new keys: %+v", o.Keys)
	}

	o, _ = overviewOf(t, scenario.FolderNotBackedUp)
	if row := o.Folders.Rows[2]; row.Date != "no backup yet" || row.Tone != ToneWarning || row.Next != "FULL" {
		t.Fatalf("a new folder: %+v", row)
	}

	o, _ = overviewOf(t, scenario.BackupDirUnreachable)
	if o.Storage.Used != "Free space unknown" || o.Storage.Segments != nil {
		t.Fatalf("unreachable: %+v", o.Storage)
	}
	if o.Hero.Primary.Action != ActionCheckAgain {
		t.Fatalf("Check again fixes it: %+v", o.Hero)
	}
}

func TestOverviewWhileChecking(t *testing.T) {
	t.Parallel()
	o := OverviewOf(nil, nil, scenario.Build(t, scenario.Empty).Now)
	if o.Hero.Title != "Checking your backups…" || o.Hero.Primary.Enabled {
		t.Fatalf("before the first snapshot: %+v", o.Hero)
	}
}

// TestEveryProblemHasWords keeps raw codes out of the hero.
func TestEveryProblemHasWords(t *testing.T) {
	t.Parallel()
	sc := scenario.Build(t, scenario.Protected)
	s := health.TakeSnapshot(health.Params{Config: sc.Config, ConfigPath: sc.ConfigPath, Now: sc.Now})
	for _, code := range []interact.Code{
		interact.CodeConfigInvalid, interact.CodeBackupDirUnreachable, interact.CodeBackupDirNotWritable,
		interact.CodeSourceMissing, interact.CodeSourceInvalid, interact.CodeBaseMissing, interact.CodeSetIncomplete,
		interact.CodeVerifyFailed, interact.CodeOverdue, interact.CodeFolderNotBackedUp, interact.CodeSkippedFiles,
		interact.CodeIncompleteNewest, interact.CodeSpaceLow, interact.CodeArgon2Capped,
	} {
		h := problemHero(health.Problem{Code: code, Folder: "Docs", Path: "C:/Docs", Count: 2}, &s, sc.Config, sc.Now, Button{Action: ActionBackUp})
		if h.Title == string(code) || h.Title == "" || h.Line == "" || h.Primary.Action == ActionNone {
			t.Fatalf("%s has no words: %+v", code, h)
		}
	}
}

// checkWriting checks the writing rules of spec 3.6 on every text of v.
func checkWriting(t *testing.T, v any) {
	t.Helper()
	var walk func(reflect.Value)
	walk = func(r reflect.Value) {
		switch r.Kind() {
		case reflect.String:
			s := r.String()
			lower := strings.ToLower(s)
			for _, bad := range []string{"!", "successfully", "please", "remedy:", "error:", "%!"} {
				if strings.Contains(lower, bad) {
					t.Errorf("text %q contains %q", s, bad)
				}
			}
		case reflect.Ptr, reflect.Interface:
			if !r.IsNil() {
				walk(r.Elem())
			}
		case reflect.Struct:
			for i := range r.NumField() {
				// Detail is the workflow's message for "Show details", as written.
				if f := r.Type().Field(i); f.IsExported() && f.Name != "Detail" {
					walk(r.Field(i))
				}
			}
		case reflect.Slice:
			for i := range r.Len() {
				walk(r.Index(i))
			}
		}
	}
	walk(reflect.ValueOf(v))
}

func TestOverviewKeysShowTheYubiKey(t *testing.T) {
	t.Parallel()
	for _, c := range []scenario.Condition{scenario.Protected, scenario.Empty} {
		sc := scenario.Build(t, c)
		s := health.TakeSnapshot(health.Params{Config: sc.Config, ConfigPath: sc.ConfigPath, Now: sc.Now})
		connected := false
		s.Keys.YubiKeyConnected = &connected
		if k := OverviewOf(&s, sc.Config, sc.Now).Keys; k.YubiKey != "YubiKey not connected" || k.YubiKeyTone != ToneInfo {
			t.Fatalf("%s: keys %+v", c, k)
		}
		connected = true
		if k := OverviewOf(&s, sc.Config, sc.Now).Keys; k.YubiKey != "YubiKey connected" {
			t.Fatalf("%s: keys %+v", c, k)
		}
		s.Keys.YubiKeyConnected = nil
		if k := OverviewOf(&s, sc.Config, sc.Now).Keys; k.YubiKey != "" {
			t.Fatalf("%s: without a YubiKey, no line: %+v", c, k)
		}
	}
}
