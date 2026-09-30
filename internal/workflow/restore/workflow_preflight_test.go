package restore

import (
	"RestoreSafe/internal/config"
	"RestoreSafe/internal/format/catalog"
	"RestoreSafe/internal/format/container"
	"RestoreSafe/internal/format/naming"
	"RestoreSafe/internal/workflow/interact"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var docsEntry = naming.BackupEntry{DirectoryName: "Docs", ChainID: "ABC123", Date: "2026-03-20"}

func TestBuildRestorePreflightReportsErrors(t *testing.T) {
	t.Parallel()

	restorePath := t.TempDir()
	missing := naming.BackupEntry{DirectoryName: "Missing", ChainID: "ABC123", Date: "2026-03-14"}
	orphan := naming.BackupEntry{DirectoryName: "Orphan", ChainID: "ABC123", Date: "2026-03-14", DiffNumber: 1}
	diff := naming.BackupEntry{DirectoryName: "Pics", ChainID: "PIC001", Date: "2026-03-20", DiffNumber: 2}
	full := catalog.SetInfo{Entry: naming.BackupEntry{DirectoryName: "Pics", ChainID: "PIC001", Date: "2026-03-01"}, Parts: []string{"a", "b"}, SizeBytes: 100, Header: &container.Header{}}
	if err := os.MkdirAll(filepath.Join(restorePath, docsEntry.DirectoryName), 0o750); err != nil {
		t.Fatal(err)
	}
	selected := []catalog.SetInfo{
		{Entry: docsEntry, Parts: []string{"p1"}, SizeBytes: 7},
		{Entry: missing, Err: errors.New("No part files found.")},
		{Entry: orphan, Parts: []string{"p1"}},
		{Entry: diff, Parts: []string{"p1"}, SizeBytes: 5},
	}

	items := buildRestorePreflight(selected, append(selected, full), restorePath)
	if len(items) != 4 {
		t.Fatalf("expected 4 preflight items, got %d", len(items))
	}
	if items[0].OutputDirErr == nil || items[0].TotalSizeBytes != 7 || items[0].PartCount != 1 {
		t.Fatalf("unexpected first item: %+v", items[0])
	}
	if items[1].Err == nil {
		t.Fatal("expected Err for missing part files")
	}
	if items[2].Err == nil || !strings.Contains(items[2].Err.Error(), "is missing") {
		t.Fatalf("expected missing-base error for orphan differential, got %v", items[2].Err)
	}
	if items[3].Err != nil || items[3].Base == nil || items[3].TotalSizeBytes != 105 {
		t.Fatalf("differential must include its full backup: %+v", items[3])
	}

	var sb strings.Builder
	interact.WriteReport(&sb, restorePreflightReport(&config.Config{}, t.TempDir(), restorePath, items[3:], false, false, func() error { return nil }))
	if !strings.Contains(sb.String(), "→ with full backup Pics_PIC001_2026-03-01_FULL (parts: 2)") {
		t.Fatalf("expected the required full backup in the preflight: %q", sb.String())
	}
}

func TestPrintRestorePreflightShowsRestoreDirectoriesWithPerDirectoryErrors(t *testing.T) {
	t.Parallel()
	backupDir := t.TempDir()
	restorePath := t.TempDir()
	outputDir := filepath.Join(restorePath, "Docs")
	items := []restorePreflightItem{{
		Entry:        docsEntry,
		PartCount:    4,
		OutputDir:    outputDir,
		OutputDirErr: errors.New("Restore directory already exists. Remedy: Choose a different restore destination or rename/delete the existing restore directory."),
	}}

	var sb strings.Builder
	interact.WriteReport(&sb, restorePreflightReport(&config.Config{}, backupDir, restorePath, items, false, false, func() error { return nil }))
	output := sb.String()

	selectionLine := "  [OK] " + docsEntry.String() + " (parts: 4)"
	directoryLine := "  [ERROR] " + filepath.ToSlash(outputDir)
	errorText := "Restore directory already exists. Remedy: Choose a different restore destination or rename/delete the existing restore directory."
	if !strings.Contains(output, selectionLine) {
		t.Fatalf("expected backup selection to remain an OK archive entry, got: %q", output)
	}
	if !strings.Contains(output, "Restored directory(s):\n"+directoryLine) {
		t.Fatalf("expected restore directory section with absolute destination path, got: %q", output)
	}
	if strings.Index(output, errorText) <= strings.Index(output, directoryLine) {
		t.Fatalf("expected restore directory error below directory line, got: %q", output)
	}
}

func TestPrintRestorePreflightShowsYubiKeyStatus(t *testing.T) {
	t.Parallel()
	items := []restorePreflightItem{{Entry: docsEntry, PartCount: 1}}

	for _, tc := range []struct {
		connected error
		want      string
	}{
		{nil, "  [OK] YubiKey connected. Keep it connected before starting restore."},
		{errors.New("no YubiKey detected"), "  [WARN] YubiKey not connected. Remedy: Connect the YubiKey before starting restore."},
	} {
		var sb strings.Builder
		connected := tc.connected
		interact.WriteReport(&sb, restorePreflightReport(&config.Config{}, t.TempDir(), t.TempDir(), items, true, false, func() error { return connected }))
		output := sb.String()
		authIdx := strings.Index(output, "Authentication: password + YubiKey")
		statusIdx := strings.Index(output, tc.want)
		if authIdx < 0 || statusIdx < 0 || statusIdx < authIdx {
			t.Fatalf("expected authentication line before %q, got: %q", tc.want, output)
		}
		if !strings.Contains(output, "Backup size") {
			t.Fatalf("expected backup-size line in output, got: %q", output)
		}
	}
}

func TestPrintRestorePreflightShowsInsufficientSpaceError(t *testing.T) {
	t.Parallel()

	items := []restorePreflightItem{{Entry: docsEntry, PartCount: 1, TotalSizeBytes: math.MaxInt64}}
	var sb strings.Builder
	interact.WriteReport(&sb, restorePreflightReport(&config.Config{}, t.TempDir(), t.TempDir(), items, false, false, func() error { return nil }))
	if !strings.Contains(sb.String(), "[ERROR] Insufficient free space for restore:") {
		t.Fatalf("expected insufficient-space restore error line, got: %q", sb.String())
	}
}

func TestValidateRestorePreflight(t *testing.T) {
	t.Parallel()

	ok := []restorePreflightItem{{Entry: naming.BackupEntry{DirectoryName: "A"}, PartCount: 1}, {Entry: naming.BackupEntry{DirectoryName: "B"}, PartCount: 2}}
	if err := validateRestorePreflight(ok); err != nil {
		t.Fatalf("expected no error for valid items, got %v", err)
	}
	partErr := []restorePreflightItem{{PartCount: 1}, {Err: errors.New("no parts found")}}
	if err := validateRestorePreflight(partErr); err == nil || !strings.Contains(err.Error(), "1 selected item") {
		t.Fatalf("expected one invalid item, got %v", err)
	}
	dirErr := []restorePreflightItem{{PartCount: 1, OutputDirErr: errors.New("already exists")}}
	if err := validateRestorePreflight(dirErr); err == nil {
		t.Fatal("expected error for item with OutputDirErr set, got nil")
	}
}

func TestValidateRestoreTargetSpace(t *testing.T) {
	t.Parallel()

	err := validateRestoreTargetSpace(t.TempDir(), []restorePreflightItem{{Entry: docsEntry, PartCount: 1, TotalSizeBytes: math.MaxInt64}})
	if err == nil || !strings.Contains(err.Error(), "Restore preflight failed: Insufficient free space for restore:") {
		t.Fatalf("expected insufficient-space error, got: %v", err)
	}
	if err := validateRestoreTargetSpace(t.TempDir(), []restorePreflightItem{{TotalSizeBytes: 0}}); err != nil {
		t.Fatalf("expected nil when estimated bytes is zero, got: %v", err)
	}
}

func TestQueryRestoreTargetFreeBytes(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	for _, p := range []string{base, filepath.Join(base, "missing", "subdir")} {
		free, err := queryRestoreTargetFreeBytes(p)
		if err != nil || free == 0 {
			t.Fatalf("queryRestoreTargetFreeBytes(%q) = %d, %v", p, free, err)
		}
	}
}

func TestEstimateRestoreBytesSkipsItemsWithErrors(t *testing.T) {
	t.Parallel()

	items := []restorePreflightItem{{TotalSizeBytes: 10}, {TotalSizeBytes: 20, Err: errors.New("x")}}
	if got := estimateRestoreBytes(items); got != 10 {
		t.Fatalf("expected 10, got %d", got)
	}
}

func TestDisplayRestoreOutputDirReturnsForwardSlashPath(t *testing.T) {
	t.Parallel()

	got := displayRestoreOutputDir(filepath.Join(t.TempDir(), "Docs"))
	if strings.Contains(got, `\`) || !strings.HasSuffix(got, "/Docs") {
		t.Fatalf("unexpected display path %q", got)
	}
}
