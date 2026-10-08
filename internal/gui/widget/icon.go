package widget

import "github.com/phsc84/restoresafe/internal/gui/win32"

// Icon draws a glyph, optionally on a filled circle (the hero, GUI spec 3.3).
type Icon struct {
	hwnd   win32.HWND
	theme  *Theme
	back   Color
	style  TextStyle
	glyph  Glyph
	fore   Color
	circle Color
}

// NoCircle draws an icon without a circle.
const NoCircle Color = 0xFFFFFFFF

// NewIcon creates an icon on parent, whose background is back; style is
// TextIcon (hero) or TextIconSmall (in text rows).
func NewIcon(t *Theme, parent win32.HWND, back Color, style TextStyle) (*Icon, error) {
	i := &Icon{theme: t, back: back, style: style, circle: NoCircle}
	hwnd, err := create(i, classIcon, parent, 0, 0, 0)
	if err != nil {
		return nil, err
	}
	i.hwnd = hwnd
	return i, nil
}

// HWND returns the icon's window.
func (i *Icon) HWND() win32.HWND { return i.hwnd }

// Set shows g in fore on a filled circle of color circle (NoCircle: none);
// name is what screen readers announce (e.g. "Protected").
func (i *Icon) Set(g Glyph, fore, circle Color, name string) {
	i.glyph, i.fore, i.circle = g, fore, circle
	win32.SetAccessibleName(i.hwnd, name)
	win32.Invalidate(i.hwnd)
}

func (i *Icon) paint(hdc uintptr, r win32.Rect) {
	fill(hdc, r, i.back)
	if i.circle != NoCircle {
		size := min(r.Width(), r.Height())
		c := win32.Rect{Left: r.Left, Top: r.Top, Right: r.Left + size, Bottom: r.Top + size}
		roundRect(hdc, c, size, i.circle, i.circle)
		r = c
	}
	style := i.style
	if !i.theme.Fonts.Glyphs {
		style = TextHero
		if i.style == TextIconSmall {
			style = TextBody
		}
	}
	text(hdc, i.theme, i.theme.Fonts.GlyphText(i.glyph), r, style, i.fore, win32.DT_CENTER|win32.DT_VCENTER|win32.DT_SINGLELINE|win32.DT_NOPREFIX)
}

func (i *Icon) message(win32.HWND, uint32, uintptr, uintptr) (uintptr, bool) { return 0, false }
