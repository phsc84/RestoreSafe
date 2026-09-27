package naming

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

func TestBackupEntryStringAndLabels(t *testing.T) {
	t.Parallel()

	full := BackupEntry{DirectoryName: "Docs", ChainID: "ABC123", Date: "2026-03-15"}
	if got := full.String(); got != "Docs_ABC123_2026-03-15_FULL" {
		t.Fatalf("unexpected full String: %q", got)
	}
	diff := BackupEntry{DirectoryName: "Docs", ChainID: "ABC123", Date: "2026-03-20", DiffNumber: 7}
	if got := diff.String(); got != "Docs_ABC123_2026-03-20_DIFF007" {
		t.Fatalf("unexpected diff String: %q", got)
	}
	if !diff.IsDiff() || full.IsDiff() {
		t.Fatal("IsDiff mismatch")
	}
	if full.ChainKey() != diff.ChainKey() {
		t.Fatal("entries of one chain must share a chain key")
	}
}

func TestPartFileNameAndParseRoundTrip(t *testing.T) {
	t.Parallel()

	backupDir := t.TempDir()
	for _, entry := range []BackupEntry{
		{DirectoryName: "Docs", ChainID: "ABC123", Date: "2026-03-15"},
		{DirectoryName: "My Docs__from__C_Root~20~A", ChainID: "ZZ9Z99", Date: "2026-03-20", DiffNumber: 12},
	} {
		fullPath := PartFileName(backupDir, entry, 7)
		parsed, seq, ok := ParsePartFileName(filepath.Base(fullPath))
		if !ok || seq != 7 || parsed != entry {
			t.Fatalf("round trip of %s failed: %#v seq=%d ok=%v", filepath.Base(fullPath), parsed, seq, ok)
		}
		if _, _, ok := ParseTempPartFileName(filepath.Base(fullPath)); ok {
			t.Fatal("complete part must not parse as temporary")
		}
		tempName := filepath.Base(fullPath) + TempSuffix
		if _, _, ok := ParsePartFileName(tempName); ok {
			t.Fatal("temporary part must not parse as complete")
		}
		if got, _, ok := ParseTempPartFileName(tempName); !ok || got != entry {
			t.Fatalf("temporary part not parsed: %#v", got)
		}
	}

	if got := filepath.Base(PartFileName(backupDir, BackupEntry{DirectoryName: "Docs", ChainID: "ABC123", Date: "2026-03-15", DiffNumber: 2}, 1)); got != "[Docs]_ABC123_2026-03-15_DIFF002-001.enc" {
		t.Fatalf("unexpected diff file name %q", got)
	}
}

func TestParsePartFileNameRejectsInvalidName(t *testing.T) {
	t.Parallel()

	invalidNames := []string{
		"invalid.enc",
		"[Docs]_abc123_2026-03-15_FULL-001.enc",
		"[Docs]_ABC123_2026-03-15_FULL-1.enc",
		"[Docs]_ABC123_2026-03-15_FULL-000.enc",
		"[Docs]_ABC123_2026-03-15_DIFF000-001.enc",
		"[Docs]_ABC123_2026-03-15_DIFF1-001.enc",
		"[Docs]_ABC123_2026-03-15-001.enc",
		"[Docs]_2026-03-15_ABC123-001.enc",
		"[]_ABC123_2026-03-15_FULL-001.enc",
	}
	for _, name := range invalidNames {
		if _, _, ok := ParsePartFileName(name); ok {
			t.Fatalf("expected ParsePartFileName to reject %q", name)
		}
	}
}

func TestIsLegacyBackupFileName(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"[Docs]_2026-03-15_ABC123-001.enc", "[Docs]_2026-03-15_ABC123.challenge"} {
		if !IsLegacyBackupFileName(name) {
			t.Fatalf("expected %q to be recognized as 1.x file", name)
		}
	}
	for _, name := range []string{"[Docs]_ABC123_2026-03-15_FULL-001.enc", "2026-03-15_ABC123.log", "notes.txt"} {
		if IsLegacyBackupFileName(name) {
			t.Fatalf("%q must not be recognized as 1.x file", name)
		}
	}
}

func TestValidateBackupEntryNameAcceptsValid(t *testing.T) {
	t.Parallel()

	validNames := []string{
		"Docs",
		"My Photos 2026",
		"backup.tar.gz",
		"a",
		"report-final_v2",
	}

	for _, name := range validNames {
		name := name
		t.Run(name, func(t *testing.T) {
			if err := ValidateBackupEntryName(name); err != nil {
				t.Fatalf("expected %q to be accepted, got error: %v", name, err)
			}
		})
	}
}

func TestValidateBackupEntryNameRejectsUnsafe(t *testing.T) {
	t.Parallel()

	invalidNames := []string{
		"",
		".",
		"..",
		"foo/bar",
		`foo\bar`,
		"C:evil",
		"trailingdot.",
		"trailingspace ",
		"CON",
		"con",
		"NUL.txt",
		"COM1",
		"lpt9.log",
	}

	for _, name := range invalidNames {
		name := name
		t.Run(name, func(t *testing.T) {
			if err := ValidateBackupEntryName(name); err == nil {
				t.Fatalf("expected ValidateBackupEntryName to reject %q", name)
			}
		})
	}
}

func TestLogFileName(t *testing.T) {
	t.Parallel()

	logPath := LogFileName(t.TempDir(), "2026-03-15", BackupID("ZX9Q1P"))
	if !strings.HasSuffix(logPath, "2026-03-15_ZX9Q1P.log") {
		t.Fatalf("unexpected log filename: %s", logPath)
	}
}

func TestDirectoryBaseName(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "Documents") + string(filepath.Separator)
	if got := DirectoryBaseName(path); got != "Documents" {
		t.Fatalf("expected Documents, got %q", got)
	}
}
