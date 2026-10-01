package gui

import (
	"RestoreSafe/internal/gui/view"
	"RestoreSafe/internal/gui/widget"
	"RestoreSafe/internal/gui/win32"
	"RestoreSafe/internal/workflow/interact"
	"strings"

	"golang.org/x/sys/windows"
)

// detailsDialog shows a report ("Check details", later "Show details") in a
// resizable modal window with a Close button.
type detailsDialog struct {
	hwnd, report, close win32.HWND
	open                bool
}

var (
	activeDetails      *detailsDialog
	detailsClassExists bool
)

const detailsClass = "RestoreSafeDetails"

// Size of the details dialog, in DIPs.
const (
	detailsWidth  = 640
	detailsHeight = 520
	detailsMargin = 12
	closeWidth    = 96
)

// showDetails shows the report r modal to owner until the user closes it.
func (a *app) showDetails(owner win32.HWND, title string, r interact.Report) {
	a.showViewer(owner, title, func(re win32.HWND) { win32.SetRichText(re, reportRTF(r, a.fontFace, a.fontPt)) })
}

// showText shows plain text (a log) modal to owner, scrolled to its end.
func (a *app) showText(owner win32.HWND, title, text string) {
	a.showViewer(owner, title, func(re win32.HWND) {
		win32.SendMessage(re, win32.EM_SETTEXTMODE, win32.TM_PLAINTEXT, 0)
		win32.SendMessage(re, win32.EM_EXLIMITTEXT, 0, 64<<20)
		win32.SetFont(re, a.monoFont)
		win32.SetText(re, strings.ReplaceAll(text, "\n", "\r\n"))
		win32.SendMessage(re, win32.WM_VSCROLL, win32.SB_BOTTOM, 0)
	})
}

// showViewer shows a read-only rich edit that fill fills, modal to owner,
// with a Close button.
func (a *app) showViewer(owner win32.HWND, title string, fill func(re win32.HWND)) {
	if !detailsClassExists {
		wc := win32.WndClassEx{
			WndProc:    windows.NewCallback(detailsProc),
			Instance:   win32.ModuleHandle(),
			Cursor:     win32.ArrowCursor(),
			Background: win32.SysColorBrush(win32.COLOR_WINDOW),
			ClassName:  win32.UTF16(detailsClass),
		}
		if err := win32.RegisterClass(&wc); err != nil {
			return
		}
		detailsClassExists = true
	}
	s := widget.Scale(a.dpi)
	const style = win32.WS_POPUP | win32.WS_CAPTION | win32.WS_SYSMENU | win32.WS_THICKFRAME
	const exStyle = win32.WS_EX_DLGMODALFRAME | win32.WS_EX_CONTROLPARENT
	frame := win32.WindowRectForClient(win32.Rect{Right: s.Px(detailsWidth), Bottom: s.Px(detailsHeight)}, style, exStyle, a.dpi)
	ownerRect := win32.WindowRect(owner)
	x := ownerRect.Left + (ownerRect.Width()-frame.Width())/2
	y := ownerRect.Top + (ownerRect.Height()-frame.Height())/3
	hwnd, err := win32.CreateWindow(exStyle, detailsClass, title, style, x, y, frame.Width(), frame.Height(), owner, 0)
	if err != nil {
		return
	}
	d := &detailsDialog{hwnd: hwnd, open: true}
	d.report, _ = win32.CreateWindow(0, win32.MSFTEDIT_CLASS, title,
		win32.WS_CHILD|win32.WS_VISIBLE|win32.WS_TABSTOP|win32.WS_VSCROLL|win32.WS_BORDER|win32.ES_MULTILINE|win32.ES_READONLY|win32.ES_AUTOVSCROLL,
		0, 0, 0, 0, hwnd, 0)
	d.close, _ = win32.CreateWindow(0, "BUTTON", view.ButtonClose, win32.WS_CHILD|win32.WS_VISIBLE|win32.WS_TABSTOP|win32.BS_DEFPUSHBUTTON, 0, 0, 0, 0, hwnd, win32.IDOK)
	win32.SetFont(d.close, a.font)
	win32.SendMessage(d.report, win32.EM_SETBKGNDCOLOR, 0, uintptr(win32.SysColor(win32.COLOR_WINDOW)))
	win32.SendMessage(d.report, win32.EM_SETZOOM, uintptr(a.dpi), uintptr(win32.DpiForSystem()))
	fill(d.report)
	win32.SetAccessibleName(d.report, title)
	activeDetails = d
	d.layout(s)

	win32.Enable(owner, false)
	win32.ShowWindow(hwnd, win32.SW_SHOWNORMAL)
	win32.SetFocus(d.close)
	var msg win32.Msg
	for d.open {
		more, err := win32.GetMessage(&msg)
		if err != nil || !more {
			win32.PostQuitMessage(int32(msg.WParam))
			break
		}
		if !win32.IsDialogMessage(hwnd, &msg) {
			win32.TranslateAndDispatch(&msg)
		}
	}
	// Re-enable the owner before the dialog disappears, so the activation
	// returns to it.
	win32.Enable(owner, true)
	win32.SetForeground(owner)
	win32.DestroyWindow(hwnd)
	activeDetails = nil
}

func (d *detailsDialog) layout(s widget.Scale) {
	area := widget.NewArea(s, win32.ClientRect(d.hwnd))
	area.Inset(detailsMargin, detailsMargin, detailsMargin, detailsMargin)
	buttons := widget.NewArea(s, area.Bottom(widget.ButtonHeight))
	win32.SetWindowPos(d.close, buttons.Right(closeWidth))
	area.Bottom(detailsMargin)
	win32.SetWindowPos(d.report, area.Rest())
}

func detailsProc(hwnd win32.HWND, msg uint32, wparam, lparam uintptr) uintptr {
	d := activeDetails
	if d == nil || d.hwnd != hwnd {
		return win32.DefWindowProc(hwnd, msg, wparam, lparam)
	}
	switch msg {
	case win32.WM_COMMAND:
		if id := win32.LoWord(wparam); id == win32.IDOK || id == win32.IDCANCEL {
			d.open = false
			return 0
		}
	case win32.WM_CLOSE:
		d.open = false
		return 0
	case win32.WM_SIZE:
		d.layout(widget.Scale(win32.DpiForWindow(hwnd)))
		return 0
	}
	return win32.DefWindowProc(hwnd, msg, wparam, lparam)
}
