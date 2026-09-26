package ui

// Progress describes the step a running operation is working on.
type Progress struct {
	// Step is what is being done, e.g. "Backing up", "Restoring",
	// "Verifying", "Copying to local staging", "Moving to backup directory".
	Step string
	// Item is the backup directory the step works on.
	Item string
	// Done and Total count bytes. Total is an estimate and 0 when unknown;
	// Done can end slightly below or above it.
	Done, Total int64
}

// Fraction returns Done/Total clamped to [0, 1], or -1 when Total is
// unknown.
func (p Progress) Fraction() float64 {
	if p.Total <= 0 {
		return -1
	}
	f := float64(p.Done) / float64(p.Total)
	switch {
	case f < 0:
		return 0
	case f > 1:
		return 1
	}
	return f
}

// ProgressReporter receives the progress of running operations.
type ProgressReporter interface {
	// Progress is called from a background goroutine several times per
	// second while a step runs, and once when it ends. It must return
	// quickly.
	Progress(p Progress)
}
