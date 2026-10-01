package widget

import "RestoreSafe/internal/gui/win32"

// SidebarItem is an entry of the navigation.
type SidebarItem struct {
	Glyph Glyph
	Text  string
}

// Sidebar is the navigation: one tab stop, arrow keys, Home and End move
// the selection; a click selects. The selected item has a filled row and an
// accent bar on its left edge (spec 3.2).
type Sidebar struct {
	hwnd     win32.HWND
	theme    *Theme
	items    []SidebarItem
	selected int
	focused  bool
	// OnSelect is called when the user selects another item.
	OnSelect func(index int)
}

// Sidebar padding around the rows, in DIPs.
const sidebarPadding = 8

// NewSidebar creates the navigation on parent.
func NewSidebar(t *Theme, parent win32.HWND, id uintptr, items []SidebarItem) (*Sidebar, error) {
	s := &Sidebar{theme: t, items: items}
	hwnd, err := create(s, classSidebar, parent, win32.WS_TABSTOP, 0, id)
	if err != nil {
		return nil, err
	}
	s.hwnd = hwnd
	s.announce()
	return s, nil
}

// HWND returns the sidebar's window.
func (s *Sidebar) HWND() win32.HWND { return s.hwnd }

// Selected returns the index of the selected item.
func (s *Sidebar) Selected() int { return s.selected }

// Select selects item index without calling OnSelect.
func (s *Sidebar) Select(index int) {
	if index < 0 || index >= len(s.items) || index == s.selected {
		return
	}
	s.selected = index
	s.announce()
	win32.Invalidate(s.hwnd)
}

// announce names the sidebar for screen readers after the selected item.
func (s *Sidebar) announce() {
	if len(s.items) > 0 {
		win32.SetAccessibleName(s.hwnd, "Navigation: "+s.items[s.selected].Text)
	}
}

// choose selects index and tells the owner.
func (s *Sidebar) choose(index int) {
	index = max(0, min(index, len(s.items)-1))
	if index == s.selected {
		return
	}
	s.Select(index)
	if s.OnSelect != nil {
		s.OnSelect(index)
	}
}

// rowRect returns the rectangle of row i.
func (s *Sidebar) rowRect(i int, client win32.Rect) win32.Rect {
	sc := s.theme.Scale
	pad := sc.Px(sidebarPadding)
	top := client.Top + pad + int32(i)*sc.Px(SidebarRowHeight)
	return win32.Rect{Left: client.Left + pad, Top: top, Right: client.Right - pad, Bottom: top + sc.Px(SidebarRowHeight)}
}

func (s *Sidebar) paint(hdc uintptr, r win32.Rect) {
	pal := s.theme.Palette
	sc := s.theme.Scale
	fill(hdc, r, pal.SurfaceAlt)
	for i, item := range s.items {
		row := s.rowRect(i, r)
		color := pal.TextSecondary
		if i == s.selected {
			color = pal.Text
			back := row
			back.Right--
			back.Bottom--
			roundRect(hdc, back, sc.Px(ControlRadius*2), pal.Control, pal.Control)
			bar := row
			bar.Right = bar.Left + sc.Px(SelectionBarWidth)
			inset := sc.Px(8)
			bar.Top += inset
			bar.Bottom -= inset
			fill(hdc, bar, pal.SelectionBar)
			if s.focused {
				win32.DrawFocusRect(hdc, row)
			}
		}
		glyph := row
		glyph.Left += sc.Px(12)
		glyph.Right = glyph.Left + sc.Px(20)
		style := TextIconSmall
		if !s.theme.Fonts.Glyphs {
			style = TextBody
		}
		text(hdc, s.theme, s.theme.Fonts.GlyphText(item.Glyph), glyph, style, color, win32.DT_CENTER|win32.DT_VCENTER|win32.DT_SINGLELINE|win32.DT_NOPREFIX)
		label := row
		label.Left = glyph.Right + sc.Px(8)
		text(hdc, s.theme, item.Text, label, TextBody, color, win32.DT_LEFT|win32.DT_VCENTER|win32.DT_SINGLELINE|win32.DT_NOPREFIX|win32.DT_END_ELLIPSIS)
	}
}

func (s *Sidebar) message(hwnd win32.HWND, msg uint32, wparam, lparam uintptr) (uintptr, bool) {
	switch msg {
	case win32.WM_GETDLGCODE:
		return win32.DLGC_WANTARROWS, true
	case win32.WM_KEYDOWN:
		switch wparam {
		case win32.VK_UP:
			s.choose(s.selected - 1)
		case win32.VK_DOWN:
			s.choose(s.selected + 1)
		case win32.VK_HOME:
			s.choose(0)
		case win32.VK_END:
			s.choose(len(s.items) - 1)
		default:
			return 0, false
		}
		return 0, true
	case win32.WM_LBUTTONDOWN:
		win32.SetFocus(hwnd)
		y := int32(int16(win32.HiWord(lparam)))
		client := win32.ClientRect(hwnd)
		for i := range s.items {
			if r := s.rowRect(i, client); y >= r.Top && y < r.Bottom {
				s.choose(i)
			}
		}
		return 0, true
	case win32.WM_SETFOCUS, win32.WM_KILLFOCUS:
		s.focused = msg == win32.WM_SETFOCUS
		win32.Invalidate(hwnd)
		return 0, true
	}
	return 0, false
}
