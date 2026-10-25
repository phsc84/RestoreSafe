package backup

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/phsc84/restoresafe/internal/format/catalog"
	"github.com/phsc84/restoresafe/internal/format/container"
	"github.com/phsc84/restoresafe/internal/logging"
	"github.com/phsc84/restoresafe/internal/testutil"
	"github.com/phsc84/restoresafe/internal/workflow/interact"
	"github.com/phsc84/restoresafe/internal/workflow/interact/interacttest"
	"github.com/phsc84/restoresafe/internal/workflow/plan"
	"github.com/phsc84/restoresafe/internal/workflow/unlock"
)

// refusals returns the YubiKey refusal questions ui was asked.
func refusals(ui *interacttest.Script) []interact.YubiKeyRefusal {
	var qs []interact.YubiKeyRefusal
	for _, q := range ui.Questions {
		if q, ok := q.(interact.YubiKeyRefusal); ok {
			qs = append(qs, q)
		}
	}
	return qs
}

var errRefused = &unlock.YubiKeyError{Err: errors.New("YubiKey authentication failed: HRESULT 0x800704C7")}

// stubUnlock makes unlocking the existing keys fail with each error of errs
// in turn, then succeed with master. Not safe for parallel use.
func stubUnlock(t *testing.T, master []byte, errs ...error) *int {
	t.Helper()
	prev := unlockKeySetFn
	t.Cleanup(func() { unlockKeySetFn = prev })
	calls := 0
	unlockKeySetFn = func(interact.UI, *container.KeySet, unlock.Options, *logging.Logger) ([]byte, error) {
		calls++
		if len(errs) > 0 {
			err := errs[0]
			errs = errs[1:]
			return nil, err
		}
		return append([]byte(nil), master...), nil
	}
	return &calls
}

func runWithAnswers(t *testing.T, env *previewEnv, starts []interact.BackupStart, refusals ...interact.YubiKeyRefusalAnswer) (*interacttest.Script, error) {
	t.Helper()
	cfg := *env.cfg
	cfg.BackupDirectory = env.dir
	ui := &interacttest.Script{Starts: starts, Refusals: refusals}
	return ui, Run(context.Background(), ui, &cfg, "")
}

func TestRunTriesTheYubiKeyAgain(t *testing.T) {
	env := newPreviewEnv(t, 0, 0)
	env.writeLargeFull(t, "AAA001", "2026-09-01")
	calls := stubUnlock(t, env.master, errRefused)

	ui, err := runWithAnswers(t, env, []interact.BackupStart{interact.BackupAsPlanned}, interact.RefusalRetry)
	if err != nil {
		t.Fatalf("backup after trying again: %v", err)
	}
	if *calls != 2 || len(refusals(ui)) != 1 || len(ui.BackupPlans) != 1 {
		t.Fatalf("unlocks=%d refusals=%d plans=%d, want 2, 1, 1", *calls, len(refusals(ui)), len(ui.BackupPlans))
	}
	infos, _ := catalog.Inventory(env.dir)
	if len(infos) != 2 || !infos[0].Entry.IsDiff() && !infos[1].Entry.IsDiff() {
		t.Fatalf("expected the full backup and a differential, got %d sets", len(infos))
	}
}

func TestRunShowsThePlanWithNewKeysAfterARefusal(t *testing.T) {
	env := newPreviewEnv(t, 0, 0)
	env.writeLargeFull(t, "AAA001", "2026-09-01")
	stubUnlock(t, env.master, errRefused)

	// Start, new keys, then cancel the plan with new keys.
	ui, err := runWithAnswers(t, env, []interact.BackupStart{interact.BackupAsPlanned, interact.BackupCancel}, interact.RefusalNewKeys)
	if err != nil {
		t.Fatalf("cancelling the new plan: %v", err)
	}
	if len(ui.BackupPlans) != 2 {
		t.Fatalf("%d plans shown, want 2", len(ui.BackupPlans))
	}
	first, second := ui.BackupPlans[0], ui.BackupPlans[1]
	if first.Keys.New || !first.Folders[0].Differential {
		t.Fatalf("the first plan should reuse the keys: %+v", first.Keys)
	}
	if !second.Keys.New || second.Keys.NewKeysReason != yubiKeyRefusedReason || second.Folders[0].Differential {
		t.Fatalf("the second plan should create new keys and back up in full: %+v", second.Keys)
	}
	if log, err := os.ReadFile(ui.LogPath); err != nil || !strings.Contains(string(log), "Backup cancelled by user before start") {
		t.Fatalf("the cancelled plan is not logged: %v", err)
	}
}

func TestRunEndsWithTheYubiKeyErrorWhenCancelled(t *testing.T) {
	env := newPreviewEnv(t, 0, 0)
	env.writeLargeFull(t, "AAA001", "2026-09-01")
	stubUnlock(t, env.master, errRefused)

	_, err := runWithAnswers(t, env, []interact.BackupStart{interact.BackupAsPlanned}, interact.RefusalCancel)
	if !errors.Is(err, errRefused) {
		t.Fatalf("got %v, want the YubiKey error", err)
	}
}

func TestAskAfterYubiKeyRefusal(t *testing.T) {
	ks, master := testutil.NewPasswordKeySet(t, []byte("pw"))
	var log *logging.Logger
	ask := func(keys plan.Keys, err error) (*interacttest.Script, interact.YubiKeyRefusalAnswer) {
		ui := &interacttest.Script{Refusals: []interact.YubiKeyRefusalAnswer{interact.RefusalNewKeys}}
		return ui, askAfterYubiKeyRefusal(ui, keys, err, log)
	}

	if ui, a := ask(plan.Keys{Existing: ks}, errors.New("disk full")); a != interact.RefusalCancel || len(refusals(ui)) != 0 {
		t.Fatal("another failure must not ask about the YubiKey")
	}
	if ui, a := ask(plan.Keys{}, errRefused); a != interact.RefusalCancel || len(refusals(ui)) != 0 {
		t.Fatal("new keys must not ask about the YubiKey")
	}
	ui, a := ask(plan.Keys{Existing: ks}, errRefused)
	if a != interact.RefusalNewKeys || len(refusals(ui)) != 1 || refusals(ui)[0].RecoveryCode {
		t.Fatalf("answer=%v refusals=%+v, want new keys and no recovery code", a, refusals(ui))
	}

	if err := ks.AddSlot(master, container.SlotRecovery, "Recovery code", []byte("code"), testutil.FastArgon2, nil, "check"); err != nil {
		t.Fatal(err)
	}
	if ui, _ := ask(plan.Keys{Existing: ks}, errRefused); len(refusals(ui)) != 1 || !refusals(ui)[0].RecoveryCode {
		t.Fatalf("refusals=%+v, want the recovery code", refusals(ui))
	}
}
