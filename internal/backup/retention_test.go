package backup

import (
	"RestoreSafe/internal/catalog"
	"RestoreSafe/internal/container"
	"RestoreSafe/internal/testutil"
	"RestoreSafe/internal/util"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type retentionEnv struct {
	dir    string
	ks     *container.KeySet
	master []byte
}

func newRetentionEnv(t *testing.T) *retentionEnv {
	t.Helper()
	ks, master := testutil.NewPasswordKeySet(t, []byte("pw"))
	return &retentionEnv{dir: t.TempDir(), ks: ks, master: master}
}

// writeFull writes a small, complete full backup set and returns its entry.
func (e *retentionEnv) writeFull(t *testing.T, directory, chainID, date string) util.BackupEntry {
	t.Helper()
	src := filepath.Join(t.TempDir(), directory)
	createFile(t, filepath.Join(src, "f.txt"), "content of "+chainID)
	entry := util.BackupEntry{DirectoryName: directory, ChainID: util.BackupID(chainID), Date: date}
	testutil.WriteFullSet(t, src, e.dir, entry, e.ks, e.master)
	return entry
}

func (e *retentionEnv) parts(t *testing.T, entry util.BackupEntry) []string {
	t.Helper()
	parts, err := catalog.CollectParts(e.dir, entry)
	if err != nil {
		t.Fatal(err)
	}
	return parts
}

func docsSources() []backupSource {
	return []backupSource{{Resolved: "C:/src/Docs", BackupName: "Docs"}}
}

func TestApplyRetentionPolicySkipsWhenDisabled(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := util.NewConsoleLogger("info")
	if err := applyRetentionPolicy(dir, 0, []backupSource{{Resolved: dir}}, nil, log); err != nil {
		t.Fatalf("expected no error when retention is disabled, got: %v", err)
	}
}

func TestApplyRetentionPolicySkipsWhenAllSourcesHaveErrors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sources := []backupSource{{Resolved: dir, Err: errors.New("inaccessible")}}
	if err := applyRetentionPolicy(dir, 1, sources, nil, util.NewConsoleLogger("info")); err != nil {
		t.Fatalf("expected nil when directorySet is empty, got: %v", err)
	}
}

func TestApplyRetentionPolicyKeepsAllWhenBelowRetentionLimit(t *testing.T) {
	env := newRetentionEnv(t)
	entry := env.writeFull(t, "Docs", "ONE001", "2026-03-14")
	if err := applyRetentionPolicy(env.dir, 2, docsSources(), nil, util.NewConsoleLogger("info")); err != nil {
		t.Fatal(err)
	}
	for _, p := range env.parts(t, entry) {
		assertExists(t, p)
	}
}

func TestApplyRetentionPolicyDeletesOlderChains(t *testing.T) {
	env := newRetentionEnv(t)
	older := env.writeFull(t, "Docs", "AAA001", "2026-03-13")
	olderParts := env.parts(t, older)
	newer := env.writeFull(t, "Docs", "BBB002", "2026-03-14")
	other := env.writeFull(t, "Pics", "AAA001", "2026-03-13")

	if err := applyRetentionPolicy(env.dir, 1, docsSources(), nil, util.NewConsoleLogger("info")); err != nil {
		t.Fatal(err)
	}
	for _, p := range olderParts {
		assertNotExists(t, p)
	}
	for _, p := range env.parts(t, newer) {
		assertExists(t, p)
	}
	// Directories that are not configured sources are never touched.
	for _, p := range env.parts(t, other) {
		assertExists(t, p)
	}
}

