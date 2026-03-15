package util

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestNewBackupIDFormat(t *testing.T) {
	t.Parallel()

	for i := 0; i < 32; i++ {
		id, err := NewBackupID()
		if err != nil {
			t.Fatalf("NewBackupID returned error: %v", err)
		}

		if len(id) != idLength {
			t.Fatalf("expected ID length %d, got %d", idLength, len(id))
		}

		for _, ch := range string(id) {
			if !strings.ContainsRune(idAlphabet, ch) {
				t.Fatalf("ID contains invalid character %q", ch)
			}
		}
	}
}

func TestDateStringFormat(t *testing.T) {
	t.Parallel()

	date := DateString()
	if ok, err := regexp.MatchString(`^\d{4}-\d{2}-\d{2}$`, date); err != nil || !ok {
		t.Fatalf("DateString returned invalid format %q", date)
	}
	if _, err := time.Parse("2006-01-02", date); err != nil {
		t.Fatalf("DateString returned unparsable date %q: %v", date, err)
	}
}

func TestBackupEntryString(t *testing.T) {
	t.Parallel()

	entry := BackupEntry{FolderName: "Docs", Date: "2026-03-15", ID: BackupID("ABC123")}
	if got := entry.String(); got != "Docs_2026-03-15_ABC123" {
		t.Fatalf("unexpected BackupEntry.String output: %q", got)
	}
}

func TestPartFileNameAndParsePartFileNameRoundTrip(t *testing.T) {
	t.Parallel()

	targetDir := t.TempDir()
	entry := BackupEntry{
		FolderName: "Docs",
		Date:       "2026-03-15",
		ID:         BackupID("ABC123"),
	}

	fullPath := PartFileName(targetDir, entry.FolderName, entry.Date, entry.ID, 7)
	if !strings.HasSuffix(fullPath, "[Docs]_2026-03-15_ABC123-007.enc") {
		t.Fatalf("unexpected part filename: %s", fullPath)
	}

	parsed, seq, ok := ParsePartFileName(filepath.Base(fullPath))
	if !ok {
		t.Fatal("expected ParsePartFileName success, got false")
	}
	if seq != 7 {
		t.Fatalf("expected seq 7, got %d", seq)
	}
	if parsed != entry {
		t.Fatalf("unexpected parsed entry: %#v", parsed)
	}
}

func TestParsePartFileNameRejectsInvalidName(t *testing.T) {
	t.Parallel()

	invalidNames := []string{
		"invalid.enc",
		"[Docs]_2026-03-15_abc123-001.enc",
		"[Docs]_2026-03-15_ABC123-1.enc",
		"[Docs]_2026_03_15_ABC123-001.enc",
		"[]_2026-03-15_ABC123-001.enc",
	}

	for _, name := range invalidNames {
		name := name
		t.Run(name, func(t *testing.T) {
			_, _, ok := ParsePartFileName(name)
			if ok {
				t.Fatalf("expected ParsePartFileName to reject %q", name)
			}
		})
	}
}

func TestLogAndChallengeFileName(t *testing.T) {
	t.Parallel()

	targetDir := t.TempDir()
	id := BackupID("ZX9Q1P")

	logPath := LogFileName(targetDir, "2026-03-15", id)
	if !strings.HasSuffix(logPath, "2026-03-15_ZX9Q1P.log") {
		t.Fatalf("unexpected log filename: %s", logPath)
	}

	challengePath := ChallengeFileName(targetDir, "Photos", "2026-03-15", id)
	if !strings.HasSuffix(challengePath, "[Photos]_2026-03-15_ZX9Q1P.challenge") {
		t.Fatalf("unexpected challenge filename: %s", challengePath)
	}
}

func TestFolderBaseName(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "Documents") + string(filepath.Separator)
	if got := FolderBaseName(path); got != "Documents" {
		t.Fatalf("expected Documents, got %q", got)
	}
}
