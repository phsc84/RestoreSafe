package e2e

import (
	"RestoreSafe/internal/config"
	"RestoreSafe/internal/format/catalog"
	"RestoreSafe/internal/testutil"
	"RestoreSafe/internal/workflow/backup"
	"RestoreSafe/internal/workflow/interact"
	"RestoreSafe/internal/workflow/interact/interacttest"
	"RestoreSafe/internal/workflow/restore"
	"RestoreSafe/internal/workflow/verify"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

// observedUI is a script that also records progress reports and
// can react to them.
type observedUI struct {
	*interacttest.Script
	mu         sync.Mutex
	reports    []interact.Progress
	onProgress func(interact.Progress)
}

func (o *observedUI) Progress(p interact.Progress) {
	o.mu.Lock()
	o.reports = append(o.reports, p)
	o.mu.Unlock()
	if o.onProgress != nil {
		o.onProgress(p)
	}
}

// last returns the last report of step for item.
func (o *observedUI) last(step, item string) (interact.Progress, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	for i := len(o.reports) - 1; i >= 0; i-- {
		if p := o.reports[i]; p.Step == step && p.Item == item {
			return p, true
		}
	}
	return interact.Progress{}, false
}

func progressConfig(sources []string, backupDir string) *config.Config {
	return &config.Config{
		SourceDirectories:  sources,
		BackupDirectory:    backupDir,
		SplitSizeMB:        1,
		LogLevel:           "info",
		VerifyAfterBackup:  true,
		AuthenticationMode: config.AuthModePassword,
		Argon2:             testutil.FastArgon2Config,
		Differential:       fullBackupsOnly,
	}
}

func TestProgressIsReportedForBackupAndRestore(t *testing.T) {
	root := t.TempDir()
	docs := filepath.Join(root, "Docs")
	writeFile(t, filepath.Join(docs, "a.txt"), "hello")
	writeFile(t, filepath.Join(docs, "sub", "big.bin"), strings.Repeat("x", 2*1024*1024))
	cfg := progressConfig([]string{docs}, filepath.Join(root, "Backups"))

	s := useScript(t, []string{"y"}, password, password)
	o := &observedUI{Script: s.ui}
	testutil.CaptureStdout(t, func() {
		if err := backup.Run(context.Background(), o, cfg, ""); err != nil {
			t.Fatalf("backup: %v", err)
		}
	})
	s.done()
	p, ok := o.last("Backing up", "Docs")
	if !ok || p.Total != 2*1024*1024+5 || p.Done != p.Total {
		t.Fatalf("backup progress must end at the source size, got %+v (found %v)", p, ok)
	}
	infos, _ := catalog.Inventory(cfg.BackupDirectory)
	if len(infos) != 1 || p.Written <= 0 || p.Written != runLog(t, cfg.BackupDirectory, infos[0]).Sets[infos[0].Entry.String()].Bytes {
		t.Fatalf("the last backup report must carry the size of the set written, got %d", p.Written)
	}
	if p, ok := o.last("Verifying", "Docs"); !ok || p.Done <= 0 || p.Done > p.Total {
		t.Fatalf("unexpected post-backup verification progress %+v (found %v)", p, ok)
	}

	dest := filepath.Join(root, "Restore")
	s = useScript(t, []string{"y"}, password)
	o = &observedUI{Script: s.ui}
	testutil.CaptureStdout(t, func() {
		if err := restore.Run(context.Background(), o, cfg, "", restore.Request{Sets: newestRun(t, cfg), Destination: dest}); err != nil {
			t.Fatalf("restore: %v", err)
		}
	})
	s.done()
	// Done counts decrypted bytes, Total the encrypted section.
	if p, ok := o.last("Restoring", "Docs"); !ok || p.Done < 2*1024*1024 || p.Done > p.Total {
		t.Fatalf("unexpected restore progress %+v (found %v)", p, ok)
	}
	assertTreesEqual(t, docs, filepath.Join(dest, "Docs"))
}

func TestCancelledBackupKeepsCompletedSets(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "First")
	second := filepath.Join(root, "Second")
	writeFile(t, filepath.Join(first, "a.txt"), "first")
	writeFile(t, filepath.Join(second, "b.txt"), "second")
	backupDir := filepath.Join(root, "Backups")
	cfg := progressConfig([]string{first, second}, backupDir)
	cfg.RetentionKeep = 1

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := useScript(t, []string{"y"}, password, password)
	// Cancel as soon as the second directory starts.
	o := &observedUI{Script: s.ui, onProgress: func(p interact.Progress) {
		if p.Item == "Second" {
			cancel()
		}
	}}
	var err error
	testutil.CaptureStdout(t, func() { err = backup.Run(ctx, o, cfg, "") })
	s.done()
	if !errors.Is(err, context.Canceled) || err.Error() != "Backup cancelled." {
		t.Fatalf("expected the cancellation, got %v", err)
	}

	infos, _ := catalog.Inventory(backupDir)
	if len(infos) != 1 || !infos[0].Complete() || infos[0].Entry.DirectoryName != "First" {
		t.Fatalf("expected only the complete set of the first directory, got %+v", infos)
	}
	if temps, _ := catalog.ListTempParts(backupDir); len(temps) != 0 {
		t.Fatalf("the interrupted set left temporary parts: %v", temps)
	}
	logs, _ := filepath.Glob(filepath.Join(backupDir, "*.log"))
	if len(logs) != 1 {
		t.Fatalf("expected one log file, got %v", logs)
	}
	if data, _ := os.ReadFile(logs[0]); !strings.Contains(string(data), "Backup cancelled.") {
		t.Fatalf("the log must record the cancellation:\n%s", data)
	}
}

