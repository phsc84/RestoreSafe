package widget

import (
	"runtime"
	"testing"
	"unsafe"

	"github.com/phsc84/restoresafe/internal/gui/win32"

	"golang.org/x/sys/windows"
)

// testTheme returns a theme at 96 dpi and a hidden top-level window to put
// widgets on.
func testTheme(t *testing.T) (*Theme, win32.HWND) {
	t.Helper()
	runtime.LockOSThread()
	t.Cleanup(runtime.UnlockOSThread)
	useCommonControls6(t)
	// Like Run: COM stays initialized on the thread, so the accessible
	// names' COM object stays valid.
	if err := win32.InitCOM(); err != nil {
		t.Fatal(err)
	}
	if err := win32.InitCommonControls(); err != nil {
		t.Fatal(err)
	}
	fonts, err := NewFonts(96)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(fonts.Close)
	host, err := win32.CreateWindow(0, "STATIC", "", win32.WS_POPUP, 0, 0, 400, 300, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { win32.DestroyWindow(host) })
	return &Theme{Palette: Light, Fonts: fonts, Scale: 96}, host
}

// procGetPixel reads the pixels a test rendered; the program never does.
var procGetPixel = windows.NewLazySystemDLL("gdi32.dll").NewProc("GetPixel")

// render sizes hwnd to w×h, paints it through WM_PRINTCLIENT, and returns
// the pixels.
func render(t *testing.T, hwnd win32.HWND, w, h int32) func(x, y int32) Color {
	t.Helper()
	win32.SetWindowPos(hwnd, win32.Rect{Right: w, Bottom: h})
	mem, px := canvas(t, w, h)
	win32.SendMessage(hwnd, win32.WM_PRINTCLIENT, mem, 0)
	return px
}

// canvas returns a memory DC of w×h pixels and a function that reads its
// pixels.
func canvas(t *testing.T, w, h int32) (uintptr, func(x, y int32) Color) {
	t.Helper()
	screen := win32.GetDC(0)
	mem := win32.CreateCompatibleDC(screen)
	bmp := win32.CreateCompatibleBitmap(screen, w, h)
	win32.ReleaseDC(0, screen)
	old := win32.SelectObject(mem, bmp)
	t.Cleanup(func() {
		win32.SelectObject(mem, old)
		win32.DeleteObject(bmp)
		win32.DeleteDC(mem)
	})
	return mem, func(x, y int32) Color {
		r, _, _ := procGetPixel.Call(mem, uintptr(x), uintptr(y))
		return Color(r)
	}
}

func expect(t *testing.T, what string, got, want Color) {
	t.Helper()
	if got != want {
		t.Errorf("%s: color %06X, want %06X", what, uint32(got), uint32(want))
	}
}

func TestPanelPaintsACard(t *testing.T) {
	th, host := testTheme(t)
	outer := RGB(1, 2, 3)
	p, err := NewPanel(th, host, 0, PanelStyle{Back: Light.Surface, Outer: outer, Card: true})
	if err != nil {
		t.Fatal(err)
	}
	px := render(t, p.HWND(), 200, 100)
	expect(t, "outside the rounded corner", px(0, 0), outer)
	expect(t, "inside", px(100, 50), Light.Surface)
	expect(t, "border", px(100, 0), Light.Lines)
}

func TestPanelPassesClicksAndLinks(t *testing.T) {
	th, host := testTheme(t)
	p, err := NewPanel(th, host, 0, PanelStyle{Back: Light.Surface})
	if err != nil {
		t.Fatal(err)
	}
	var got [][2]uint16
	p.OnCommand = func(id, code uint16) { got = append(got, [2]uint16{id, code}) }
	button := p.Button("&Check again", 41)
	link := p.Link("Show details", 42)
	if button == 0 || link == 0 {
		t.Fatal("controls not created")
	}
	win32.SendMessage(p.HWND(), win32.WM_COMMAND, uintptr(41)|uintptr(win32.BN_CLICKED)<<16, uintptr(button))
	hdr := win32.NMHdr{HwndFrom: link, IDFrom: 42, Code: win32.NM_CLICK}
	win32.SendMessage(p.HWND(), win32.WM_NOTIFY, 42, uintptr(unsafe.Pointer(&hdr)))
	if len(got) != 2 || got[0] != [2]uint16{41, win32.BN_CLICKED} || got[1] != [2]uint16{42, 0} {
		t.Fatalf("commands %v", got)
	}

	p.Clear()
	if win32.IsWindow(button) || win32.IsWindow(link) {
		t.Fatal("Clear must destroy the children")
	}
}

