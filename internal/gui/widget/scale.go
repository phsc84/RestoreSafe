package widget

import "github.com/phsc84/restoresafe/internal/gui/win32"

// Scale converts device-independent pixels (DIP, 1/96 inch) to pixels at a
// DPI; its value is the DPI.
type Scale uint32

// Px returns dip scaled to the DPI, rounded to the nearest pixel.
func (s Scale) Px(dip int32) int32 {
	return (dip*int32(s) + 48) / 96
}

// Rect returns a rectangle from DIP coordinates.
func (s Scale) Rect(x, y, w, h int32) win32.Rect {
	return win32.Rect{Left: s.Px(x), Top: s.Px(y), Right: s.Px(x + w), Bottom: s.Px(y + h)}
}

// Dip returns px pixels at the DPI in DIPs, rounded to the nearest DIP.
func (s Scale) Dip(px int32) int32 {
	return (px*96 + int32(s)/2) / int32(s)
}
