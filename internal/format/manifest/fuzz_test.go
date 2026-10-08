package manifest

import (
	"path/filepath"
	"strings"
	"testing"
)

// FuzzValidatePath checks that a path ValidatePath accepts never leads out
// of the restore destination.
func FuzzValidatePath(f *testing.F) {
	for _, seed := range []string{"a", "a/b.txt", "../a", "a/../../b", "C:/x", "//server/share", "a\b", "con", "a/./b", " a", "a:b"} {
		f.Add(seed)
	}
	dest := filepath.Join("C:\\", "restore", "dest")
	f.Fuzz(func(t *testing.T, p string) {
		if ValidatePath(p) != nil {
			return
		}
		joined := filepath.Join(dest, filepath.FromSlash(p))
		rel, err := filepath.Rel(dest, joined)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			t.Fatalf("ValidatePath accepted %q, which joins to %q outside %q", p, joined, dest)
		}
		if filepath.VolumeName(joined) != filepath.VolumeName(dest) {
			t.Fatalf("ValidatePath accepted %q, which changes the volume: %q", p, joined)
		}
	})
}
