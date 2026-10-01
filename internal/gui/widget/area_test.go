package widget

import (
	"RestoreSafe/internal/gui/win32"
	"testing"
)

func TestScale(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		dpi  Scale
		dip  int32
		want int32
	}{{96, 10, 10}, {120, 10, 13}, {144, 10, 15}, {192, 10, 20}, {144, 1, 2}} {
		if got := tc.dpi.Px(tc.dip); got != tc.want {
			t.Fatalf("%d dip at %d dpi = %d px, want %d", tc.dip, tc.dpi, got, tc.want)
		}
	}
}

func TestAreaCutsRowsAndColumns(t *testing.T) {
	t.Parallel()
	a := NewArea(144, win32.Rect{Right: 600, Bottom: 300})
	a.Inset(10, 10, 10, 10) // 15 px at 144 dpi
	if a.R != (win32.Rect{Left: 15, Top: 15, Right: 585, Bottom: 285}) {
		t.Fatalf("inset: %+v", a.R)
	}
	top := a.Top(20)
	bottom := a.Bottom(20)
	left := a.Left(100)
	right := a.Right(100)
	if top != (win32.Rect{Left: 15, Top: 15, Right: 585, Bottom: 45}) || bottom.Top != 255 || left.Right != 165 || right.Left != 435 {
		t.Fatalf("cuts: top %+v bottom %+v left %+v right %+v", top, bottom, left, right)
	}
	if rest := a.Rest(); rest != (win32.Rect{Left: 165, Top: 45, Right: 435, Bottom: 255}) {
		t.Fatalf("rest: %+v", rest)
	}

	cols := NewArea(96, win32.Rect{Right: 310, Bottom: 50}).Columns(10, 1, 2)
	if cols[0].R.Right != 100 || cols[1].R.Left != 110 || cols[1].R.Right != 310 {
		t.Fatalf("columns: %+v %+v", cols[0].R, cols[1].R)
	}
}

func TestAreaNeverGoesNegative(t *testing.T) {
	t.Parallel()
	a := NewArea(192, win32.Rect{Right: 50, Bottom: 40})
	a.Top(100)
	if a.Height() != 0 || a.Width() != 50 {
		t.Fatalf("a row larger than the area takes what is left: %+v", a.R)
	}
	a.Inset(100, 100, 100, 100)
	if a.Width() < 0 || a.Height() < 0 {
		t.Fatalf("inset larger than the area: %+v", a.R)
	}
	for _, c := range NewArea(96, win32.Rect{Right: 10, Bottom: 10}).Columns(20, 1, 1, 1) {
		if c.Width() < 0 {
			t.Fatalf("columns wider than the area: %+v", c.R)
		}
	}
}