func TestCancelledRestoreAndVerify(t *testing.T) {
	root := t.TempDir()
	docs := filepath.Join(root, "Docs")
	writeFile(t, filepath.Join(docs, "a.txt"), "hello")
	cfg := progressConfig([]string{docs}, filepath.Join(root, "Backups"))

	s := useScript(t, []string{"y"}, password, password)
	testutil.CaptureStdout(t, func() {
		if err := backup.Run(context.Background(), s.ui, cfg, ""); err != nil {
			t.Fatalf("backup: %v", err)
		}
	})
	s.done()

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	s = useScript(t, []string{"y"}, password)
	var err error
	testutil.CaptureStdout(t, func() {
		err = restore.Run(cancelled, s.ui, cfg, "", restore.Request{Sets: newestRun(t, cfg), Destination: filepath.Join(root, "Restore")})
	})
	s.done()
	if !errors.Is(err, context.Canceled) || err.Error() != "Restore cancelled." {
		t.Fatalf("restore: expected the cancellation, got %v", err)
	}

	s = useScript(t, []string{"y"}, password)
	testutil.CaptureStdout(t, func() { err = verify.Run(cancelled, s.ui, cfg, "", verify.Request{Sets: newestRun(t, cfg)}) })
	s.done()
	if !errors.Is(err, context.Canceled) || err.Error() != "Verification cancelled." {
		t.Fatalf("verify: expected the cancellation, got %v", err)
	}
}

// phases returns the sequence of phase steps reported (each phase and
// position once, in order) and fails when Done decreases within a step or a
// step's Count changes.
func (o *observedUI) phases(t *testing.T) []string {
	t.Helper()
	o.mu.Lock()
	defer o.mu.Unlock()
	var out []string
	var last interact.Progress
	for i, p := range o.reports {
		if p.Phase == interact.PhaseNone {
			t.Fatalf("report without a phase: %+v", p)
		}
		if i > 0 && p.Phase == last.Phase && p.Index == last.Index {
			if p.Done < last.Done || p.Count != last.Count {
				t.Fatalf("within a step, Done must not decrease and Count must not change: %+v after %+v", p, last)
			}
			last = p
			continue
		}
		out = append(out, fmt.Sprintf("%d %d/%d", p.Phase, p.Index, p.Count))
		last = p
	}
	return out
}

func TestProgressPhasesFollowTheRun(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "First")
	second := filepath.Join(root, "Second")
	writeFile(t, filepath.Join(first, "a.txt"), strings.Repeat("a", 100_000))
	writeFile(t, filepath.Join(second, "b.txt"), strings.Repeat("b", 100_000))
	cfg := progressConfig([]string{first, second}, filepath.Join(root, "Backups"))
	cfg.RetentionKeep = 1

	step := func(phase interact.Phase, index, count int) string {
		return fmt.Sprintf("%d %d/%d", phase, index, count)
	}
	check := func(name string, o *observedUI, want ...string) {
		t.Helper()
		if got := o.phases(t); !slices.Equal(got, want) {
			t.Fatalf("%s phases %v, want %v", name, got, want)
		}
	}

	s := useScript(t, []string{"y"}, password, password)
	o := &observedUI{Script: s.ui}
	testutil.CaptureStdout(t, func() {
		if err := backup.Run(context.Background(), o, cfg, ""); err != nil {
			t.Fatalf("backup: %v", err)
		}
	})
	s.done()
	check("backup", o,
		step(interact.PhaseUnlocking, 0, 0),
		step(interact.PhaseBackingUp, 1, 2), step(interact.PhaseBackingUp, 2, 2),
		step(interact.PhaseVerifying, 1, 2), step(interact.PhaseVerifying, 2, 2),
		step(interact.PhaseCleaningUp, 0, 0))

	s = useScript(t, []string{"y"}, password)
	o = &observedUI{Script: s.ui}
	testutil.CaptureStdout(t, func() {
		req := restore.Request{Sets: newestRun(t, cfg), Destination: filepath.Join(root, "Restore")}
		if err := restore.Run(context.Background(), o, cfg, "", req); err != nil {
			t.Fatalf("restore: %v", err)
		}
	})
	s.done()
	check("restore", o, step(interact.PhaseUnlocking, 0, 0), step(interact.PhaseRestoring, 1, 2), step(interact.PhaseRestoring, 2, 2))

	s = useScript(t, []string{"y"}, password)
	o = &observedUI{Script: s.ui}
	testutil.CaptureStdout(t, func() {
		if err := verify.Run(context.Background(), o, cfg, "", verify.Request{Sets: newestRun(t, cfg)}); err != nil {
			t.Fatalf("verify: %v", err)
		}
	})
	s.done()
	check("verify", o, step(interact.PhaseUnlocking, 0, 0), step(interact.PhaseVerifying, 1, 2), step(interact.PhaseVerifying, 2, 2))
}
