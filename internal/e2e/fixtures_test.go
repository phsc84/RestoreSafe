package e2e

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"math/rand/v2"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/phsc84/restoresafe/internal/config"
	"github.com/phsc84/restoresafe/internal/format/catalog"
	"github.com/phsc84/restoresafe/internal/testutil"
	"github.com/phsc84/restoresafe/internal/testutil/filelock"
	"github.com/phsc84/restoresafe/internal/workflow/restore"

	"golang.org/x/sys/windows"
)

// The format fixtures (refactoring 2.0 RF-41) are backups written by
// RestoreSafe 2.0.0, committed to internal/format/testdata/v2.0.0. Every later
// 2.x build must restore them to exactly the recorded listings (core spec: the
// format of a major version is frozen). They are never changed after 2.0.0 is
// released; a later version adds fixtures of its own next to them.
//
// To write them (once, before the release; refused if they exist):
//
//	$env:RESTORESAFE_WRITE_FIXTURES = "1"; go test -run TestWriteFormatFixtures ./internal/e2e
const (
	fixtureDir      = "../format/testdata/v2.0.0"
	fixturePassword = "fixture password 2.0.0"
)

// fixtureCase is one backup directory of the fixtures.
type fixtureCase struct {
	name         string // folder below fixtureDir
	recoveryCode bool   // keys with a recovery slot; the test unlocks with the code
}

var fixtureCases = []fixtureCase{{name: "password"}, {name: "recovery", recoveryCode: true}}

// Fixed times of the source tree; restore must bring them back exactly.
var (
	fixtureCreated  = time.Date(2018, 1, 2, 3, 4, 5, 600_000_000, time.UTC)
	fixtureModified = time.Date(2019, 5, 6, 7, 8, 9, 100_000_000, time.UTC)
)

// longName is a 120-character file name; inside two 100-character folders
// the path is longer than MAX_PATH (260) wherever it is restored.
var longName = strings.Repeat("n", 116) + ".txt"

