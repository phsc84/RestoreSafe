package widget

import (
	"RestoreSafe/internal/gui/win32"
	"strings"

	"golang.org/x/sys/windows"
)

// PanelStyle is the look of a panel.
type PanelStyle struct {
	// Back is the background; Outer the color around a card's rounded
	// corners (the parent's background).
	Back, Outer Color
	// Card draws a rounded border.
	Card bool
}

// Panel is a container: pages and cards. It paints its background (and a
// card's border), gives its text controls their colors, draws its primary
// buttons, and passes commands and link clicks to OnCommand. All text is in
// standard controls, so screen readers and UI Automation see names, roles
// and control IDs (spec 3.4).
type Panel struct {
	hwnd     win32.HWND
	theme    *Theme
	style    PanelStyle
	children []child
	brush    windows.Handle
	// OnCommand receives the clicks of buttons and links (id, BN_CLICKED or
	// 0 for a link) and the notifications of other controls.
	OnCommand func(id, code uint16)
	// OnNotify receives WM_NOTIFY of controls other than links.
	OnNotify func(hdr *win32.NMHdr) uintptr
}

// child is a control the panel created; it styles it.
type child struct {
	hwnd    win32.HWND
	style   TextStyle
	color   Color
	primary bool
	link    bool
	styled  bool // false for widgets that draw their own text
}

// NewPanel creates a panel as a child of parent.
func NewPanel(t *Theme, parent win32.HWND, id uintptr, style PanelStyle) (*Panel, error) {
	p := &Panel{theme: t, style: style}
	hwnd, err := create(p, classPanel, parent, win32.WS_CLIPCHILDREN, win32.WS_EX_CONTROLPARENT, id)
	if err != nil {
		return nil, err
	}
	p.hwnd = hwnd
	return p, nil
}

// HWND returns the panel's window.
func (p *Panel) HWND() win32.HWND { return p.hwnd }

// Back returns the panel's background, for the widgets on it.
func (p *Panel) Back() Color { return p.style.Back }

// Label creates a text control. Its text is shown as is (no & prefixes).
func (p *Panel) Label(text string, style TextStyle, color Color) win32.HWND {
	return p.add(child{style: style, color: color, styled: true}, "STATIC", text, win32.SS_NOPREFIX|win32.SS_ENDELLIPSIS, 0)
}

// Button creates a standard push button; & marks its access key.
func (p *Panel) Button(text string, id uintptr) win32.HWND {
	return p.add(child{style: TextBody, styled: true}, "BUTTON", text, win32.WS_TABSTOP|win32.BS_PUSHBUTTON, id)
}

// PrimaryButton creates the accent-filled button of the page (spec 2:
// one per window).
func (p *Panel) PrimaryButton(text string, id uintptr) win32.HWND {
	return p.add(child{style: TextBody, primary: true, styled: true}, "BUTTON", text, win32.WS_TABSTOP|win32.BS_OWNERDRAW, id)
}

// Link creates a link; its click reaches OnCommand with code 0.
func (p *Panel) Link(text string, id uintptr) win32.HWND {
	markup := "<a>" + strings.ReplaceAll(text, "<", "") + "</a>"
	return p.add(child{style: TextSmall, link: true, styled: true}, win32.WC_LINK, markup, win32.WS_TABSTOP|win32.LWS_TRANSPARENT, id)
}

// Adopt makes the panel destroy hwnd with its other children on Clear.
func (p *Panel) Adopt(hwnd win32.HWND) { p.children = append(p.children, child{hwnd: hwnd}) }

func (p *Panel) add(c child, class, text string, style uint32, id uintptr) win32.HWND {
	hwnd, err := win32.CreateWindow(0, class, text, win32.WS_CHILD|win32.WS_VISIBLE|style, 0, 0, 0, 0, p.hwnd, id)
	if err != nil {
		return 0
	}
	c.hwnd = hwnd
	win32.SetFont(hwnd, p.theme.Fonts.Get(c.style))
	p.children = append(p.children, c)
	return hwnd
}

// SetColor changes the text color of a label.
func (p *Panel) SetColor(hwnd win32.HWND, color Color) {
	for i := range p.children {
		if p.children[i].hwnd == hwnd {
			p.children[i].color = color
			win32.Invalidate(hwnd)
		}
	}
}

// Clear destroys the panel's children.
func (p *Panel) Clear() {
	for _, c := range p.children {
		win32.DestroyWindow(c.hwnd)
	}
	p.children = nil
}

// Restyle applies the theme's current fonts, after a DPI change.
func (p *Panel) Restyle() {
	for _, c := range p.children {
		if c.styled {
			win32.SetFont(c.hwnd, p.theme.Fonts.Get(c.style))
		}
	}
	win32.Invalidate(p.hwnd)
}

// Destroy destroys the panel and its children.
func (p *Panel) Destroy() {
	win32.DestroyWindow(p.hwnd)
	if p.brush != 0 {
		win32.DeleteObject(p.brush)
		p.brush = 0
	}
}

