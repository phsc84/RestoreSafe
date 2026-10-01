package widget

import "RestoreSafe/internal/gui/win32"

// Area is a rectangle in pixels that a layout cuts into rows and columns.
// Sizes are given in DIPs and scaled by S. A cut never makes the area
// negative: what does not fit is cut to what is left.
type Area struct {
	S Scale
	R win32.Rect
}

// NewArea returns the area r at scale s.
func NewArea(s Scale, r win32.Rect) Area { return Area{S: s, R: r} }

// Width and Height are the size of the area in pixels.
func (a Area) Width() int32  { return a.R.Width() }
func (a Area) Height() int32 { return a.R.Height() }

// Inset shrinks the area by the given DIPs on each side.
func (a *Area) Inset(left, top, right, bottom int32) {
	a.R.Left = min(a.R.Left+a.S.Px(left), a.R.Right)
	a.R.Top = min(a.R.Top+a.S.Px(top), a.R.Bottom)
	a.R.Right = max(a.R.Right-a.S.Px(right), a.R.Left)
	a.R.Bottom = max(a.R.Bottom-a.S.Px(bottom), a.R.Top)
}

// Top cuts a row of dip height off the top and returns it.
func (a *Area) Top(dip int32) win32.Rect { return a.TopPx(a.S.Px(dip)) }

// TopPx cuts a row of px pixels off the top and returns it.
func (a *Area) TopPx(px int32) win32.Rect {
	h := min(px, a.R.Height())
	r := win32.Rect{Left: a.R.Left, Top: a.R.Top, Right: a.R.Right, Bottom: a.R.Top + h}
	a.R.Top += h
	return r
}

// Bottom cuts a row of dip height off the bottom and returns it.
func (a *Area) Bottom(dip int32) win32.Rect {
	h := min(a.S.Px(dip), a.R.Height())
	r := win32.Rect{Left: a.R.Left, Top: a.R.Bottom - h, Right: a.R.Right, Bottom: a.R.Bottom}
	a.R.Bottom -= h
	return r
}

// Left cuts a column of dip width off the left and returns it.
func (a *Area) Left(dip int32) win32.Rect {
	w := min(a.S.Px(dip), a.R.Width())
	r := win32.Rect{Left: a.R.Left, Top: a.R.Top, Right: a.R.Left + w, Bottom: a.R.Bottom}
	a.R.Left += w
	return r
}

// Right cuts a column of dip width off the right and returns it.
func (a *Area) Right(dip int32) win32.Rect {
	w := min(a.S.Px(dip), a.R.Width())
	r := win32.Rect{Left: a.R.Right - w, Top: a.R.Top, Right: a.R.Right, Bottom: a.R.Bottom}
	a.R.Right -= w
	return r
}

// Rest returns what is left of the area.
func (a Area) Rest() win32.Rect { return a.R }

// Columns divides the area into columns separated by gap DIPs, sized in
// proportion to weights; the last column takes the rounding remainder.
func (a Area) Columns(gap int32, weights ...int32) []Area {
	if len(weights) == 0 {
		return nil
	}
	total := int32(0)
	for _, w := range weights {
		total += w
	}
	g := a.S.Px(gap)
	free := max(a.R.Width()-g*int32(len(weights)-1), 0)
	cols := make([]Area, len(weights))
	x := a.R.Left
	for i, w := range weights {
		width := free * w / max(total, 1)
		if i == len(weights)-1 {
			width = a.R.Right - x
		}
		width = max(width, 0)
		cols[i] = Area{S: a.S, R: win32.Rect{Left: x, Top: a.R.Top, Right: x + width, Bottom: a.R.Bottom}}
		x += width + g
	}
	return cols
}
