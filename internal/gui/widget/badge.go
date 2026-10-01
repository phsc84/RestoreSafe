package widget

import "RestoreSafe/internal/gui/win32"

// badgePadding is the space left and right of a badge's text, in DIPs.
const badgePadding = 7

// Badge is a small label on a colored background: FULL, DIFF 3.
type Badge struct {
	hwnd       win32.HWND
	theme      *Theme
	back       Color
	text       string
	fore, fill Color
}

// NewBadge creates a badge on parent, whose background is back.
func NewBadge(t *Theme, parent win32.HWND, back Color) (*Badge, error) {
	b := &Badge{theme: t, back: back}
	hwnd, err := create(b, classBadge, parent, 0, 0, 0)
	if err != nil {
		return nil, err
	}
	b.hwnd = hwnd
	return b, nil
}

// HWND returns the badge's window.
func (b *Badge) HWND() win32.HWND { return b.hwnd }

// Set shows text in fore on fill; name is what screen readers announce
// (e.g. "Differential 3").
func (b *Badge) Set(text string, fore, fill Color, name string) {
	b.text, b.fore, b.fill = text, fore, fill
	win32.SetAccessibleName(b.hwnd, name)
	win32.Invalidate(b.hwnd)
}

// Width returns the width in pixels the badge needs for its text.
func (b *Badge) Width() int32 {
	w, _ := b.theme.Fonts.Measure(b.text, TextCaption)
	return w + 2*b.theme.Scale.Px(badgePadding)
}

func (b *Badge) paint(hdc uintptr, r win32.Rect) {
	fill(hdc, r, b.back)
	inner := r
	inner.Right--
	inner.Bottom--
	roundRect(hdc, inner, b.theme.Scale.Px(ControlRadius*2), b.fill, b.fill)
	text(hdc, b.theme, b.text, r, TextCaption, b.fore, win32.DT_CENTER|win32.DT_VCENTER|win32.DT_SINGLELINE|win32.DT_NOPREFIX)
}

func (b *Badge) message(win32.HWND, uint32, uintptr, uintptr) (uintptr, bool) { return 0, false }
