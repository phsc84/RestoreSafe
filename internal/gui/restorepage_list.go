package gui

import (
	"RestoreSafe/internal/gui/view"
	"RestoreSafe/internal/gui/widget"
	"RestoreSafe/internal/gui/win32"
	"strings"
)

type rowRef struct{ group, row int }

// fillList rebuilds the list when its runs or sets changed and otherwise
// updates the texts in place, so the selection and scrolling stay.
func (b *restorePage) fillList() {
	v := b.view
	var sig strings.Builder
	for _, g := range v.Groups {
		sig.WriteString(string(g.RunID))
		sig.WriteByte('|')
		sig.WriteString(g.Header)
		sig.WriteByte('|')
		sig.WriteString(g.LogPath)
		sig.WriteByte(0)
		for _, r := range g.Rows {
			sig.WriteString(r.Set)
			sig.WriteByte(0)
		}
	}
	lv := b.list
	if sig.String() == b.listSig {
		for i, ref := range b.rows {
			if ref.row >= 0 {
				r := v.Groups[ref.group].Rows[ref.row]
				win32.ListSetText(lv, b.itemOf(i), 5, r.Status.Text)
			}
		}
		win32.Invalidate(lv)
		return
	}
	// Keep what the user collapsed or expanded: a group whose state differs
	// from how it was shown.
	for gi := range b.groupsShown() {
		if gi < len(b.lastGroups) && gi < len(b.shownCollapsed) {
			if now := win32.ListGroupCollapsed(lv, int32(gi)); now != b.shownCollapsed[gi] {
				b.collapsed[b.lastGroups[gi]] = now
			}
		}
	}
	b.shownCollapsed = nil
	b.listSig = sig.String()
	win32.SendMessage(lv, win32.WM_SETREDRAW, 0, 0)
	win32.ListClear(lv)
	b.rows = nil
	b.lastGroups = nil
	selected := -1
	for gi, g := range v.Groups {
		collapsed, known := b.collapsed[g.RunID]
		if !known || g.RunID == "" {
			collapsed = !g.Expanded
		}
		link := ""
		if g.LogPath != "" {
			link = view.RunLogLink
		}
		win32.ListInsertGroup(lv, int32(gi), g.Header, link, collapsed)
		b.shownCollapsed = append(b.shownCollapsed, collapsed)
		b.lastGroups = append(b.lastGroups, g.RunID)
		if len(g.Rows) == 0 {
			b.rows = append(b.rows, rowRef{gi, -1})
			win32.ListInsertItem(lv, g.Placeholder, int32(gi), uintptr(len(b.rows)))
			continue
		}
		for ri, r := range g.Rows {
			b.rows = append(b.rows, rowRef{gi, ri})
			i := win32.ListInsertItem(lv, r.Folder, int32(gi), uintptr(len(b.rows)))
			for col, text := range []string{r.Badge.Text, r.BasedOn, r.Size, r.Chain, r.Status.Text} {
				win32.ListSetText(lv, i, col+1, text)
			}
			if r.Set == b.selSet && b.selSet != "" {
				selected = i
			}
		}
	}
	win32.SendMessage(lv, win32.WM_SETREDRAW, 1, 0)
	if selected >= 0 {
		win32.ListSelect(lv, selected)
	} else if b.selSet != "" {
		b.selSet = ""
	}
	win32.Invalidate(lv)
}

// groupsShown returns the indexes of the groups in the list.
func (b *restorePage) groupsShown() []int {
	out := make([]int, len(b.lastGroups))
	for i := range out {
		out[i] = i
	}
	return out
}

// itemOf returns the list index of the item with param index i+1.
func (b *restorePage) itemOf(i int) int {
	n := win32.ListItemCount(b.list)
	for item := range n {
		if int(win32.ListParam(b.list, item)) == i+1 {
			return item
		}
	}
	return -1
}

// rowAt returns the group and row of list item i; ok is false for none.
func (b *restorePage) rowAt(item int) (rowRef, bool) {
	if item < 0 {
		return rowRef{}, false
	}
	p := int(win32.ListParam(b.list, item))
	if p < 1 || p > len(b.rows) {
		return rowRef{}, false
	}
	return b.rows[p-1], true
}

