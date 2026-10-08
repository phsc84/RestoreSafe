package flow

import (
	"RestoreSafe/internal/workflow/interact"
	"errors"
	"sync"
	"testing"
	"time"
)

// uiThread simulates the UI thread: it handles the bridge's notifications
// in order, like the window's message loop.
type uiThread struct {
	notes chan int
	b     *Bridge
}

func newUIThread() *uiThread {
	u := &uiThread{notes: make(chan int, 100)}
	u.b = NewBridge(func(kind int) { u.notes <- kind })
	return u
}

func (u *uiThread) next(t *testing.T) int {
	t.Helper()
	select {
	case k := <-u.notes:
		return k
	case <-time.After(5 * time.Second):
		t.Fatal("no notification")
		return -1
	}
}

func TestBridgeQuestionIsAnsweredOnce(t *testing.T) {
	t.Parallel()
	u := newUIThread()
	result := make(chan any, 1)
	go func() {
		v, _ := u.b.Ask(func(answer func(any, error)) {
			answer("first", nil)
			answer("second", nil) // ignored
		}, "cancelled", nil)
		result <- v
	}()
	if k := u.next(t); k != NoteQuestion {
		t.Fatalf("expected a question notification, got %d", k)
	}
	u.b.ShowNext()
	if v := <-result; v != "first" {
		t.Fatalf("got %v, want the first answer", v)
	}
}

func TestBridgeCloseAnswersShownAndLaterQuestions(t *testing.T) {
	t.Parallel()
	u := newUIThread()
	errs := make(chan error, 2)
	go func() {
		// Shown but never answered by the UI (e.g. buttons under the
		// preflight).
		_, err := u.b.Ask(func(func(any, error)) {}, nil, interact.ErrCancelled)
		errs <- err
		// Asked after the close: answered immediately.
		_, err = u.b.Ask(func(answer func(any, error)) { answer(nil, nil) }, nil, interact.ErrCancelled)
		errs <- err
	}()
	u.next(t)
	u.b.ShowNext()
	if !u.b.Close() {
		t.Fatal("close must report the shown question")
	}
	for i := 0; i < 2; i++ {
		if err := <-errs; !errors.Is(err, interact.ErrCancelled) {
			t.Fatalf("question %d: got %v, want the cancel answer", i, err)
		}
	}
}

func TestBridgeOutputKeepsOrderAndIsCoalesced(t *testing.T) {
	t.Parallel()
	u := newUIThread()
	for _, s := range []string{"one\n", "two\n", "Wrong password. 2 attempt(s) remaining.\n"} {
		u.b.Write([]byte(s)) //nolint:errcheck
	}
	if k := u.next(t); k != NoteOutput {
		t.Fatalf("expected an output notification, got %d", k)
	}
	if len(u.notes) != 0 {
		t.Fatalf("output must be posted once until taken, got %d more notifications", len(u.notes))
	}
	if got := u.b.TakeOutput(); got != "one\ntwo\nWrong password. 2 attempt(s) remaining.\n" {
		t.Fatalf("unexpected output %q", got)
	}
	u.b.Write([]byte("three\n")) //nolint:errcheck
	if k := u.next(t); k != NoteOutput {
		t.Fatal("output after takeOutput must be posted again")
	}
}

func TestBridgeProgressKeepsTheLatestReport(t *testing.T) {
	t.Parallel()
	u := newUIThread()
	var wg sync.WaitGroup
	for i := 1; i <= 50; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); u.b.Progress(interact.Progress{Done: int64(i)}) }()
	}
	wg.Wait()
	u.b.Progress(interact.Progress{Step: "Backing up", Done: 99, Total: 100})
	if k := u.next(t); k != NoteProgress {
		t.Fatalf("expected a progress notification, got %d", k)
	}
	if len(u.notes) != 0 {
		t.Fatalf("progress must be posted once until taken, got %d more", len(u.notes))
	}
	if p := u.b.TakeProgress(); p.Step != "Backing up" || p.Done != 99 {
		t.Fatalf("unexpected progress %+v", p)
	}
}