func TestButtonsArePainted(t *testing.T) {
	th, host := testTheme(t)
	p, err := NewPanel(th, host, 0, PanelStyle{Back: Light.Surface})
	if err != nil {
		t.Fatal(err)
	}
	primary := p.PrimaryButton("&Back up now…", 7)
	px := render(t, primary, 120, 28)
	expect(t, "primary", px(4, 14), Light.Accent)
	expect(t, "corner", px(0, 0), Light.Surface)
	win32.Enable(primary, false)
	expect(t, "disabled primary", render(t, primary, 120, 28)(4, 14), Light.Control)

	secondary := p.Button("&Refresh", 8)
	px = render(t, secondary, 120, 28)
	expect(t, "secondary", px(4, 14), Light.Surface)
	expect(t, "secondary border", px(60, 0), Light.Lines)
	win32.SendMessage(secondary, win32.BM_SETSTATE, 1, 0)
	expect(t, "pushed", render(t, secondary, 120, 28)(4, 14), Light.Control)
	win32.SendMessage(secondary, win32.BM_SETSTATE, 0, 0)

	// With the keyboard cues shown, the focused button has the solid
	// outline of the sidebar, 2 pixels wide.
	win32.SetFocus(secondary)
	const uisClear, cues = 2, win32.UISF_HIDEFOCUS | win32.UISF_HIDEACCEL
	win32.SendMessage(secondary, win32.WM_UPDATEUISTATE, uisClear|cues<<16, 0)
	if win32.SendMessage(secondary, win32.BM_GETSTATE, 0, 0)&win32.BST_FOCUS == 0 {
		t.Skip("the test window can't take the focus")
	}
	px = render(t, secondary, 120, 28)
	expect(t, "focus outline", px(60, 0), Light.Text)
	expect(t, "focus outline inside", px(60, 1), Light.Text)
}

func TestIconAndBar(t *testing.T) {
	th, host := testTheme(t)
	back := RGB(9, 9, 9)

	i, err := NewIcon(th, host, back, TextIcon)
	if err != nil {
		t.Fatal(err)
	}
	px := render(t, i.HWND(), 52, 52)
	expect(t, "icon without circle", px(26, 3), back)
	i.Set(GlyphCheck, Light.Success, Light.SuccessBack, "Protected")
	px = render(t, i.HWND(), 52, 52)
	expect(t, "icon corner", px(0, 0), back)
	expect(t, "icon circle", px(26, 3), Light.SuccessBack)

	bar, err := NewBar(th, host, back, Light.SurfaceAlt)
	if err != nil {
		t.Fatal(err)
	}
	bar.Set([]Segment{{0.25, Light.BarBackups}, {0.25, Light.BarOther}}, "530 of 900 GB used")
	px = render(t, bar.HWND(), 400, 8)
	expect(t, "bar backups", px(50, 4), Light.BarBackups)
	expect(t, "bar other", px(150, 4), Light.BarOther)
	expect(t, "bar free", px(300, 4), Light.SurfaceAlt)
	expect(t, "bar corner", px(0, 0), back)
}

