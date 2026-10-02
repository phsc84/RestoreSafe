package win32

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// List view (SysListView32) in report mode with groups.

const (
	WC_LISTVIEW = "SysListView32"

	LVS_REPORT           = 0x0001
	LVS_SINGLESEL        = 0x0004
	LVS_SHOWSELALWAYS    = 0x0008
	LVS_NOSORTHEADER     = 0x8000
	LVS_EX_FULLROWSELECT = 0x00000020
	LVS_EX_DOUBLEBUFFER  = 0x00010000

	lvmFirst             = 0x1000
	LVM_DELETEALLITEMS   = lvmFirst + 9
	LVM_GETNEXTITEM      = lvmFirst + 12
	LVM_ENSUREVISIBLE    = lvmFirst + 19
	LVM_SETCOLUMNWIDTH   = lvmFirst + 30
	LVM_SETITEMSTATE     = lvmFirst + 43
	LVM_GETITEMCOUNT     = lvmFirst + 4
	LVM_SETEXTENDEDSTYLE = lvmFirst + 54
	LVM_GETSUBITEMRECT   = lvmFirst + 56
	LVM_SUBITEMHITTEST   = lvmFirst + 57
	LVM_GETITEMW         = lvmFirst + 75
	LVM_SETITEMW         = lvmFirst + 76
	LVM_INSERTITEMW      = lvmFirst + 77
	LVM_INSERTCOLUMNW    = lvmFirst + 97
	LVM_SETITEMTEXTW     = lvmFirst + 116
	LVM_INSERTGROUP      = lvmFirst + 145
	LVM_SETGROUPINFO     = lvmFirst + 147
	LVM_GETGROUPINFO     = lvmFirst + 149
	LVM_REMOVEALLGROUPS  = lvmFirst + 160
	LVM_ENABLEGROUPVIEW  = lvmFirst + 157
	LVM_GETFOCUSEDGROUP  = lvmFirst + 93
	LVM_HITTEST          = lvmFirst + 18
	LVM_GETGROUPRECT     = lvmFirst + 98

	LVNI_SELECTED = 0x0002

	LVIF_TEXT    = 0x0001
	LVIF_PARAM   = 0x0004
	LVIF_STATE   = 0x0008
	LVIF_GROUPID = 0x0100

	LVIS_FOCUSED  = 0x0001
	LVIS_SELECTED = 0x0002

	LVCF_FMT     = 0x0001
	LVCF_WIDTH   = 0x0002
	LVCF_TEXT    = 0x0004
	LVCFMT_LEFT  = 0x0000
	LVCFMT_RIGHT = 0x0001

	LVGF_HEADER      = 0x00000001
	LVGF_STATE       = 0x00000004
	LVGF_GROUPID     = 0x00000010
	LVGS_NORMAL      = 0x00000000
	LVGS_COLLAPSED   = 0x00000001
	LVGS_COLLAPSIBLE = 0x00000008
	LVGS_FOCUSED     = 0x00000010
	LVGS_SELECTED    = 0x00000020

	LVHT_EX_GROUP_HEADER = 0x10000000
	LVHT_ONITEM          = 0x0000000E

	lvnFirst        = ^uint32(100 - 1) // -100
	LVN_ITEMCHANGED = lvnFirst - 1
	LVN_KEYDOWN     = lvnFirst - 55
	LVN_LINKCLICK   = lvnFirst - 84
	NM_RCLICK       = ^uint32(4)  // -5
	NM_CUSTOMDRAW   = ^uint32(11) // -12
	NM_SETFOCUS     = ^uint32(6)  // -7

	CDDS_PREPAINT          = 0x00000001
	CDDS_ITEM              = 0x00010000
	CDDS_SUBITEM           = 0x00020000
	CDDS_ITEMPREPAINT      = CDDS_ITEM | CDDS_PREPAINT
	CDRF_DODEFAULT         = 0x00000000
	CDRF_NEWFONT           = 0x00000002
	CDRF_SKIPDEFAULT       = 0x00000004
	CDRF_NOTIFYITEMDRAW    = 0x00000020
	CDRF_NOTIFYSUBITEMDRAW = 0x00000020
	LVCDI_ITEM             = 0x00000000
	CDIS_SELECTED          = 0x0001
)

// lvColumn is LVCOLUMNW.
type lvColumn struct {
	Mask      uint32
	Fmt       int32
	Cx        int32
	Text      *uint16
	TextMax   int32
	SubItem   int32
	Image     int32
	Order     int32
	CxMin     int32
	CxDefault int32
	CxIdeal   int32
}

