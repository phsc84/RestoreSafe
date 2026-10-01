package flow

import "time"

// Speed estimates the rate of a step from its progress reports (spec BR-3):
// the bytes done over the last few seconds, and the time left once the
// step has run long enough for the estimate to settle.
type Speed struct {
	start   time.Time
	samples []sample
}

type sample struct {
	at   time.Time
	done int64
}

const (
	// speedWindow is the span the rate is measured over.
	speedWindow = 5 * time.Second
	// settleTime is how long a step runs before its time left is shown.
	settleTime = 10 * time.Second
)

// Add records done bytes at time at. Done going back starts over.
func (s *Speed) Add(at time.Time, done int64) {
	if n := len(s.samples); n > 0 && done < s.samples[n-1].done {
		*s = Speed{}
	}
	if s.start.IsZero() {
		s.start = at
	}
	s.samples = append(s.samples, sample{at, done})
	cut := 0
	for cut < len(s.samples)-2 && at.Sub(s.samples[cut+1].at) >= speedWindow {
		cut++
	}
	s.samples = s.samples[cut:]
}

// Rate returns bytes per second over the last few seconds, 0 without enough
// samples.
func (s *Speed) Rate() float64 {
	if len(s.samples) < 2 {
		return 0
	}
	first, last := s.samples[0], s.samples[len(s.samples)-1]
	span := last.at.Sub(first.at).Seconds()
	if span <= 0 {
		return 0
	}
	return float64(last.done-first.done) / span
}

// Left returns the time left until total is done, when the step has run for
// settleTime, the total is known and bytes are moving.
func (s *Speed) Left(total int64, now time.Time) (time.Duration, bool) {
	rate := s.Rate()
	if total <= 0 || rate <= 0 || s.start.IsZero() || now.Sub(s.start) < settleTime || len(s.samples) == 0 {
		return 0, false
	}
	remaining := total - s.samples[len(s.samples)-1].done
	if remaining <= 0 {
		return 0, true
	}
	return time.Duration(float64(remaining) / rate * float64(time.Second)), true
}
