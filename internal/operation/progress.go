package operation

import (
	"RestoreSafe/internal/util"
	"sync/atomic"
	"time"
)

const (
	progressLogInterval = 2 * time.Second
	stallWarnAfter      = 10 * time.Second
)

// LogStreamProgress formats and logs stream I/O progress for backup/restore operations.
func LogStreamProgress(log *util.Logger, folderName, processedLabel string, inBytes, outBytes, outWriteCalls *atomic.Int64, isFinal bool) {
	inMB := float64(inBytes.Load()) / (1024 * 1024)
	outMB := float64(outBytes.Load()) / (1024 * 1024)
	calls := outWriteCalls.Load()
	avgWriteKB := 0.0
	if calls > 0 {
		avgWriteKB = float64(outBytes.Load()) / float64(calls) / 1024
	}

	suffix := ""
	if isFinal {
		suffix = " final"
	}

	log.Debug("I/O progress [%s]%s: input=%.2f MB, %s=%.2f MB, writes=%d, avg write=%.2f KB", folderName, suffix, inMB, processedLabel, outMB, calls, avgWriteKB)
}

// LogProgressUntilDone periodically logs stream progress until done is closed.
func LogProgressUntilDone(log *util.Logger, folderName, processedLabel string, inBytes, outBytes, outWriteCalls *atomic.Int64, done <-chan struct{}) {
	if log == nil {
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
			LogStreamProgress(log, folderName, processedLabel, inBytes, outBytes, outWriteCalls, true)
			return
		case <-ticker.C:
			currentIn := inBytes.Load()
			currentOut := outBytes.Load()
			currentCalls := outWriteCalls.Load()

			changed := currentIn != lastIn || currentOut != lastOut || currentCalls != lastCalls
			if changed {
				LogStreamProgress(log, folderName, processedLabel, inBytes, outBytes, outWriteCalls, false)
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
				log.Warn("I/O appears stalled [%s]: input=%.2f MB, %s=%.2f MB, writes=%d unchanged for %.0f s. Remedy: Check source/destination drive or network availability and retry.", folderName, float64(currentIn)/(1024*1024), processedLabel, float64(currentOut)/(1024*1024), currentCalls, time.Since(stalledSince).Seconds())
				stallWarned = true
			}
		}
	}
}