// lvItem is LVITEMW.
type lvItem struct {
	Mask      uint32
	Item      int32
	SubItem   int32
	State     uint32
	StateMask uint32
	Text      *uint16
	TextMax   int32
	Image     int32
	Param     uintptr
	Indent    int32
	GroupID   int32
	Columns   uint32
	PuColumns uintptr
	PiColFmt  uintptr
	Group     int32
}

// lvGroup is LVGROUP (comctl32 v6).
type lvGroup struct {
	Size           uint32
	Mask           uint32
	Header         *uint16
	HeaderMax      int32
	Footer         *uint16
	FooterMax      int32
	GroupID        int32
	StateMask      uint32
	State          uint32
	Align          uint32
	Subtitle       *uint16
	SubtitleMax    uint32
	Task           *uint16
	TaskMax        uint32
	DescTop        *uint16
	DescTopMax     uint32
	DescBottom     *uint16
	DescBottomMax  uint32
	TitleImage     int32
	ExtendedImage  int32
	FirstItem      int32
	Items          uint32
	SubsetTitle    *uint16
	SubsetTitleMax uint32
}

// lvHitTestInfo is LVHITTESTINFO.
type lvHitTestInfo struct {
	Pt      Point
	Flags   uint32
	Item    int32
	SubItem int32
	Group   int32
}

// NMListView is NMLISTVIEW.
type NMListView struct {
	Hdr      NMHdr
	Item     int32
	SubItem  int32
	NewState uint32
	OldState uint32
	Changed  uint32
	Action   Point
	Param    uintptr
}

// NMLVKeyDown is NMLVKEYDOWN.
type NMLVKeyDown struct {
	Hdr   NMHdr
	VKey  uint16
	Flags uint32
}

// NMLVCustomDraw is NMLVCUSTOMDRAW.
type NMLVCustomDraw struct {
	Hdr        NMHdr
	DrawStage  uint32
	HDC        uintptr
	Rc         Rect
	ItemSpec   uintptr
	ItemState  uint32
	ItemParam  uintptr
	ClrText    uint32
	ClrTextBk  uint32
	SubItem    int32
	ItemType   uint32
	ClrFace    uint32
	IconEffect int32
	IconPhase  int32
	PartID     int32
	StateID    int32
	RcText     Rect
	Align      uint32
}

// NMListViewParam returns the NMLISTVIEW of a notification.
func NMListViewParam(lparam uintptr) *NMListView { return *(**NMListView)(unsafe.Pointer(&lparam)) }

// NMLVKeyDownParam returns the NMLVKEYDOWN of a notification.
func NMLVKeyDownParam(lparam uintptr) *NMLVKeyDown { return *(**NMLVKeyDown)(unsafe.Pointer(&lparam)) }

// NMLVCustomDrawParam returns the NMLVCUSTOMDRAW of a notification.
func NMLVCustomDrawParam(lparam uintptr) *NMLVCustomDraw {
	return *(**NMLVCustomDraw)(unsafe.Pointer(&lparam))
}

// ListSetup prepares a report-mode list: full-row selection, double
// buffering, group view.
func ListSetup(lv HWND) {
	ex := uintptr(LVS_EX_FULLROWSELECT | LVS_EX_DOUBLEBUFFER)
	SendMessage(lv, LVM_SETEXTENDEDSTYLE, ex, ex)
	SendMessage(lv, LVM_ENABLEGROUPVIEW, 1, 0)
}

// ListInsertColumn adds column i with its header text and width (pixels);
// right aligns the column.
func ListInsertColumn(lv HWND, i int, text string, width int32, right bool) {
	t, _ := windows.UTF16PtrFromString(text)
	c := lvColumn{Mask: LVCF_FMT | LVCF_WIDTH | LVCF_TEXT, Cx: width, Text: t}
	if right {
		c.Fmt = LVCFMT_RIGHT
	}
	SendMessage(lv, LVM_INSERTCOLUMNW, uintptr(i), uintptr(unsafe.Pointer(&c)))
}

// ListSetColumnWidth sets the width of column i in pixels.
func ListSetColumnWidth(lv HWND, i int, width int32) {
	SendMessage(lv, LVM_SETCOLUMNWIDTH, uintptr(i), uintptr(width))
}

