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
