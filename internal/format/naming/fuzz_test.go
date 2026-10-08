package naming

import (
	"path/filepath"
	"testing"
)

// FuzzParsePartFileName checks that a parsed part name names the same part
// when written again.
func FuzzParsePartFileName(f *testing.F) {
	f.Add("[Docs]_ABC123_2026-03-15_FULL-001.enc")
	f.Add("[Docs]_ABC123_2026-03-15_DIFF002-003.enc")
	f.Add("[a]_b]_ABC123_2026-03-15_FULL-001.enc.tmp")
	f.Fuzz(func(t *testing.T, name string) {
		e, seq, ok := ParsePartFileName(name)
		if !ok {
			return
		}
		again := filepath.Base(PartFileName(`C:\backups`, e, seq))
		e2, seq2, ok := ParsePartFileName(again)
		if !ok || e2 != e || seq2 != seq {
			t.Fatalf("%q parsed as %+v part %d; written again as %q, which parses as %+v part %d (ok %v)", name, e, seq, again, e2, seq2, ok)
		}
	})
}

// FuzzParseLogFileName checks the same for log file names.
func FuzzParseLogFileName(f *testing.F) {
	f.Add("2026-03-15_ABC123.log")
	f.Fuzz(func(t *testing.T, name string) {
		date, id, ok := ParseLogFileName(name)
		if !ok {
			return
		}
		again := filepath.Base(LogFileName(`C:\backups`, date, id))
		date2, id2, ok := ParseLogFileName(again)
		if !ok || date2 != date || id2 != id {
			t.Fatalf("%q parsed as %s/%s; written again as %q, which parses as %s/%s (ok %v)", name, date, id, again, date2, id2, ok)
		}
	})
}
