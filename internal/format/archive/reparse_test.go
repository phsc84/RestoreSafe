package archive

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// junction makes link a junction to target, as mklink /J does without
// administrator rights.
func junction(t *testing.T, link, target string) {
	t.Helper()
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Fatalf("mklink /J: %v: %s", err, out)
	}
}

func TestRestoreRefusesJunctionAsDestination(t *testing.T) {
	t.Parallel()

	src := t.TempDir()
	mustWrite(t, filepath.Join(src, "dir", "f.txt"), []byte("x"))
	m, tarData := build(t, src)

	elsewhere := t.TempDir()
	dest := filepath.Join(t.TempDir(), "dest")
	junction(t, dest, elsewhere)
	err := restore(t, m, tarData, dest, false)
	if err == nil || !strings.Contains(err.Error(), "is a link to another place") {
		t.Fatalf("expected the junction to be refused, got %v", err)
	}
	if entries, _ := os.ReadDir(elsewhere); len(entries) != 0 {
		t.Fatalf("restored through the junction: %v", entries)
	}
}

func TestRestoreRefusesFolderSwappedForJunction(t *testing.T) {
	t.Parallel()

	src := t.TempDir()
	mustWrite(t, filepath.Join(src, "dir", "f.txt"), []byte("x"))
	m, tarData := build(t, src)

	dest := t.TempDir()
	r := NewRestorer(m, dest, false)
	if err := r.CreateDirectories(); err != nil {
		t.Fatal(err)
	}
	// Another process swaps the created folder for a junction.
	elsewhere := t.TempDir()
	if err := os.Remove(filepath.Join(dest, "dir")); err != nil {
		t.Fatal(err)
	}
	junction(t, filepath.Join(dest, "dir"), elsewhere)

	err := r.ExtractSection(bytes.NewReader(tarData), DecideOwn(m))
	if err == nil || !strings.Contains(err.Error(), "is a link to another place") {
		t.Fatalf("expected the junction to be refused, got %v", err)
	}
	if entries, _ := os.ReadDir(elsewhere); len(entries) != 0 {
		t.Fatalf("restored through the junction: %v", entries)
	}
}
