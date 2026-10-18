package widget

import (
	"testing"

	"github.com/phsc84/restoresafe/internal/gui/win32"
)

func TestScaleConvertsRectsAndBack(t *testing.T) {
	t.Parallel()
	s := Scale(144)
	if r := s.Rect(10, 20, 30, 40); r != (win32.Rect{Left: 15, Top: 30, Right: 60, Bottom: 90}) {
		t.Fatalf("Rect: %+v", r)
	}
	if s.Dip(15) != 10 || s.Dip(s.Px(33)) != 33 {
		t.Fatalf("Dip: %d, %d", s.Dip(15), s.Dip(s.Px(33)))
	}
	a := NewArea(96, win32.Rect{Right: 100, Bottom: 20})
	if r := a.RightPx(30); r.Left != 70 || a.R.Right != 70 {
		t.Fatalf("RightPx: %+v, left %+v", r, a.R)
	}
	if r := a.RightPx(500); r.Left != 0 || a.R.Width() != 0 {
		t.Fatalf("RightPx wider than the area: %+v, left %+v", r, a.R)
	}
}

func TestCurrentPaletteFollowsTheSystem(t *testing.T) {
	want := Light
	if win32.HighContrastOn() {
		want = HighContrast()
	}
	if CurrentPalette() != want {
		t.Fatal("the palette must follow the high contrast setting")
	}
}

func TestDrawBadgeAndFillRectInACell(t *testing.T) {
	th, _ := testTheme(t)
	hdc, px := canvas(t, 120, 24)
	cell := win32.Rect{Right: 120, Bottom: 24}
	FillRect(hdc, cell, Light.Surface)
	DrawBadge(hdc, th, cell, "DIFF 3", Light.Diff, Light.DiffBack)
	expect(t, "left of the badge", px(2, 12), Light.Surface)
	expect(t, "badge fill", px(9, 12), Light.DiffBack)
	expect(t, "right of the badge", px(118, 12), Light.Surface)
}

func TestSplitterPaintsItsGrip(t *testing.T) {
	th, host := testTheme(t)
	s, err := NewSplitter(th, host, Light.Surface)
	if err != nil {
		t.Fatal(err)
	}
	px := render(t, s.HWND(), 200, 8)
	expect(t, "splitter", px(10, 4), Light.Surface)
	expect(t, "grip", px(100, 4), Light.Lines)
}

func TestListHeaderIsDrawnInTheTheme(t *testing.T) {
	th, host := testTheme(t)
	if win32.HighContrastOn() {
		t.Skip("in high contrast the header keeps the system look")
	}
	lv, err := win32.CreateWindow(0, win32.WC_LISTVIEW, "", win32.WS_CHILD|win32.WS_VISIBLE|win32.LVS_REPORT, 0, 0, 300, 200, host, 0)
	if err != nil {
		t.Fatal(err)
	}
	win32.ListSetup(lv)
	win32.ListInsertColumn(lv, 0, "Folder", 120, false)
	win32.ListInsertColumn(lv, 1, "Size", 80, true)
	StyleListHeader(th, lv)
	header := win32.ListHeader(lv)
	h := win32.ClientRect(header).Height()
	if h <= 2 {
		t.Fatalf("header height %d", h)
	}
	px := render(t, header, 300, h)
	expect(t, "column background", px(3, 2), Light.SurfaceAlt)
	expect(t, "line under the header", px(60, h-1), Light.Lines)
	expect(t, "divider", px(119, h/2), Light.Lines)
	expect(t, "right of the last column", px(250, 2), Light.SurfaceAlt)
}

func TestPanelControlsAndLifecycle(t *testing.T) {
	th, host := testTheme(t)
	p, err := NewPanel(th, host, 0, PanelStyle{Back: Light.Surface})
	if err != nil {
		t.Fatal(err)
	}
	if p.Back() != Light.Surface {
		t.Fatalf("Back: %06X", uint32(p.Back()))
	}
	para := p.Paragraph("A paragraph that wraps.", TextBody, Light.Text)
	p.SetText(para, "abc")
	if n := win32.SendMessage(para, wmGetTextLength, 0, 0); n != 3 {
		t.Fatalf("SetText: length %d", n)
	}

	// A label's color reaches the text through WM_CTLCOLORSTATIC.
	label := p.Label("█████", TextTitle, Light.Text)
	p.SetColor(label, Light.Error)
	win32.SetWindowPos(p.HWND(), win32.Rect{Right: 300, Bottom: 100})
	win32.SetWindowPos(label, win32.Rect{Right: 200, Bottom: 40})
	px := render(t, label, 200, 40)
	if !anyPixel(px, 200, 40, Light.Error) {
		t.Fatal("the label's text must be drawn in the color set")
	}

	adopted, err := win32.CreateWindow(0, "STATIC", "", win32.WS_CHILD, 0, 0, 10, 10, p.HWND(), 0)
	if err != nil {
		t.Fatal(err)
	}
	p.Adopt(adopted)
	p.Restyle()
	p.Show(false)
	if win32.IsEnabled(p.HWND()) {
		t.Fatal("a hidden panel is disabled")
	}
	p.Show(true)
	p.Clear()
	if win32.IsWindow(adopted) || win32.IsWindow(label) {
		t.Fatal("Clear destroys the panel's controls, the adopted one too")
	}
	p.Destroy()
	if win32.IsWindow(p.HWND()) {
		t.Fatal("Destroy destroys the panel")
	}
}

func TestPanelScrollsByLineAndPage(t *testing.T) {
	th, host := testTheme(t)
	p, err := NewPanel(th, host, 0, PanelStyle{Back: Light.Surface})
	if err != nil {
		t.Fatal(err)
	}
	win32.SetWindowPos(p.HWND(), win32.Rect{Right: 200, Bottom: 100})
	p.SetScroll(300)
	line := th.Scale.Px(wheelStep / 2)
	for _, step := range []struct {
		code uintptr
		want int32
	}{
		{win32.SB_LINEDOWN, line},
		{win32.SB_PAGEDOWN, line + 100},
		{win32.SB_LINEUP, 100},
		{win32.SB_PAGEUP, 0},
		{win32.SB_BOTTOM, 200},
		{win32.SB_TOP, 0},
	} {
		win32.SendMessage(p.HWND(), win32.WM_VSCROLL, step.code, 0)
		if p.ScrollOffset() != step.want {
			t.Fatalf("scroll code %d: offset %d, want %d", step.code, p.ScrollOffset(), step.want)
		}
	}
}

// wmGetTextLength asks a control for the length of its text; the program
// never needs it.
const wmGetTextLength = 0x000E

// anyPixel reports whether a pixel of the w×h image has color c.
func anyPixel(px func(x, y int32) Color, w, h int32, c Color) bool {
	for y := int32(0); y < h; y++ {
		for x := int32(0); x < w; x++ {
			if px(x, y) == c {
				return true
			}
		}
	}
	return false
}
