package view

import (
	"strings"
	"testing"

	"github.com/phsc84/restoresafe/internal/config"
	"github.com/phsc84/restoresafe/internal/problem"
	"github.com/phsc84/restoresafe/internal/testutil/scenario"
	"github.com/phsc84/restoresafe/internal/workflow/health"
)

func settingsOf(t *testing.T, c scenario.Condition, reloadErr error, busy bool) SettingsPage {
	t.Helper()
	sc := scenario.Build(t, c)
	s := health.TakeSnapshot(health.Params{Config: sc.Config, ConfigPath: sc.ConfigPath, Now: sc.Now})
	return SettingsOf(sc.Config, sc.ConfigPath, s.BackupDir, &s, reloadErr, busy, "")
}

func value(rows []Setting, label string) string {
	for _, r := range rows {
		if r.Label == label {
			return r.Value
		}
	}
	return ""
}

func TestSettingsOfAProtectedSetup(t *testing.T) {
	t.Parallel()
	p := settingsOf(t, scenario.Protected, nil, false)
	if !p.Reload.Enabled || p.ConfigError != "" || len(p.Folders) != 2 || p.Folders[0].Status != "Found" {
		t.Fatalf("page %+v", p)
	}
	if p.BackupDirState.Tone != ToneSuccess || !strings.HasPrefix(p.BackupDirState.Value, "Reachable, ") {
		t.Fatalf("backup directory %+v", p.BackupDirState)
	}
	if v := value(p.FolderRows, "Unreadable files"); v != "Stop the backup of that folder" {
		t.Fatalf("unreadable %q", v)
	}
	if v := value(p.Differential.Rows, "New full backup after"); v != "30 days" {
		t.Fatalf("interval %q", v)
	}
	if v := value(p.Checks.Rows, "Reminder"); v != "After 7 days without a backup" {
		t.Fatalf("reminder %q", v)
	}
	if v := value(p.Keys.Rows, "Unlock with"); v != "Password only" || len(p.Keys.More) != 3 {
		t.Fatalf("keys %+v", p.Keys)
	}
	for _, card := range []SettingsCard{p.Differential, p.Retention, p.Checks, p.Keys, p.Logging} {
		for _, r := range card.Rows {
			if r.Key == "" {
				t.Fatalf("%s: %q needs its config.yaml key", card.Title, r.Label)
			}
		}
	}
	checkWriting(t, p)
}

func TestSettingsShowProblemsAndReload(t *testing.T) {
	t.Parallel()
	p := settingsOf(t, scenario.SourceMissing, nil, true)
	var pics FolderSetting
	for _, f := range p.Folders {
		if f.Name == "Pics" {
			pics = f
		}
	}
	if pics.Status != "Not found" || pics.Tone != ToneError {
		t.Fatalf("missing folder %+v", pics)
	}
	if p.Reload.Enabled || p.Reload.Reason == "" {
		t.Fatal("Reload waits for the running operation")
	}

	p = settingsOf(t, scenario.BackupDirUnreachable, problem.New("Config file is invalid: yaml: line 3: mapping values are not allowed").WithRemedyOnOwnLine("Check YAML syntax."), false)
	if p.BackupDirState.Value != "Not reachable" || p.ConfigError != "Config file is invalid: yaml: line 3: mapping values are not allowed. Check YAML syntax." {
		t.Fatalf("dir %+v, error %q", p.BackupDirState, p.ConfigError)
	}

	p = settingsOf(t, scenario.NewKeysNeeded, nil, false)
	if !strings.Contains(p.Keys.Note, "Your next backup creates new keys") {
		t.Fatalf("keys note %q", p.Keys.Note)
	}
}

func TestSettingsRetentionInWords(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		keep, diffs  int
		chains, rest string
	}{
		{0, 0, "All backups", ""},
		{3, 0, "3 chains per folder", "All of a chain"},
		{1, 5, "1 chain per folder", "5 per chain"},
	} {
		cfg := &config.Config{RetentionKeep: tc.keep, SourceDirectories: []string{`C:\Docs`}}
		cfg.Differential.RetentionKeepDifferentials = tc.diffs
		p := SettingsOf(cfg, `C:\config.yaml`, `D:\Backups`, nil, nil, false, "")
		if value(p.Retention.Rows, "Keep") != tc.chains || value(p.Retention.Rows, "Differentials") != tc.rest {
			t.Fatalf("%d/%d: %+v", tc.keep, tc.diffs, p.Retention.Rows)
		}
		if p.Folders[0].Status != "Checking…" {
			t.Fatalf("before the first check %+v", p.Folders)
		}
	}
}

func TestSettingsNameMissingKeys(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{SourceDirectories: []string{`C:\Docs`}}
	p := SettingsOf(cfg, `C:\config.yaml`, `D:\Backups`, nil, nil, false, "")
	if p.Missing != "" || p.AddMissing.Action != ActionNone {
		t.Fatalf("nothing missing: %q", p.Missing)
	}

	cfg.MissingKeys = []string{"reminder_days"}
	p = SettingsOf(cfg, `C:\config.yaml`, `D:\Backups`, nil, nil, false, "")
	if p.Missing != "reminder_days isn't in config.yaml, so its default applies." || !p.AddMissing.Enabled {
		t.Fatalf("one missing: %q %+v", p.Missing, p.AddMissing)
	}

	cfg.MissingKeys = []string{"reminder_days", "recovery_code", "differential"}
	p = SettingsOf(cfg, `C:\config.yaml`, `D:\Backups`, nil, nil, true, `C:\config.yaml.2026-10-04_153012.bak`)
	if p.Missing != "3 settings aren't in config.yaml, so their defaults apply: reminder_days, recovery_code, differential." {
		t.Fatalf("three missing: %q", p.Missing)
	}
	if p.AddMissing.Enabled || p.AddMissing.Reason == "" {
		t.Fatal("adding waits for the running operation")
	}
	if !strings.HasSuffix(p.Added, "saved as config.yaml.2026-10-04_153012.bak.") {
		t.Fatalf("added %q", p.Added)
	}
	checkWriting(t, p)
}

func TestSettingsSayKeepingAllIsTheDefault(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{SourceDirectories: []string{`C:\Docs`}}
	if v := value(SettingsOf(cfg, `C:\config.yaml`, `D:\Backups`, nil, nil, false, "").Retention.Rows, "Keep"); v != "All backups" {
		t.Fatalf("chosen in the file: %q", v)
	}
	cfg.MissingKeys = []string{"retention_keep"}
	if v := value(SettingsOf(cfg, `C:\config.yaml`, `D:\Backups`, nil, nil, false, "").Retention.Rows, "Keep"); v != "All backups (the default; 3 chains recommended)" {
		t.Fatalf("missing from the file: %q", v)
	}
	cfg.Differential.RetentionKeepDifferentials = 2
	if v := value(SettingsOf(cfg, `C:\config.yaml`, `D:\Backups`, nil, nil, false, "").Retention.Rows, "Keep"); v != "All chains (the default; 3 chains recommended)" {
		t.Fatalf("missing, with differentials limited: %q", v)
	}
}