// writeSourceTree writes the special cases the format must keep: an empty
// file and folder, Unicode and long names, read-only, hidden and system
// attributes, a file over two parts, and fixed creation and modification
// times.
func writeSourceTree(t *testing.T, docs string) {
	t.Helper()
	deep := filepath.Join("deep", strings.Repeat("d", 100), strings.Repeat("e", 100), longName)
	big := make([]byte, 1_200_000) // more than the 1 MB split size: two parts
	r := rand.New(rand.NewPCG(2, 0))
	for i := range big {
		big[i] = byte(r.Uint32())
	}
	files := map[string][]byte{
		"letter.txt":               []byte("Dear RestoreSafe"),
		"empty.txt":                nil,
		"Ünïcødé ✓ 日本語.txt":        []byte("unicode name"),
		"readonly.txt":             []byte("read-only"),
		"hidden.txt":               []byte("hidden"),
		"system.txt":               []byte("system"),
		"Hidden folder/inside.txt": []byte("in a hidden folder"),
		"busy.db":                  []byte("database, version 1"),
		"deleteme.txt":             []byte("deleted before the differential"),
		"big.bin":                  big,
		"Mail/archive.pst":         []byte("mail archive"),
		deep:                       []byte("long path"),
	}
	for rel, data := range files {
		p := filepath.Join(docs, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(docs, "Empty folder"), 0o750); err != nil {
		t.Fatal(err)
	}
	// Times last, deepest first, so writing a file does not touch its folder's.
	var paths []string
	filepath.WalkDir(docs, func(p string, _ fs.DirEntry, err error) error { //nolint:errcheck
		paths = append(paths, p)
		return err
	})
	sort.Sort(sort.Reverse(sort.StringSlice(paths)))
	for _, p := range paths {
		setTimes(t, p, fixtureCreated, fixtureModified)
	}
	for rel, attr := range map[string]uint32{
		"readonly.txt":  windows.FILE_ATTRIBUTE_READONLY,
		"hidden.txt":    windows.FILE_ATTRIBUTE_HIDDEN,
		"system.txt":    windows.FILE_ATTRIBUTE_SYSTEM,
		"Hidden folder": windows.FILE_ATTRIBUTE_HIDDEN,
	} {
		p, _ := windows.UTF16PtrFromString(filepath.Join(docs, rel))
		if err := windows.SetFileAttributes(p, attr); err != nil {
			t.Fatal(err)
		}
	}
}

func setTimes(t *testing.T, path string, created, modified time.Time) {
	t.Helper()
	p, _ := windows.UTF16PtrFromString(path)
	h, err := windows.CreateFile(p, windows.FILE_WRITE_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer windows.CloseHandle(h)
	c, m := windows.NsecToFiletime(created.UnixNano()), windows.NsecToFiletime(modified.UnixNano())
	if err := windows.SetFileTime(h, &c, nil, &m); err != nil {
		t.Fatalf("set times of %s: %v", path, err)
	}
}

// writeFixtureBackups writes the backup directories of fixtureCases below
// root and returns the recovery code of the "recovery" case.
func writeFixtureBackups(t *testing.T, root string) (recoveryCode string) {
	t.Helper()
	for _, c := range fixtureCases {
		docs := filepath.Join(t.TempDir(), "Documents")
		writeSourceTree(t, docs)
		cfg := &config.Config{
			SourceDirectories:  []string{docs},
			BackupDirectory:    filepath.Join(root, c.name),
			SplitSizeMB:        1,
			LogLevel:           "info",
			AuthenticationMode: config.AuthModePassword,
			RecoveryCode:       c.recoveryCode,
			OnUnreadableFile:   config.OnUnreadableSkip,
			Argon2:             testutil.FastArgon2Config,
		}
		if err := os.MkdirAll(cfg.BackupDirectory, 0o750); err != nil {
			t.Fatal(err)
		}

		// Full backup: the mail archive is open in a mail program and skipped.
		release := filelock.Hold(t, filepath.Join(docs, "Mail", "archive.pst"))
		out := runBackup(t, cfg, []string{"y"}, fixturePassword, fixturePassword)
		release()
		if !strings.Contains(out, "Skipped (could not be read): Mail/archive.pst") {
			t.Fatalf("the full backup must skip the locked file: %q", out)
		}
		if c.recoveryCode {
			m := regexp.MustCompile(`Your recovery code:\n\n    (\S+)\n`).FindStringSubmatch(out)
			if m == nil {
				t.Fatalf("no recovery code in %q", out)
			}
			recoveryCode = m[1]
		}

		// Differential: one file changed, one new, one deleted, the mail
		// archive readable again, and the changed database open (stale).
		writeFile(t, filepath.Join(docs, "letter.txt"), "Dear RestoreSafe, version 2")
		writeFile(t, filepath.Join(docs, "new.txt"), "added for the differential")
		if err := os.Remove(filepath.Join(docs, "deleteme.txt")); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(docs, "busy.db"), "database, version 2")
		release = filelock.Hold(t, filepath.Join(docs, "busy.db"))
		out = runBackup(t, cfg, []string{"y"}, fixturePassword)
		release()
		if !strings.Contains(out, "DIFF001") {
			t.Fatalf("the second backup must be a differential: %q", out)
		}
	}
	return recoveryCode
}

// fixtureRuns returns the run IDs of a backup directory, oldest first.
func fixtureRuns(t *testing.T, dir string) []string {
	t.Helper()
	infos, err := catalog.Inventory(dir)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]time.Time{}
	for _, info := range infos {
		seen[string(info.Header.RunID)] = info.Header.Created()
	}
	runs := make([]string, 0, len(seen))
	for id := range seen {
		runs = append(runs, id)
	}
	sort.Slice(runs, func(i, j int) bool { return seen[runs[i]].Before(seen[runs[j]]) })
	return runs
}