// ListClear removes all items and groups.
func ListClear(lv HWND) {
	SendMessage(lv, LVM_DELETEALLITEMS, 0, 0)
	SendMessage(lv, LVM_REMOVEALLGROUPS, 0, 0)
}

// ListInsertGroup adds a collapsible group with header text.
func ListInsertGroup(lv HWND, id int32, header string, collapsed bool) {
	h, _ := windows.UTF16PtrFromString(header)
	g := lvGroup{Mask: LVGF_HEADER | LVGF_GROUPID | LVGF_STATE, Header: h, GroupID: id,
		StateMask: LVGS_COLLAPSIBLE | LVGS_COLLAPSED, State: LVGS_COLLAPSIBLE}
	if collapsed {
		g.State |= LVGS_COLLAPSED
	}
	g.Size = uint32(unsafe.Sizeof(g))
	SendMessage(lv, LVM_INSERTGROUP, ^uintptr(0), uintptr(unsafe.Pointer(&g)))
}

// ListGroupCollapsed reports whether group id is collapsed.
func ListGroupCollapsed(lv HWND, id int32) bool {
	g := lvGroup{Mask: LVGF_STATE, StateMask: LVGS_COLLAPSED}
	g.Size = uint32(unsafe.Sizeof(g))
	SendMessage(lv, LVM_GETGROUPINFO, uintptr(id), uintptr(unsafe.Pointer(&g)))
	return g.State&LVGS_COLLAPSED != 0
}

// ListSetGroupCollapsed collapses or expands group id.
func ListSetGroupCollapsed(lv HWND, id int32, collapsed bool) {
	g := lvGroup{Mask: LVGF_STATE, StateMask: LVGS_COLLAPSED}
	if collapsed {
		g.State = LVGS_COLLAPSED
	}
	g.Size = uint32(unsafe.Sizeof(g))
	SendMessage(lv, LVM_SETGROUPINFO, uintptr(id), uintptr(unsafe.Pointer(&g)))
}

// ListInsertItem adds an item with the text of column 0 to group and
// returns its index; param identifies it.
func ListInsertItem(lv HWND, text string, group int32, param uintptr) int {
	t, _ := windows.UTF16PtrFromString(text)
	it := lvItem{Mask: LVIF_TEXT | LVIF_PARAM | LVIF_GROUPID, Item: 1 << 30, Text: t, Param: param, GroupID: group}
	return int(int32(SendMessage(lv, LVM_INSERTITEMW, 0, uintptr(unsafe.Pointer(&it)))))
}

// ListSetText sets the text of column sub of item i.
func ListSetText(lv HWND, i, sub int, text string) {
	t, _ := windows.UTF16PtrFromString(text)
	it := lvItem{SubItem: int32(sub), Text: t}
	SendMessage(lv, LVM_SETITEMTEXTW, uintptr(i), uintptr(unsafe.Pointer(&it)))
}

// ListItemCount returns the number of items.
func ListItemCount(lv HWND) int { return int(SendMessage(lv, LVM_GETITEMCOUNT, 0, 0)) }

// ListSelected returns the selected item, or -1.
func ListSelected(lv HWND) int {
	return int(int32(SendMessage(lv, LVM_GETNEXTITEM, ^uintptr(0), LVNI_SELECTED)))
}

// ListSelect selects and focuses item i and scrolls it into view; -1
// clears the selection.
func ListSelect(lv HWND, i int) {
	clear := lvItem{StateMask: LVIS_SELECTED | LVIS_FOCUSED}
	SendMessage(lv, LVM_SETITEMSTATE, ^uintptr(0), uintptr(unsafe.Pointer(&clear)))
	if i < 0 {
		return
	}
	it := lvItem{State: LVIS_SELECTED | LVIS_FOCUSED, StateMask: LVIS_SELECTED | LVIS_FOCUSED}
	SendMessage(lv, LVM_SETITEMSTATE, uintptr(i), uintptr(unsafe.Pointer(&it)))
	SendMessage(lv, LVM_ENSUREVISIBLE, uintptr(i), 0)
}

// ListParam returns the param of item i.
func ListParam(lv HWND, i int) uintptr {
	it := lvItem{Mask: LVIF_PARAM, Item: int32(i)}
	SendMessage(lv, LVM_GETITEMW, 0, uintptr(unsafe.Pointer(&it)))
	return it.Param
}

