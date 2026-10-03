package gui

import (
	"RestoreSafe/internal/gui/view"
	"RestoreSafe/internal/gui/widget"
	"RestoreSafe/internal/gui/win32"
)

// table shows a view.Table in a list view whose columns the user can
// widen. It is not a child the parent panel owns: a card or dialog that
// clears and rebuilds its rows keeps the table, with its column widths and
// scroll position.
type table struct {
	theme *widget.Theme
	hwnd  win32.HWND
	v     view.Table
	sig   string
	// ncols is the number of columns the list has.
	ncols int
	// rowsSig is the first column of the rows the items were built for.
	rowsSig string
	// fitted is the client width the columns were fitted to: the user's
	// widths stay until the table's width changes.
	fitted int32
}

// Heights of a table, in rows.
const tableMaxRows = 5

func newTable(t *widget.Theme, parent win32.HWND, id uintptr) *table {
	h, err := win32.CreateWindow(0, win32.WC_LISTVIEW, "",
		win32.WS_CHILD|win32.WS_VISIBLE|win32.WS_TABSTOP|win32.WS_BORDER|win32.LVS_REPORT|win32.LVS_SINGLESEL|win32.LVS_NOSORTHEADER,
		0, 0, 0, 0, parent, id)
	if err != nil {
		return &table{theme: t}
	}
	win32.ListSetupPlain(h, false)
	widget.StyleListHeader(t, h)
	win32.ListEnableInfoTips(h)
	tb := &table{theme: t, hwnd: h}
	tb.restyle()
	return tb
}

// set shows v: new columns when they changed, new items when the rows
// changed, otherwise the texts in place.
func (tb *table) set(v view.Table) {
	if tb.hwnd == 0 {
		return
	}
	s := tb.theme.Scale
	if sig := v.Signature(); sig != tb.sig {
		win32.ListClear(tb.hwnd)
		win32.ListDeleteColumns(tb.hwnd, tb.ncols)
		for i, c := range v.Columns {
			win32.ListInsertColumn(tb.hwnd, i, c.Title, s.Px(max(c.Width, 60)), c.Right)
		}
		tb.ncols = len(v.Columns)
		win32.SetAccessibleName(tb.hwnd, v.Name)
		tb.sig, tb.rowsSig, tb.fitted = sig, "\x01", 0
	}
	tb.v = v
	rowsSig := ""
	for _, r := range v.Rows {
		if len(r.Cells) > 0 {
			rowsSig += r.Cells[0].Text
		}
		rowsSig += "\x00"
	}
	if rowsSig != tb.rowsSig {
		win32.SendMessage(tb.hwnd, win32.WM_SETREDRAW, 0, 0)
		win32.ListClear(tb.hwnd)
		for i, r := range v.Rows {
			item := win32.ListAddItem(tb.hwnd, cellText(r, 0), uintptr(i+1))
			for col := 1; col < len(v.Columns); col++ {
				win32.ListSetText(tb.hwnd, item, col, cellText(r, col))
			}
		}
		win32.SendMessage(tb.hwnd, win32.WM_SETREDRAW, 1, 0)
		tb.rowsSig = rowsSig
	} else {
		for i, r := range v.Rows {
			for col := 1; col < len(v.Columns); col++ {
				win32.ListSetText(tb.hwnd, i, col, cellText(r, col))
			}
		}
	}
	win32.Invalidate(tb.hwnd)
}

// cellText is the text of a cell; a badge's text is drawn over it.
func cellText(r view.TableRow, col int) string {
	if col >= len(r.Cells) {
		return ""
	}
	c := r.Cells[col]
	if c.Badge != nil {
		return c.Badge.Text
	}
	return c.Text
}

// height returns the height in DIPs the table needs to show its rows, at
// most tableMaxRows; more rows scroll within it.
func (tb *table) height() int32 {
	if tb.hwnd == 0 {
		return 0
	}
	s := tb.theme.Scale
	n := min(max(len(tb.v.Rows), 1), tableMaxRows)
	px := win32.ListViewHeight(tb.hwnd, n) + 2*win32.SystemMetric(win32.SM_CXBORDER, uint32(s)) + s.Px(2)
	return (px*96 + int32(s) - 1) / int32(s)
}

