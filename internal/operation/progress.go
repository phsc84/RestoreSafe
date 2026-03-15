package operation

import (
	"RestoreSafe/internal/util"
	"sync/atomic"
	"time"
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

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			LogStreamProgress(log, folderName, processedLabel, inBytes, outBytes, outWriteCalls, true)
			return
		case <-ticker.C:
			LogStreamProgress(log, folderName, processedLabel, inBytes, outBytes, outWriteCalls, false)
		}
	}
}
