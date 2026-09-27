package gui

import (
	"RestoreSafe/internal/gui/win32"
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
	empty := win32.Rect{}
	for _, dpi := range []uint32{96, 144, 192} {
		s := scale(dpi)
		w, h := s.px(windowMinWidth), s.px(windowMinHeight)
		for content := contentLog; content <= contentDestination; content++ {
			for _, progress := range []bool{false, true} {
				l := layoutOperation(s, w, h, content, progress)
				var rects []win32.Rect
				for _, r := range []win32.Rect{l.title, l.detail, l.progress, l.report, l.log, l.tree, l.destLabel, l.destEdit, l.destBrowse, l.destCheck, l.destNote} {
					if r != empty {
						rects = append(rects, r)
					}
				}
				rects = append(rects, l.buttons[:]...)
				client := win32.Rect{Right: w, Bottom: h}
				for i, r := range rects {
					if !inside(r, client) {
						t.Fatalf("dpi %d content %d: control %d %+v outside %+v", dpi, content, i, r, client)
					}
					for j := i + 1; j < len(rects); j++ {
						if overlap(r, rects[j]) {
							t.Fatalf("dpi %d content %d: controls %d %+v and %d %+v overlap", dpi, content, i, r, j, rects[j])
						}
					}
				}
				if content == contentReport && l.report.Bottom < l.buttons[0].Top-s.px(2*gap) {
					t.Fatalf("dpi %d: the report must fill the content area", dpi)
				}
				if content == contentTree && l.tree.Height() < s.px(200) {
					t.Fatalf("dpi %d: tree too small (%d px)", dpi, l.tree.Height())
				}
			}
		}
	}
}
