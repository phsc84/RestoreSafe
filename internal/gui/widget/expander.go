package widget

import "github.com/phsc84/restoresafe/internal/gui/win32"

// Chevrons of an expander.
var (
	GlyphChevronDown = Glyph{0xE70D, "▾"}
	GlyphChevronUp   = Glyph{0xE70E, "▴"}
)

// Expander is the title of a part that opens and closes: the title as a
// card heading, with a chevron after it (GUI spec YK-8, YK-9). It is one
// tab stop; a click, Enter and Space toggle it.
type Expander struct {
	hwnd    win32.HWND
	theme   *Theme
	back    Color
	title   string
	open    bool
	focused bool
	// keyboard is whether the user reached it with the keyboard: only then
	// does it show the focus outline, as standard controls do.
	keyboard bool
	hot      bool
	// OnToggle is called after a click or a key opened or closed it.
	OnToggle func(open bool)
}

// NewExpander creates an expander on parent, whose background is back.
func NewExpander(t *Theme, parent win32.HWND, back Color, id uintptr) (*Expander, error) {
	e := &Expander{theme: t, back: back}
	hwnd, err := create(e, classExpander, parent, win32.WS_TABSTOP, 0, id)
	if err != nil {
		return nil, err
	}
	e.hwnd = hwnd
	return e, nil
}

// HWND returns the expander's window.
func (e *Expander) HWND() win32.HWND { return e.hwnd }

// Set shows title, open or closed.
func (e *Expander) Set(title string, open bool) {
	e.title, e.open = title, open
	state := "collapsed"
	if open {
		state = "expanded"
	}
	win32.SetAccessibleName(e.hwnd, title+", "+state)
	win32.Invalidate(e.hwnd)
}

// Open tells whether it is open.
func (e *Expander) Open() bool { return e.open }

// Width returns the width it needs, in pixels.
func (e *Expander) Width() int32 {
	w, _ := e.theme.Fonts.Measure(e.title, TextStrong)
	return w + e.theme.Scale.Px(36)
}

func (e *Expander) toggle() {
	e.Set(e.title, !e.open)
	if e.OnToggle != nil {
		e.OnToggle(e.open)
	}
}

func (e *Expander) paint(hdc uintptr, r win32.Rect) {
	pal := e.theme.Palette
	sc := e.theme.Scale
	fill(hdc, r, e.back)
	if e.focused && e.keyboard {
		inner := r
		inner.Right--
		inner.Bottom--
		roundRect(hdc, inner, sc.Px(ControlRadius*2), e.back, pal.Text)
	}
	color := pal.Text
	if e.hot {
		color = pal.AccentText
	}
	label := r
	label.Left += sc.Px(2)
	text(hdc, e.theme, e.title, label, TextStrong, color, win32.DT_LEFT|win32.DT_VCENTER|win32.DT_SINGLELINE|win32.DT_NOPREFIX|win32.DT_END_ELLIPSIS)
	w, _ := e.theme.Fonts.Measure(e.title, TextStrong)
	chevron := r
	chevron.Left = label.Left + w + sc.Px(8)
	chevron.Right = min(chevron.Left+sc.Px(20), r.Right)
	glyph := GlyphChevronDown
	if e.open {
		glyph = GlyphChevronUp
	}
	style := TextIconSmall
	if !e.theme.Fonts.Glyphs {
		style = TextBody
	}
	text(hdc, e.theme, e.theme.Fonts.GlyphText(glyph), chevron, style, color, win32.DT_CENTER|win32.DT_VCENTER|win32.DT_SINGLELINE|win32.DT_NOPREFIX)
}

func (e *Expander) message(hwnd win32.HWND, msg uint32, wparam, lparam uintptr) (uintptr, bool) {
	switch msg {
	case win32.WM_GETDLGCODE:
		// Enter and Space reach the expander instead of the dialog manager.
		if m := win32.MsgParam(lparam); m != nil && m.Message == win32.WM_KEYDOWN && (m.WParam == win32.VK_RETURN || m.WParam == win32.VK_SPACE) {
			return win32.DLGC_WANTMESSAGE, true
		}
		return 0, true
	case win32.WM_KEYDOWN:
		if wparam == win32.VK_RETURN || wparam == win32.VK_SPACE {
			e.keyboard = true
			e.toggle()
			return 0, true
		}
		return 0, false
	case win32.WM_LBUTTONDOWN:
		e.keyboard = false
		win32.SetFocus(hwnd)
		e.toggle()
		return 0, true
	case win32.WM_MOUSEMOVE:
		if !e.hot {
			e.hot = true
			win32.TrackMouseLeave(hwnd)
			win32.Invalidate(hwnd)
		}
		return 0, true
	case win32.WM_MOUSELEAVE:
		e.hot = false
		win32.Invalidate(hwnd)
		return 0, true
	case win32.WM_SETFOCUS, win32.WM_KILLFOCUS:
		e.focused = msg == win32.WM_SETFOCUS
		if e.focused && win32.KeyDown(win32.VK_TAB) {
			e.keyboard = true
		}
		win32.Invalidate(hwnd)
		return 0, true
	}
	return 0, false
}
