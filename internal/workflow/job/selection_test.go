package job

import (
	"strings"
	"testing"

	"github.com/phsc84/restoresafe/internal/format/catalog"
	"github.com/phsc84/restoresafe/internal/format/container"
	"github.com/phsc84/restoresafe/internal/format/naming"
)

func TestSelectSets(t *testing.T) {
	t.Parallel()
	a := catalog.SetInfo{Entry: naming.BackupEntry{DirectoryName: "A", ChainID: "ABC123", Date: "2026-03-15"}, Header: &container.Header{}}
	b := catalog.SetInfo{Entry: naming.BackupEntry{DirectoryName: "B", ChainID: "ABC123", Date: "2026-03-15"}, Header: &container.Header{}}
	broken := catalog.SetInfo{Entry: naming.BackupEntry{DirectoryName: "C", ChainID: "ABC123", Date: "2026-03-15"}, Err: &container.ErrIncomplete{}}
	infos := []catalog.SetInfo{a, b, broken}

	got, err := SelectSets(infos, []naming.BackupEntry{b.Entry, a.Entry})
	if err != nil || len(got) != 2 || got[0].Entry != b.Entry || got[1].Entry != a.Entry {
		t.Fatalf("expected B and A in request order, got %+v, %v", got, err)
	}
	for _, tc := range []struct {
		name      string
		requested []naming.BackupEntry
		want      string
	}{
		{"nothing", nil, "No backup chosen"},
		{"unknown", []naming.BackupEntry{a.Entry, {DirectoryName: "Z", ChainID: "ZZZ999", Date: "2026-03-15"}}, "no longer in the backup directory"},
		{"incomplete", []naming.BackupEntry{broken.Entry}, "cannot be used"},
	} {
		if _, err := SelectSets(infos, tc.requested); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: expected an error with %q, got %v", tc.name, tc.want, err)
		}
	}
}
