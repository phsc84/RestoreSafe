package restore

import (
	"RestoreSafe/internal/catalog"
	"RestoreSafe/internal/testutil"
	"os"
	"testing"
)

func TestStageBackupEntryLocallyCopiesParts(t *testing.T) {
	password := []byte("integration-test-password")
	fx := testutil.NewRestoreFixture(t, password)

	stagedDir, err := stageBackupEntryLocally(fx.TargetDir, fx.Entry, t.TempDir(), nil)
	if err != nil {
		t.Fatalf("stageBackupEntryLocally returned error: %v", err)
	}
	defer os.RemoveAll(stagedDir)

	originalParts, err := catalog.CollectParts(fx.TargetDir, fx.Entry)
	if err != nil {
		t.Fatalf("failed to collect original parts: %v", err)
	}
	stagedParts, err := catalog.CollectParts(stagedDir, fx.Entry)
	if err != nil {
		t.Fatalf("failed to collect staged parts: %v", err)
	}
	if len(stagedParts) != len(originalParts) {
		t.Fatalf("expected %d staged parts, got %d", len(originalParts), len(stagedParts))
	}

	for i := range originalParts {
		originalData, err := os.ReadFile(originalParts[i])
		if err != nil {
			t.Fatalf("failed to read original part %q: %v", originalParts[i], err)
		}
		stagedData, err := os.ReadFile(stagedParts[i])
		if err != nil {
			t.Fatalf("failed to read staged part %q: %v", stagedParts[i], err)
		}
		if string(originalData) != string(stagedData) {
			t.Fatalf("staged part %d does not match original", i)
		}
	}
}
