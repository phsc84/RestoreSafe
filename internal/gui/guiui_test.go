package gui

import "testing"

func TestCodeLines(t *testing.T) {
	t.Parallel()
	if got := codeLines("AAAAA-BBBBB-CCCCC-DDDDD-EEEEE-FFFFF"); got != "AAAAA-BBBBB-CCCCC\r\nDDDDD-EEEEE-FFFFF" {
		t.Fatalf("codeLines = %q", got)
	}
	if got := codeLines("ABC"); got != "ABC" {
		t.Fatalf("codeLines without groups = %q", got)
	}
}
