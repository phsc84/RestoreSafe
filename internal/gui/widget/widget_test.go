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
