package widget

import (
	"RestoreSafe/internal/gui/win32"
	"math"
	"testing"
)

// contrast is the WCAG 2 contrast ratio of two colors.
func contrast(a, b Color) float64 {
	lum := func(c Color) float64 {
		channel := func(v uint8) float64 {
			s := float64(v) / 255
			if s <= 0.03928 {
				return s / 12.92
			}
			return math.Pow((s+0.055)/1.055, 2.4)
		}
		r, g, bl := uint8(c), uint8(c>>8), uint8(c>>16)
		return 0.2126*channel(r) + 0.7152*channel(g) + 0.0722*channel(bl)
	}
	l1, l2 := lum(a), lum(b)
	if l1 < l2 {
		l1, l2 = l2, l1
	}
	return (l1 + 0.05) / (l2 + 0.05)
}

// TestPaletteTextContrast checks spec 15: text has a contrast of at least
// 4.5:1 on every background it is drawn on.
func TestPaletteTextContrast(t *testing.T) {
	t.Parallel()
	p := Light
	white := RGB(0xFF, 0xFF, 0xFF)
	for name, pair := range map[string][2]Color{
		"text on surface":           {p.Text, p.Surface},
		"text on alternate surface": {p.Text, p.SurfaceAlt},
		"text on selection":         {p.Text, p.Selection},
		"secondary on surface":      {p.TextSecondary, p.Surface},
		"secondary on alt surface":  {p.TextSecondary, p.SurfaceAlt},
		"accent text on surface":    {p.AccentText, p.Surface},
		"button text on accent":     {white, p.Accent},
		"success on its background": {p.Success, p.SuccessBack},
		"warning on its background": {p.Warning, p.WarningBack},
		"error on its background":   {p.Error, p.ErrorBack},
		"success on surface":        {p.Success, p.Surface},
		"warning on surface":        {p.Warning, p.Surface},
		"error on surface":          {p.Error, p.Surface},
		"full badge":                {p.Full, p.FullBack},
		"diff badge":                {p.Diff, p.DiffBack},
		"neutral on its background": {p.Neutral, p.NeutralBack},
	} {
		if c := contrast(pair[0], pair[1]); c < 4.5 {
			t.Errorf("%s: contrast %.2f, want at least 4.5", name, c)
		}
	}
}

func TestHighContrastUsesTheSystemColors(t *testing.T) {
	p := HighContrast()
	if p.Text != Color(win32.SysColor(win32.COLOR_WINDOWTEXT)) || p.Surface != Color(win32.SysColor(win32.COLOR_WINDOW)) || p.OnAccent != Color(win32.SysColor(win32.COLOR_HIGHLIGHTTEXT)) {
		t.Fatalf("palette %+v", p)
	}
	if p.Success != p.Text || p.Error != p.Text {
		t.Fatal("in high contrast, status is carried by glyphs and words, not by color")
	}
}
