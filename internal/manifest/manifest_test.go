package manifest

import (
	"bytes"
	"strings"
	"testing"
)

var testHash = strings.Repeat("ab", 32)

func off(v int64) *int64 { return &v }

func fullHeader() Header {
	return Header{SetType: SetTypeFull, ChainID: "ABC123", DirectoryName: "Documents", SourcePath: "C:/Users/me/Documents"}
}

func diffHeader() Header {
	return Header{SetType: SetTypeDiff, ChainID: "ABC123", DiffNumber: 2, DirectoryName: "Documents", SourcePath: "C:/Users/me/Documents"}
}

func buildFull(t *testing.T) []byte {
	t.Helper()
	b := NewBuilder(fullHeader())
	b.Add(Entry{Path: "docs", Type: TypeDir, ModTime: 1})
	b.Add(Entry{Path: "docs/a.txt", Type: TypeFile, Size: 10, ModTime: 2, Hash: testHash, Origin: OriginFull, Offset: off(0)})
	b.Add(Entry{Path: "empty.bin", Type: TypeFile, Size: 0, Hash: testHash, Origin: OriginFull, Offset: off(1024), Attributes: AttrReadOnly})
	b.Add(Entry{Path: "locked.pst", Type: TypeSkipped, Reason: "in use", Void: true})
	data, err := b.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	return data
}

func TestRoundTripFull(t *testing.T) {
	t.Parallel()

	m, err := Parse(buildFull(t))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(m.Entries) != 4 {
		t.Fatalf("entries = %d, want 4", len(m.Entries))
	}
	want := Footer{End: true, Entries: 4, Files: 2, Dirs: 1, Skipped: 1, TotalBytes: 10, DataBytes: 10}
	if m.Footer != want {
		t.Fatalf("footer = %+v, want %+v", m.Footer, want)
	}
	if m.Header.ManifestVersion != Version || m.Header.HashAlg != HashAlg {
		t.Fatalf("header defaults not set: %+v", m.Header)
	}
	if m.Entries[2].Offset == nil || *m.Entries[2].Offset != 1024 {
		t.Fatalf("offset not preserved: %+v", m.Entries[2])
	}
	if m.Entries[0].Offset != nil {
		t.Fatal("directory must not have an offset")
	}
}

func TestRoundTripDiffWithStaleEntry(t *testing.T) {
	t.Parallel()

	b := NewBuilder(diffHeader())
	b.Add(Entry{Path: "old.txt", Type: TypeFile, Size: 5, Hash: testHash, Origin: OriginFull})
	b.Add(Entry{Path: "new.txt", Type: TypeFile, Size: 7, Hash: testHash, Origin: OriginDiff, Offset: off(0)})
	b.Add(Entry{Path: "busy.db", Type: TypeFile, Size: 3, Hash: testHash, Origin: OriginFull, Stale: true, Void: true})
	data, err := b.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	m, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if m.Footer.TotalBytes != 15 || m.Footer.DataBytes != 7 || m.Footer.Stale != 1 {
		t.Fatalf("unexpected footer: %+v", m.Footer)
	}
}

func TestSkipLastDirectory(t *testing.T) {
	t.Parallel()

	b := NewBuilder(fullHeader())
	b.Add(Entry{Path: "ok", Type: TypeDir})
	b.Add(Entry{Path: "ok/locked", Type: TypeDir})
	if b.SkipLastDirectory("ok", "denied") {
		t.Fatal("only the last entry may be converted")
	}
	if !b.SkipLastDirectory("ok/locked", "denied") {
		t.Fatal("expected the last directory to be converted")
	}
	data, err := b.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	m, _ := Parse(data)
	if m.Entries[1].Type != TypeSkipped || m.Entries[1].Reason != "denied" || m.Footer.Dirs != 1 || m.Footer.Skipped != 1 {
		t.Fatalf("unexpected result: %+v %+v", m.Entries[1], m.Footer)
	}
}

