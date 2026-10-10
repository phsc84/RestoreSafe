package view

import (
	"testing"
	"time"
)

func TestSize(t *testing.T) {
	t.Parallel()
	for bytes, want := range map[int64]string{
		0: "0 B", 512: "512 B", 1023: "1023 B", 1024: "1.0 KB", 1536: "1.5 KB",
		10 * 1024: "10 KB", 1288490188: "1.2 GB", 38 << 30: "38 GB", 9_999 << 20: "9.8 GB", -5: "0 B",
	} {
		if got := Size(bytes); got != want {
			t.Errorf("Size(%d) = %q, want %q", bytes, got, want)
		}
	}
}

func TestCount(t *testing.T) {
	t.Parallel()
	for n, want := range map[int]string{0: "0", 999: "999", 1240: "1,240", 1234567: "1,234,567"} {
		if got := Count(n); got != want {
			t.Errorf("Count(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestWhenAndDay(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 30, 10, 0, 0, 0, time.Local)
	for _, tc := range []struct {
		t         time.Time
		when, day string
		shortDay  string
	}{
		{time.Date(2026, 9, 30, 9, 12, 0, 0, time.Local), "today, 09:12", "today", "30 Sep"},
		{time.Date(2026, 9, 29, 23, 59, 0, 0, time.Local), "yesterday, 23:59", "yesterday", "29 Sep"},
		{time.Date(2026, 9, 27, 20, 5, 0, 0, time.Local), "Sun 27 Sep, 20:05", "Sun 27 Sep", "27 Sep"},
		{time.Date(2025, 12, 31, 17, 45, 0, 0, time.Local), "Wed 31 Dec 2025, 17:45", "Wed 31 Dec 2025", "31 Dec 2025"},
	} {
		if got := When(tc.t, now); got != tc.when {
			t.Errorf("When(%v) = %q, want %q", tc.t, got, tc.when)
		}
		if got := Day(tc.t, now); got != tc.day {
			t.Errorf("Day(%v) = %q, want %q", tc.t, got, tc.day)
		}
		if got := ShortDay(tc.t, now); got != tc.shortDay {
			t.Errorf("ShortDay(%v) = %q, want %q", tc.t, got, tc.shortDay)
		}
	}
	// Across midnight, a backup of 23:59 is yesterday's even a minute later.
	if got := Day(time.Date(2026, 9, 29, 23, 59, 0, 0, time.Local), time.Date(2026, 9, 30, 0, 0, 30, 0, time.Local)); got != "yesterday" {
		t.Errorf("across midnight: %q", got)
	}
}

func TestDuration(t *testing.T) {
	t.Parallel()
	for d, want := range map[time.Duration]string{
		10 * time.Second: "less than a minute", 40 * time.Second: "1 min", 4 * time.Minute: "4 min",
		52*time.Minute + 20*time.Second: "52 min", time.Hour: "1 h", 65 * time.Minute: "1 h 5 min",
	} {
		if got := Duration(d); got != want {
			t.Errorf("Duration(%v) = %q, want %q", d, got, want)
		}
	}
}
