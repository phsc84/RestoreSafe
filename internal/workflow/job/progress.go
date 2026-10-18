package job

import (
	"sync/atomic"
	"time"

	"github.com/phsc84/restoresafe/internal/fsx"
	"github.com/phsc84/restoresafe/internal/logging"
	"github.com/phsc84/restoresafe/internal/workflow/interact"
)

// StartProgressTracking launches a goroutine that logs the I/O progress of a
// stream at regular intervals; label names what Out counts ("encrypted").
// Pass log=nil to suppress output (the goroutine still runs so that the
// channels stay in sync). The returned stop function must be called exactly
// once; defer it at the call site.
func StartProgressTracking(log *logging.Logger, directoryName, label string, c fsx.StreamCounters) func() {
	s := streamProgress{log: log, name: directoryName, label: label, c: c}
	progressDone := make(chan struct{})
	progressStopped := make(chan struct{})
	go func() {
		s.untilDone(progressDone)
		close(progressStopped)
	}()
	return func() {
		close(progressDone)
		<-progressStopped
	}
}

const (
	progressLogInterval = 2 * time.Second
	stallWarnAfter      = 10 * time.Second
)

// streamProgress logs the I/O progress of one stream.
type streamProgress struct {
	log *logging.Logger
	// name is the backup directory; label names what c.Out counts.
	name, label string
	c           fsx.StreamCounters
}

// load returns the count of p, 0 for nil.
func load(p *atomic.Int64) int64 {
	if p == nil {
		return 0
	}
	return p.Load()
}

// logLine logs the counts at debug level; final marks the line at the end.
func (s streamProgress) logLine(final bool) {
	in, out, calls := load(s.c.In), load(s.c.Out), load(s.c.Calls)
	avgWriteKB := 0.0
	if calls > 0 {
		avgWriteKB = float64(out) / float64(calls) / 1024
	}
	suffix := ""
	if final {
		suffix = " final"
	}
	s.log.Debug("I/O progress [%s]%s: input=%.2f MB, %s=%.2f MB, writes=%d, avg write=%.2f KB", s.name, suffix, float64(in)/(1024*1024), s.label, float64(out)/(1024*1024), calls, avgWriteKB)
}

// untilDone logs the progress periodically, and warns once when it stalls,
// until done is closed.
func (s streamProgress) untilDone(done <-chan struct{}) {
	if s.log == nil {
		<-done
		return
	}

	ticker := time.NewTicker(progressLogInterval)
	defer ticker.Stop()

	lastIn := int64(-1)
	lastOut := int64(-1)
	lastCalls := int64(-1)
	var stalledSince time.Time
	stallWarned := false

	for {
		select {
		case <-done:
			s.logLine(true)
			return
		case <-ticker.C:
			currentIn, currentOut, currentCalls := load(s.c.In), load(s.c.Out), load(s.c.Calls)

			changed := currentIn != lastIn || currentOut != lastOut || currentCalls != lastCalls
			if changed {
				s.logLine(false)
				lastIn = currentIn
				lastOut = currentOut
				lastCalls = currentCalls
				stalledSince = time.Time{}
				stallWarned = false
				continue
			}

			if stalledSince.IsZero() {
				stalledSince = time.Now()
				continue
			}
			if !stallWarned && time.Since(stalledSince) >= stallWarnAfter {
				s.log.Warn("I/O appears stalled [%s]: input=%.2f MB, %s=%.2f MB, writes=%d unchanged for %.0f s. Remedy: Check source/destination drive or network availability and retry.", s.name, float64(currentIn)/(1024*1024), s.label, float64(currentOut)/(1024*1024), currentCalls, time.Since(stalledSince).Seconds())
				stallWarned = true
			}
		}
	}
}

// progressReportInterval is how often TrackProgress reports to the user.
const progressReportInterval = 250 * time.Millisecond

// TrackProgress reports p to rep with Done read from done every
// progressReportInterval, until the returned stop function is called; stop
// reports the final value. A nil rep reports nothing.
func TrackProgress(rep interact.ProgressReporter, p interact.Progress, done *atomic.Int64) (stop func()) {
	if rep == nil {
		return func() {}
	}
	report := func() {
		p.Done = done.Load()
		rep.Progress(p)
	}
	report()
	quit := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(progressReportInterval)
		defer ticker.Stop()
		for {
			select {
			case <-quit:
				return
			case <-ticker.C:
				report()
			}
		}
	}()
	return func() {
		close(quit)
		<-stopped
		report()
	}
}

// Stamp returns a reporter that sets phase and the position of the item
// (index of count, from 1) on every report before passing it to rep. A nil
// rep stays nil.
func Stamp(rep interact.ProgressReporter, phase interact.Phase, index, count int) interact.ProgressReporter {
	if rep == nil {
		return nil
	}
	return stamped{rep: rep, phase: phase, index: index, count: count}
}

type stamped struct {
	rep          interact.ProgressReporter
	phase        interact.Phase
	index, count int
}

func (s stamped) Progress(p interact.Progress) {
	p.Phase, p.Index, p.Count = s.phase, s.index, s.count
	s.rep.Progress(p)
}

// ReportPhase reports the start of a phase without items or byte counts,
// e.g. unlocking the keys. A nil rep reports nothing.
func ReportPhase(rep interact.ProgressReporter, phase interact.Phase, step string) {
	if rep != nil {
		rep.Progress(interact.Progress{Phase: phase, Step: step})
	}
}
