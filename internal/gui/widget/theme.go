package widget

// Color is a Windows COLORREF: 0x00BBGGRR.
type Color uint32

// RGB returns the color with the given components.
func RGB(r, g, b uint8) Color { return Color(r) | Color(g)<<8 | Color(b)<<16 }

// Palette are the colors of the interface (spec 3.3). Pages and widgets
// use roles, never literal colors.
type Palette struct {
	Text, TextSecondary   Color
	Surface, SurfaceAlt   Color // window content and cards; sidebar, title bars, group headers
	Lines                 Color // card borders, dividers
	Accent, AccentText    Color // primary button, selection bar; accent text and links
	Selection             Color // selected rows
	Success, SuccessBack  Color
	Warning, WarningBack  Color
	Error, ErrorBack      Color
	Full, FullBack        Color // FULL badge
	Diff, DiffBack        Color // DIFF badge
	Neutral, NeutralBack  Color // empty state, unknown
	BarBackups, BarOther  Color // storage bar segments; the free part uses SurfaceAlt
	SelectionBar, Control Color // sidebar selection bar; control backgrounds
	OnAccent              Color // text on Accent (the primary button)
}

// Light is the light palette of spec 3.3.
var Light = Palette{
	Text:          RGB(0x1B, 0x20, 0x26),
	TextSecondary: RGB(0x5A, 0x65, 0x72),
	Surface:       RGB(0xFF, 0xFF, 0xFF),
	SurfaceAlt:    RGB(0xF0, 0xF2, 0xF5),
	Lines:         RGB(0xD8, 0xDC, 0xE2),
	Accent:        RGB(0x0F, 0x6C, 0xBD),
	AccentText:    RGB(0x0B, 0x5C, 0xAD),
	Selection:     RGB(0xE3, 0xEF, 0xFB),
	Success:       RGB(0x17, 0x7A, 0x3C),
	SuccessBack:   RGB(0xE3, 0xF4, 0xEA),
	Warning:       RGB(0x8A, 0x5A, 0x00),
	WarningBack:   RGB(0xFD, 0xF1, 0xD3),
	Error:         RGB(0xB3, 0x26, 0x1E),
	ErrorBack:     RGB(0xFB, 0xE6, 0xE4),
	Full:          RGB(0x5B, 0x3F, 0xA6),
	FullBack:      RGB(0xEC, 0xE7, 0xF8),
	Diff:          RGB(0x0B, 0x5C, 0xAD),
	DiffBack:      RGB(0xE3, 0xEF, 0xFB),
	Neutral:       RGB(0x5A, 0x65, 0x72),
	NeutralBack:   RGB(0xE2, 0xE6, 0xEA),
	BarBackups:    RGB(0x0F, 0x6C, 0xBD),
	BarOther:      RGB(0xB9, 0xC0, 0xC9),
	SelectionBar:  RGB(0x0F, 0x6C, 0xBD),
	Control:       RGB(0xE2, 0xE6, 0xEA),
	OnAccent:      RGB(0xFF, 0xFF, 0xFF),
}

// Sizes in DIPs (spec 3.2 and 3.3).
const (
	WindowWidth, WindowHeight       = 1000, 700
	WindowMinWidth, WindowMinHeight = 820, 600
	SidebarWidth                    = 160
	SidebarRowHeight                = 34
	StatusBarHeight                 = 24
	ContentPaddingX                 = 18
	ContentPaddingY                 = 16
	CardGap                         = 12
	CardPadding                     = 12
	CardRadius                      = 8
	ControlRadius                   = 4
	ButtonHeight                    = 28
	SmallButtonHeight               = 26
	EditHeight                      = 26
	ListRowHeight                   = 28
	HeroIconSize                    = 52
	SelectionBarWidth               = 3
)

// TextStyle names a font role.
type TextStyle int

const (
	// TextBody is body text, 13 px.
	TextBody TextStyle = iota
	// TextSmall is secondary and table text, 12 px.
	TextSmall
	// TextCaption is captions and the status bar, 11 px.
	TextCaption
	// TextHero is the hero title, 20 px semibold.
	TextHero
	// TextTitle is a page title, 18 px semibold.
	TextTitle
	// TextStrong is a card heading, 13 px semibold.
	TextStrong
	// TextMono is the log and IDs, Consolas 12 px.
	TextMono
	// TextIcon is an icon glyph of the hero, 28 px Segoe Fluent Icons.
	TextIcon
	// TextIconSmall is an icon glyph in text, 16 px.
	TextIconSmall
	textStyles
)

// textPx are the font sizes of the styles, in DIPs at 96 dpi.
var textPx = [textStyles]int32{13, 12, 11, 20, 18, 13, 12, 28, 16}

// Glyph is an icon of the Segoe Fluent Icons font, with a text marker for
// systems without it.
type Glyph struct {
	Code     rune
	Fallback string
}

// The icons the interface uses.
var (
	GlyphCheck    = Glyph{0xE73E, "✔"}
	GlyphWarning  = Glyph{0xE7BA, "⚠"}
	GlyphError    = Glyph{0xE711, "✖"}
	GlyphInfo     = Glyph{0xE946, "ⓘ"}
	GlyphShield   = Glyph{0xEA18, "◯"}
	GlyphArchive  = Glyph{0xE7B8, "▤"}
	GlyphHistory  = Glyph{0xE81C, "≡"}
	GlyphSettings = Glyph{0xE713, "⚙"}
	GlyphFolder   = Glyph{0xE8B7, "▭"}
	GlyphDrive    = Glyph{0xEDA2, "▭"}
	GlyphKey      = Glyph{0xE8D7, "⚿"}
	GlyphPlay     = Glyph{0xE768, "▶"}
	GlyphRefresh  = Glyph{0xE72C, "↻"}
)

// FontFace is the face of the interface text; Windows 11 has it. Without
// it, Windows substitutes the closest face.
const (
	FontFace     = "Segoe UI Variable Text"
	MonoFontFace = "Consolas"
	IconFontFace = "Segoe Fluent Icons"
)
