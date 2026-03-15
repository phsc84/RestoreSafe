package util

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestNewBackupIDFormat(t *testing.T) {
	id, err := NewBackupID()
	if err != nil {
		t.Fatalf("NewBackupID returned error: %v", err)
	}

	if len(id) != 6 {
		t.Fatalf("expected ID length 6, got %d", len(id))
	}

	for _, ch := range string(id) {
		if !strings.ContainsRune(idAlphabet, ch) {
			t.Fatalf("ID contains invalid character %q", ch)
		}
	}
}

func TestPartFileNameAndParsePartFileName(t *testing.T) {
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
	_, _, ok := ParsePartFileName("invalid.enc")
	if ok {
		t.Fatal("expected ParsePartFileName to reject invalid filename")
	}
}

func TestLogAndChallengeFileName(t *testing.T) {
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

func TestResolveDir(t *testing.T) {
	base := t.TempDir()
	relative := ResolveDir("sub/folder", base)
	expected := filepath.Join(base, "sub", "folder")
	if relative != expected {
		t.Fatalf("expected %q, got %q", expected, relative)
	}

	absolute := filepath.Join(base, "already-absolute")
	if got := ResolveDir(absolute, "ignored"); got != absolute {
		t.Fatalf("expected absolute path unchanged, got %q", got)
	}
}

func TestFolderBaseName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Documents") + string(filepath.Separator)
	if got := FolderBaseName(path); got != "Documents" {
		t.Fatalf("expected Documents, got %q", got)
	}
}

func TestFormatBytesBinary(t *testing.T) {
	cases := []struct {
		in   uint64
		want string
	}{
		{0, "0 B"},
		{1023, "1023 B"},
		{1024, "1.00 KiB"},
		{1536, "1.50 KiB"},
		{1024 * 1024, "1.00 MiB"},
	}

	for _, tc := range cases {
		if got := FormatBytesBinary(tc.in); got != tc.want {
			t.Fatalf("FormatBytesBinary(%d): expected %q, got %q", tc.in, tc.want, got)
		}
	}
}
