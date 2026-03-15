package testutil

import (
	"bytes"
	"os"
	"testing"
)

// AssertFileContentEqual fails the test if the two files do not have identical content.
func AssertFileContentEqual(t testing.TB, expectedPath, actualPath string) {
	t.Helper()

	expected, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("failed to read expected file %s: %v", expectedPath, err)
	}
	actual, err := os.ReadFile(actualPath)
	if err != nil {
		t.Fatalf("failed to read actual file %s: %v", actualPath, err)
	}

	if !bytes.Equal(expected, actual) {
		t.Fatalf("file contents differ for %s vs %s", expectedPath, actualPath)
	}
}
