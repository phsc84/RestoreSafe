package widget

import "RestoreSafe/internal/gui/win32"

// HighContrast is the palette of the Windows high-contrast theme in use:
// the system colors. Status keeps its meaning through glyphs and words
// (GUI spec 15), so the semantic colors are the text color.
func HighContrast() Palette {
	c := func(index int32) Color { return Color(win32.SysColor(index)) }
	text, back, face := c(win32.COLOR_WINDOWTEXT), c(win32.COLOR_WINDOW), c(win32.COLOR_BTNFACE)
	highlight, link := c(win32.COLOR_HIGHLIGHT), c(win32.COLOR_HOTLIGHT)
	return Palette{
		Text: text, TextSecondary: text,
		Surface: back, SurfaceAlt: face,
		Lines:  text,
		Accent: highlight, AccentText: link, OnAccent: c(win32.COLOR_HIGHLIGHTTEXT),
		Selection: highlight,
		Success:   text, SuccessBack: back,
		Warning: text, WarningBack: back,
		Error: text, ErrorBack: back,
		Full: c(win32.COLOR_BTNTEXT), FullBack: face,
		Diff: c(win32.COLOR_BTNTEXT), DiffBack: face,
		Neutral: text, NeutralBack: back,
		BarBackups: highlight, BarOther: c(win32.COLOR_GRAYTEXT),
		SelectionBar: highlight, Control: face,
	}
}

// CurrentPalette is the palette for the system settings: high contrast
// when Windows uses it, Light otherwise.
func CurrentPalette() Palette {
	if win32.HighContrastOn() {
		return HighContrast()
	}
	return Light
}
