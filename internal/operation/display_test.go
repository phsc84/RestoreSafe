package operation

import (
	"strings"
	"testing"
)

func TestPrintFieldFormatsAlignedOutput(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	PrintField(&sb, 14, "Log level", "info")
	output := sb.String()
	if !strings.Contains(output, "Log level") {
		t.Fatalf("expected label in output, got: %q", output)
	}
	if !strings.Contains(output, "info") {
		t.Fatalf("expected value in output, got: %q", output)
	}
}
