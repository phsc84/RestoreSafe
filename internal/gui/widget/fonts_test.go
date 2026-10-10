package widget

import (
	"strings"
	"testing"
)

func TestFontsAtEveryDPI(t *testing.T) {
	for _, dpi := range []Scale{96, 120, 144, 192} {
		f, err := NewFonts(dpi)
		if err != nil {
			t.Fatal(err)
		}
		_, small := f.Measure("Backup", TextBody)
		_, large := f.Measure("Backup", TextHero)
		if small <= 0 || large <= small {
			t.Fatalf("%d dpi: body %d px, hero %d px: the hero must be larger", dpi, small, large)
		}
		if f.GlyphText(GlyphCheck) == "" {
			t.Fatal("every glyph must draw something")
		}
		f.Close()
	}
	f, err := NewFonts(96)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if !f.Glyphs {
		t.Log("Segoe Fluent Icons is missing on this system: icons fall back to text markers")
	}
	f.Glyphs = false
	if got := f.GlyphText(GlyphCheck); got != GlyphCheck.Fallback {
		t.Fatalf("without the icon font: %q", got)
	}
}

func TestMeasureWrappedGrowsWithTheText(t *testing.T) {
	th, _ := testTheme(t)
	one := th.Fonts.MeasureWrapped("Short.", TextBody, 300)
	long := th.Fonts.MeasureWrapped(strings.Repeat("A sentence that wraps. ", 20), TextBody, 300)
	if one <= 0 || long < 3*one {
		t.Fatalf("one line %d px, long text %d px", one, long)
	}
}
