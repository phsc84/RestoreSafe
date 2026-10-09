package backup

import (
	"os"
	"path/filepath"

	"github.com/phsc84/restoresafe/internal/format/setwriter"
)

// logPartSummary logs the parts of a set and, at debug level, how the
// encrypted data was written (counters) and the size of every part.
func (o *operation) logPartSummary(parts []string, directoryName string, counters setwriter.Counters) {
	if o.log.DebugEnabled() {
		avgEncryptWriteKB := 0.0
		if calls := counters.Calls.Load(); calls > 0 {
			avgEncryptWriteKB = float64(counters.Out.Load()) / float64(calls) / 1024
		}
		o.log.Debug("I/O diagnostics [%s]: encrypt writes=%d, avg encrypt write=%.2f KB", directoryName, counters.Calls.Load(), avgEncryptWriteKB)
		for i, p := range parts {
			fi, err := os.Stat(p)
			if err != nil {
				o.log.Warn("Failed to inspect part file %s: %v", filepath.Base(p), err)
				continue
			}
			o.log.Debug("  Part %03d size: %.2f MB", i+1, float64(fi.Size())/(1024*1024))
		}
	}
	o.log.Info("  Created: %d part file(s) - [%s] successfully backed up", len(parts), directoryName)
}
