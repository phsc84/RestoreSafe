package widget

import (
	"RestoreSafe/internal/gui/win32"

	"golang.org/x/sys/windows"
)

// Font weights.
const (
	weightRegular  = 400
	weightSemibold = 600
)

const clearTypeQuality = 5

// Fonts are the fonts of the text styles at one DPI. Create new Fonts when
// the DPI changes and Close the old ones after applying the new ones.
type Fonts struct {
	handles [textStyles]windows.Handle
	// Glyphs is set when the icon font is installed; without it, icons are
	// drawn as text markers.
	Glyphs bool
}

// NewFonts creates the fonts for scale s.
func NewFonts(s Scale) (*Fonts, error) {
	f := &Fonts{}
	for style := TextStyle(0); style < textStyles; style++ {
		lf := win32.LogFont{Height: -s.Px(textPx[style]), Weight: weightRegular, Quality: clearTypeQuality}
		face := FontFace
		switch style {
		case TextHero, TextTitle, TextStrong:
			lf.Weight = weightSemibold
		case TextMono:
			face = MonoFontFace
		case TextIcon, TextIconSmall:
			face = IconFontFace
		}
		copy(lf.FaceName[:len(lf.FaceName)-1], windows.StringToUTF16(face))
		h, err := win32.CreateFont(&lf)
		if err != nil {
			f.Close()
			return nil, err
		}
		f.handles[style] = h
	}
	f.Glyphs = f.faceOf(TextIcon) == IconFontFace
	return f, nil
}

// Get returns the font of style.
func (f *Fonts) Get(style TextStyle) windows.Handle { return f.handles[style] }

// Close deletes the fonts.
func (f *Fonts) Close() {
	for i, h := range f.handles {
		if h != 0 {
			win32.DeleteObject(h)
			f.handles[i] = 0
		}
	}
}

// faceOf returns the face Windows uses for the font of style.
func (f *Fonts) faceOf(style TextStyle) string {
	hdc := win32.GetDC(0)
	defer win32.ReleaseDC(0, hdc)
	old := win32.SelectFont(hdc, f.handles[style])
	defer win32.SelectFont(hdc, old)
	return win32.TextFace(hdc)
}

// GlyphText returns the text that draws g: its code point in the icon font,
// or its text marker when the icon font is missing.
func (f *Fonts) GlyphText(g Glyph) string {
	if f.Glyphs {
		return string(g.Code)
	}
	return g.Fallback
}

// Measure returns the size in pixels that text needs in style.
func (f *Fonts) Measure(text string, style TextStyle) (w, h int32) {
	hdc := win32.GetDC(0)
	defer win32.ReleaseDC(0, hdc)
	old := win32.SelectFont(hdc, f.handles[style])
	defer win32.SelectFont(hdc, old)
	r := win32.DrawText(hdc, text, win32.Rect{}, win32.DT_CALCRECT|win32.DT_SINGLELINE|win32.DT_NOPREFIX)
	return r.Width(), r.Height()
}
