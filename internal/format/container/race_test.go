//go:build race

package container

// raceEnabled reports whether the tests run with the race detector, which
// makes the memory test slow and its heap figures meaningless.
const raceEnabled = true