func TestBuilderRejectsInvalidManifest(t *testing.T) {
	t.Parallel()

	b := NewBuilder(fullHeader())
	b.Add(Entry{Path: "../escape", Type: TypeFile, Hash: testHash, Origin: OriginFull, Offset: off(0)})
	if _, err := b.Bytes(); err == nil {
		t.Fatal("expected builder to reject an invalid entry")
	}
}

func TestParseRejectsStructuralProblems(t *testing.T) {
	t.Parallel()

	valid := buildFull(t)
	lines := strings.Split(strings.TrimSuffix(string(valid), "\n"), "\n")
	join := func(ls ...string) []byte { return []byte(strings.Join(ls, "\n") + "\n") }

	cases := []struct {
		name string
		data []byte
		want string
	}{
		{"empty", nil, "empty manifest"},
		{"missing footer", join(lines[:len(lines)-1]...), "missing footer"},
		{"data after footer", join(append(lines, lines[1])...), "after the footer"},
		{"unknown header field", join(append([]string{strings.Replace(lines[0], `{`, `{"extra":1,`, 1)}, lines[1:]...)...), "invalid header"},
		{"unknown entry field", join(append([]string{lines[0], strings.Replace(lines[1], `{`, `{"zz":1,`, 1)}, lines[2:]...)...), "invalid entry"},
		{"footer mismatch", join(append(lines[:len(lines)-1], strings.Replace(lines[len(lines)-1], `"files":2`, `"files":3`, 1))...), "footer totals"},
		{"removed entry", join(append([]string{lines[0]}, lines[2:]...)...), "before its parent"},
	}
	for _, tc := range cases {
		_, err := Parse(tc.data)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: expected error containing %q, got %v", tc.name, tc.want, err)
		}
	}
}

func TestValidateRejectsInvalidEntries(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		header Header
		entry  Entry
		want   string
	}{
		{"bad hash", fullHeader(), Entry{Path: "a", Type: TypeFile, Hash: "xyz", Origin: OriginFull, Offset: off(0)}, "invalid hash"},
		{"negative size", fullHeader(), Entry{Path: "a", Type: TypeFile, Size: -1, Hash: testHash, Origin: OriginFull, Offset: off(0)}, "negative size"},
		{"unknown attrs", fullHeader(), Entry{Path: "a", Type: TypeFile, Hash: testHash, Origin: OriginFull, Offset: off(0), Attributes: 0x20}, "attribute"},
		{"diff origin in full", fullHeader(), Entry{Path: "a", Type: TypeFile, Hash: testHash, Origin: OriginDiff, Offset: off(0)}, "differential origin"},
		{"full without content", fullHeader(), Entry{Path: "a", Type: TypeFile, Hash: testHash, Origin: OriginFull}, "no content"},
		{"stale in full", fullHeader(), Entry{Path: "a", Type: TypeFile, Hash: testHash, Origin: OriginFull, Offset: off(0), Stale: true}, "stale marker"},
		{"F with offset in diff", diffHeader(), Entry{Path: "a", Type: TypeFile, Hash: testHash, Origin: OriginFull, Offset: off(0)}, "has an offset"},
		{"D without offset", diffHeader(), Entry{Path: "a", Type: TypeFile, Hash: testHash, Origin: OriginDiff}, "invalid differential content"},
		{"void without stale", diffHeader(), Entry{Path: "a", Type: TypeFile, Hash: testHash, Origin: OriginFull, Void: true}, "void marker"},
		{"bad origin", fullHeader(), Entry{Path: "a", Type: TypeFile, Hash: testHash, Origin: "X", Offset: off(0)}, "invalid origin"},
		{"dir with hash", fullHeader(), Entry{Path: "a", Type: TypeDir, Hash: testHash}, "file fields"},
		{"skipped without reason", fullHeader(), Entry{Path: "a", Type: TypeSkipped}, "no reason"},
		{"unknown type", fullHeader(), Entry{Path: "a", Type: "l"}, "unknown entry type"},
	}
	for _, tc := range cases {
		m := &Manifest{Header: tc.header, Entries: []Entry{tc.entry}}
		m.Header.ManifestVersion = Version
		m.Header.HashAlg = HashAlg
		m.Footer = computeFooter(m.Entries)
		err := m.Validate()
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: expected error containing %q, got %v", tc.name, tc.want, err)
		}
	}
}