// truncateSet damages a set's last part so its trailer is missing (the set
// becomes incomplete) and sets the part's modification time.
func (e *retentionEnv) truncateSet(t *testing.T, entry util.BackupEntry, mtime time.Time) string {
	t.Helper()
	parts := e.parts(t, entry)
	last := parts[len(parts)-1]
	fi, err := os.Stat(last)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(last, fi.Size()-10); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(last, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	return last
}

func TestApplyRetentionPolicyHandlesIncompleteSets(t *testing.T) {
	env := newRetentionEnv(t)
	oldEntry := env.writeFull(t, "Docs", "OLD001", "2026-03-10")
	recentEntry := env.writeFull(t, "Docs", "NEW003", "2026-03-15")
	env.writeFull(t, "Docs", "BBB002", "2026-03-14")

	old := env.truncateSet(t, oldEntry, time.Now().Add(-24*time.Hour))
	recent := env.truncateSet(t, recentEntry, time.Now().Add(time.Hour))

	if err := applyRetentionPolicy(env.dir, 5, docsSources(), nil, util.NewConsoleLogger("info")); err != nil {
		t.Fatal(err)
	}
	assertNotExists(t, old)
	assertExists(t, recent)
}

func TestApplyRetentionPolicySkipsWhenASetIsUnreadable(t *testing.T) {
	env := newRetentionEnv(t)
	older := env.writeFull(t, "Docs", "AAA001", "2026-03-13")
	env.writeFull(t, "Docs", "BBB002", "2026-03-14")
	foreign := util.PartFileName(env.dir, util.BackupEntry{DirectoryName: "Docs", ChainID: "ZZZ999", Date: "2026-03-15"}, 1)
	createFile(t, foreign, "not a RestoreSafe backup")

	if err := applyRetentionPolicy(env.dir, 1, docsSources(), nil, util.NewConsoleLogger("info")); err != nil {
		t.Fatal(err)
	}
	for _, p := range env.parts(t, older) {
		assertExists(t, p)
	}
	assertExists(t, foreign)
}

func TestApplyRetentionPolicyNeverTouchesLegacyFiles(t *testing.T) {
	env := newRetentionEnv(t)
	env.writeFull(t, "Docs", "BBB002", "2026-03-14")
	legacyPart := filepath.Join(env.dir, "[Docs]_2026-01-01_OLD001-001.enc")
	legacyChallenge := filepath.Join(env.dir, "[Docs]_2026-01-01_OLD001.challenge")
	legacyLog := filepath.Join(env.dir, "2026-01-01_OLD001.log")
	for _, p := range []string{legacyPart, legacyChallenge, legacyLog} {
		createFile(t, p, "1.x")
	}

	if err := applyRetentionPolicy(env.dir, 1, docsSources(), nil, util.NewConsoleLogger("info")); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{legacyPart, legacyChallenge, legacyLog} {
		assertExists(t, p)
	}
}

func TestApplyRetentionPolicyHoldsDirectoriesWithSkippedFiles(t *testing.T) {
	env := newRetentionEnv(t)
	older := env.writeFull(t, "Docs", "AAA001", "2026-03-13")
	env.writeFull(t, "Docs", "BBB002", "2026-03-14")

	var err error
	out := testutil.CaptureStdout(t, func() {
		err = applyRetentionPolicy(env.dir, 1, docsSources(), map[string]bool{"Docs": true}, util.NewConsoleLogger("info"))
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range env.parts(t, older) {
		assertExists(t, p)
	}
	if !strings.Contains(out, "Cleanup old data skipped for [Docs]") {
		t.Fatalf("expected hold message, got %q", out)
	}
}

func TestDeleteOrphanLogFilesKeepsActiveRunLogs(t *testing.T) {
	env := newRetentionEnv(t)
	active := env.writeFull(t, "Docs", "ABC123", "2026-03-14")

	activeLog := util.LogFileName(env.dir, active.Date, active.ChainID)
	orphanLog := util.LogFileName(env.dir, "2026-03-13", "ZZZ999")
	unrelated := filepath.Join(env.dir, "notes.log")
	createFile(t, activeLog, "active")
	createFile(t, orphanLog, "orphan")
	createFile(t, unrelated, "keep")

	deleted, err := deleteOrphanLogFiles(env.dir)
	if err != nil {
		t.Fatalf("deleteOrphanLogFiles returned error: %v", err)
	}
	if len(deleted) != 1 {
		t.Fatalf("expected exactly 1 deleted orphan log, got %v", deleted)
	}
	assertExists(t, activeLog)
	assertNotExists(t, orphanLog)
	assertExists(t, unrelated)
}

func TestDeleteOrphanLogFilesReturnZeroWhenTargetMissing(t *testing.T) {
	t.Parallel()
	deleted, err := deleteOrphanLogFiles(filepath.Join(t.TempDir(), "nonexistent"))
	if err != nil || len(deleted) != 0 {
		t.Fatalf("expected no error and no deletions, got %v, %v", deleted, err)
	}
}

func createFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("failed to create parent directories: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("failed to create file %s: %v", path, err)
	}
}

func assertExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file to exist: %s (%v)", path, err)
	}
}

func assertNotExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected file to be removed: %s", path)
	}
}
