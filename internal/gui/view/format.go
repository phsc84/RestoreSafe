package view

import (
	"fmt"
	"strconv"
	"time"
)

// Size formats a byte count in binary units labeled like Explorer: "512 B",
// "1.2 GB", "38 GB" (one decimal below 10, spec 3.6).
func Size(bytes int64) string {
	if bytes < 1024 {
		return fmt.Sprintf("%d B", max(bytes, 0))
	}
	units := []string{"KB", "MB", "GB", "TB", "PB"}
	v := float64(bytes) / 1024
	i := 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	if v < 9.95 {
		return fmt.Sprintf("%.1f %s", v, units[i])
	}
	return fmt.Sprintf("%.0f %s", v, units[i])
}

// Count formats n with thousands separators: "1,240".
func Count(n int) string {
	s := strconv.Itoa(max(n, 0))
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// When describes t relative to now: "today, 09:12", "yesterday, 18:40",
// "Sun 27 Sep, 20:05" within the year, "Tue 1 Sep 2025, 17:45" before.
func When(t, now time.Time) string {
	t, now = t.Local(), now.Local()
	return Day(t, now) + ", " + t.Format("15:04")
}

// Day describes the day of t relative to now: "today", "yesterday",
// "Sun 27 Sep", "Tue 1 Sep 2025".
func Day(t, now time.Time) string {
	t, now = t.Local(), now.Local()
	switch days := daysBetween(t, now); {
	case days == 0:
		return "today"
	case days == 1:
		return "yesterday"
	case t.Year() == now.Year():
		return t.Format("Mon 2 Jan")
	}
	return t.Format("Mon 2 Jan 2006")
}

// ShortDay is the day of t without the weekday: "1 Sep", "1 Sep 2025".
func ShortDay(t, now time.Time) string {
	t, now = t.Local(), now.Local()
	if t.Year() == now.Year() {
		return t.Format("2 Jan")
	}
	return t.Format("2 Jan 2006")
}

// daysBetween counts the calendar days from t to now.
func daysBetween(t, now time.Time) int {
	ty, tm, td := t.Date()
	ny, nm, nd := now.Date()
	a := time.Date(ty, tm, td, 0, 0, 0, 0, time.UTC)
	b := time.Date(ny, nm, nd, 0, 0, 0, 0, time.UTC)
	return int(b.Sub(a).Hours() / 24)
}

// Duration formats d roughly: "less than a minute", "4 min", "1 h 5 min".
func Duration(d time.Duration) string {
	minutes := int(d.Round(time.Minute) / time.Minute)
	switch {
	case d < 30*time.Second:
		return "less than a minute"
	case minutes < 60:
		return fmt.Sprintf("%d min", max(minutes, 1))
	case minutes%60 == 0:
		return fmt.Sprintf("%d h", minutes/60)
	}
	return fmt.Sprintf("%d h %d min", minutes/60, minutes%60)
}
