package util

import "testing"

func TestFormatBytesBinary(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   uint64
		want string
	}{
		{0, "0 B"},
		{1023, "1023 B"},
		{1024, "1.00 KB"},
		{1536, "1.50 KB"},
		{1024 * 1024, "1.00 MB"},
		{3 * 1024 * 1024 * 1024, "3.00 GB"},
	}

	for _, tc := range cases {
		if got := FormatBytesBinary(tc.in); got != tc.want {
			t.Fatalf("FormatBytesBinary(%d): expected %q, got %q", tc.in, tc.want, got)
		}
	}
}

func TestFormatInsufficientBackupSpaceMessage(t *testing.T) {
	t.Parallel()

	got := FormatInsufficientBackupSpaceMessage(2*1024*1024, 512*1024)
	want := "Insufficient free space for backup: needed 2.00 MB, available 512.00 KB. Remedy: Free disk space or choose a different target folder."
	if got != want {
		t.Fatalf("unexpected message: got %q want %q", got, want)
	}
}

func TestFormatInsufficientRestoreSpaceMessage(t *testing.T) {
	t.Parallel()

	got := FormatInsufficientRestoreSpaceMessage(2*1024*1024, 512*1024)
	want := "Insufficient free space for restore: needed 2.00 MB, available 512.00 KB. Remedy: Free disk space or choose a different restore destination."
	if got != want {
		t.Fatalf("unexpected message: got %q want %q", got, want)
	}
}
