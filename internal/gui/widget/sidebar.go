package widget

import "github.com/phsc84/restoresafe/internal/gui/win32"

// SidebarItem is an entry of the navigation.
type SidebarItem struct {
	Glyph Glyph
	Text  string
	// Bottom puts the item in the group at the sidebar's bottom edge,
	// below a line (GUI spec YK-1).
	Bottom bool
}

// Sidebar is the navigation: one tab stop, arrow keys, Home and End move
// the selection; a click selects. The selected item has a filled row and an
// accent bar on its left edge (GUI spec 3.2). Items can be hidden; the keys
// and the mouse skip them.
type Sidebar struct {
	hwnd     win32.HWND
	theme    *Theme
	items    []SidebarItem
	hidden   []bool
	selected int
	focused  bool
	// keyboard is whether the user reached or moved the sidebar with the
	// keyboard: only then does the selected row show the focus outline,
	// as in standard Windows controls.
	keyboard bool
	// OnSelect is called when the user selects another item.
	OnSelect func(index int)
}

// Sidebar padding around the rows, in DIPs.
const sidebarPadding = 8

// NewSidebar creates the navigation on parent.
func NewSidebar(t *Theme, parent win32.HWND, id uintptr, items []SidebarItem) (*Sidebar, error) {
	s := &Sidebar{theme: t, items: items, hidden: make([]bool, len(items))}
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
	if index < 0 || index >= len(s.items) || index == s.selected || s.hidden[index] {
		return
	}
	s.selected = index
	s.announce()
	win32.Invalidate(s.hwnd)
}

// SetHidden hides or shows item index. The caller selects another item
// before it hides the selected one.
func (s *Sidebar) SetHidden(index int, hidden bool) {
	if index < 0 || index >= len(s.items) || s.hidden[index] == hidden {
		return
	}
	s.hidden[index] = hidden
	win32.Invalidate(s.hwnd)
}

// Hidden tells whether item index is hidden.
func (s *Sidebar) Hidden(index int) bool { return s.hidden[index] }

// announce names the sidebar for screen readers after the selected item.
func (s *Sidebar) announce() {
	if len(s.items) > 0 {
		win32.SetAccessibleName(s.hwnd, "Navigation: "+s.items[s.selected].Text)
	}
}

// choose selects index and tells the owner.
func (s *Sidebar) choose(index int) {
	if index < 0 || index >= len(s.items) || index == s.selected || s.hidden[index] {
		return
	}
	s.Select(index)
	if s.OnSelect != nil {
		s.OnSelect(index)
	}
}

// step returns the next shown item from the selected one in direction dir
// (+1 or -1), or the selected one at the end.
func (s *Sidebar) step(dir int) int {
	for i := s.selected + dir; i >= 0 && i < len(s.items); i += dir {
		if !s.hidden[i] {
			return i
		}
	}
	return s.selected
}

// end returns the first (dir -1) or last (dir +1) shown item.
func (s *Sidebar) end(dir int) int {
	i := 0
	if dir > 0 {
		i = len(s.items) - 1
	}
	for ; i >= 0 && i < len(s.items); i -= dir {
		if !s.hidden[i] {
			return i
		}
	}
	return s.selected
}

// rowRect returns the rectangle of row i: the top group from the top edge,
// the bottom group from the bottom edge, both without hidden rows.
func (s *Sidebar) rowRect(i int, client win32.Rect) win32.Rect {
	sc := s.theme.Scale
	pad := sc.Px(sidebarPadding)
	row := sc.Px(SidebarRowHeight)
	if s.items[i].Bottom {
		below := 0
		for j := i + 1; j < len(s.items); j++ {
			if s.items[j].Bottom && !s.hidden[j] {
				below++
			}
		}
		bottom := client.Bottom - pad - int32(below)*row
		return win32.Rect{Left: client.Left + pad, Top: bottom - row, Right: client.Right - pad, Bottom: bottom}
	}
	above := 0
	for j := range i {
		if !s.items[j].Bottom && !s.hidden[j] {
			above++
		}
	}
	top := client.Top + pad + int32(above)*row
	return win32.Rect{Left: client.Left + pad, Top: top, Right: client.Right - pad, Bottom: top + row}
}

// lineRect returns the line above the bottom group; ok is false when the
// group shows no item.
func (s *Sidebar) lineRect(client win32.Rect) (win32.Rect, bool) {
	top := client.Bottom
	for i, item := range s.items {
		if item.Bottom && !s.hidden[i] {
			top = min(top, s.rowRect(i, client).Top)
		}
	}
	if top == client.Bottom {
		return win32.Rect{}, false
	}
	sc := s.theme.Scale
	y := top - sc.Px(sidebarPadding/2)
	return win32.Rect{Left: client.Left + sc.Px(sidebarPadding), Top: y, Right: client.Right - sc.Px(sidebarPadding), Bottom: y + max(sc.Px(1), 1)}, true
}

func (s *Sidebar) paint(hdc uintptr, r win32.Rect) {
	pal := s.theme.Palette
	sc := s.theme.Scale
	fill(hdc, r, pal.SurfaceAlt)
	if line, ok := s.lineRect(r); ok {
		fill(hdc, line, pal.Lines)
	}
	for i, item := range s.items {
		if s.hidden[i] {
			continue
		}
		row := s.rowRect(i, r)
		color := pal.TextSecondary
		if i == s.selected {
			color = pal.Text
			back := row
			back.Right--
			back.Bottom--
			line := pal.Control
			if s.focused && s.keyboard {
				line = pal.Text
			}
			roundRect(hdc, back, sc.Px(ControlRadius*2), pal.Control, line)
			if line != pal.Control {
				// A second pass inside the first makes the focus outline
				// 2 pixels wide.
				inner := win32.Rect{Left: back.Left + 1, Top: back.Top + 1, Right: back.Right - 1, Bottom: back.Bottom - 1}
				roundRect(hdc, inner, sc.Px(ControlRadius*2)-2, pal.Control, line)
			}
			bar := row
			bar.Right = bar.Left + sc.Px(SelectionBarWidth)
			inset := sc.Px(8)
			bar.Top += inset
			bar.Bottom -= inset
			fill(hdc, bar, pal.SelectionBar)
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
		if !s.keyboard {
			s.keyboard = true
			win32.Invalidate(hwnd)
		}
		switch wparam {
		case win32.VK_UP:
			s.choose(s.step(-1))
		case win32.VK_DOWN:
			s.choose(s.step(+1))
		case win32.VK_HOME:
			s.choose(s.end(-1))
		case win32.VK_END:
			s.choose(s.end(+1))
		default:
			return 0, false
		}
		return 0, true
	case win32.WM_LBUTTONDOWN:
		s.keyboard = false
		win32.SetFocus(hwnd)
		win32.Invalidate(hwnd)
		y := int32(int16(win32.HiWord(lparam)))
		client := win32.ClientRect(hwnd)
		for i := range s.items {
			if r := s.rowRect(i, client); !s.hidden[i] && y >= r.Top && y < r.Bottom {
				s.choose(i)
			}
		}
		return 0, true
	case win32.WM_SETFOCUS, win32.WM_KILLFOCUS:
		s.focused = msg == win32.WM_SETFOCUS
		// Reached with Tab: the outline shows. Focus that comes back with
		// the window (after a click) leaves it as it was.
		if s.focused && win32.KeyDown(win32.VK_TAB) {
			s.keyboard = true
		}
		win32.Invalidate(hwnd)
		return 0, true
	}
	return 0, false
}
