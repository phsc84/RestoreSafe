package widget

import "RestoreSafe/internal/gui/win32"

// SplitterHeight is the height of a splitter in DIPs.
const SplitterHeight = 8

// Splitter is a horizontal bar between two panes that the user drags up
// and down.
type Splitter struct {
	hwnd     win32.HWND
	theme    *Theme
	back     Color
	dragging bool
	grab     int32
	// OnMove receives the new top of the splitter in the parent's client
	// coordinates while it is dragged.
	OnMove func(top int32)
}

// NewSplitter creates a splitter on parent, whose background is back.
func NewSplitter(t *Theme, parent win32.HWND, back Color) (*Splitter, error) {
	s := &Splitter{theme: t, back: back}
	hwnd, err := create(s, classSplitter, parent, 0, 0, 0)
	if err != nil {
		return nil, err
	}
	s.hwnd = hwnd
	return s, nil
}

// HWND returns the splitter's window.
func (s *Splitter) HWND() win32.HWND { return s.hwnd }

func (s *Splitter) paint(hdc uintptr, r win32.Rect) {
	fill(hdc, r, s.back)
	sc := s.theme.Scale
	w := sc.Px(40)
	grip := win32.Rect{Left: r.Left + (r.Width()-w)/2, Top: r.Top + r.Height()/2 - 1, Bottom: r.Top + r.Height()/2 + 1}
	grip.Right = grip.Left + w
	fill(hdc, grip, s.theme.Palette.Lines)
}

func (s *Splitter) message(hwnd win32.HWND, msg uint32, wparam, lparam uintptr) (uintptr, bool) {
	switch msg {
	case win32.WM_SETCURSOR:
		win32.SetSizeNSCursor()
		return 1, true
	case win32.WM_LBUTTONDOWN:
		s.dragging = true
		s.grab = win32.PointParam(lparam).Y
		win32.SetCapture(hwnd)
		return 0, true
	case win32.WM_MOUSEMOVE:
		if s.dragging && s.OnMove != nil {
			pt := win32.ClientToScreen(hwnd, win32.PointParam(lparam))
			pt = win32.ScreenToClient(win32.Parent(hwnd), pt)
			s.OnMove(pt.Y - s.grab)
		}
		return 0, true
	case win32.WM_LBUTTONUP:
		if s.dragging {
			s.dragging = false
			win32.ReleaseCapture()
		}
		return 0, true
	}
	return 0, false
}