// ListHitGroup returns the group whose header is at pt (client
// coordinates), or -1.
func ListHitGroup(lv HWND, pt Point) int32 {
	h := lvHitTestInfo{Pt: pt, Item: -1, Group: -1}
	r := int32(SendMessage(lv, LVM_HITTEST, ^uintptr(0), uintptr(unsafe.Pointer(&h))))
	if h.Flags&LVHT_EX_GROUP_HEADER != 0 {
		return r
	}
	return -1
}

// ListHitItem returns the item at pt (client coordinates), or -1.
func ListHitItem(lv HWND, pt Point) int {
	h := lvHitTestInfo{Pt: pt, Item: -1}
	SendMessage(lv, LVM_SUBITEMHITTEST, 0, uintptr(unsafe.Pointer(&h)))
	if h.Flags&LVHT_ONITEM == 0 {
		return -1
	}
	return int(h.Item)
}

// ListFocusedGroup returns the group whose header has the keyboard focus,
// or -1.
func ListFocusedGroup(lv HWND) int32 {
	return int32(SendMessage(lv, LVM_GETFOCUSEDGROUP, 0, 0))
}

// ListSubItemRect returns the bounds of column sub of item i.
func ListSubItemRect(lv HWND, i, sub int) Rect {
	r := Rect{Top: int32(sub)} // LVIR_BOUNDS in Left
	SendMessage(lv, LVM_GETSUBITEMRECT, uintptr(i), uintptr(unsafe.Pointer(&r)))
	return r
}

// Custom-draw stages after painting.
const (
	CDDS_POSTPAINT       = 0x00000002
	CDDS_ITEMPOSTPAINT   = CDDS_ITEM | CDDS_POSTPAINT
	CDRF_NOTIFYPOSTPAINT = 0x00000010
	BS_AUTORADIOBUTTON   = 0x00000009
	BS_PUSHLIKE          = 0x00001000
	WS_GROUP             = 0x00020000
	WM_SETREDRAW         = 0x000B
)

// The notifications of a list view, from the header the parent received.

// ListChangeOf returns the NMLISTVIEW behind hdr.
func ListChangeOf(hdr *NMHdr) *NMListView { return (*NMListView)(unsafe.Pointer(hdr)) }

// ListKeyOf returns the NMLVKEYDOWN behind hdr.
func ListKeyOf(hdr *NMHdr) *NMLVKeyDown { return (*NMLVKeyDown)(unsafe.Pointer(hdr)) }

// ListDrawOf returns the NMLVCUSTOMDRAW behind hdr.
func ListDrawOf(hdr *NMHdr) *NMLVCustomDraw { return (*NMLVCustomDraw)(unsafe.Pointer(hdr)) }

// Plain lists and checkboxes.
const (
	LVS_EX_CHECKBOXES   = 0x00000004
	LVM_GETITEMSTATE    = lvmFirst + 44
	LVIS_STATEIMAGEMASK = 0xF000
)

// ListSetupPlain prepares a report-mode list without groups: full-row
// selection, double buffering and, with checkboxes, a checkbox per item.
func ListSetupPlain(lv HWND, checkboxes bool) {
	ex := uintptr(LVS_EX_FULLROWSELECT | LVS_EX_DOUBLEBUFFER)
	if checkboxes {
		ex |= LVS_EX_CHECKBOXES
	}
	SendMessage(lv, LVM_SETEXTENDEDSTYLE, ex, ex)
}

// ListAddItem appends an item with the text of column 0; param identifies
// it. It returns the item's index.
func ListAddItem(lv HWND, text string, param uintptr) int {
	t, _ := windows.UTF16PtrFromString(text)
	it := lvItem{Mask: LVIF_TEXT | LVIF_PARAM, Item: 1 << 30, Text: t, Param: param}
	return int(int32(SendMessage(lv, LVM_INSERTITEMW, 0, uintptr(unsafe.Pointer(&it)))))
}

// ListChecked reports whether the checkbox of item i is checked.
func ListChecked(lv HWND, i int) bool {
	state := SendMessage(lv, LVM_GETITEMSTATE, uintptr(i), LVIS_STATEIMAGEMASK)
	return state>>12 == 2
}

// ListSetChecked checks or clears the checkbox of item i.
func ListSetChecked(lv HWND, i int, checked bool) {
	image := uint32(1)
	if checked {
		image = 2
	}
	it := lvItem{State: image << 12, StateMask: LVIS_STATEIMAGEMASK}
	SendMessage(lv, LVM_SETITEMSTATE, uintptr(i), uintptr(unsafe.Pointer(&it)))
}
