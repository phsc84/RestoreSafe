package plan

import (
	"RestoreSafe/internal/config"
	"RestoreSafe/internal/format/catalog"
	"RestoreSafe/internal/format/container"
	"RestoreSafe/internal/format/naming"
	"strings"
	"testing"
	"time"
)

var planNow = time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)

func planInfo(chain string, diff int, created time.Time, dataLen int64, keySetID string) catalog.SetInfo {
	return catalog.SetInfo{
		Entry:   naming.BackupEntry{DirectoryName: "Docs", ChainID: naming.BackupID(chain), Date: created.Format("2006-01-02"), DiffNumber: diff},
		Header:  &container.Header{CreatedUTC: created.Format(time.RFC3339Nano), KeySet: container.KeySet{ID: keySetID}},
		Trailer: container.Trailer{DataLength: dataLen},
	}
}

func planFor(cfg *config.Config, infos []catalog.SetInfo, keys Keys, force bool) *Folder {
	sources := []Source{{Resolved: "C:/src/Docs", BackupName: "Docs"}}
	return Folders(cfg, infos, sources, keys, force, planNow)["Docs"]
}

func TestFolderRules(t *testing.T) {
	t.Parallel()

	keys := Keys{Existing: &container.KeySet{ID: "current"}}
	cfg := &config.Config{}
	day := 24 * time.Hour
	full := planInfo("ABC123", 0, planNow.Add(-10*day), 1000, "current")

	disabled := false
	cases := []struct {
		name   string
		cfg    *config.Config
		infos  []catalog.SetInfo
		keys   Keys
		force  bool
		diff   int
		reason string
	}{
		{"first differential", cfg, []catalog.SetInfo{full}, keys, false, 1, "base: full"},
		{"next number after existing diffs", cfg, []catalog.SetInfo{planInfo("ABC123", 4, planNow.Add(-day), 100, "current"), full}, keys, false, 5, "base: full"},
		{"incomplete diff still counts", cfg, []catalog.SetInfo{{Entry: naming.BackupEntry{DirectoryName: "Docs", ChainID: "ABC123", DiffNumber: 7}, Err: &container.ErrIncomplete{}}, full}, keys, false, 8, "base: full"},
		{"forced full", cfg, []catalog.SetInfo{full}, keys, true, 0, "full backup requested"},
		{"new keys", cfg, []catalog.SetInfo{full}, Keys{NewKeysReason: "x"}, false, 0, "new keys"},
		{"disabled", &config.Config{Differential: config.Differential{Enabled: &disabled}}, []catalog.SetInfo{full}, keys, false, 0, "disabled"},
		{"no full", cfg, nil, keys, false, 0, "no complete full backup"},
		{"older keys", cfg, []catalog.SetInfo{planInfo("ABC123", 0, planNow.Add(-day), 1000, "old")}, keys, false, 0, "older keys"},
		{"too old", cfg, []catalog.SetInfo{planInfo("ABC123", 0, planNow.Add(-30*day), 1000, "current")}, keys, false, 0, "30 days old (limit 30)"},
		{"custom interval", &config.Config{Differential: config.Differential{FullBackupIntervalDays: 7}}, []catalog.SetInfo{full}, keys, false, 0, "10 days old (limit 7)"},
		{"diff too large", cfg, []catalog.SetInfo{planInfo("ABC123", 1, planNow.Add(-day), 500, "current"), full}, keys, false, 0, "50% of the full backup (limit 50%)"},
		{"diff below limit", cfg, []catalog.SetInfo{planInfo("ABC123", 1, planNow.Add(-day), 499, "current"), full}, keys, false, 2, "base: full"},
		{"999 differentials", cfg, []catalog.SetInfo{planInfo("ABC123", 999, planNow.Add(-day), 1, "current"), full}, keys, false, 0, "maximum of 999"},
	}
	for _, tc := range cases {
		p := planFor(tc.cfg, tc.infos, tc.keys, tc.force)
		if p.DiffNumber != tc.diff || p.IsDiff() != (tc.diff > 0) || !strings.Contains(p.Reason, tc.reason) {
			t.Fatalf("%s: got diff=%d reason=%q, want diff=%d reason containing %q", tc.name, p.DiffNumber, p.Reason, tc.diff, tc.reason)
		}
	}
}

func TestFoldersUseNewestCompleteFullAsBase(t *testing.T) {
	t.Parallel()

	keys := Keys{Existing: &container.KeySet{ID: "current"}}
	day := 24 * time.Hour
	newest := planInfo("NEW002", 0, planNow.Add(-2*day), 1000, "current")
	older := planInfo("OLD001", 0, planNow.Add(-5*day), 1000, "current")
	broken := catalog.SetInfo{Entry: naming.BackupEntry{DirectoryName: "Docs", ChainID: "BRK003"}, Err: &container.ErrIncomplete{}}

	p := planFor(&config.Config{}, []catalog.SetInfo{broken, newest, older}, keys, false)
	if !p.IsDiff() || p.Base.Entry.ChainID != "NEW002" || p.DiffNumber != 1 {
		t.Fatalf("expected differential 1 of NEW002, got %+v", p)
	}
	if !AnyDifferential(map[string]*Folder{"Docs": p}) {
		t.Fatal("AnyDifferential must see the differential")
	}
}
