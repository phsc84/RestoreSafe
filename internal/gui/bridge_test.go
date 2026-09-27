package gui

import (
	"RestoreSafe/internal/ui"
	"errors"
	"sync"
	"testing"
	"time"
)

// uiThread simulates the UI thread: it handles the bridge's notifications
// in order, like the window's message loop.
type uiThread struct {
	notes chan int
	b     *bridge
}

func newUIThread() *uiThread {
	u := &uiThread{notes: make(chan int, 100)}
	u.b = newBridge(func(kind int) { u.notes <- kind })
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
		v, _ := u.b.ask(func(answer func(any, error)) {
			answer("first", nil)
			answer("second", nil) // ignored
		}, "cancelled", nil)
		result <- v
	}()
	if k := u.next(t); k != noteQuestion {
		t.Fatalf("expected a question notification, got %d", k)
	}
	u.b.showNext()
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
		_, err := u.b.ask(func(func(any, error)) {}, nil, ui.ErrCancelled)
		errs <- err
		// Asked after the close: answered immediately.
		_, err = u.b.ask(func(answer func(any, error)) { answer(nil, nil) }, nil, ui.ErrCancelled)
		errs <- err
	}()
	u.next(t)
	u.b.showNext()
	if !u.b.close() {
		t.Fatal("close must report the shown question")
	}
	for i := 0; i < 2; i++ {
		if err := <-errs; !errors.Is(err, ui.ErrCancelled) {
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
	if k := u.next(t); k != noteOutput {
		t.Fatalf("expected an output notification, got %d", k)
	}
	if len(u.notes) != 0 {
		t.Fatalf("output must be posted once until taken, got %d more notifications", len(u.notes))
	}
	if got := u.b.takeOutput(); got != "one\ntwo\nWrong password. 2 attempt(s) remaining.\n" {
		t.Fatalf("unexpected output %q", got)
	}
	if _, last := u.b.outputMark(); last != "Wrong password. 2 attempt(s) remaining." {
		t.Fatalf("unexpected last line %q", last)
	}
	u.b.Write([]byte("three\n")) //nolint:errcheck
	if k := u.next(t); k != noteOutput {
		t.Fatal("output after takeOutput must be posted again")
	}
}

func TestBridgeProgressKeepsTheLatestReport(t *testing.T) {
	t.Parallel()
	u := newUIThread()
	var wg sync.WaitGroup
	for i := 1; i <= 50; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); u.b.Progress(ui.Progress{Done: int64(i)}) }()
	}
	wg.Wait()
	u.b.Progress(ui.Progress{Step: "Backing up", Done: 99, Total: 100})
	if k := u.next(t); k != noteProgress {
		t.Fatalf("expected a progress notification, got %d", k)
	}
	if len(u.notes) != 0 {
		t.Fatalf("progress must be posted once until taken, got %d more", len(u.notes))
	}
	if p := u.b.takeProgress(); p.Step != "Backing up" || p.Done != 99 {
		t.Fatalf("unexpected progress %+v", p)
	}
}