// runSelected reports whether group gi is the selected run.
func (b *restorePage) runSelected(gi int) bool {
	return gi >= 0 && gi < len(b.view.Groups) && b.selSet == "" && b.selRun != "" && b.view.Groups[gi].RunID == b.selRun
}

// selectItem makes the run of list item i the selection: a run is restored
// and verified whole, so its sets aren't selected one by one. Incomplete
// sets have no run; such a set is selected itself.
func (b *restorePage) selectItem(item int) {
	ref, ok := b.rowAt(item)
	if !ok {
		return
	}
	g := b.view.Groups[ref.group]
	b.selRun, b.selSet = g.RunID, ""
	if g.RunID != "" {
		// The item keeps the focus, so the arrow keys move on from it.
		win32.ListDeselect(b.list, item)
	} else if ref.row >= 0 {
		b.selSet = g.Rows[ref.row].Set
	}
	b.updateBar()
}

// selectRun makes the run of group gi the selection.
func (b *restorePage) selectRun(gi int) {
	if gi < 0 || gi >= len(b.view.Groups) {
		return
	}
	g := b.view.Groups[gi]
	b.selRun, b.selSet = g.RunID, ""
	win32.ListSelect(b.list, -1)
	b.updateBar()
}

// showRunLog shows the log of group gi in the log window (GUI spec BK-5).
func (b *restorePage) showRunLog(gi int) {
	if gi < 0 || gi >= len(b.view.Groups) {
		return
	}
	g := b.view.Groups[gi]
	b.a.showLog(b.a.hwnd, g.LogPath, g.When)
}

// notify handles the list's notifications.
func (b *restorePage) notify(hdr *win32.NMHdr) uintptr {
	if hdr.HwndFrom != b.list {
		return 0
	}
	switch hdr.Code {
	case win32.LVN_ITEMCHANGED:
		n := win32.ListChangeOf(hdr)
		if n.Changed&win32.LVIF_STATE != 0 && n.NewState&win32.LVIS_SELECTED != 0 && n.OldState&win32.LVIS_SELECTED == 0 {
			b.selectItem(int(n.Item))
		}
		// A click on a group header selects the group's first item and
		// focuses the header; it means the run.
		win32.PostMessage(b.a.hwnd, msgListFocus, 0, 0) //nolint:errcheck
	case win32.NM_CLICK, win32.LVN_KEYDOWN:
		// After the list moved the focus, a group header may have it.
		win32.PostMessage(b.a.hwnd, msgListFocus, 0, 0) //nolint:errcheck
	case win32.NM_DBLCLK:
		if b.bar.Restore.Enabled {
			b.a.do(view.ActionRestore)
		}
	case win32.NM_RCLICK:
		b.contextMenu()
	case win32.LVN_LINKCLICK:
		// The log window runs its own message loop: it opens after the
		// list has finished with the click.
		win32.PostMessage(b.a.hwnd, msgRunLog, uintptr(win32.ListLinkGroup(hdr)), 0) //nolint:errcheck
	case win32.LVN_GETINFOTIP:
		n := win32.ListInfoTipOf(hdr)
		if ref, ok := b.rowAt(int(n.Item)); ok && ref.row >= 0 {
			n.SetText(b.view.Groups[ref.group].Rows[ref.row].Tip)
		}
	case win32.NM_CUSTOMDRAW:
		return b.customDraw(win32.ListDrawOf(hdr))
	}
	return 0
}

// focusChanged selects the run whose group header has the keyboard focus.
func (b *restorePage) focusChanged() {
	g := int(win32.ListFocusedGroup(b.list))
	if g < 0 || g >= len(b.view.Groups) {
		return
	}
	if b.selSet != "" || b.selRun != b.view.Groups[g].RunID {
		b.selectRun(g)
	}
}

