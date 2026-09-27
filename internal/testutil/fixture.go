// Package testutil provides shared test fixtures and helpers for integration-style tests.
package testutil

import (
	"RestoreSafe/internal/config"
	"RestoreSafe/internal/format/container"
	"RestoreSafe/internal/format/naming"
	"RestoreSafe/internal/format/setio"
	"RestoreSafe/internal/security/cryptox"
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const (
	defaultSplitSizeMB = 1
)

// FastArgon2 are the minimum valid Argon2 parameters; tests use them to stay fast.
var FastArgon2 = cryptox.Argon2Params{Time: cryptox.MinArgonTime, MemoryKB: cryptox.MinArgonMemoryKB, Threads: cryptox.MinArgonThreads}

// FastArgon2Config is FastArgon2 in config.yaml units.
var FastArgon2Config = config.Argon2{Time: config.Argon2MinTime, MemoryMB: config.Argon2MinMemoryMB, Threads: config.Argon2MinThreads}

// BackupFixture holds workspace paths and metadata for an integration-style backup test.
type BackupFixture struct {
	SrcDir    string
	BackupDir string
	Entry     naming.BackupEntry
	Parts     int
	Password  []byte
	KeySet    *container.KeySet
	Master    []byte
}

// NewPasswordKeySet creates a password-mode key set protected by password.
func NewPasswordKeySet(t testing.TB, password []byte) (*container.KeySet, []byte) {
	t.Helper()
	ks, master, err := container.NewKeySet(container.AuthModePassword)
	if err != nil {
		t.Fatalf("NewKeySet: %v", err)
	}
	if err := ks.AddSlot(master, container.SlotPassword, "Password", password, FastArgon2, nil, ""); err != nil {
		t.Fatalf("AddSlot: %v", err)
	}
	return ks, master
}

// NewBackupFixture creates a workspace with source files and a completed encrypted split backup.
func NewBackupFixture(t testing.TB, password []byte) *BackupFixture {
	t.Helper()

	workspace := t.TempDir()
	srcDir := filepath.Join(workspace, "src-data")
	backupDir := filepath.Join(workspace, "target")

	mustMkdirAll(t, filepath.Join(srcDir, "nested"), 0o750)
	mustMkdirAll(t, backupDir, 0o750)
	mustWriteFile(t, filepath.Join(srcDir, "nested", "small.txt"), []byte("hello restoresafe"))
	mustWriteFile(t, filepath.Join(srcDir, "large.bin"), bytes.Repeat([]byte("A"), 2*1024*1024+256))

	ks, master := NewPasswordKeySet(t, password)
	entry := naming.BackupEntry{DirectoryName: filepath.Base(srcDir), ChainID: "FIX001", Date: "2026-03-14"}
	parts := WriteFullSet(t, srcDir, backupDir, entry, ks, master)

	return &BackupFixture{
		SrcDir:    srcDir,
		BackupDir: backupDir,
		Entry:     entry,
		Parts:     parts,
		Password:  password,
		KeySet:    ks,
		Master:    master,
	}
}

// RestoreFixture extends BackupFixture with a restore output directory in the same workspace.
type RestoreFixture struct {
	*BackupFixture
	RestoreRoot string
}

// NewRestoreFixture creates a BackupFixture and an additional restore output directory.
func NewRestoreFixture(t testing.TB, password []byte) *RestoreFixture {
	t.Helper()

	bf := NewBackupFixture(t, password)
	restoreRoot := filepath.Join(filepath.Dir(bf.SrcDir), "restore")
	mustMkdirAll(t, restoreRoot, 0o750)

	return &RestoreFixture{BackupFixture: bf, RestoreRoot: restoreRoot}
}

// CreateBackupInDir writes a second, independent full backup set for entry
// into backupDir with the fixture's key set, using a small synthetic source.
func (f *BackupFixture) CreateBackupInDir(t testing.TB, entry naming.BackupEntry) {
	t.Helper()

	srcDir := filepath.Join(t.TempDir(), "_src_"+entry.DirectoryName)
	mustMkdirAll(t, srcDir, 0o750)
	mustWriteFile(t, filepath.Join(srcDir, "data.txt"), []byte("secondary backup content for "+entry.DirectoryName))
	WriteFullSet(t, srcDir, f.BackupDir, entry, f.KeySet, f.Master)
}

// WriteFullSet writes a full backup of srcDir as entry into backupDir and
// returns the number of parts.
func WriteFullSet(t testing.TB, srcDir, backupDir string, entry naming.BackupEntry, ks *container.KeySet, master []byte) int {
	t.Helper()

	res, err := setio.Write(setio.Params{
		SourceDir:      srcDir,
		ExcludeDirs:    []string{backupDir},
		OutputDir:      backupDir,
		Entry:          entry,
		RunID:          entry.ChainID,
		KeySet:         *ks,
		Master:         master,
		SplitSizeBytes: defaultSplitSizeMB * 1024 * 1024,
	})
	if err != nil {
		t.Fatalf("WriteFullSet: %v", err)
	}
	return len(res.Parts)
}

// WriteDiffSet writes differential diffNumber of the full backup base (in
// backupDir) for srcDir, dated date, and returns its entry.
func WriteDiffSet(t testing.TB, srcDir, backupDir string, base naming.BackupEntry, diffNumber int, date string, ks *container.KeySet, master []byte) naming.BackupEntry {
	t.Helper()

	escape := strings.NewReplacer("[", "[[]", "*", "[*]", "?", "[?]")
	parts, err := filepath.Glob(filepath.Join(backupDir, escape.Replace("["+base.DirectoryName+"]_"+string(base.ChainID)+"_"+base.Date+"_FULL-")+"*.enc"))
	if err != nil || len(parts) == 0 {
		t.Fatalf("base parts not found: %v", err)
	}
	sort.Strings(parts)
	set, err := container.Open(parts)
	if err != nil {
		t.Fatalf("open base: %v", err)
	}
	defer set.Close()
	keys, err := set.SectionKeys(master)
	if err != nil {
		t.Fatal(err)
	}
	m, sum, err := set.ReadManifest(keys)
	if err != nil {
		t.Fatalf("read base manifest: %v", err)
	}

	entry := naming.BackupEntry{DirectoryName: base.DirectoryName, ChainID: base.ChainID, Date: date, DiffNumber: diffNumber}
	runID, _ := naming.NewBackupID()
	_, err = setio.Write(setio.Params{
		SourceDir:      srcDir,
		ExcludeDirs:    []string{backupDir},
		OutputDir:      backupDir,
		Entry:          entry,
		Base:           &setio.Base{Header: set.Header, Manifest: m, ManifestSHA256: sum},
		RunID:          runID,
		KeySet:         *ks,
		Master:         master,
		SplitSizeBytes: defaultSplitSizeMB * 1024 * 1024,
	})
	if err != nil {
		t.Fatalf("setio.Write (differential): %v", err)
	}
	return entry
}

func mustMkdirAll(t testing.TB, path string, perm os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(path, perm); err != nil {
		t.Fatalf("failed to create directory %s: %v", path, err)
	}
}

func mustWriteFile(t testing.TB, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("failed to write file %s: %v", path, err)
	}
}
