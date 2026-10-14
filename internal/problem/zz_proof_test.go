package problem

import "testing"

func TestProofRace(t *testing.T) {
	n := 0
	done := make(chan struct{})
	go func() {
		n++
		close(done)
	}()
	n++
	<-done
	_ = n
}
