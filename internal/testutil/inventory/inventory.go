// Package inventory reads the backup sets of a test's backup directory with
// catalog.Inventory, as the program does. It is apart from testutil because
// the tests of catalog use testutil.
package inventory

import (
	"testing"

	"github.com/phsc84/restoresafe/internal/format/catalog"
	"github.com/phsc84/restoresafe/internal/format/naming"
)

// Read returns the sets in backupDir, newest first.
func Read(t testing.TB, backupDir string) []catalog.SetInfo {
	t.Helper()
	infos, err := catalog.Inventory(backupDir)
	if err != nil {
		t.Fatal(err)
	}
	return infos
}

// Find returns the set of entry in backupDir; the test fails without it.
func Find(t testing.TB, backupDir string, entry naming.BackupEntry) catalog.SetInfo {
	t.Helper()
	for _, info := range Read(t, backupDir) {
		if info.Entry == entry {
			return info
		}
	}
	t.Fatalf("%s is not in the inventory", entry)
	return catalog.SetInfo{}
}
