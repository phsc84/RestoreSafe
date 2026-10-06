package flow

import (
	"RestoreSafe/internal/workflow/interact"
	"strings"
	"sync"
)

// Notifications the bridge posts to the window. The window handles them on
// the UI thread.
const (
	NoteQuestion = iota
	NoteOutput
	NoteProgress
)

// Bridge connects the worker goroutine running a workflow with the UI
// thread (docs/SPEC-gui.md, section 12.2). Questions block the
// worker until the UI thread answers them; output and progress never block
// and are coalesced, so the window is posted at most one message of each
// kind at a time.
type Bridge struct {
	notify func(kind int)

	mu         sync.Mutex
	queue      []*question
	current    *question // shown, not yet answered
	closed     bool
	output     strings.Builder
	outPosted  bool
	outSeq     int
	lastLine   string
	progress   interact.Progress
	progPosted bool
	result     *interact.Result
	logPath    string
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

// Write implements io.Writer for interact.UI.Output. It never blocks on the UI.
func (b *Bridge) Write(p []byte) (int, error) {
	b.mu.Lock()
	b.output.Write(p)
	b.outSeq++
	for _, line := range strings.Split(string(p), "\n") {
		if l := strings.TrimSpace(line); l != "" {
			b.lastLine = l
		}
	}
	post := !b.outPosted
	b.outPosted = true
	b.mu.Unlock()
	if post {
		b.notify(NoteOutput)
	}
	return len(p), nil
}

// TakeOutput returns the output written since the last call.
func (b *Bridge) TakeOutput() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.output.String()
	b.output.Reset()
	b.outPosted = false
	return s
}

// OutputMark returns a mark of the output so far and its last non-empty
// line, to tell whether something was written since an earlier mark.
func (b *Bridge) OutputMark() (seq int, lastLine string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.outSeq, b.lastLine
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
