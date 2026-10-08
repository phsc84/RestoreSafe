package widget

import "github.com/phsc84/restoresafe/internal/gui/win32"

// headerPadding is the space before a column title, in DIPs, about the
// list view's own indent of a cell's text.
const headerPadding = 6

// StyleListHeader draws the column header of the list view lv on the
// secondary surface, with a line under it and between the columns, so the
// titles stand apart from the rows (GUI spec 3.3). Dragging a divider works as
// before. In high contrast the header keeps the system look.
func StyleListHeader(t *Theme, lv win32.HWND) {
	header := win32.ListHeader(lv)
	if header == 0 {
		return
	}
	win32.Subclass(lv, func(h win32.HWND, msg uint32, wparam, lparam uintptr, def func() uintptr) uintptr {
		if msg != win32.WM_NOTIFY || win32.HighContrastOn() {
			return def()
		}
		hdr := win32.NMHdrParam(lparam)
		if hdr.HwndFrom != header || hdr.Code != win32.NM_CUSTOMDRAW {
			return def()
		}
		// NMLVCUSTOMDRAW starts with the NMCUSTOMDRAW the header sends;
		// only those fields are read.
		cd := win32.ListDrawOf(hdr)
		switch cd.DrawStage {
		case win32.CDDS_PREPAINT:
			return win32.CDRF_NOTIFYITEMDRAW | win32.CDRF_NOTIFYPOSTPAINT
		case win32.CDDS_ITEMPREPAINT:
			drawHeaderItem(t, header, cd.HDC, cd.Rc, int(cd.ItemSpec))
			return win32.CDRF_SKIPDEFAULT
		case win32.CDDS_POSTPAINT:
			// The part right of the last column.
			client := win32.ClientRect(header)
			if n := win32.HeaderCount(header); n > 0 {
				client.Left = win32.HeaderItemRect(header, n-1).Right
			}
			if client.Left < client.Right {
				drawHeaderBack(t, cd.HDC, client)
			}
		}
		return win32.CDRF_DODEFAULT
	})
}

// drawHeaderBack fills r with the header's background and the line under
// it.
func drawHeaderBack(t *Theme, hdc uintptr, r win32.Rect) {
	fill(hdc, r, t.Palette.SurfaceAlt)
	fill(hdc, win32.Rect{Left: r.Left, Top: r.Bottom - 1, Right: r.Right, Bottom: r.Bottom}, t.Palette.Lines)
}

// drawHeaderItem draws column i in r: background, title and the divider at
// its right.
func drawHeaderItem(t *Theme, header win32.HWND, hdc uintptr, r win32.Rect, i int) {
	s := t.Scale
	drawHeaderBack(t, hdc, r)
	fill(hdc, win32.Rect{Left: r.Right - 1, Top: r.Top + s.Px(4), Right: r.Right, Bottom: r.Bottom - s.Px(4)}, t.Palette.Lines)
	title, right := win32.HeaderItem(header, i)
	tr := r
	tr.Left += s.Px(headerPadding)
	tr.Right -= s.Px(headerPadding)
	flags := uint32(win32.DT_SINGLELINE | win32.DT_VCENTER | win32.DT_END_ELLIPSIS | win32.DT_NOPREFIX)
	if right {
		flags |= win32.DT_RIGHT
	}
	text(hdc, t, title, tr, TextBody, t.Palette.Text, flags)
}