// restoreFixtureRun restores one run of a backup directory into dest,
// unlocking with the password or, for keys with a recovery slot, with the
// recovery code.
func restoreFixtureRun(t *testing.T, c fixtureCase, backupDir, runID, dest, code string) {
	t.Helper()
	cfg := &config.Config{BackupDirectory: backupDir, LogLevel: "info", Argon2: testutil.FastArgon2Config}
	sets := runSets(t, cfg, runID)
	s := useScript(t, []string{"y"}, fixturePassword)
	if c.recoveryCode {
		s = useScript(t, []string{"y", "r"}, code)
	}
	if err := restore.Run(context.Background(), s.ui, cfg, "", restore.Request{Sets: sets, Destination: dest}); err != nil {
		t.Fatalf("restore %s run %s: %v\n%s", c.name, runID, err, s.out.String())
	}
	s.done()
}

// listTree describes every file and folder below root, one line each:
// path, type, size, SHA-256, creation and modification time, and the
// restorable attributes.
func listTree(t *testing.T, root string) string {
	t.Helper()
	var b strings.Builder
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == root {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		info, err := d.Info()
		if err != nil {
			return err
		}
		a := info.Sys().(*syscall.Win32FileAttributeData)
		attrs := ""
		for _, f := range []struct {
			bit    uint32
			letter string
		}{{windows.FILE_ATTRIBUTE_READONLY, "R"}, {windows.FILE_ATTRIBUTE_HIDDEN, "H"}, {windows.FILE_ATTRIBUTE_SYSTEM, "S"}} {
			if a.FileAttributes&f.bit != 0 {
				attrs += f.letter
			}
		}
		kind, size, sum := "d", "-", "-"
		if !d.IsDir() {
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			h := sha256.Sum256(data)
			kind, size, sum = "f", fmt.Sprint(len(data)), hex.EncodeToString(h[:])
		}
		ft := func(f syscall.Filetime) string { return time.Unix(0, f.Nanoseconds()).UTC().Format(time.RFC3339Nano) }
		created, modified := ft(a.CreationTime), ft(a.LastWriteTime)
		if !strings.Contains(filepath.ToSlash(rel), "/") {
			// The restored source folder is new; the format keeps the times
			// of what is inside it.
			created, modified = "-", "-"
		}
		fmt.Fprintf(&b, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", filepath.ToSlash(rel), kind, size, sum, created, modified, attrs)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// copyDir copies the backup directory src to dst: a restore appends to the
// run's log, and the fixtures must stay unchanged.
func copyDir(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o750)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func listingPath(c fixtureCase, run int) string {
	return filepath.Join(fixtureDir, fmt.Sprintf("%s-run%d.listing", c.name, run+1))
}

// TestWriteFormatFixtures writes the fixtures and their listings. It runs
// only on request and refuses to overwrite existing fixtures.
func TestWriteFormatFixtures(t *testing.T) {
	if os.Getenv("RESTORESAFE_WRITE_FIXTURES") != "1" {
		t.Skip("set RESTORESAFE_WRITE_FIXTURES=1 to write the format fixtures")
	}
	if _, err := os.Stat(fixtureDir); err == nil {
		t.Fatalf("%s exists: the fixtures of a release are never rewritten", fixtureDir)
	}
	code := writeFixtureBackups(t, fixtureDir)
	if err := os.WriteFile(filepath.Join(fixtureDir, "recovery-code.txt"), []byte(code+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, c := range fixtureCases {
		backupDir := filepath.Join(t.TempDir(), "backups")
		copyDir(t, filepath.Join(fixtureDir, c.name), backupDir)
		for i, run := range fixtureRuns(t, backupDir) {
			dest := filepath.Join(t.TempDir(), "restore")
			restoreFixtureRun(t, c, backupDir, run, dest, code)
			if err := os.WriteFile(listingPath(c, i), []byte(listTree(t, dest)), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// TestFormatFixturesRestore restores every run of the 2.0.0 fixtures and
// compares the result with the recorded listing.
func TestFormatFixturesRestore(t *testing.T) {
	t.Parallel()
	code, err := os.ReadFile(filepath.Join(fixtureDir, "recovery-code.txt"))
	if err != nil {
		t.Fatalf("the 2.0.0 format fixtures are missing: %v", err)
	}
	for _, c := range fixtureCases {
		backupDir := filepath.Join(t.TempDir(), "backups")
		copyDir(t, filepath.Join(fixtureDir, c.name), backupDir)
		runs := fixtureRuns(t, backupDir)
		if len(runs) != 2 {
			t.Fatalf("%s: expected a full and a differential run, got %d runs", c.name, len(runs))
		}
		for i, run := range runs {
			want, err := os.ReadFile(listingPath(c, i))
			if err != nil {
				t.Fatal(err)
			}
			dest := filepath.Join(t.TempDir(), "restore")
			restoreFixtureRun(t, c, backupDir, run, dest, strings.TrimSpace(string(code)))
			if got := listTree(t, dest); got != string(want) {
				t.Errorf("%s run %d restores differently from 2.0.0.\nwant:\n%s\ngot:\n%s", c.name, i+1, want, got)
			}
		}
	}
}

// TestFormatFixturesStructure writes the same backups with the current code
// and compares every set's on-disk structure with its fixture: the header
// prefix and the keys and value types of the header JSON, and the trailer.
// Values differ (keys, nonces, dates are new); the structure must not.
func TestFormatFixturesStructure(t *testing.T) {
	t.Parallel()
	fresh := t.TempDir()
	writeFixtureBackups(t, fresh)
	for _, c := range fixtureCases {
		want := setStructures(t, filepath.Join(fixtureDir, c.name))
		got := setStructures(t, filepath.Join(fresh, c.name))
		if len(got) != len(want) {
			t.Fatalf("%s: %d sets, the fixtures have %d", c.name, len(got), len(want))
		}
		for role, w := range want {
			if g, ok := got[role]; !ok || g != w {
				t.Errorf("%s %s: structure differs from 2.0.0.\nwant: %s\ngot:  %s", c.name, role, w, g)
			}
		}
	}
}

// setStructures returns the structure of each set in dir, keyed by its role
// (directory name, set type, part count).
func setStructures(t *testing.T, dir string) map[string]string {
	t.Helper()
	infos, err := catalog.Inventory(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, info := range infos {
		parts, err := catalog.CollectParts(dir, info.Entry)
		if err != nil {
			t.Fatal(err)
		}
		first, err := os.ReadFile(parts[0])
		if err != nil {
			t.Fatal(err)
		}
		bodyLen := binary.BigEndian.Uint32(first[8:12])
		var header any
		if err := json.Unmarshal(first[12:12+bodyLen], &header); err != nil {
			t.Fatalf("%s: header JSON: %v", parts[0], err)
		}
		last, err := os.ReadFile(parts[len(parts)-1])
		if err != nil {
			t.Fatal(err)
		}
		trailer := last[len(last)-64:]
		role := fmt.Sprintf("%s %s %d part(s)", info.Header.DirectoryName, info.Header.SetType, len(parts))
		out[role] = fmt.Sprintf("prefix %q, header %s, trailer magic %q, reserved %x", first[:8], shape(header), trailer[:8], trailer[44:48])
	}
	return out
}

// shape describes the keys and value types of a decoded JSON value.
func shape(v any) string {
	switch v := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = k + ":" + shape(v[k])
		}
		return "{" + strings.Join(parts, ",") + "}"
	case []any:
		parts := make([]string, len(v))
		for i, e := range v {
			parts[i] = shape(e)
		}
		return "[" + strings.Join(parts, ",") + "]"
	case string:
		return "string"
	case float64:
		return "number"
	case bool:
		return "bool"
	case nil:
		return "null"
	}
	return fmt.Sprintf("%T", v)
}
