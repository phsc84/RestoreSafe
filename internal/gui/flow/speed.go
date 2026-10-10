package flow

import "time"

// Speed measures the rate of a folder from its progress reports (GUI spec
// BR-3): the bytes done over the last few seconds.
type Speed struct {
	samples []sample
}

type sample struct {
	at   time.Time
	done int64
}

// speedWindow is the span the rate is measured over.
const speedWindow = 5 * time.Second

// Add records done bytes at time at. Done going back starts over.
func (s *Speed) Add(at time.Time, done int64) {
	if n := len(s.samples); n > 0 && done < s.samples[n-1].done {
		*s = Speed{}
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
