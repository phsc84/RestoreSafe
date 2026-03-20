package backup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyBackupResultsCopiesOnlyEncryptedAndChallengeFiles(t *testing.T) {
	t.Parallel()

	stagingDir := t.TempDir()
	targetDir := t.TempDir()

	files := map[string]string{
		"alpha-001.enc":               "enc-part",
		"alpha_2026_AAA111.challenge": "challenge",
		"notes.txt":                   "ignore",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(stagingDir, name), []byte(content), 0o600); err != nil {
			t.Fatalf("failed to create staging file %s: %v", name, err)
		}
	}

	if err := copyBackupResults(stagingDir, targetDir, nil); err != nil {
		t.Fatalf("copyBackupResults returned error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(targetDir, "alpha-001.enc")); err != nil {
		t.Fatalf("expected .enc file to be copied: %v", err)
	}
	if _, err := os.Stat(filepath.Join(targetDir, "alpha_2026_AAA111.challenge")); err != nil {
		t.Fatalf("expected .challenge file to be copied: %v", err)
	}
	if _, err := os.Stat(filepath.Join(targetDir, "notes.txt")); err == nil {
		t.Fatal("did not expect non-backup file to be copied")
	}
}

func TestCopyBackupResultsFailsWhenStagingDirMissing(t *testing.T) {
	t.Parallel()

	err := copyBackupResults(filepath.Join(t.TempDir(), "missing"), t.TempDir(), nil)
	if err == nil {
		t.Fatal("expected error for missing staging directory, got nil")
	}
}
