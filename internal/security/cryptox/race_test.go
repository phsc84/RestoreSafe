//go:build race

package cryptox

// raceEnabled reports whether the tests run with the race detector, whose
// instrumentation allocates and makes allocation counts meaningless.
const raceEnabled = true
