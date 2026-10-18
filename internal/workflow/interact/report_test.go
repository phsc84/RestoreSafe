package interact

import (
	"testing"

	"github.com/phsc84/restoresafe/internal/problem"
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

func TestStatusNamesAndRowConstructors(t *testing.T) {
	t.Parallel()

	for s, want := range map[Status]string{StatusNone: "", StatusOK: "OK", StatusInfo: "INFO", StatusWarn: "WARN", StatusError: "ERROR"} {
		if s.String() != want {
			t.Fatalf("Status(%d).String() = %q, want %q", s, s.String(), want)
		}
	}
	if r := Heading("Sources"); r.Kind != RowHeading || r.Label != "Sources" {
		t.Fatalf("Heading: %+v", r)
	}
	if r := Item(StatusWarn, "Docs", "a", "b"); r.Kind != RowItem || r.Status != StatusWarn || r.Text != "Docs" || len(r.Details) != 2 {
		t.Fatalf("Item: %+v", r)
	}
	if r := Field("Keys", "existing"); r.Kind != RowField || r.Label != "Keys" || r.Text != "existing" {
		t.Fatalf("Field: %+v", r)
	}
}

func TestIssueOfSplitsTheRemedy(t *testing.T) {
	t.Parallel()

	err := problem.New("The backup directory is full.").WithRemedy("Free some space.")
	issue := IssueOf(StatusError, CodeBackupDirUnreachable, err)
	if issue.Text != "The backup directory is full." || issue.Remedy != "Free some space." || issue.Code != CodeBackupDirUnreachable || issue.Status != StatusError {
		t.Fatalf("IssueOf: %+v", issue)
	}
	if issue.Full() != err.Error() {
		t.Fatalf("Full() = %q, want the error text %q", issue.Full(), err.Error())
	}
}

func TestPlansBlockOnlyOnErrors(t *testing.T) {
	t.Parallel()

	warn := []Issue{{Status: StatusWarn, Text: "w"}}
	both := []Issue{{Status: StatusWarn, Text: "w"}, {Status: StatusError, Text: "e"}}
	if (BackupPlan{Issues: warn}).HasErrors() || (RestorePlan{Issues: warn}).HasErrors() || (VerifyPlan{Issues: warn}).HasErrors() {
		t.Fatal("a warning must not block a plan")
	}
	if !(BackupPlan{Issues: both}).HasErrors() || !(RestorePlan{Issues: both}).HasErrors() || !(VerifyPlan{Issues: both}).HasErrors() {
		t.Fatal("an error must block a plan")
	}
}

func TestProgressFractionAndRetry(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		done, total int64
		want        float64
	}{{5, 0, -1}, {5, 10, 0.5}, {-1, 10, 0}, {20, 10, 1}} {
		if got := (Progress{Done: tc.done, Total: tc.total}).Fraction(); got != tc.want {
			t.Fatalf("Fraction(%d/%d) = %v, want %v", tc.done, tc.total, got, tc.want)
		}
	}
	if (Attempt{N: 1}).Retry() || !(Attempt{N: 2}).Retry() {
		t.Fatal("only a second try is a retry")
	}
}
