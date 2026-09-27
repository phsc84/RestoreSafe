package gui

import (
	"RestoreSafe/internal/win32"
	"testing"
)

func TestScale(t *testing.T) {
	t.Parallel()
	if got := scale(96).px(16); got != 16 {
		t.Fatalf("100%%: %d", got)
	}
	if got := scale(144).px(16); got != 24 {
		t.Fatalf("150%%: %d", got)
	}
	if got := scale(120).px(15); got != 19 { // 18.75 rounds to 19
		t.Fatalf("125%%: %d", got)
	}
}

func inside(r, bounds win32.Rect) bool {
	return r.Left >= bounds.Left && r.Top >= bounds.Top && r.Right <= bounds.Right && r.Bottom <= bounds.Bottom && r.Width() >= 0 && r.Height() >= 0
}

func overlap(a, b win32.Rect) bool {
	return a.Left < b.Right && b.Left < a.Right && a.Top < b.Bottom && b.Top < a.Bottom
}

func TestLayoutHomeFitsAndDoesNotOverlap(t *testing.T) {
	t.Parallel()
	for _, dpi := range []uint32{96, 120, 144, 192} {
		s := scale(dpi)
		for _, size := range [][2]int32{{windowMinWidth, windowMinHeight}, {windowWidth, windowHeight}, {1600, 1000}} {
			w, h := s.px(size[0]), s.px(size[1])
			l := layoutHome(s, w, h)
			rects := []win32.Rect{l.configLabel, l.configPath, l.configOpen, l.backupLabel, l.backupPath, l.backupOpen, l.report, l.blocked, l.backup, l.restore, l.verify, l.recheck}
			client := win32.Rect{Right: w, Bottom: h}
			for i, r := range rects {
				if !inside(r, client) {
					t.Fatalf("dpi %d size %v: control %d %+v outside the client area %+v", dpi, size, i, r, client)
				}
				for j := i + 1; j < len(rects); j++ {
					if overlap(r, rects[j]) {
						t.Fatalf("dpi %d size %v: controls %d %+v and %d %+v overlap", dpi, size, i, r, j, rects[j])
					}
				}
			}
			if l.report.Height() < s.px(150) {
				t.Fatalf("dpi %d size %v: report too small (%d px)", dpi, size, l.report.Height())
			}
		}
	}
}

func TestLayoutOperationFitsAndDoesNotOverlap(t *testing.T) {
	t.Parallel()
	for _, dpi := range []uint32{96, 144, 192} {
		s := scale(dpi)
		w, h := s.px(windowMinWidth), s.px(windowMinHeight)
		for _, panes := range [][3]bool{{true, false, false}, {false, true, true}, {true, true, false}} {
			l := layoutOperation(s, w, h, panes[0], panes[1], panes[2])
			rects := []win32.Rect{l.title, l.detail}
			if panes[2] {
				rects = append(rects, l.progress)
			}
			if panes[0] {
				rects = append(rects, l.report)
			}
			if panes[1] {
				rects = append(rects, l.log)
			}
			rects = append(rects, l.buttons[:]...)
			client := win32.Rect{Right: w, Bottom: h}
			for i, r := range rects {
				if !inside(r, client) {
					t.Fatalf("dpi %d panes %v: control %d %+v outside %+v", dpi, panes, i, r, client)
				}
				for j := i + 1; j < len(rects); j++ {
					if overlap(r, rects[j]) {
						t.Fatalf("dpi %d panes %v: controls %d and %d overlap", dpi, panes, i, j)
					}
				}
			}
			if panes[0] && !panes[1] && l.report.Bottom < l.buttons[0].Top-s.px(2*gap) {
				t.Fatalf("dpi %d: a report without log must fill the content area", dpi)
			}
		}
	}
}
