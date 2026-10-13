package interact

import (
	"testing"
)

func TestReportHasErrors(t *testing.T) {
	t.Parallel()
	if (Report{Issues: []Issue{{Status: StatusWarn, Text: "w"}}}).HasErrors() {
		t.Fatal("a warning must not block")
	}
	if !(Report{Issues: []Issue{{Status: StatusWarn, Text: "w"}, {Status: StatusError, Text: "e"}}}).HasErrors() {
		t.Fatal("an error must block")
	}
}
