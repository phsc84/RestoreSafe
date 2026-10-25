package archive

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// TestKindOfClassifiesByAttributesAndTag covers the table of core spec
// 6.4.1 with the tags Windows uses, also for a cloud placeholder that
// Windows would expose, which can't be made in a test without a sync root.
func TestKindOfClassifiesByAttributesAndTag(t *testing.T) {
	t.Parallel()
	const (
		dir     = windows.FILE_ATTRIBUTE_DIRECTORY
		reparse = windows.FILE_ATTRIBUTE_REPARSE_POINT
	)
	for _, tc := range []struct {
		name    string
		attrs   uint32
		tag     uint32
		regular bool
		want    entryKind
	}{
		{"file", windows.FILE_ATTRIBUTE_ARCHIVE, 0, true, kindFile},
		{"directory", dir, 0, false, kindDir},
		{"symbolic link to a file", reparse, windows.IO_REPARSE_TAG_SYMLINK, false, kindLink},
		{"symbolic link to a folder", dir | reparse, windows.IO_REPARSE_TAG_SYMLINK, false, kindLink},
		{"junction", dir | reparse, windows.IO_REPARSE_TAG_MOUNT_POINT, false, kindLink},
		{"cloud file", reparse | attrRecallOnDataAccess, 0x9000001A, false, kindFile},
		{"cloud folder", dir | reparse, 0x9000101A, false, kindDir},
		{"deduplicated file", reparse, 0x80000013, true, kindFile},
		{"WOF-compressed file", reparse, 0x80000017, false, kindFile},
		{"device or pipe", 0, 0, false, kindOther},
	} {
		if got := kindOf(tc.attrs, tc.tag, tc.regular); got != tc.want {
			t.Errorf("%s: kind %d, want %d", tc.name, got, tc.want)
		}
	}
}

// TestBuildTarCountsAndReportsALinkInsideTheSource: a junction inside the
// source is not followed, not recorded, but counted and reported (core
// spec 6.4.1: nothing is left out without a word).
func TestBuildTarCountsAndReportsALinkInsideTheSource(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	mustWrite(t, filepath.Join(src, "own.txt"), []byte("mine"))
	elsewhere := t.TempDir()
	mustWrite(t, filepath.Join(elsewhere, "foreign.txt"), []byte("not mine"))
	junction(t, filepath.Join(src, "linked"), elsewhere)

	var stats BuildStats
	var leftOut []string
	m, _, err := buildWith(t, BuildOptions{SourceDir: src, Stats: &stats, OnLeftOut: func(rel, what string) { leftOut = append(leftOut, rel+" ("+what+")") }})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Links != 1 || len(leftOut) != 1 || leftOut[0] != "linked (link)" {
		t.Fatalf("links %d, reported %v", stats.Links, leftOut)
	}
	for _, e := range m.Entries {
		if strings.HasPrefix(e.Path, "linked") {
			t.Fatalf("the junction was recorded: %+v", e)
		}
	}
	if len(m.Entries) != 1 || m.Entries[0].Path != "own.txt" {
		t.Fatalf("entries %+v", m.Entries)
	}

	ms, err := MeasureSource(BuildOptions{SourceDir: src}, time.Time{})
	if err != nil || ms.LeftOut != 1 || ms.Total != int64(len("mine")) {
		t.Fatalf("measure %+v, %v", ms, err)
	}
}

// TestBuildTarResolvesASourceThatIsALink: a source directory that is a
// junction (a redirected folder) is resolved once and its target backed up;
// before, the walk saw a file and the backup was empty (core spec 6.4.1).
func TestBuildTarResolvesASourceThatIsALink(t *testing.T) {
	t.Parallel()
	target := t.TempDir()
	mustWrite(t, filepath.Join(target, "a.txt"), []byte("alpha"))
	mustWrite(t, filepath.Join(target, "sub", "b.txt"), []byte("beta"))
	src := filepath.Join(t.TempDir(), "Documents")
	junction(t, src, target)

	m, _, err := buildWith(t, BuildOptions{SourceDir: src})
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, e := range m.Entries {
		paths = append(paths, e.Path)
	}
	if strings.Join(paths, ",") != "a.txt,sub,sub/b.txt" {
		t.Fatalf("entries %v, want the target's", paths)
	}
	ms, err := MeasureSource(BuildOptions{SourceDir: src}, time.Time{})
	if err != nil || ms.Total != int64(len("alpha")+len("beta")) {
		t.Fatalf("measure %+v, %v", ms, err)
	}
}

// TestBuildTarBacksUpAWOFCompressedFile: compact /exe hides its reparse
// point, so such a file is a plain file to the walk (measured 2026-10-25).
func TestBuildTarBacksUpAWOFCompressedFile(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	path := filepath.Join(src, "compressed.txt")
	mustWrite(t, path, []byte(strings.Repeat("RestoreSafe test line\n", 20_000)))
	if out, err := exec.Command("compact", "/c", "/exe:xpress4k", path).CombinedOutput(); err != nil {
		t.Skipf("compact /exe is not available here: %v: %s", err, out)
	}
	var stats BuildStats
	m, _, err := buildWith(t, BuildOptions{SourceDir: src, Stats: &stats})
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Entries) != 1 || m.Entries[0].Path != "compressed.txt" || stats.Links+stats.Other != 0 {
		t.Fatalf("entries %+v, stats %+v", m.Entries, stats)
	}
	if fi, err := os.Stat(path); err != nil || m.Entries[0].Size != fi.Size() {
		t.Fatalf("size %d, file %v", m.Entries[0].Size, err)
	}
}