// place puts the table at r and fits the fill columns to its width.
func (tb *table) place(r win32.Rect) {
	if tb.hwnd == 0 {
		return
	}
	win32.SetWindowPos(tb.hwnd, r)
	tb.fit()
}

// fit gives the fill columns the width the others leave, when the table's
// width changed since the last fit.
func (tb *table) fit() {
	width := win32.ClientRect(tb.hwnd).Width()
	if width <= 0 || width == tb.fitted {
		return
	}
	tb.fitted = width
	s := tb.theme.Scale
	fixed, fills := int32(0), int32(0)
	for i, c := range tb.v.Columns {
		if c.Fill {
			fills++
			continue
		}
		fixed += win32.ListColumnWidth(tb.hwnd, i)
	}
	if fills == 0 {
		return
	}
	w := max((width-fixed)/fills, s.Px(80))
	for i, c := range tb.v.Columns {
		if c.Fill {
			win32.ListSetColumnWidth(tb.hwnd, i, w)
		}
	}
}

// show shows or hides the table.
func (tb *table) show(shown bool) {
	if tb.hwnd != 0 {
		setShown(tb.hwnd, shown)
	}
}

// restyle applies the theme's font after creation and DPI changes.
func (tb *table) restyle() {
	if tb.hwnd == 0 {
		return
	}
	win32.SetFont(tb.hwnd, tb.theme.Fonts.Get(widget.TextBody))
	// New columns get the widths for the new scale.
	tb.sig = ""
	if tb.ncols > 0 {
		tb.set(tb.v)
	}
}

// notify handles the table's notifications: tooltips and colors. It
// returns false for another control's.
func (tb *table) notify(hdr *win32.NMHdr) (uintptr, bool) {
	if tb.hwnd == 0 || hdr.HwndFrom != tb.hwnd {
		return 0, false
	}
	switch hdr.Code {
	case win32.LVN_GETINFOTIP:
		n := win32.ListInfoTipOf(hdr)
		if r, ok := tb.row(int(n.Item)); ok {
			n.SetText(r.Tip)
		}
	case win32.NM_CUSTOMDRAW:
		return tb.customDraw(win32.ListDrawOf(hdr)), true
	}
	return 0, true
}

// row returns the row of list item i.
func (tb *table) row(i int) (view.TableRow, bool) {
	if i < 0 || i >= len(tb.v.Rows) {
		return view.TableRow{}, false
	}
	return tb.v.Rows[i], true
}

// customDraw colors the cells by their tone and draws the badges over
// their cells.
func (tb *table) customDraw(cd *win32.NMLVCustomDraw) uintptr {
	t := tb.theme
	pal := t.Palette
	r, ok := tb.row(int(cd.ItemParam) - 1)
	col := int(cd.SubItem)
	switch cd.DrawStage {
	case win32.CDDS_PREPAINT:
		return win32.CDRF_NOTIFYITEMDRAW
	case win32.CDDS_ITEMPREPAINT:
		return win32.CDRF_NOTIFYSUBITEMDRAW
	case win32.CDDS_ITEMPREPAINT | win32.CDDS_SUBITEM:
		cd.ClrText = uint32(pal.Text)
		if !ok || col >= len(r.Cells) {
			return win32.CDRF_DODEFAULT
		}
		c := r.Cells[col]
		cd.ClrText = uint32(toneColor(pal, c.Tone))
		if c.Badge != nil && win32.ListSelected(tb.hwnd) != int(cd.ItemSpec) {
			// The selection highlight shows the badge's text.
			return win32.CDRF_NOTIFYPOSTPAINT
		}
	case win32.CDDS_ITEMPOSTPAINT | win32.CDDS_SUBITEM:
		if !ok || col >= len(r.Cells) || r.Cells[col].Badge == nil {
			return win32.CDRF_DODEFAULT
		}
		b := r.Cells[col].Badge
		cell := win32.ListSubItemRect(tb.hwnd, int(cd.ItemSpec), col)
		widget.FillRect(cd.HDC, cell, pal.Surface)
		fore, back := badgeColors(pal, b.Kind)
		widget.DrawBadge(cd.HDC, t, cell, b.Text, fore, back)
	}
	return win32.CDRF_DODEFAULT
}

// cell returns a card row cell that holds the table over the row's width.
func (tb *table) cell() cell {
	return cell{hwnd: tb.hwnd, place: tb.place, fill: true}
}
