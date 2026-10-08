package gui

import (
	"github.com/phsc84/restoresafe/internal/gui/view"
	"github.com/phsc84/restoresafe/internal/gui/widget"
	"github.com/phsc84/restoresafe/internal/gui/win32"
)

// layout places the controls.
func (w *restoreDialog) layout() {
	t := w.win.theme
	s := t.Scale
	p := w.win.panel
	area := widget.NewArea(s, win32.ClientRect(p.HWND()))
	area.Inset(restoreMargin, restoreMargin, restoreMargin, restoreMargin)

	// Start and Cancel at the bottom right, as in the backup plan.
	row := widget.NewArea(s, area.Bottom(widget.ButtonHeight))
	win32.SetWindowPos(w.cancel, row.RightPx(buttonWidth(t, w.cancel)))
	row.Right(8)
	win32.SetWindowPos(w.start, row.RightPx(buttonWidth(t, w.start)))
	area.Bottom(12)

	win32.SetWindowPos(w.heading, area.Top(restoreHeading))
	area.Top(10)
	to := widget.NewArea(s, area.Top(widget.ButtonHeight))
	label := to.Left(stackLabelWidth)
	label.Top += (label.Height() - s.Px(stackLineHeight)) / 2
	label.Bottom = label.Top + s.Px(stackLineHeight)
	win32.SetWindowPos(w.toLabel, label)
	win32.SetWindowPos(w.browse, to.Right(browseButton))
	to.Right(8)
	edit := to.Rest()
	edit.Top += (edit.Height() - s.Px(widget.EditHeight)) / 2
	edit.Bottom = edit.Top + s.Px(widget.EditHeight)
	win32.SetWindowPos(w.destEdit, edit)
	area.Top(4)
	link := area.Top(stackLineHeight)
	link.Left += s.Px(stackLabelWidth)
	lw, _ := t.Fonts.Measure(view.RestoreIntoBackupDir, widget.TextSmall)
	link.Right = min(link.Left+lw+s.Px(linkPadding), link.Right)
	win32.SetWindowPos(w.intoBackupDir, link)
	area.Top(12)

	// The table as high as its rows (or as dragged), the splitter, the
	// lines; the table gives way when the lines need the room.
	split := s.Px(widget.SplitterHeight)
	want, least := w.listHeights()
	h := max(min(want, area.Height()-split-w.linesHeight()), least)
	w.listTop = area.R.Top
	list := area.TopPx(h)
	win32.SetWindowPos(w.list, list)
	w.fitColumns(list.Width())
	if w.split != nil {
		win32.SetWindowPos(w.split.HWND(), area.TopPx(split))
	}
	if w.lines != nil {
		win32.SetWindowPos(w.lines.HWND(), area.Rest())
		if w.linesSt != nil {
			w.linesSt.place(0, 0)
		}
	}
}

// listHeights returns the table's height in pixels as its rows (up to
// restoreMaxRows) or the splitter want it, and the least it shows.
func (w *restoreDialog) listHeights() (want, least int32) {
	n := max(len(w.folders), 1)
	least = w.rowsHeight(min(n, tableMinRows))
	want = w.rowsHeight(min(n, restoreMaxRows))
	if w.listHeight > 0 {
		want = w.win.theme.Scale.Px(w.listHeight)
	}
	return max(want, least), least
}

// linesHeight returns the height of the lines below the table in pixels.
func (w *restoreDialog) linesHeight() int32 {
	if w.linesSt == nil {
		return 0
	}
	return w.linesSt.height()
}

// fitHeight sizes the window to its content, as the backup plan does,
// until the user sizes it; at most the height of the screen.
func (w *restoreDialog) fitHeight() {
	if w.userSized || w.list == 0 {
		return
	}
	s := w.win.theme.Scale
	want, _ := w.listHeights()
	// Margins, Start and Cancel, heading, To, the link, the gaps (layout).
	fixed := s.Px(2*restoreMargin+widget.ButtonHeight+12+restoreHeading+10+widget.ButtonHeight+4+stackLineHeight+12) + s.Px(widget.SplitterHeight)
	h := max(fixed+want+w.linesHeight(), s.Px(restoreMinHeight))
	h = min(h, win32.SystemMetric(win32.SM_CYFULLSCREEN, uint32(s))*9/10)
	cur := win32.ClientRect(w.win.hwnd)
	if cur.Height() == h {
		return
	}
	w.fitting = true
	w.win.resize(cur.Width(), h, false)
	w.fitting = false
}

// rowsHeight returns the height in pixels of the table showing n rows.
func (w *restoreDialog) rowsHeight(n int) int32 {
	s := w.win.theme.Scale
	return win32.ListViewHeight(w.list, n) + 2*win32.SystemMetric(win32.SM_CXBORDER, uint32(s)) + s.Px(2)
}

// splitMoved sets the table's height for the splitter's new top.
func (w *restoreDialog) splitMoved(top int32) {
	s := w.win.theme.Scale
	w.listHeight = max(s.Dip(top-w.listTop), 1)
	w.fitHeight() // a window that fits its content grows with the table
	w.layout()
	w.listHeight = s.Dip(win32.WindowRect(w.list).Height()) // as far as it got
}

// fitColumns gives the filling column of the table the width the others
// leave.
func (w *restoreDialog) fitColumns(width int32) {
	s := w.win.theme.Scale
	fixed := int32(0)
	fill := -1
	for i, c := range restoreColumns {
		if c.width == 0 {
			fill = i
			continue
		}
		fixed += s.Px(c.width)
	}
	scroll := win32.SystemMetric(win32.SM_CXVSCROLL, uint32(s)) + s.Px(4)
	if fill >= 0 {
		win32.ListSetColumnWidth(w.list, fill, max(width-fixed-scroll, s.Px(80)))
	}
}
