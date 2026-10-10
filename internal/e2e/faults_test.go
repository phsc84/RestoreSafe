package e2e

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phsc84/restoresafe/internal/config"
	"github.com/phsc84/restoresafe/internal/format/catalog"
	"github.com/phsc84/restoresafe/internal/format/container"
	"github.com/phsc84/restoresafe/internal/testutil"
	"github.com/phsc84/restoresafe/internal/workflow/backup"
	"github.com/phsc84/restoresafe/internal/workflow/restore"
	"github.com/phsc84/restoresafe/internal/workflow/verify"
)

// damagedSet is a backup of one folder in three parts of 1 MB, before any
// fault: the configuration, the set's parts, and its trailer.
type damagedSet struct {
	cfg     *config.Config
	parts   []string
	trailer container.Trailer
	split   int64
}

func backUpThreeParts(t *testing.T) damagedSet {
	t.Helper()
	root := t.TempDir()
	docs := filepath.Join(root, "Documents")
	writeFile(t, filepath.Join(docs, "letter.txt"), "Dear RestoreSafe")
	writeFile(t, filepath.Join(docs, "big.bin"), strings.Repeat("restoresafe", 230_000)) // 2.5 MB
	cfg := &config.Config{
		SourceDirectories:  []string{docs},
		BackupDirectory:    filepath.Join(root, "Backups"),
		SplitSizeMB:        1,
		LogLevel:           "info",
		AuthenticationMode: config.AuthModePassword,
		Argon2:             testutil.FastArgon2Config,
		Differential:       fullBackupsOnly,
	}
	s := useScript(t, []string{"y"}, password, password)
	if err := backup.Run(context.Background(), s.ui, cfg, ""); err != nil {
		t.Fatalf("backup: %v", err)
	}
	infos, err := catalog.Inventory(cfg.BackupDirectory)
	if err != nil || len(infos) != 1 || !infos[0].Complete() || len(infos[0].Parts) != 3 {
		t.Fatalf("expected one complete set of 3 parts: %+v, %v", infos, err)
	}
	return damagedSet{cfg: cfg, parts: infos[0].Parts, trailer: infos[0].Trailer, split: 1 << 20}
}

// copyTo copies the backup directory into a new one and returns the
// configuration and part paths for the copy.
func (d damagedSet) copyTo(t *testing.T) (*config.Config, []string) {
	t.Helper()
	dir := t.TempDir()
	entries, err := os.ReadDir(d.cfg.BackupDirectory)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(d.cfg.BackupDirectory, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, e.Name()), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cfg := *d.cfg
	cfg.BackupDirectory = dir
	parts := make([]string, len(d.parts))
	for i, p := range d.parts {
		parts[i] = filepath.Join(dir, filepath.Base(p))
	}
	return &cfg, parts
}

// flipBit flips the lowest bit at offset off of the set, counted over all parts.
func (d damagedSet) flipBit(t *testing.T, parts []string, off int64) {
	t.Helper()
	p := parts[off/d.split]
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	data[off%d.split] ^= 0x01
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// headerValueOffset returns the offset of the last digit of the header's
// created_utc: flipping its lowest bit keeps a valid digit, so the header
// stays valid JSON and only authentication can notice.
func headerValueOffset(t *testing.T, part string) int64 {
	t.Helper()
	data, err := os.ReadFile(part)
	if err != nil {
		t.Fatal(err)
	}
	key := []byte(`"created_utc":"`)
	start := bytes.Index(data, key)
	end := bytes.IndexByte(data[start+len(key):], 'Z')
	if start < 0 || end < 1 {
		t.Fatal("created_utc not found in the header")
	}
	return int64(start + len(key) + end - 1)
}

// TestDamagedSetIsRefused is the corruption part of the fault-injection
// matrix (doc.go): a part deleted or truncated, and one bit flipped in each
// region of the set. Verify and restore both fail with the cause. A fault
// found before reading (preflight) leaves nothing behind; a fault found while
// reading leaves the partial restore in place, marked INCOMPLETE (SPEC-core
// section 9).
func TestDamagedSetIsRefused(t *testing.T) {
	d := backUpThreeParts(t)
	tr := d.trailer
	total := tr.ManifestOffset + tr.ManifestLength + container.TrailerLen
	flip := func(off func(parts []string) int64) func(t *testing.T, parts []string) {
		return func(t *testing.T, parts []string) { d.flipBit(t, parts, off(parts)) }
	}
	at := func(off int64) func([]string) int64 { return func([]string) int64 { return off } }
	faults := []struct {
		name      string
		damage    func(t *testing.T, parts []string)
		want      string
		preflight bool
	}{
		{"part deleted", func(t *testing.T, parts []string) {
			if err := os.Remove(parts[1]); err != nil {
				t.Fatal(err)
			}
		}, "Missing part file 002", true},
		{"part truncated", func(t *testing.T, parts []string) {
			fi, err := os.Stat(parts[2])
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Truncate(parts[2], fi.Size()-10); err != nil {
				t.Fatal(err)
			}
		}, "trailer not found", true},
		{"bit in header syntax", flip(at(40)), "Invalid backup header", true},
		{"bit in header value", flip(func(parts []string) int64 { return headerValueOffset(t, parts[0]) }), "failed authentication", false},
		{"bit in data", flip(at(tr.DataOffset + tr.DataLength/2)), "failed authentication (corrupted or modified) in the data section", false},
		{"bit in manifest", flip(at(tr.ManifestOffset + tr.ManifestLength/2)), "failed authentication (corrupted or modified) in the manifest section", false},
		{"bit in trailer", flip(at(total - container.TrailerLen + 20)), "trailer checksum mismatch", true},
	}
	for _, f := range faults {
		t.Run(f.name, func(t *testing.T) {
			cfg, parts := d.copyTo(t)
			sets := newestRun(t, d.cfg)
			f.damage(t, parts)

			s := useScript(t, []string{"y"}, password)
			err := verify.Run(context.Background(), s.ui, cfg, "", verify.Request{Sets: sets})
			if err == nil || !strings.Contains(err.Error(), f.want) {
				t.Errorf("verify: got %v, want %q", err, f.want)
			}

			dest := filepath.Join(t.TempDir(), "Restore")
			s = useScript(t, []string{"y"}, password)
			err = restore.Run(context.Background(), s.ui, cfg, "", restore.Request{Sets: sets, Destination: dest})
			if err == nil || !strings.Contains(err.Error(), f.want) {
				t.Errorf("restore: got %v, want %q", err, f.want)
			}
			_, statErr := os.Stat(filepath.Join(dest, "Documents"))
			switch {
			case f.preflight && !os.IsNotExist(statErr):
				t.Error("a set refused in the preflight must leave nothing behind")
			case !f.preflight && !strings.Contains(s.out.String(), "INCOMPLETE"):
				t.Errorf("a partial restore must be marked INCOMPLETE: %s", s.out.String())
			}
		})
	}
}
