package plan

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/phsc84/restoresafe/internal/config"
	"github.com/phsc84/restoresafe/internal/format/catalog"
	"github.com/phsc84/restoresafe/internal/format/container"
	"github.com/phsc84/restoresafe/internal/format/naming"
)

var docs = []Source{{Resolved: "C:/src/Docs", BackupName: "Docs"}}

// names returns the set names of infos, e.g. "Docs_AAA001_2026-09-16_DIFF001".
func names(infos []catalog.SetInfo) []string {
	out := make([]string, 0, len(infos))
	for _, info := range infos {
		out = append(out, info.Entry.String())
	}
	slices.Sort(out)
	return out
}

// chainsOf returns n chains of Docs, the oldest first, each a full backup
// plus diffs differentials, created one day apart before planNow.
func chainsOf(n, diffs int) []catalog.SetInfo {
	var infos []catalog.SetInfo
	ids := []string{"AAA001", "BBB002", "CCC003", "DDD004"}
	for i := range n {
		created := planNow.Add(-time.Duration(10*(n-i)) * 24 * time.Hour)
		infos = append(infos, planInfo(ids[i], 0, created, 1000, "current"))
		for d := 1; d <= diffs; d++ {
			infos = append(infos, planInfo(ids[i], d, created.Add(time.Duration(d)*time.Hour), 100, "current"))
		}
	}
	slices.Reverse(infos) // newest first, as catalog.Inventory returns them
	return infos
}

func TestRetentionKeepsNewestChainsAndDifferentials(t *testing.T) {
	t.Parallel()
	infos := chainsOf(3, 2)

	removed, err := Retention(docs, infos, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"Docs_AAA001_" + infos[8].Entry.Date + "_FULL",
		"Docs_AAA001_" + infos[7].Entry.Date + "_DIFF001",
		"Docs_AAA001_" + infos[6].Entry.Date + "_DIFF002",
		"Docs_BBB002_" + infos[5].Entry.Date + "_DIFF001",
		"Docs_CCC003_" + infos[2].Entry.Date + "_DIFF001",
	}
	slices.Sort(want)
	if got := names(removed["Docs"]); !slices.Equal(got, want) {
		t.Fatalf("removed %v, want %v", got, want)
	}
}

func TestRetentionRemovesNothingWhenDisabledOrForOtherDirectories(t *testing.T) {
	t.Parallel()
	infos := chainsOf(3, 1)

	if removed, err := Retention(docs, infos, 0, 0); err != nil || removed != nil {
		t.Fatalf("disabled: %v, %v", removed, err)
	}
	failing := []Source{{Resolved: "C:/src/Docs", BackupName: "Docs", Err: errors.New("missing")}}
	if removed, err := Retention(failing, infos, 1, 0); err != nil || len(removed) != 0 {
		t.Fatalf("source with error: %v, %v", removed, err)
	}
	other := []Source{{Resolved: "C:/src/Pictures", BackupName: "Pictures"}}
	if removed, err := Retention(other, infos, 1, 0); err != nil || len(removed) != 0 {
		t.Fatalf("other directory: %v, %v", removed, err)
	}
}

func TestRetentionStopsAtAnUnreadableSet(t *testing.T) {
	t.Parallel()
	unreadable := catalog.SetInfo{Entry: naming.BackupEntry{DirectoryName: "Docs", ChainID: "ZZZ009", Date: "2026-09-01"}, Err: errors.New("header checksum mismatch")}
	infos := append(chainsOf(3, 0), unreadable)

	_, err := Retention(docs, infos, 1, 0)
	var target *UnreadableSetError
	if !errors.As(err, &target) || target.Set.Entry.ChainID != "ZZZ009" {
		t.Fatalf("expected UnreadableSetError for ZZZ009, got %v", err)
	}
	// An incomplete set is no reason to stop: it is removed or kept by age.
	incomplete := catalog.SetInfo{Entry: unreadable.Entry, Err: &container.ErrIncomplete{}}
	if _, err := Retention(docs, append(chainsOf(3, 0), incomplete), 1, 0); err != nil {
		t.Fatalf("incomplete set: %v", err)
	}
}

func TestRetentionPreviewCountsThePlannedSet(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{RetentionKeep: 2, Differential: config.Differential{RetentionKeepDifferentials: 2}}
	infos := chainsOf(2, 2)
	keys := Keys{Existing: &container.KeySet{ID: "current"}}

	// A planned full backup starts a third chain, so the oldest goes.
	full := Folders(cfg, infos, docs, keys, true, planNow)
	got := names(RetentionPreview(cfg, infos, docs, full, planNow))
	if len(got) != 3 || got[0] != "Docs_AAA001_"+infos[5].Entry.Date+"_DIFF001" {
		t.Fatalf("full planned: removed %v, want the 3 sets of chain AAA001", got)
	}

	// A planned differential is the third of its chain, so DIFF001 goes.
	diff := Folders(cfg, infos, docs, keys, false, planNow)
	if !diff["Docs"].IsDiff() {
		t.Fatalf("expected a differential to be planned, got %+v", diff["Docs"])
	}
	got = names(RetentionPreview(cfg, infos, docs, diff, planNow))
	if !slices.Equal(got, []string{"Docs_BBB002_" + infos[2].Entry.Date + "_DIFF001"}) {
		t.Fatalf("differential planned: removed %v", got)
	}
}

func TestRetentionPreviewIsEmptyWithoutRetentionOrWithAnUnreadableSet(t *testing.T) {
	t.Parallel()
	infos := chainsOf(3, 0)
	keys := Keys{Existing: &container.KeySet{ID: "current"}}

	cfg := &config.Config{}
	if got := RetentionPreview(cfg, infos, docs, Folders(cfg, infos, docs, keys, true, planNow), planNow); len(got) != 0 {
		t.Fatalf("retention disabled: removed %v", names(got))
	}
	cfg = &config.Config{RetentionKeep: 1}
	unreadable := catalog.SetInfo{Entry: naming.BackupEntry{DirectoryName: "Docs", ChainID: "ZZZ009", Date: "2026-09-01"}, Err: errors.New("bad header")}
	withBad := append(slices.Clone(infos), unreadable)
	if got := RetentionPreview(cfg, withBad, docs, Folders(cfg, withBad, docs, keys, true, planNow), planNow); len(got) != 0 {
		t.Fatalf("unreadable set: removed %v", names(got))
	}
}