func TestSidebarSelection(t *testing.T) {
	th, host := testTheme(t)
	items := []SidebarItem{{Glyph: GlyphArchive, Text: "Create backup"}, {Glyph: GlyphHistory, Text: "Restore backup"}, {Glyph: GlyphSettings, Text: "Settings"}}
	s, err := NewSidebar(th, host, 0, items)
	if err != nil {
		t.Fatal(err)
	}
	var chosen []int
	s.OnSelect = func(i int) { chosen = append(chosen, i) }

	px := render(t, s.HWND(), SidebarWidth, 200)
	row := s.rowRect(0, win32.ClientRect(s.HWND()))
	expect(t, "selected row", px(row.Right-10, row.Top+row.Height()/2), Light.Control)
	expect(t, "selection bar", px(row.Left+1, row.Top+row.Height()/2), Light.SelectionBar)
	expect(t, "other row", px(row.Right-10, row.Bottom+row.Height()/2), Light.SurfaceAlt)

	for _, key := range []uintptr{win32.VK_DOWN, win32.VK_END, win32.VK_DOWN, win32.VK_HOME, win32.VK_UP} {
		win32.SendMessage(s.HWND(), win32.WM_KEYDOWN, key, 0)
	}
	if want := []int{1, 2, 0}; len(chosen) != len(want) || chosen[0] != 1 || chosen[1] != 2 || chosen[2] != 0 {
		t.Fatalf("keys selected %v, want %v (past the ends nothing changes)", chosen, want)
	}
	second := s.rowRect(1, win32.ClientRect(s.HWND()))
	win32.SendMessage(s.HWND(), win32.WM_LBUTTONDOWN, 0, uintptr(second.Top+2)<<16|uintptr(second.Left+2))
	if s.Selected() != 1 || chosen[len(chosen)-1] != 1 {
		t.Fatalf("a click must select the row: selected %d, %v", s.Selected(), chosen)
	}
	s.Select(2)
	if len(chosen) != 4 {
		t.Fatal("Select must not call OnSelect")
	}

	// The row under the mouse is filled, until the mouse leaves.
	first := s.rowRect(0, win32.ClientRect(s.HWND()))
	win32.SendMessage(s.HWND(), win32.WM_MOUSEMOVE, 0, uintptr(first.Top+2)<<16|uintptr(first.Left+2))
	px = render(t, s.HWND(), SidebarWidth, 200)
	expect(t, "hovered row", px(first.Right-10, first.Top+first.Height()/2), Light.Control)
	expect(t, "no bar on the hovered row", px(first.Left+1, first.Top+first.Height()/2), Light.Control)
	win32.SendMessage(s.HWND(), win32.WM_MOUSELEAVE, 0, 0)
	px = render(t, s.HWND(), SidebarWidth, 200)
	expect(t, "row after the mouse left", px(first.Right-10, first.Top+first.Height()/2), Light.SurfaceAlt)
}

// The YubiKey item sits at the bottom below a line and can be hidden; the
// keys and the mouse skip a hidden item (GUI spec YK-1).
func TestSidebarBottomGroupAndHiddenItems(t *testing.T) {
	th, host := testTheme(t)
	items := []SidebarItem{{Glyph: GlyphArchive, Text: "Create backup"}, {Glyph: GlyphHistory, Text: "Restore backup"}, {Glyph: GlyphSettings, Text: "Settings"}, {Glyph: GlyphKey, Text: "YubiKey", Bottom: true}}
	s, err := NewSidebar(th, host, 0, items)
	if err != nil {
		t.Fatal(err)
	}
	var chosen []int
	s.OnSelect = func(i int) { chosen = append(chosen, i) }

	const height = 400
	px := render(t, s.HWND(), SidebarWidth, height)
	client := win32.ClientRect(s.HWND())
	yk := s.rowRect(3, client)
	if yk.Bottom != client.Bottom-th.Scale.Px(sidebarPadding) || yk.Top <= s.rowRect(2, client).Bottom {
		t.Fatalf("the bottom item must sit at the bottom edge: %+v in %+v", yk, client)
	}
	line, ok := s.lineRect(client)
	if !ok || line.Bottom > yk.Top {
		t.Fatalf("a line must separate the bottom group: %+v, %v", line, ok)
	}
	expect(t, "line", px(client.Width()/2, line.Top), Light.Lines)

	for _, key := range []uintptr{win32.VK_END, win32.VK_UP} {
		win32.SendMessage(s.HWND(), win32.WM_KEYDOWN, key, 0)
	}
	if len(chosen) != 2 || chosen[0] != 3 || chosen[1] != 2 {
		t.Fatalf("End reaches the bottom item: %v", chosen)
	}

	s.SetHidden(3, true)
	if !s.Hidden(3) {
		t.Fatal("SetHidden")
	}
	if _, ok := s.lineRect(client); ok {
		t.Fatal("no line without a shown bottom item")
	}
	chosen = nil
	for _, key := range []uintptr{win32.VK_DOWN, win32.VK_END} {
		win32.SendMessage(s.HWND(), win32.WM_KEYDOWN, key, 0)
	}
	win32.SendMessage(s.HWND(), win32.WM_LBUTTONDOWN, 0, uintptr(yk.Top+2)<<16|uintptr(yk.Left+2))
	s.Select(3)
	if len(chosen) != 0 || s.Selected() != 2 {
		t.Fatalf("a hidden item can't be chosen: %v, selected %d", chosen, s.Selected())
	}

	// Hiding the top item: Home goes to the first shown one.
	s.SetHidden(3, false)
	s.SetHidden(0, true)
	win32.SendMessage(s.HWND(), win32.WM_KEYDOWN, win32.VK_HOME, 0)
	if s.Selected() != 1 || s.rowRect(1, client).Top != client.Top+th.Scale.Px(sidebarPadding) {
		t.Fatalf("Home with the first item hidden: selected %d", s.Selected())
	}
}

