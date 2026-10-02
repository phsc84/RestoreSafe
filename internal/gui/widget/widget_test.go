package widget

import (
	"RestoreSafe/internal/gui/win32"
	"runtime"
	"testing"
	"unsafe"
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

// render sizes hwnd to w×h, paints it through WM_PRINTCLIENT, and returns
// the pixels.
func render(t *testing.T, hwnd win32.HWND, w, h int32) func(x, y int32) Color {
	t.Helper()
	win32.SetWindowPos(hwnd, win32.Rect{Right: w, Bottom: h})
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
	win32.SendMessage(hwnd, win32.WM_PRINTCLIENT, mem, 0)
	return func(x, y int32) Color { return Color(win32.Pixel(mem, x, y)) }
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

func TestPrimaryButtonIsAccentFilled(t *testing.T) {
	th, host := testTheme(t)
	p, err := NewPanel(th, host, 0, PanelStyle{Back: Light.Surface})
	if err != nil {
		t.Fatal(err)
	}
	button := p.PrimaryButton("&Back up now…", 7)
	draw := func(state uint32) func(x, y int32) Color {
		screen := win32.GetDC(0)
		mem := win32.CreateCompatibleDC(screen)
		bmp := win32.CreateCompatibleBitmap(screen, 120, 28)
		win32.ReleaseDC(0, screen)
		old := win32.SelectObject(mem, bmp)
		t.Cleanup(func() { win32.SelectObject(mem, old); win32.DeleteObject(bmp); win32.DeleteDC(mem) })
		di := win32.DrawItemStruct{HwndItem: button, HDC: mem, Item: win32.Rect{Right: 120, Bottom: 28}, ItemState: state}
		win32.SendMessage(p.HWND(), win32.WM_DRAWITEM, 7, uintptr(unsafe.Pointer(&di)))
		return func(x, y int32) Color { return Color(win32.Pixel(mem, x, y)) }
	}
	expect(t, "enabled", draw(0)(4, 14), Light.Accent)
	expect(t, "disabled", draw(win32.ODS_DISABLED)(4, 14), Light.Control)
	expect(t, "corner", draw(0)(0, 0), Light.Surface)
}

func TestBadgeIconAndBar(t *testing.T) {
	th, host := testTheme(t)
	back := RGB(9, 9, 9)

	b, err := NewBadge(th, host, back)
	if err != nil {
		t.Fatal(err)
	}
	b.Set("DIFF 3", Light.Diff, Light.DiffBack, "Differential 3")
	if b.Width() <= 2*badgePadding {
		t.Fatalf("badge width %d", b.Width())
	}
	px := render(t, b.HWND(), b.Width(), 18)
	expect(t, "badge corner", px(0, 0), back)
	expect(t, "badge fill", px(2, 9), Light.DiffBack)

	i, err := NewIcon(th, host, back, TextIcon)
	if err != nil {
		t.Fatal(err)
	}
	px = render(t, i.HWND(), 52, 52)
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
	items := []SidebarItem{{GlyphHome, "Overview"}, {GlyphHistory, "Backups"}, {GlyphSettings, "Settings"}}
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
	win32.ListInsertGroup(lv, 0, "Today, 09:12 · 2 folders", false)
	win32.ListInsertGroup(lv, 1, "Sun 27 Sep, 20:05 · 1 folder", true)
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
	win32.ListSetGroupCollapsed(lv, 1, false)
	if win32.ListGroupCollapsed(lv, 1) {
		t.Fatal("group 1 expanded")
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

func TestTooltipsAddAndClear(t *testing.T) {
	th, host := testTheme(t)
	p, err := NewPanel(th, host, 0, PanelStyle{Back: Light.Surface})
	if err != nil {
		t.Fatal(err)
	}
	label := p.Label("Docs", TextBody, Light.Text)
	tips, err := NewTooltips(th, host)
	if err != nil {
		t.Fatal(err)
	}
	tips.Set(label, `C:\Users\phs\Documents`)
	const ttmGetToolCount = 0x040D
	if n := win32.SendMessage(tips.tip, ttmGetToolCount, 0, 0); n != 1 {
		t.Fatalf("%d tools", n)
	}
	if win32.Style(label)&win32.SS_NOTIFY == 0 {
		t.Fatal("a static gets the mouse for its tip")
	}
	tips.Clear()
	if n := win32.SendMessage(tips.tip, ttmGetToolCount, 0, 0); n != 0 {
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