func TestValidateRejectsDuplicatePaths(t *testing.T) {
	t.Parallel()

	for _, pair := range [][2]string{{"a.txt", "a.txt"}, {"a.txt", "A.TXT"}} {
		m := &Manifest{Header: fullHeader(), Entries: []Entry{
			{Path: pair[0], Type: TypeFile, Hash: testHash, Origin: OriginFull, Offset: off(0)},
			{Path: pair[1], Type: TypeFile, Hash: testHash, Origin: OriginFull, Offset: off(512)},
		}}
		m.Header.ManifestVersion = Version
		m.Header.HashAlg = HashAlg
		m.Footer = computeFooter(m.Entries)
		if err := m.Validate(); err == nil {
			t.Fatalf("expected duplicate %v to be rejected", pair)
		}
	}
}

func TestValidateRejectsBadHeader(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		mutate func(*Header)
		want   string
	}{
		{"version", func(h *Header) { h.ManifestVersion = 2 }, "Unsupported backup manifest version"},
		{"hash alg", func(h *Header) { h.HashAlg = "md5" }, "hash algorithm"},
		{"set type", func(h *Header) { h.SetType = "incr" }, "unknown set type"},
		{"diff number in full", func(h *Header) { h.DiffNumber = 1 }, "differential number"},
		{"missing chain", func(h *Header) { h.ChainID = "" }, "missing chain ID"},
	}
	for _, tc := range cases {
		h := fullHeader()
		h.ManifestVersion = Version
		h.HashAlg = HashAlg
		tc.mutate(&h)
		m := &Manifest{Header: h, Footer: Footer{End: true}}
		if err := m.Validate(); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: expected error containing %q, got %v", tc.name, tc.want, err)
		}
	}
}

func TestValidatePath(t *testing.T) {
	t.Parallel()

	for _, p := range []string{"a", "a/b.txt", "dir with space/f", "ümlaut/ß"} {
		if err := ValidatePath(p); err != nil {
			t.Fatalf("ValidatePath(%q) = %v, want nil", p, err)
		}
	}
	for _, p := range []string{"", " ", "/abs", "../x", "a/../b", "a/./b", "a//b", "a/", `a\b`, "C:/x", "a:b", "a\x00b", "."} {
		if err := ValidatePath(p); err == nil {
			t.Fatalf("ValidatePath(%q) = nil, want error", p)
		}
	}
}

func TestSerializationDoesNotEscapeHTML(t *testing.T) {
	t.Parallel()

	b := NewBuilder(fullHeader())
	b.Add(Entry{Path: "a&b<c>.txt", Type: TypeFile, Hash: testHash, Origin: OriginFull, Offset: off(0)})
	data, err := b.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	if !bytes.Contains(data, []byte(`"a&b<c>.txt"`)) {
		t.Fatalf("expected unescaped path in %s", data)
	}
}

func FuzzParse(f *testing.F) {
	b := NewBuilder(fullHeader())
	b.Add(Entry{Path: "a", Type: TypeDir})
	b.Add(Entry{Path: "a/b", Type: TypeFile, Size: 3, Hash: testHash, Origin: OriginFull, Offset: off(0)})
	seed, _ := b.Bytes()
	f.Add(seed)
	f.Add([]byte("{}\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		m, err := Parse(data)
		if err != nil {
			return
		}
		// Anything Parse accepts must pass validation again.
		if err := m.Validate(); err != nil {
			t.Fatalf("parsed manifest fails validation: %v", err)
		}
	})
}