func TestTrailHighlightsTheCurrentStep(t *testing.T) {
	th, host := testTheme(t)
	tr, err := NewTrail(th, host, Light.Surface)
	if err != nil {
		t.Fatal(err)
	}
	countPill := func(steps []TrailStep) int {
		tr.Set(steps)
		px := render(t, tr.HWND(), 400, 24)
		n := 0
		for x := int32(0); x < 400; x++ {
			if px(x, 3) == Light.Selection {
				n++
			}
		}
		return n
	}
	if n := countPill([]TrailStep{{"Unlock keys", StepDone}, {"Back up", StepWaiting}}); n != 0 {
		t.Fatalf("no current step, but %d pill pixels", n)
	}
	if n := countPill([]TrailStep{{"Unlock keys", StepDone}, {"Back up 1 of 2", StepCurrent}, {"Clean up", StepWaiting}}); n < 40 {
		t.Fatalf("the current step needs its pill, got %d pixels", n)
	}
}

func TestProgressBarSwitchesToMarquee(t *testing.T) {
	_, host := testTheme(t)
	p, err := NewProgressBar(host)
	if err != nil {
		t.Fatal(err)
	}
	p.Set(-1)
	if win32.Style(p.HWND())&win32.PBS_MARQUEE == 0 {
		t.Fatal("an unknown total shows a marquee")
	}
	p.Set(0.5)
	if win32.Style(p.HWND())&win32.PBS_MARQUEE != 0 {
		t.Fatal("a known fraction shows the bar")
	}
	if pos := win32.SendMessage(p.HWND(), win32.PBM_GETPOS, 0, 0); pos != progressRange/2 {
		t.Fatalf("position %d", pos)
	}
}

func TestSplitterReportsItsNewTop(t *testing.T) {
	th, host := testTheme(t)
	s, err := NewSplitter(th, host, Light.Surface)
	if err != nil {
		t.Fatal(err)
	}
	win32.SetWindowPos(s.HWND(), win32.Rect{Top: 100, Right: 400, Bottom: 108})
	var tops []int32
	s.OnMove = func(top int32) { tops = append(tops, top) }
	at := func(x, y int32) uintptr { return uintptr(uint16(x)) | uintptr(uint16(y))<<16 }
	win32.SendMessage(s.HWND(), win32.WM_MOUSEMOVE, 0, at(10, 4)) // not dragging
	win32.SendMessage(s.HWND(), win32.WM_LBUTTONDOWN, 0, at(10, 4))
	win32.SendMessage(s.HWND(), win32.WM_MOUSEMOVE, 0, at(10, 24))
	win32.SendMessage(s.HWND(), win32.WM_LBUTTONUP, 0, at(10, 24))
	win32.SendMessage(s.HWND(), win32.WM_MOUSEMOVE, 0, at(10, 50)) // released
	if len(tops) != 1 || tops[0] != 120 {
		t.Fatalf("tops %v, want [120]", tops)
	}
}

