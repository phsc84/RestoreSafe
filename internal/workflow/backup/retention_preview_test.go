package backup

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/phsc84/restoresafe/internal/config"
	"github.com/phsc84/restoresafe/internal/format/catalog"
	"github.com/phsc84/restoresafe/internal/format/naming"
	"github.com/phsc84/restoresafe/internal/logging"
	"github.com/phsc84/restoresafe/internal/testutil"
	"github.com/phsc84/restoresafe/internal/workflow/interact/interacttest"
	"github.com/phsc84/restoresafe/internal/workflow/plan"
)

// previewEnv is a backup directory with chains of the source directory Docs
// and a configuration that backs up a real Docs directory into it.
type previewEnv struct {
	*retentionEnv
	cfg *config.Config
}

func newPreviewEnv(t *testing.T, keep, keepDiffs int) *previewEnv {
	t.Helper()
	src := filepath.Join(t.TempDir(), "Docs")
	createFile(t, filepath.Join(src, "f.txt"), "current content")
	return &previewEnv{
		retentionEnv: newRetentionEnv(t),
		cfg: &config.Config{
			SourceDirectories:  []string{src},
			AuthenticationMode: config.AuthModePassword,
			SplitSizeMB:        1,
			RetentionKeep:      keep,
			Differential:       config.Differential{RetentionKeepDifferentials: keepDiffs},
			Argon2:             testutil.FastArgon2Config,
		},
	}
}

// writeLargeFull writes a full backup with a large file, so that later small
// differentials stay below max_size_percent and a differential is planned.
func (e *previewEnv) writeLargeFull(t *testing.T, chainID, date string) naming.BackupEntry {
	t.Helper()
	src := filepath.Join(t.TempDir(), "Docs")
	createFile(t, filepath.Join(src, "big.bin"), strings.Repeat("x", 200_000))
	entry := naming.BackupEntry{DirectoryName: "Docs", ChainID: naming.BackupID(chainID), Date: date}
	testutil.WriteFullSet(t, src, e.dir, entry, e.ks, e.master)
	return entry
}

