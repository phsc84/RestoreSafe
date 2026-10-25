package flow

import (
	"sync"

	"github.com/phsc84/restoresafe/internal/workflow/interact"
)

// Notifications the bridge posts to the window. The window handles them on
// the UI thread.
const (
	NoteQuestion = iota
	NoteProgress
)

// Bridge connects the worker goroutine running a workflow with the UI
// thread (docs/SPEC-gui.md, section 12.2). Questions block the
// worker until the UI thread answers them; progress never blocks and is
// coalesced, so the window is posted at most one progress message at a time.
type Bridge struct {
	notify func(kind int)

	mu         sync.Mutex
	queue      []*question
	current    *question // shown, not yet answered
	closed     bool
	progress   interact.Progress
	progPosted bool
	result     *interact.Result
	logPath    string
	logFailure *interact.LogFailure
}

type answer struct {
	value any
	err   error
}

// question is one question of the worker. show runs on the UI thread and
// must lead to exactly one call of the answer function it receives (later
// calls are ignored). When the bridge is closed, the question gets the
// cancel answer instead.
type question struct {
	show   func(answer func(value any, err error))
	cancel answer
	reply  chan answer
	once   sync.Once
}

func NewBridge(notify func(kind int)) *Bridge {
	return &Bridge{notify: notify}
}

func (q *question) answer(a answer) {
	q.once.Do(func() { q.reply <- a })
}

// Ask queues a question for the UI thread and waits for the answer. On a
// closed bridge it returns the cancel answer immediately.
func (b *Bridge) Ask(show func(answer func(any, error)), cancelValue any, cancelErr error) (any, error) {
	q := &question{show: show, cancel: answer{cancelValue, cancelErr}, reply: make(chan answer, 1)}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return cancelValue, cancelErr
	}
	b.queue = append(b.queue, q)
	b.mu.Unlock()
	b.notify(NoteQuestion)
	a := <-q.reply
	return a.value, a.err
}

// ShowNext shows the next queued question; it runs on the UI thread.
func (b *Bridge) ShowNext() {
	b.mu.Lock()
	if len(b.queue) == 0 || b.current != nil {
		b.mu.Unlock()
		return
	}
	q := b.queue[0]
	b.queue = b.queue[1:]
	b.current = q
	b.mu.Unlock()

	q.show(func(value any, err error) {
		b.mu.Lock()
		if b.current == q {
			b.current = nil
		}
		more := len(b.queue) > 0
		b.mu.Unlock()
		q.answer(answer{value, err})
		if more {
			b.notify(NoteQuestion)
		}
	})
}

// Close answers the shown and all queued questions with their cancel
// answers and makes later questions return theirs immediately. It reports
// whether a question was being shown, so the UI can take it down.
func (b *Bridge) Close() (hadCurrent bool) {
	b.mu.Lock()
	b.closed = true
	pending := b.queue
	b.queue = nil
	current := b.current
	b.current = nil
	b.mu.Unlock()
	if current != nil {
		current.answer(current.cancel)
	}
	for _, q := range pending {
		q.answer(q.cancel)
	}
	return current != nil
}

// Progress implements interact.ProgressReporter; it keeps only the latest report.
func (b *Bridge) Progress(p interact.Progress) {
	b.mu.Lock()
	b.progress = p
	post := !b.progPosted
	b.progPosted = true
	b.mu.Unlock()
	if post {
		b.notify(NoteProgress)
	}
}

// TakeProgress returns the latest progress report.
func (b *Bridge) TakeProgress() interact.Progress {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.progPosted = false
	return b.progress
}

// SetResult records the result of a completed operation (interact.UI.ShowResult).
func (b *Bridge) SetResult(r interact.Result) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.result = &r
}

// FinalResult returns the recorded result, or nil.
func (b *Bridge) FinalResult() *interact.Result {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.result
}

// SetLogPath records the log file of the operation (interact.UI.LogStarted).
func (b *Bridge) SetLogPath(path string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.logPath = path
}

// LogPath returns the recorded log file, or "".
func (b *Bridge) LogPath() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.logPath
}

// SetLogFailure records a problem with the log file (interact.UI.LogFailed).
// The first one stays: a log that could not be opened has nothing to close.
func (b *Bridge) SetLogFailure(f interact.LogFailure) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.logFailure == nil {
		b.logFailure = &f
	}
}

// LogFailure returns the recorded problem with the log file, or nil.
func (b *Bridge) LogFailure() *interact.LogFailure {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.logFailure
}

// RunLog returns the file that holds the operation's log lines: the copy in
// TEMP when they could not be added to the log file, else the log file;
// "" when there is none.
func (b *Bridge) RunLog() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if f := b.logFailure; f != nil && f.Kind == interact.LogKept {
		return f.Kept
	}
	return b.logPath
}
