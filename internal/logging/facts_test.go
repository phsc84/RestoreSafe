package logging_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/phsc84/restoresafe/internal/logging"
)

func TestFactsAreWrittenToTheFileOnlyAndReadBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "2026-09-30_FACT01.log")
	var console bytes.Buffer
	log, err := logging.NewLogger(path, "info", &console)
	if err != nil {
		t.Fatal(err)
	}
	before := time.Now().Add(-time.Second)
	log.Info("Backup started")
	log.Fact(logging.Fact{Kind: logging.FactVerify, Result: logging.ResultFailed, Set: "My Docs_ABC123_2026-09-30_FULL", Error: "checksum mismatch"})
	log.Fact(logging.Fact{Kind: logging.FactVerify, Result: logging.ResultOK, Set: "My Docs_ABC123_2026-09-30_FULL"})
	log.Fact(logging.Fact{Kind: logging.FactVerify, Result: logging.ResultOK, Set: "Pics_ABC123_2026-09-30_FULL"})
	log.Fact(logging.Fact{Kind: logging.FactBackup, Result: logging.ResultWarnings, Warnings: 2, Seconds: 252})
	log.Fact(logging.Fact{Kind: logging.FactSet, Result: logging.ResultOK, Set: "Pics_ABC123_2026-09-30_FULL", Bytes: 4096})
	log.Fact(logging.Fact{Kind: logging.FactCleanup, Result: logging.ResultOK, Removed: 3, Bytes: 1 << 30})
	log.Fact(logging.Fact{Kind: logging.FactRestore, Result: logging.ResultWarnings, Set: "My Docs_ABC123_2026-09-30_DIFF001", Skipped: 1, Stale: 2})
	log.Close()

	if strings.Contains(console.String(), "FACT") {
		t.Fatalf("facts must not reach the user's output: %q", console.String())
	}
	facts, err := logging.ReadFacts(path)
	if err != nil {
		t.Fatal(err)
	}
	b := facts.Backup
	if b == nil || b.Result != logging.ResultWarnings || b.Warnings != 2 || b.Seconds != 252 || b.Time.Before(before) {
		t.Fatalf("unexpected backup fact: %+v", b)
	}
	// The newer verification of a set replaces the older one.
	if v := facts.Verify["My Docs_ABC123_2026-09-30_FULL"]; v.Result != logging.ResultOK || v.Error != "" {
		t.Fatalf("expected the newest verify fact, got %+v", v)
	}
	if len(facts.Verify) != 2 {
		t.Fatalf("expected facts for 2 sets, got %+v", facts.Verify)
	}
	if s := facts.Sets["Pics_ABC123_2026-09-30_FULL"]; s.Bytes != 4096 {
		t.Fatalf("unexpected set fact: %+v", s)
	}
	if c := facts.Cleanup; c == nil || c.Removed != 3 || c.Bytes != 1<<30 {
		t.Fatalf("unexpected cleanup fact: %+v", c)
	}
	if r := facts.Restored["My Docs_ABC123_2026-09-30_DIFF001"]; r.Skipped != 1 || r.Stale != 2 || r.Unread() != 3 {
		t.Fatalf("unexpected restore fact: %+v", r)
	}
}

func TestReadFactsSkipsWhatItCannotParse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.log")
	content := strings.Join([]string{
		"[2026-09-30 09:12:03] INFO  - RestoreSafe v2.0.0",
		"[2026-09-30 09:12:04] FACT  - {not json",
		"[2026-09-30 09:12:05] FACT  - {\"result\":\"ok\"}",
		"FACT  - {\"kind\":\"backup\",\"result\":\"ok\"}",
		"[garbage",
		"",
		"[2026-09-30 09:16:02] FACT  - {\"kind\":\"backup\",\"result\":\"ok\",\"seconds\":4,\"future_field\":1}",
	}, "\n")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	facts, err := logging.ReadFacts(path)
	if err != nil {
		t.Fatal(err)
	}
	if facts.Backup == nil || facts.Backup.Seconds != 4 || facts.Backup.Time.Format("15:04:05") != "09:16:02" {
		t.Fatalf("expected only the last, valid fact: %+v", facts.Backup)
	}

	if _, err := logging.ReadFacts(filepath.Join(t.TempDir(), "missing.log")); err == nil {
		t.Fatal("a log that cannot be read is an error")
	}
}
