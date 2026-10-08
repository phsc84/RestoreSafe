package gui

import (
	"RestoreSafe/internal/gui/view"
	"RestoreSafe/internal/gui/widget"
	"RestoreSafe/internal/gui/win32"
)

// buildList creates the folder table with a checkbox per folder (RW-5).
func (w *restoreDialog) buildList() {
	t := w.win.theme
	lv := w.child(win32.WC_LISTVIEW, win32.WS_TABSTOP|win32.WS_BORDER|win32.LVS_REPORT|win32.LVS_SINGLESEL|win32.LVS_SHOWSELALWAYS|win32.LVS_NOSORTHEADER, idRestoreList)
	win32.ListSetupPlain(lv, true)
	widget.StyleListHeader(t, lv)
	for i, c := range restoreColumns {
		win32.ListInsertColumn(lv, i, c.title, t.Scale.Px(max(c.width, 60)), c.right)
	}
	win32.SetAccessibleName(lv, view.PlanColumnFolder)
	w.list = lv
	w.filling = true
	for i, f := range w.folders {
		item := win32.ListAddItem(lv, f.Folder, uintptr(i+1))
		win32.ListSetText(lv, item, 1, f.Badge.Text)
		win32.ListSetText(lv, item, 2, f.About)
		win32.ListSetText(lv, item, 3, view.CheckNone)
		win32.ListSetChecked(lv, item, f.Enabled && w.checked[f.Set])
	}
	w.filling = false
}

func (w *restoreDialog) notify(hdr *win32.NMHdr) uintptr {
	if hdr.HwndFrom != w.list || w.list == 0 {
		return 0
	}
	switch hdr.Code {
	case win32.LVN_ITEMCHANGED:
		n := win32.ListChangeOf(hdr)
		if w.filling || n.Changed&win32.LVIF_STATE == 0 {
			return 0
		}
		i := int(win32.ListParam(w.list, int(n.Item))) - 1
		if (n.NewState^n.OldState)&win32.LVIS_STATEIMAGEMASK != 0 && i >= 0 && i < len(w.folders) {
			f := w.folders[i]
			checked := win32.ListChecked(w.list, int(n.Item))
			if checked && !f.Enabled {
				win32.ListSetChecked(w.list, int(n.Item), false)
				return 0
			}
			if w.checked[f.Set] != checked {
				w.checked[f.Set] = checked
				w.startCheck(0)
			}
		}
	case win32.NM_CUSTOMDRAW:
		return w.customDraw(win32.ListDrawOf(hdr))
	}
	return 0
}

// customDraw greys the rows that cannot be restored, colors the Check
// column and draws the type badges.
func (w *restoreDialog) customDraw(cd *win32.NMLVCustomDraw) uintptr {
	t := w.win.theme
	pal := t.Palette
	switch cd.DrawStage {
	case win32.CDDS_PREPAINT:
		return win32.CDRF_NOTIFYITEMDRAW
	case win32.CDDS_ITEMPREPAINT:
		return win32.CDRF_NOTIFYSUBITEMDRAW
	}
	i := int(cd.ItemParam) - 1
	if i < 0 || i >= len(w.folders) {
		return win32.CDRF_DODEFAULT
	}
	f := w.folders[i]
	selected := win32.ListSelected(w.list) == int(cd.ItemSpec)
	switch cd.DrawStage {
	case win32.CDDS_ITEMPREPAINT | win32.CDDS_SUBITEM:
		cd.ClrText = uint32(pal.Text)
		switch {
		case !f.Enabled:
			cd.ClrText = uint32(pal.TextSecondary)
		case cd.SubItem == 1 && !selected:
			return win32.CDRF_NOTIFYPOSTPAINT
		case cd.SubItem == 3 && !selected:
			if c, ok := w.view.Checks[f.Set]; ok && w.checked[f.Set] {
				cd.ClrText = uint32(toneColor(pal, c.Tone))
			} else {
				cd.ClrText = uint32(pal.TextSecondary)
			}
		}
		return win32.CDRF_DODEFAULT
	case win32.CDDS_ITEMPOSTPAINT | win32.CDDS_SUBITEM:
		cell := win32.ListSubItemRect(w.list, int(cd.ItemSpec), 1)
		widget.FillRect(cd.HDC, cell, pal.Surface)
		fore, back := badgeColors(pal, f.Badge.Kind)
		widget.DrawBadge(cd.HDC, t, cell, f.Badge.Text, fore, back)
	}
	return win32.CDRF_DODEFAULT
}
