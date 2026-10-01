package widget

import "RestoreSafe/internal/gui/win32"

// Segment is a part of a bar: a fraction of its width in a color.
type Segment struct {
	Fraction float64
	Color    Color
}

// Bar is a segmented bar with rounded ends: the storage bar (backups, other
// data, free).
type Bar struct {
	hwnd     win32.HWND
	theme    *Theme
	back     Color
	track    Color
	segments []Segment
}

// NewBar creates a bar on parent, whose background is back; the part of the
// bar no segment covers is drawn in track.
func NewBar(t *Theme, parent win32.HWND, back, track Color) (*Bar, error) {
	b := &Bar{theme: t, back: back, track: track}
	hwnd, err := create(b, classBar, parent, 0, 0, 0)
	if err != nil {
		return nil, err
	}
	b.hwnd = hwnd
	return b, nil
}

// HWND returns the bar's window.
func (b *Bar) HWND() win32.HWND { return b.hwnd }

// Set shows the segments, from the left; name is what screen readers
// announce (e.g. "530 of 900 GB used").
func (b *Bar) Set(segments []Segment, name string) {
	b.segments = segments
	win32.SetAccessibleName(b.hwnd, name)
	win32.Invalidate(b.hwnd)
}

func (b *Bar) paint(hdc uintptr, r win32.Rect) {
	fill(hdc, r, b.back)
	diameter := r.Height()
	inner := r
	inner.Right--
	inner.Bottom--
	roundRect(hdc, inner, diameter, b.track, b.track)
	win32.ClipRoundRect(hdc, inner, diameter)
	defer win32.ClipNone(hdc)
	x := float64(r.Left)
	gap := float64(b.theme.Scale.Px(2))
	for _, s := range b.segments {
		w := s.Fraction * float64(r.Width())
		if w < 1 {
			continue
		}
		seg := win32.Rect{Left: int32(x), Top: r.Top, Right: int32(x + w), Bottom: r.Bottom}
		fill(hdc, seg, s.Color)
		x += w + gap
	}
}

func (b *Bar) message(win32.HWND, uint32, uintptr, uintptr) (uintptr, bool) { return 0, false }