func TestListViewGroupsAndItems(t *testing.T) {
	_, host := testTheme(t)
	lv, err := win32.CreateWindow(0, win32.WC_LISTVIEW, "", win32.WS_CHILD|win32.WS_VISIBLE|win32.LVS_REPORT|win32.LVS_SINGLESEL, 0, 0, 400, 300, host, 0)
	if err != nil {
		t.Fatal(err)
	}
	win32.ListSetup(lv)
	win32.ListInsertColumn(lv, 0, "Folder", 120, false)
	win32.ListInsertColumn(lv, 1, "Size", 80, true)
	win32.ListInsertGroup(lv, 0, "Today, 09:12 · 2 folders", "Show log", false)
	win32.ListInsertGroup(lv, 1, "Sun 27 Sep, 20:05 · 1 folder", "", true)
	a := win32.ListInsertItem(lv, "Docs", 0, 7)
	win32.ListSetText(lv, a, 1, "1.0 GB")
	win32.ListInsertItem(lv, "Pics", 0, 8)
	win32.ListInsertItem(lv, "Docs", 1, 9)
	if n := win32.ListItemCount(lv); n != 3 {
		t.Fatalf("%d items", n)
	}
	if !win32.ListGroupCollapsed(lv, 1) || win32.ListGroupCollapsed(lv, 0) {
		t.Fatal("group 1 starts collapsed, group 0 expanded")
	}
	win32.ListSelect(lv, 1)
	if sel := win32.ListSelected(lv); sel != 1 || win32.ListParam(lv, sel) != 8 {
		t.Fatalf("selected %d with param %d", sel, win32.ListParam(lv, sel))
	}
	win32.ListClear(lv)
	if win32.ListItemCount(lv) != 0 || win32.ListSelected(lv) != -1 {
		t.Fatal("cleared")
	}
}

func TestListViewCheckboxes(t *testing.T) {
	_, host := testTheme(t)
	lv, err := win32.CreateWindow(0, win32.WC_LISTVIEW, "", win32.WS_CHILD|win32.WS_VISIBLE|win32.LVS_REPORT, 0, 0, 300, 200, host, 0)
	if err != nil {
		t.Fatal(err)
	}
	win32.ListSetupPlain(lv, true)
	win32.ListInsertColumn(lv, 0, "Folder", 120, false)
	a := win32.ListAddItem(lv, "Docs", 1)
	b := win32.ListAddItem(lv, "Pics", 2)
	win32.ListSetChecked(lv, a, true)
	if !win32.ListChecked(lv, a) || win32.ListChecked(lv, b) || win32.ListParam(lv, b) != 2 {
		t.Fatal("check states")
	}
	win32.ListSetChecked(lv, a, false)
	if win32.ListChecked(lv, a) {
		t.Fatal("cleared")
	}
}

func TestPanelScrolls(t *testing.T) {
	th, host := testTheme(t)
	p, err := NewPanel(th, host, 0, PanelStyle{Back: Light.Surface})
	if err != nil {
		t.Fatal(err)
	}
	win32.SetWindowPos(p.HWND(), win32.Rect{Right: 200, Bottom: 100})
	scrolled := 0
	p.OnScroll = func() { scrolled++ }
	p.SetScroll(300)
	win32.SendMessage(p.HWND(), win32.WM_VSCROLL, win32.SB_PAGEDOWN, 0)
	if p.ScrollOffset() != 100 || scrolled != 1 {
		t.Fatalf("page down: offset %d, %d calls", p.ScrollOffset(), scrolled)
	}
	win32.SendMessage(p.HWND(), win32.WM_VSCROLL, win32.SB_BOTTOM, 0)
	if p.ScrollOffset() != 200 {
		t.Fatalf("bottom: offset %d, want 200", p.ScrollOffset())
	}
	wheelUp := uintptr(uint16(win32.WHEEL_DELTA)) << 16
	win32.SendMessage(p.HWND(), win32.WM_MOUSEWHEEL, wheelUp, 0)
	if p.ScrollOffset() != 140 {
		t.Fatalf("wheel: offset %d, want 140", p.ScrollOffset())
	}
	p.SetScroll(80) // everything fits
	if p.ScrollOffset() != 0 {
		t.Fatalf("fits: offset %d", p.ScrollOffset())
	}
}