// contextMenu offers the actions of the item under the cursor (GUI spec BK-4).
func (b *restorePage) contextMenu() {
	screen := win32.CursorPos()
	item := win32.ListHitItem(b.list, win32.ScreenToClient(b.list, screen))
	if item < 0 {
		return
	}
	win32.ListSelect(b.list, item)
	b.selectItem(item)
	ref, _ := b.rowAt(item)
	logPath := b.view.Groups[ref.group].LogPath
	m := view.RestoreMenu()
	set := ""
	if ref.row >= 0 {
		set = b.view.Groups[ref.group].Rows[ref.row].Set
	}
	busy := b.a.machine.Busy()
	switch win32.ShowMenu(b.a.hwnd, screen, []win32.MenuItem{
		{ID: menuRestore, Text: m.Restore, Disabled: !b.bar.Restore.Enabled || busy},
		{ID: menuVerify, Text: m.Verify, Disabled: !b.bar.Verify.Enabled || busy},
		{ID: menuShowLog, Text: m.ShowLog, Disabled: logPath == ""},
		{},
		{ID: menuCopyName, Text: m.CopyName, Disabled: set == ""},
		{ID: menuOpenFolder, Text: m.OpenFolder},
	}) {
	case menuRestore:
		b.a.do(view.ActionRestore)
	case menuVerify:
		b.a.do(view.ActionVerify)
	case menuShowLog:
		b.showRunLog(ref.group)
	case menuCopyName:
		win32.CopyText(b.a.hwnd, set) //nolint:errcheck
	case menuOpenFolder:
		b.a.do(view.ActionOpenBackupDir)
	}
}

// customDraw colors the status, sets the chain in the monospaced font and
// draws the type badges over their cells.
func (b *restorePage) customDraw(cd *win32.NMLVCustomDraw) uintptr {
	t := b.a.theme
	pal := t.Palette
	switch cd.DrawStage {
	case win32.CDDS_PREPAINT:
		if cd.ItemType == win32.LVCDI_GROUP {
			// A group (ItemSpec is its ID; Rc holds its rows too): the
			// selected run's header is filled like a selected row, and the
			// list draws its text on top.
			if id := int32(cd.ItemSpec); b.runSelected(int(id)) {
				widget.FillRect(cd.HDC, win32.ListGroupHeaderRect(b.list, id), pal.Selection)
			}
			return win32.CDRF_DODEFAULT
		}
		return win32.CDRF_NOTIFYITEMDRAW
	case win32.CDDS_ITEMPREPAINT:
		return win32.CDRF_NOTIFYSUBITEMDRAW
	case win32.CDDS_ITEMPREPAINT | win32.CDDS_SUBITEM:
		ref, ok := b.refOf(cd.ItemParam)
		font := t.Fonts.Get(widget.TextBody)
		cd.ClrText = uint32(pal.Text)
		if ok && ref.row < 0 {
			cd.ClrText = uint32(pal.TextSecondary)
		}
		if ok && ref.row >= 0 {
			r := b.view.Groups[ref.group].Rows[ref.row]
			switch cd.SubItem {
			case 1:
				win32.SelectFont(cd.HDC, font)
				if item := int(cd.ItemSpec); win32.ListSelected(b.list) == item {
					// The selection highlight shows the badge's text.
					return win32.CDRF_NEWFONT
				}
				return win32.CDRF_NEWFONT | win32.CDRF_NOTIFYPOSTPAINT
			case 4:
				font = t.Fonts.Get(widget.TextMono)
				cd.ClrText = uint32(pal.TextSecondary)
			case 5:
				cd.ClrText = uint32(toneColor(pal, r.Status.Tone))
			}
		}
		win32.SelectFont(cd.HDC, font)
		return win32.CDRF_NEWFONT
	case win32.CDDS_ITEMPOSTPAINT | win32.CDDS_SUBITEM:
		ref, ok := b.refOf(cd.ItemParam)
		if !ok || ref.row < 0 || cd.SubItem != 1 {
			return win32.CDRF_DODEFAULT
		}
		r := b.view.Groups[ref.group].Rows[ref.row]
		cell := win32.ListSubItemRect(b.list, int(cd.ItemSpec), 1)
		widget.FillRect(cd.HDC, cell, pal.Surface)
		fore, fill := badgeColors(pal, r.Badge.Kind)
		widget.DrawBadge(cd.HDC, t, cell, r.Badge.Text, fore, fill)
	}
	return win32.CDRF_DODEFAULT
}

func (b *restorePage) refOf(param uintptr) (rowRef, bool) {
	p := int(param)
	if p < 1 || p > len(b.rows) {
		return rowRef{}, false
	}
	return b.rows[p-1], true
}
