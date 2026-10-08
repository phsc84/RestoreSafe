package gui

import (
	"github.com/phsc84/restoresafe/internal/gui/view"
	"github.com/phsc84/restoresafe/internal/gui/widget"
)

// The colors and glyphs of the widgets for the tones, badges, segments and
// glyphs of package view.

// toneColor is the text color of a tone.
func toneColor(p widget.Palette, t view.Tone) widget.Color {
	switch t {
	case view.ToneSuccess:
		return p.Success
	case view.ToneWarning:
		return p.Warning
	case view.ToneError:
		return p.Error
	case view.ToneInfo:
		return p.AccentText
	case view.ToneSecondary:
		return p.TextSecondary
	}
	return p.Text
}

// heroColors are the colors of a hero's glyph and of the circle behind it.
func heroColors(p widget.Palette, t view.Tone) (fore, circle widget.Color) {
	switch t {
	case view.ToneSuccess:
		return p.Success, p.SuccessBack
	case view.ToneWarning:
		return p.Warning, p.WarningBack
	case view.ToneError:
		return p.Error, p.ErrorBack
	}
	return p.Neutral, p.NeutralBack
}

// badgeColors are the colors of a backup type badge (FULL, DIFF).
func badgeColors(p widget.Palette, k view.BadgeKind) (fore, back widget.Color) {
	if k == view.BadgeDiff {
		return p.Diff, p.DiffBack
	}
	return p.Full, p.FullBack
}

// segmentColor is the color of a segment of the storage bar.
func segmentColor(p widget.Palette, k view.SegmentKind) widget.Color {
	if k == view.SegmentBackups {
		return p.BarBackups
	}
	return p.BarOther
}

// glyphOf is the widget glyph of a view glyph.
func glyphOf(g view.Glyph) widget.Glyph {
	switch g {
	case view.GlyphCheck:
		return widget.GlyphCheck
	case view.GlyphWarning:
		return widget.GlyphWarning
	case view.GlyphError:
		return widget.GlyphError
	case view.GlyphInfo:
		return widget.GlyphInfo
	case view.GlyphFolder:
		return widget.GlyphFolder
	case view.GlyphDrive:
		return widget.GlyphDrive
	case view.GlyphKey:
		return widget.GlyphKey
	}
	return widget.GlyphShield
}