func TestPanelTips(t *testing.T) {
	th, host := testTheme(t)
	p, err := NewPanel(th, host, 0, PanelStyle{Back: Light.Surface})
	if err != nil {
		t.Fatal(err)
	}
	const long = `C:\Users\phs\Documents\Projects\RestoreSafe\sandbox`
	path := p.PathLabel(long, TextBody, Light.Text)
	name := p.Label("Docs", TextBody, Light.Text)
	p.Tip(name, `C:\Users\phs\Documents`)
	win32.SetWindowPos(path, win32.Rect{Right: 60, Bottom: 20})
	win32.SetWindowPos(name, win32.Rect{Right: 200, Bottom: 20})
	const ttmGetToolCount = 0x040D
	if n := win32.SendMessage(p.tip, ttmGetToolCount, 0, 0); n != 2 {
		t.Fatalf("%d tools", n)
	}
	if win32.Style(name)&win32.SS_NOTIFY == 0 {
		t.Fatal("a static gets the mouse for its tip")
	}
	if got := p.tipOf(path); got != long {
		t.Fatalf("a shortened path shows in full: %q", got)
	}
	if got := p.tipOf(name); got != `C:\Users\phs\Documents` {
		t.Fatalf("a label that fits shows only its own tip: %q", got)
	}
	win32.SetWindowPos(path, win32.Rect{Right: 2000, Bottom: 20})
	if got := p.tipOf(path); got != "" {
		t.Fatalf("a path that fits has no tip: %q", got)
	}
	p.Clear()
	if n := win32.SendMessage(p.tip, ttmGetToolCount, 0, 0); n != 0 {
		t.Fatalf("%d tools after Clear", n)
	}
}

func TestTrailReportsClicksOnDoneSteps(t *testing.T) {
	th, host := testTheme(t)
	tr, err := NewTrail(th, host, Light.Surface)
	if err != nil {
		t.Fatal(err)
	}
	var clicked []int
	tr.OnClick = func(i int) { clicked = append(clicked, i) }
	tr.Set([]TrailStep{{"1 When", StepDone}, {"2 Folders", StepCurrent}, {"3 Destination", StepWaiting}})
	render(t, tr.HWND(), 400, 24) // paints, which records where the steps are
	at := func(x int32) uintptr { return uintptr(uint16(x)) | uintptr(12)<<16 }
	win32.SendMessage(tr.HWND(), win32.WM_LBUTTONUP, 0, at(tr.spans[0][0]+4)) // done: reported
	win32.SendMessage(tr.HWND(), win32.WM_LBUTTONUP, 0, at(tr.spans[1][0]+4)) // current: not
	if len(clicked) != 1 || clicked[0] != 0 {
		t.Fatalf("clicks %v", clicked)
	}
}

func TestExpanderToggles(t *testing.T) {
	th, host := testTheme(t)
	e, err := NewExpander(th, host, Light.Surface, 0)
	if err != nil {
		t.Fatal(err)
	}
	var got []bool
	e.OnToggle = func(open bool) { got = append(got, open) }
	e.Set("Technical details", false)
	if e.Open() || e.Width() <= 0 {
		t.Fatalf("closed: open %v, width %d", e.Open(), e.Width())
	}
	px := render(t, e.HWND(), 300, 24)
	expect(t, "background", px(299, 12), Light.Surface)

	win32.SendMessage(e.HWND(), win32.WM_LBUTTONDOWN, 0, 0)
	win32.SendMessage(e.HWND(), win32.WM_KEYDOWN, win32.VK_RETURN, 0)
	win32.SendMessage(e.HWND(), win32.WM_KEYDOWN, win32.VK_SPACE, 0)
	win32.SendMessage(e.HWND(), win32.WM_KEYDOWN, win32.VK_DOWN, 0)
	if len(got) != 3 || !got[0] || got[1] || !got[2] || !e.Open() {
		t.Fatalf("a click, Enter and Space toggle it, other keys don't: %v", got)
	}
	m := win32.Msg{Message: win32.WM_KEYDOWN, WParam: win32.VK_RETURN}
	if r := win32.SendMessage(e.HWND(), win32.WM_GETDLGCODE, 0, uintptr(unsafe.Pointer(&m))); r != win32.DLGC_WANTMESSAGE {
		t.Fatalf("Enter must reach the expander: %#x", r)
	}
	if r := win32.SendMessage(e.HWND(), win32.WM_GETDLGCODE, 0, 0); r != 0 {
		t.Fatalf("without a message: %#x", r)
	}
	win32.SendMessage(e.HWND(), win32.WM_MOUSEMOVE, 0, 0)
	win32.SendMessage(e.HWND(), win32.WM_MOUSELEAVE, 0, 0)
	render(t, e.HWND(), 300, 24) // paints open, with the focus outline after the keys
}
