package backup

import (
	"RestoreSafe/internal/logging"
	"os"
	"path/filepath"
	"sync/atomic"
)

func logPartSummary(parts []string, directoryName string, ioDiagnostics bool, outBytes, outWriteCalls *atomic.Int64, log *logging.Logger) {
	if ioDiagnostics {
		avgEncryptWriteKB := 0.0
		if calls := outWriteCalls.Load(); calls > 0 {
			avgEncryptWriteKB = float64(outBytes.Load()) / float64(calls) / 1024
		}
		log.Debug("I/O diagnostics [%s]: encrypt writes=%d, avg encrypt write=%.2f KB", directoryName, outWriteCalls.Load(), avgEncryptWriteKB)
		for i, p := range parts {
			fi, err := os.Stat(p)
			if err != nil {
				log.Warn("Failed to inspect part file %s: %v", filepath.Base(p), err)
				continue
			}
			log.Debug("  Part %03d size: %.2f MB", i+1, float64(fi.Size())/(1024*1024))
		}
	}
	log.Info("  Created: %d part file(s) - [%s] successfully backed up", len(parts), directoryName)
}
