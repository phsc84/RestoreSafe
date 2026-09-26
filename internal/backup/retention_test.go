package backup

import (
	"RestoreSafe/internal/catalog"
	"RestoreSafe/internal/container"
	"RestoreSafe/internal/testutil"
	"RestoreSafe/internal/util"
	"errors"
	"fmt"
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

// writeDiffs writes differentials 1..n of the full backup base (created
// with writeFull) and returns their entries.
func (e *retentionEnv) writeDiffs(t *testing.T, base util.BackupEntry, n int) []util.BackupEntry {
	t.Helper()
	src := filepath.Join(t.TempDir(), base.DirectoryName)
	var out []util.BackupEntry
	for i := 1; i <= n; i++ {
		createFile(t, filepath.Join(src, "f.txt"), fmt.Sprintf("content of %s, change %d", base.ChainID, i))
		out = append(out, testutil.WriteDiffSet(t, src, e.dir, base, i, base.Date, e.ks, e.master))
	}
	return out
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
	log := util.NewConsoleLogger("info", nil)
	if err := applyRetentionPolicy(dir, 0, 0, []backupSource{{Resolved: dir}}, nil, log); err != nil {
		t.Fatalf("expected no error when retention is disabled, got: %v", err)
	}
}

func TestApplyRetentionPolicySkipsWhenAllSourcesHaveErrors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sources := []backupSource{{Resolved: dir, Err: errors.New("inaccessible")}}
	if err := applyRetentionPolicy(dir, 1, 0, sources, nil, util.NewConsoleLogger("info", nil)); err != nil {
		t.Fatalf("expected nil when directorySet is empty, got: %v", err)
	}
}

func TestApplyRetentionPolicyKeepsAllWhenBelowRetentionLimit(t *testing.T) {
	env := newRetentionEnv(t)
	entry := env.writeFull(t, "Docs", "ONE001", "2026-03-14")
	if err := applyRetentionPolicy(env.dir, 2, 0, docsSources(), nil, util.NewConsoleLogger("info", nil)); err != nil {
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

	if err := applyRetentionPolicy(env.dir, 1, 0, docsSources(), nil, util.NewConsoleLogger("info", nil)); err != nil {
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

	if err := applyRetentionPolicy(env.dir, 5, 0, docsSources(), nil, util.NewConsoleLogger("info", nil)); err != nil {
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

	if err := applyRetentionPolicy(env.dir, 1, 0, docsSources(), nil, util.NewConsoleLogger("info", nil)); err != nil {
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

	if err := applyRetentionPolicy(env.dir, 1, 0, docsSources(), nil, util.NewConsoleLogger("info", nil)); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{legacyPart, legacyChallenge, legacyLog} {
		assertExists(t, p)
	}
}

func TestApplyRetentionPolicyDeletesChainWithItsDifferentials(t *testing.T) {
	env := newRetentionEnv(t)
	older := env.writeFull(t, "Docs", "AAA001", "2026-03-13")
	olderDiffs := env.writeDiffs(t, older, 2)
	var olderParts []string
	for _, e := range append([]util.BackupEntry{older}, olderDiffs...) {
		olderParts = append(olderParts, env.parts(t, e)...)
	}
	newer := env.writeFull(t, "Docs", "BBB002", "2026-03-14")
	newerDiffs := env.writeDiffs(t, newer, 1)

	if err := applyRetentionPolicy(env.dir, 1, 0, docsSources(), nil, util.NewConsoleLogger("info", nil)); err != nil {
		t.Fatal(err)
	}
	for _, p := range olderParts {
		assertNotExists(t, p)
	}
	for _, e := range append([]util.BackupEntry{newer}, newerDiffs...) {
		for _, p := range env.parts(t, e) {
			assertExists(t, p)
		}
	}
}

func TestApplyRetentionPolicyKeepsNewestDifferentials(t *testing.T) {
	env := newRetentionEnv(t)
	// A large file in the full backup keeps the differentials small relative
	// to it, so the next backup is planned as a differential.
	src := filepath.Join(t.TempDir(), "Docs")
	createFile(t, filepath.Join(src, "big.bin"), strings.Repeat("x", 200_000))
	full := util.BackupEntry{DirectoryName: "Docs", ChainID: "AAA001", Date: "2026-03-13"}
	testutil.WriteFullSet(t, src, env.dir, full, env.ks, env.master)
	diffs := env.writeDiffs(t, full, 3)

	var err error
	out := testutil.CaptureStdout(t, func() {
		err = applyRetentionPolicy(env.dir, 0, 1, docsSources(), nil, util.NewConsoleLogger("info", nil))
	})
	if err != nil {
		t.Fatal(err)
	}
	infos, _ := catalog.Inventory(env.dir)
	kept := map[util.BackupEntry]bool{}
	for _, info := range infos {
		kept[info.Entry] = true
	}
	if !kept[full] || !kept[diffs[2]] || kept[diffs[0]] || kept[diffs[1]] || len(infos) != 2 {
		t.Fatalf("expected full + DIFF003 to remain, got %v", kept)
	}
	if !strings.Contains(out, "retention: keep all chains, 1 differential(s) per chain") {
		t.Fatalf("expected the policy in the log, got %q", out)
	}

	// The next differential continues the numbering: numbers are never reused.
	cfg := &util.Config{}
	p := planDirectory(cfg, infos, "Docs", keyPlan{Existing: env.ks}, false, time.Now())
	if !p.IsDiff() || p.DiffNumber != 4 {
		t.Fatalf("expected differential 004 next, got %+v", p)
	}
}

func TestApplyRetentionPolicyAppliesDifferentialLimitToEveryKeptChain(t *testing.T) {
	env := newRetentionEnv(t)
	older := env.writeFull(t, "Docs", "AAA001", "2026-03-13")
	olderDiffs := env.writeDiffs(t, older, 2)
	newer := env.writeFull(t, "Docs", "BBB002", "2026-03-14")
	newerDiffs := env.writeDiffs(t, newer, 2)

	if err := applyRetentionPolicy(env.dir, 2, 1, docsSources(), nil, util.NewConsoleLogger("info", nil)); err != nil {
		t.Fatal(err)
	}
	for _, gone := range []util.BackupEntry{olderDiffs[0], newerDiffs[0]} {
		for _, p := range env.parts(t, gone) {
			assertNotExists(t, p)
		}
	}
	infos, _ := catalog.Inventory(env.dir)
	if len(infos) != 4 {
		t.Fatalf("expected 2 fulls + newest differential of each, got %d sets", len(infos))
	}
}

func TestApplyRetentionPolicyHoldsDirectoriesWithSkippedFiles(t *testing.T) {
	env := newRetentionEnv(t)
	older := env.writeFull(t, "Docs", "AAA001", "2026-03-13")
	env.writeFull(t, "Docs", "BBB002", "2026-03-14")

	var err error
	out := testutil.CaptureStdout(t, func() {
		err = applyRetentionPolicy(env.dir, 1, 0, docsSources(), map[string]bool{"Docs": true}, util.NewConsoleLogger("info", nil))
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
