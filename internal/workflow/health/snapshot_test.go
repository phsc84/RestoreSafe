package health

import (
	"RestoreSafe/internal/testutil/scenario"
	"RestoreSafe/internal/workflow/interact"
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func snapshotOf(t *testing.T, s scenario.Scenario) Snapshot {
	t.Helper()
	return TakeSnapshot(Params{Config: s.Config, ConfigPath: s.ConfigPath, Now: s.Now})
}

func codes(problems []Problem) []interact.Code {
	out := make([]interact.Code, 0, len(problems))
	for _, p := range problems {
		out = append(out, p.Code)
	}
	return out
}

// TestSnapshotOfEveryScenario covers spec 3.5, 11.1 and 11.9.
func TestSnapshotOfEveryScenario(t *testing.T) {
	t.Parallel()
	cases := []struct {
		condition scenario.Condition
		state     State
		problem   interact.Code // the first problem, "" for none
		folder    string        // its folder
		note      interact.Code // a note, "" for none
	}{
		{scenario.Protected, StateProtected, "", "", ""},
		{scenario.Empty, StateEmpty, "", "", ""},
		{scenario.Overdue, StateWarning, interact.CodeOverdue, "", ""},
		{scenario.SkippedFiles, StateWarning, interact.CodeSkippedFiles, "Docs", ""},
		{scenario.BaseMissing, StateError, interact.CodeBaseMissing, "Docs", ""},
		{scenario.SourceMissing, StateError, interact.CodeSourceMissing, "Pics", ""},
		{scenario.BackupDirUnreachable, StateError, interact.CodeBackupDirUnreachable, "", ""},
		{scenario.IncompleteNewest, StateWarning, interact.CodeIncompleteNewest, "Docs", ""},
		{scenario.VerifyFailed, StateError, interact.CodeVerifyFailed, "Docs", ""},
		{scenario.Argon2Capped, StateWarning, interact.CodeArgon2Capped, "", ""},
		{scenario.NewKeysNeeded, StateProtected, "", "", interact.CodeNewKeysNeeded},
		{scenario.Legacy1x, StateProtected, "", "", interact.CodeLegacyBackups},
		{scenario.LeftoverTmp, StateProtected, "", "", interact.CodeLeftoverTempFiles},
		{scenario.FolderNotBackedUp, StateWarning, interact.CodeFolderNotBackedUp, "Music", ""},
	}
	if len(cases) != len(scenario.All) {
		t.Fatalf("%d cases for %d scenarios: every scenario needs a case", len(cases), len(scenario.All))
	}
	for _, tc := range cases {
		t.Run(string(tc.condition), func(t *testing.T) {
			t.Parallel()
			s := snapshotOf(t, scenario.Build(t, tc.condition))
			if s.State != tc.state {
				t.Fatalf("state %d, want %d; problems %v", s.State, tc.state, codes(s.Problems))
			}
			if tc.problem == "" && len(s.Problems) > 0 {
				t.Fatalf("expected no problems, got %v", codes(s.Problems))
			}
			if tc.problem != "" && (len(s.Problems) != 1 || s.Problems[0].Code != tc.problem || s.Problems[0].Folder != tc.folder) {
				t.Fatalf("expected only %s for %q, got %+v", tc.problem, tc.folder, s.Problems)
			}
			if tc.note != "" && (len(s.Notes) != 1 || s.Notes[0].Code != tc.note) {
				t.Fatalf("expected the note %s, got %v", tc.note, codes(s.Notes))
			}
			if tc.note == "" && len(s.Notes) != 0 {
				t.Fatalf("expected no notes, got %v", codes(s.Notes))
			}
			for _, p := range append(s.Problems, s.Notes...) {
				if p.Status != interact.StatusError && p.Status != interact.StatusWarn && p.Status != interact.StatusInfo {
					t.Fatalf("problem without a status: %+v", p)
				}
			}
		})
	}
}

func TestSnapshotDescribesTheBackups(t *testing.T) {
	t.Parallel()
	sc := scenario.Build(t, scenario.Protected)
	s := snapshotOf(t, sc)

	if len(s.Folders) != 2 || len(s.Runs) != 1 || len(s.Sets) != 2 {
		t.Fatalf("expected 2 folders, 1 run, 2 sets: %d, %d, %d", len(s.Folders), len(s.Runs), len(s.Sets))
	}
	for _, f := range s.Folders {
		if f.Newest == nil || f.Next == nil || !f.Next.IsDiff() || f.Next.Base.Entry != f.Newest.Entry {
			t.Fatalf("%s: expected its full backup and a differential next: %+v", f.BackupName, f)
		}
	}
	run := s.Facts[s.Runs[0].RunID]
	if run.Backup == nil || run.Backup.Seconds != 3 || len(s.SetFacts) != 2 {
		t.Fatalf("expected the run's facts: %+v, %+v", run, s.SetFacts)
	}
	st := s.Storage
	if !st.Known || st.BackupBytes != s.Sets[0].SizeBytes+s.Sets[1].SizeBytes || st.FullEstimate != st.BackupBytes || st.FreeBytes <= 0 || st.TotalBytes < st.FreeBytes {
		t.Fatalf("unexpected storage: %+v", st)
	}
	k := s.Keys
	if !k.Exists || k.Methods == "" || k.Created.IsZero() || k.SpareYubiKey || k.RecoveryCode || k.YubiKeyConnected != nil || k.NewKeysReason != "" {
		t.Fatalf("unexpected keys: %+v", k)
	}
	if len(s.Retention) != 0 || s.Check.BlocksBackup() || !s.Checked.Equal(sc.Now) {
		t.Fatalf("unexpected snapshot: %+v", s)
	}

	// With retention, the next backups would remove nothing yet: each
	// folder has one chain, and a differential starts none.
	sc.Config.RetentionKeep = 1
	if s := snapshotOf(t, sc); len(s.Retention) != 0 {
		t.Fatalf("a differential next removes nothing: %+v", s.Retention)
	}
}

func TestSnapshotKeysAfterAConfigurationChange(t *testing.T) {
	t.Parallel()
	s := snapshotOf(t, scenario.Build(t, scenario.NewKeysNeeded))
	if s.Keys.NewKeysReason == "" || s.Notes[0].Detail == "" {
		t.Fatalf("expected the reason for new keys: %+v", s.Keys)
	}
	for _, f := range s.Folders {
		if f.Next == nil || f.Next.IsDiff() {
			t.Fatalf("new keys mean full backups: %+v", f.Next)
		}
	}
}

// The tests below replace package variables, so they do not run in
// parallel; parallel tests start only after them.

func TestSnapshotWarnsWhenAFullBackupDoesNotFit(t *testing.T) {
	sc := scenario.Build(t, scenario.Protected)
	defer func(f func(string) (uint64, uint64, error)) { queryDiskSpace = f }(queryDiskSpace)
	queryDiskSpace = func(string) (uint64, uint64, error) { return 1000, 1 << 40, nil }

	s := snapshotOf(t, sc)
	if s.State != StateWarning || len(s.Problems) != 1 || s.Problems[0].Code != interact.CodeSpaceLow || s.Problems[0].Bytes != s.Storage.FullEstimate {
		t.Fatalf("expected SPACE_LOW, got %+v", s.Problems)
	}
}

func TestSnapshotNotesADisconnectedYubiKey(t *testing.T) {
	sc := scenario.Build(t, scenario.Protected)
	defer func(f func() error) { checkYubiKeyConnected = f }(checkYubiKeyConnected)
	checkYubiKeyConnected = func() error { return errors.New("no YubiKey") }

	sc.Config.AuthenticationMode = 2
	s := snapshotOf(t, sc)
	// A YubiKey on the key ring is no warning; the keys also need new keys
	// now, which is a note too.
	if hasStatus(s.Problems, interact.StatusWarn) || s.Keys.YubiKeyConnected == nil || *s.Keys.YubiKeyConnected {
		t.Fatalf("a disconnected YubiKey is a note, not a warning: %+v", s.Problems)
	}
	found := false
	for _, n := range s.Notes {
		found = found || n.Code == interact.CodeYubiKeyNotConnected
	}
	if !found {
		t.Fatalf("expected the YubiKey note, got %v", codes(s.Notes))
	}
}

func TestEveryWarningAndErrorItemHasACode(t *testing.T) {
	for _, c := range scenario.All {
		sc := scenario.Build(t, c)
		for _, item := range inspect(sc.Config, "", sc.ConfigPath).items {
			if item.Severity != healthOK && item.Code == "" {
				t.Fatalf("%s: a finding without a code: %+v", c, item)
			}
		}
	}
}

func TestCheckerWaitsOnlyUntilTheDeadline(t *testing.T) {
	sc := scenario.Build(t, scenario.Protected)
	params := Params{Config: sc.Config, ConfigPath: sc.ConfigPath, Now: sc.Now}
	release := make(chan struct{})
	var started atomic.Int32
	c := &Checker{take: func(p Params) Snapshot {
		started.Add(1)
		<-release
		return TakeSnapshot(p)
	}}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	s := c.Snapshot(ctx, params)
	if s.State != StateError || len(s.Problems) != 1 || s.Problems[0].Code != interact.CodeBackupDirUnreachable || !s.Check.BlocksBackup() {
		t.Fatalf("a check past its deadline reports the backup directory unreachable: %+v", s)
	}

	// While the first check hangs, another caller waits for it instead of
	// starting a second one.
	ctx2, cancel2 := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel2()
	c.Snapshot(ctx2, params)
	if n := started.Load(); n != 1 {
		t.Fatalf("expected one running check, got %d", n)
	}

	close(release)
	if s := c.Snapshot(context.Background(), params); s.State != StateProtected {
		t.Fatalf("after the check finished: state %d, problems %v", s.State, codes(s.Problems))
	}
}
