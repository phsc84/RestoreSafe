package widget

import "github.com/phsc84/restoresafe/internal/gui/win32"

// badgePadding is the space left and right of a badge's text, in DIPs.
const badgePadding = 7

// DrawBadge paints a badge with text into r of hdc, left aligned and
// vertically centered: for cells that other controls draw (list views).
func DrawBadge(hdc uintptr, t *Theme, r win32.Rect, label string, fore, back Color) {
	w, h := t.Fonts.Measure(label, TextCaption)
	pad := t.Scale.Px(badgePadding)
	height := h + t.Scale.Px(4)
	pill := win32.Rect{Left: r.Left + t.Scale.Px(6), Top: r.Top + (r.Height()-height)/2}
	pill.Right, pill.Bottom = pill.Left+w+2*pad, pill.Top+height
	roundRect(hdc, pill, t.Scale.Px(ControlRadius*2), back, back)
	text(hdc, t, label, pill, TextCaption, fore, win32.DT_CENTER|win32.DT_VCENTER|win32.DT_SINGLELINE|win32.DT_NOPREFIX)
}