func (p *Panel) paint(hdc uintptr, r win32.Rect) {
	if !p.style.Card {
		fill(hdc, r, p.style.Back)
		return
	}
	fill(hdc, r, p.style.Outer)
	s := p.theme.Scale
	r.Right--
	r.Bottom--
	roundRect(hdc, r, s.Px(CardRadius*2), p.style.Back, p.theme.Palette.Lines)
}

func (p *Panel) backBrush() windows.Handle {
	if p.brush == 0 {
		p.brush = win32.CreateSolidBrush(uint32(p.style.Back))
	}
	return p.brush
}

func (p *Panel) find(hwnd win32.HWND) *child {
	for i := range p.children {
		if p.children[i].hwnd == hwnd {
			return &p.children[i]
		}
	}
	return nil
}

func (p *Panel) message(hwnd win32.HWND, msg uint32, wparam, lparam uintptr) (uintptr, bool) {
	switch msg {
	case win32.WM_CTLCOLORSTATIC:
		win32.SetBkModeTransparent(wparam)
		color := p.theme.Palette.Text
		if c := p.find(win32.HWND(lparam)); c != nil && c.color != 0 {
			color = c.color
		}
		win32.SetTextColor(wparam, uint32(color))
		return uintptr(p.backBrush()), true
	case win32.WM_CTLCOLORBTN:
		return uintptr(p.backBrush()), true
	case win32.WM_COMMAND:
		if p.OnCommand != nil {
			p.OnCommand(win32.LoWord(wparam), win32.HiWord(wparam))
		}
		return 0, true
	case win32.WM_NOTIFY:
		hdr := win32.NMHdrParam(lparam)
		if c := p.find(hdr.HwndFrom); c != nil && c.link && (hdr.Code == win32.NM_CLICK || hdr.Code == win32.NM_RETURN) {
			if p.OnCommand != nil {
				p.OnCommand(uint16(hdr.IDFrom), 0)
			}
			return 0, true
		}
		if p.OnNotify != nil {
			return p.OnNotify(hdr), true
		}
		return 0, true
	case win32.WM_DRAWITEM:
		di := win32.DrawItemParam(lparam)
		if c := p.find(di.HwndItem); c != nil && c.primary {
			p.drawPrimary(di)
			return 1, true
		}
	case win32.WM_DESTROY:
		if p.brush != 0 {
			win32.DeleteObject(p.brush)
			p.brush = 0
		}
	}
	return 0, false
}

// drawPrimary draws an accent-filled button with white text.
func (p *Panel) drawPrimary(di *win32.DrawItemStruct) {
	pal := p.theme.Palette
	back, fore := pal.Accent, RGB(0xFF, 0xFF, 0xFF)
	switch {
	case di.ItemState&win32.ODS_DISABLED != 0:
		back, fore = pal.Control, pal.TextSecondary
	case di.ItemState&win32.ODS_SELECTED != 0:
		back = pal.AccentText
	}
	r := di.Item
	fill(di.HDC, r, p.style.Back)
	inner := r
	inner.Right--
	inner.Bottom--
	roundRect(di.HDC, inner, p.theme.Scale.Px(ControlRadius*2), back, back)
	flags := uint32(win32.DT_CENTER | win32.DT_VCENTER | win32.DT_SINGLELINE)
	if di.ItemState&win32.ODS_NOACCEL != 0 {
		flags |= win32.DT_HIDEPREFIX
	}
	text(di.HDC, p.theme, win32.Text(di.HwndItem), r, TextBody, fore, flags)
	if di.ItemState&win32.ODS_FOCUS != 0 {
		focus := r
		inset := p.theme.Scale.Px(3)
		focus.Left += inset
		focus.Top += inset
		focus.Right -= inset
		focus.Bottom -= inset
		win32.DrawFocusRect(di.HDC, focus)
	}
}

// RightLabel creates a right-aligned text control.
func (p *Panel) RightLabel(text string, style TextStyle, color Color) win32.HWND {
	return p.add(child{style: style, color: color, styled: true}, "STATIC", text, win32.SS_NOPREFIX|win32.SS_ENDELLIPSIS|win32.SS_RIGHT, 0)
}

// SetText changes the text of a control.
func (p *Panel) SetText(hwnd win32.HWND, text string) { win32.SetText(hwnd, text) }

// Show shows or hides the panel. A hidden panel is also disabled, so the
// access keys of its controls cannot fire.
func (p *Panel) Show(shown bool) {
	win32.SetVisible(p.hwnd, shown)
	win32.Enable(p.hwnd, shown)
}

// Paragraph creates a text control that wraps its text at word breaks;
// MeasureWrapped tells the height it needs.
func (p *Panel) Paragraph(text string, style TextStyle, color Color) win32.HWND {
	return p.add(child{style: style, color: color, styled: true}, "STATIC", text, win32.SS_NOPREFIX, 0)
}

// PathLabel creates a one-line text control that shortens a path in its
// text in the middle ("C:\Users\...\Backups") when it does not fit.
func (p *Panel) PathLabel(text string, style TextStyle, color Color) win32.HWND {
	return p.add(child{style: style, color: color, styled: true}, "STATIC", text, win32.SS_NOPREFIX|win32.SS_PATHELLIPSIS, 0)
}