func (e *previewEnv) setNames(t *testing.T) []string {
	t.Helper()
	infos, err := catalog.Inventory(e.dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, info := range infos {
		out = append(out, info.Entry.String())
	}
	return out
}

// startAnswers are the answers to the start question: "f" shows the full
// plan, which is then started with "y".
func startAnswers(answer string) []string {
	if answer == "f" {
		return []string{"f", "y"}
	}
	return []string{answer}
}

// previewAndRun computes the retention preview the way the backup plan does,
// runs the backup with answer to the start question, and returns the
// previewed and the actually deleted set names, sorted.
func (e *previewEnv) previewAndRun(t *testing.T, answer string) (previewed, deleted []string) {
	t.Helper()
	cfg := *e.cfg
	cfg.BackupDirectory = e.dir

	infos, err := catalog.Inventory(e.dir)
	if err != nil {
		t.Fatal(err)
	}
	sources := plan.ResolveSources(cfg.SourceDirectories, "")
	keys := plan.KeysFor(&cfg, infos)
	folders := plan.Folders(&cfg, infos, sources, keys, answer == "f", time.Now())
	for _, info := range plan.RetentionPreview(&cfg, infos, sources, folders, time.Now()) {
		previewed = append(previewed, info.Entry.String())
	}

	before := e.setNames(t)
	ui := &interacttest.Script{
		ReadLine:     interacttest.Answers(startAnswers(answer)...),
		ReadPassword: func(string) ([]byte, error) { return []byte("pw"), nil },
	}
	if err := Run(context.Background(), ui, &cfg, ""); err != nil {
		t.Fatalf("backup failed: %v", err)
	}
	if ui.LogPath == "" || !strings.HasPrefix(filepath.Base(ui.LogPath), time.Now().Format("2006-01-02")) {
		t.Fatalf("the backup reports its log file, got %q", ui.LogPath)
	}
	after := e.setNames(t)
	for _, name := range before {
		if !slices.Contains(after, name) {
			deleted = append(deleted, name)
		}
	}
	if removed := cleanupFact(t, e.dir).Removed; removed != len(deleted) {
		t.Fatalf("the cleanup fact counts %d removed sets, but %d were deleted", removed, len(deleted))
	}
	slices.Sort(previewed)
	slices.Sort(deleted)
	return previewed, deleted
}

func assertPreviewMatches(t *testing.T, previewed, deleted []string, wantCount int) {
	t.Helper()
	if !slices.Equal(previewed, deleted) {
		t.Fatalf("preview %v, but the backup deleted %v", previewed, deleted)
	}
	if len(deleted) != wantCount {
		t.Fatalf("expected %d deleted sets, got %v", wantCount, deleted)
	}
}

func TestRetentionPreviewMatchesDeletionForANewChain(t *testing.T) {
	for _, keep := range []int{1, 2, 3} {
		t.Run("keep "+string(rune('0'+keep)), func(t *testing.T) {
			env := newPreviewEnv(t, keep, 0)
			a := env.writeFull(t, "Docs", "AAA001", "2026-09-01")
			env.writeDiffs(t, a, 2)
			env.writeFull(t, "Docs", "BBB002", "2026-09-10")
			env.writeFull(t, "Docs", "CCC003", "2026-09-20")

			// "f": a full backup starts a fourth chain.
			previewed, deleted := env.previewAndRun(t, "f")
			assertPreviewMatches(t, previewed, deleted, []int{0, 5, 4, 3}[keep])
		})
	}
}

func TestRetentionPreviewMatchesDeletionForANewDifferential(t *testing.T) {
	for _, keepDiffs := range []int{1, 2} {
		t.Run("keep differentials "+string(rune('0'+keepDiffs)), func(t *testing.T) {
			env := newPreviewEnv(t, 0, keepDiffs)
			full := env.writeLargeFull(t, "AAA001", "2026-09-01")
			env.writeDiffs(t, full, 3)

			previewed, deleted := env.previewAndRun(t, "y")
			assertPreviewMatches(t, previewed, deleted, 4-keepDiffs)
		})
	}
}

func TestRetentionPreviewMatchesDeletionWithBothLimits(t *testing.T) {
	env := newPreviewEnv(t, 2, 1)
	old := env.writeLargeFull(t, "AAA001", "2026-09-01")
	env.writeDiffs(t, old, 2)
	full := env.writeLargeFull(t, "BBB002", "2026-09-10")
	env.writeDiffs(t, full, 2)

	// A differential of BBB002: AAA001 keeps its place (2 chains), but both
	// chains keep only their newest differential.
	previewed, deleted := env.previewAndRun(t, "y")
	assertPreviewMatches(t, previewed, deleted, 3)
}

func TestRetentionPreviewMatchesDeletionOfAnOldIncompleteSet(t *testing.T) {
	env := newPreviewEnv(t, 3, 0)
	env.writeFull(t, "Docs", "AAA001", "2026-09-01")
	broken := env.writeFull(t, "Docs", "BBB002", "2026-09-10")
	env.truncateSet(t, broken, time.Now().Add(-time.Hour))

	previewed, deleted := env.previewAndRun(t, "f")
	assertPreviewMatches(t, previewed, deleted, 1)
}

func TestRetentionPreviewMatchesWhenNothingIsRemoved(t *testing.T) {
	for name, env := range map[string]*previewEnv{
		"retention disabled": newPreviewEnv(t, 0, 0),
		"below the limits":   newPreviewEnv(t, 5, 5),
	} {
		t.Run(name, func(t *testing.T) {
			a := env.writeFull(t, "Docs", "AAA001", "2026-09-01")
			env.writeDiffs(t, a, 2)
			previewed, deleted := env.previewAndRun(t, "y")
			assertPreviewMatches(t, previewed, deleted, 0)
		})
	}
}

// cleanupFact returns the cleanup fact of the newest log in dir, a zero
// fact when retention did not run.
func cleanupFact(t *testing.T, dir string) logging.Fact {
	t.Helper()
	logs, err := filepath.Glob(filepath.Join(dir, "*.log"))
	if err != nil || len(logs) == 0 {
		t.Fatalf("no log in %s: %v", dir, err)
	}
	newest, newestTime := "", time.Time{}
	for _, l := range logs {
		if fi, err := os.Stat(l); err == nil && fi.ModTime().After(newestTime) {
			newest, newestTime = l, fi.ModTime()
		}
	}
	facts, err := logging.ReadFacts(newest)
	if err != nil {
		t.Fatalf("cannot read the facts of %s: %v", newest, err)
	}
	if facts.Cleanup == nil {
		return logging.Fact{}
	}
	return *facts.Cleanup
}
