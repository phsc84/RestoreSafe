package gui

import (
	"RestoreSafe/internal/gui/widget"
	"RestoreSafe/internal/gui/win32"
	"testing"
)

func TestLayoutOperationFitsAndDoesNotOverlap(t *testing.T) {
	t.Parallel()
	empty := win32.Rect{}
	for _, dpi := range []uint32{96, 144, 192} {
		s := widget.Scale(dpi)
		w, h := s.Px(widget.WindowMinWidth), s.Px(widget.WindowMinHeight)
		for content := contentLog; content <= contentDestination; content++ {
			for _, progress := range []bool{false, true} {
				l := layoutOperation(s, w, h, content, progress)
				var rects []win32.Rect
				for _, r := range []win32.Rect{l.title, l.detail, l.progress, l.report, l.log, l.destLabel, l.destEdit, l.destBrowse, l.destCheck, l.destNote} {
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
				if content == contentReport && l.report.Bottom < l.buttons[0].Top-s.Px(2*gap) {
					t.Fatalf("dpi %d: the report must fill the content area", dpi)
				}
			}
		}
	}
}
func inside(r, bounds win32.Rect) bool {
	return r.Left >= bounds.Left && r.Top >= bounds.Top && r.Right <= bounds.Right && r.Bottom <= bounds.Bottom && r.Width() >= 0 && r.Height() >= 0
}
func overlap(a, b win32.Rect) bool {
	return a.Left < b.Right && b.Left < a.Right && a.Top < b.Bottom && b.Top < a.Bottom
}
