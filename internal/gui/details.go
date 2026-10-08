package gui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/phsc84/restoresafe/internal/gui/view"
	"github.com/phsc84/restoresafe/internal/gui/widget"
	"github.com/phsc84/restoresafe/internal/gui/win32"
	"github.com/phsc84/restoresafe/internal/workflow/interact"
)

// detailsDialog shows a report ("Check details", later "Show details") in a
// resizable modal window with a Close button.
type detailsDialog struct {
	hwnd, report, close win32.HWND
	open                bool
	// filters are All and Warnings and errors of a log; refilter shows the
	// log with a filter. Both are nil for a report.
	filters  []win32.HWND
	refilter func(re win32.HWND, f view.LogFilter)
	// openButton opens the log file in an editor with openFile; both are
	// nil for a report.
	openButton win32.HWND
	openFile   func()
}

// Control IDs of the log filter and Open in Editor.
const (
	idViewerAll = 101 + iota
	idViewerWarnings
	idViewerOpen
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

// showLog shows the log file at path modal to owner, with a filter and
// Open in Editor (GUI spec BK-5, RW-8); when names the run in the title.
func (a *app) showLog(owner win32.HWND, path, when string) {
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		win32.MessageBox(owner, fmt.Sprintf("Cannot open %s: %v", filepath.ToSlash(path), err), "RestoreSafe", win32.MB_OK|win32.MB_ICONERROR)
		return
	}
	text := string(data)
	show := func(re win32.HWND, f view.LogFilter) {
		win32.SetRichText(re, logRTF(view.LogLinesOf(text, f), a.fontPt))
		win32.SendMessage(re, win32.WM_VSCROLL, win32.SB_BOTTOM, 0)
	}
	a.openViewer(owner, view.LogWindowTitle(when, filepath.Base(path)), func(re win32.HWND) { show(re, view.LogAll) }, show, func() { a.open(path, true) })
}

// showViewer shows a read-only rich edit that fill fills, modal to owner,
// with a Close button.
func (a *app) showViewer(owner win32.HWND, title string, fill func(re win32.HWND)) {
	a.openViewer(owner, title, fill, nil, nil)
}

// openViewer shows the viewer; refilter, when set, adds the log filter, and
// open the button that opens the file in an editor.
func (a *app) openViewer(owner win32.HWND, title string, fill func(re win32.HWND), refilter func(re win32.HWND, f view.LogFilter), open func()) {
	if err := registerClass(win32.WndClassEx{}, detailsClass); err != nil {
		return
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
	d := &detailsDialog{hwnd: hwnd, open: true, refilter: refilter, openFile: open}
	d.report, _ = win32.CreateWindow(0, win32.MSFTEDIT_CLASS, title,
		win32.WS_CHILD|win32.WS_VISIBLE|win32.WS_TABSTOP|win32.WS_VSCROLL|win32.WS_BORDER|win32.ES_MULTILINE|win32.ES_READONLY|win32.ES_AUTOVSCROLL,
		0, 0, 0, 0, hwnd, 0)
	d.close, _ = win32.CreateWindow(0, "BUTTON", view.ButtonClose, win32.WS_CHILD|win32.WS_VISIBLE|win32.WS_TABSTOP|win32.BS_DEFPUSHBUTTON, 0, 0, 0, 0, hwnd, win32.IDOK)
	win32.SetFont(d.close, a.font)
	if refilter != nil {
		lp := view.LogViewerOf()
		all, _ := win32.CreateWindow(0, "BUTTON", lp.All, win32.WS_CHILD|win32.WS_VISIBLE|win32.WS_TABSTOP|win32.WS_GROUP|win32.BS_AUTORADIOBUTTON|win32.BS_PUSHLIKE, 0, 0, 0, 0, hwnd, idViewerAll)
		warn, _ := win32.CreateWindow(0, "BUTTON", lp.Warnings, win32.WS_CHILD|win32.WS_VISIBLE|win32.BS_AUTORADIOBUTTON|win32.BS_PUSHLIKE, 0, 0, 0, 0, hwnd, idViewerWarnings)
		for _, b := range []win32.HWND{all, warn} {
			win32.SetFont(b, a.font)
		}
		win32.SetChecked(all, true)
		d.filters = []win32.HWND{all, warn}
	}
	buttons := append([]win32.HWND{d.close}, d.filters...)
	if open != nil {
		d.openButton, _ = win32.CreateWindow(0, "BUTTON", view.LogViewerOf().Open, win32.WS_CHILD|win32.WS_VISIBLE|win32.WS_TABSTOP, 0, 0, 0, 0, hwnd, idViewerOpen)
		win32.SetFont(d.openButton, a.font)
		buttons = append(buttons, d.openButton)
	}
	for _, h := range buttons {
		widget.StyleButton(a.theme, h, widget.Color(win32.SysColor(win32.COLOR_WINDOW)), false)
	}
	win32.SendMessage(d.report, win32.EM_SETBKGNDCOLOR, 0, uintptr(win32.SysColor(win32.COLOR_WINDOW)))
	win32.SendMessage(d.report, win32.EM_SETZOOM, uintptr(a.dpi), uintptr(win32.DpiForSystem()))
	fill(d.report)
	win32.SetAccessibleName(d.report, title)
	handlers[hwnd] = d
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
	delete(handlers, hwnd)
}

func (d *detailsDialog) layout(s widget.Scale) {
	area := widget.NewArea(s, win32.ClientRect(d.hwnd))
	area.Inset(detailsMargin, detailsMargin, detailsMargin, detailsMargin)
	buttons := widget.NewArea(s, area.Bottom(widget.ButtonHeight))
	win32.SetWindowPos(d.close, buttons.Right(closeWidth))
	if d.openButton != 0 {
		buttons.Right(6)
		win32.SetWindowPos(d.openButton, buttons.Right(int32(1.5*closeWidth)))
	}
	for i, b := range d.filters {
		w := int32(closeWidth)
		if i == 1 {
			w = 2 * closeWidth
		}
		r := buttons.Left(w)
		win32.SetWindowPos(b, r)
		buttons.Left(6)
	}
	area.Bottom(detailsMargin)
	win32.SetWindowPos(d.report, area.Rest())
}

// message handles the messages of the viewer window.
func (d *detailsDialog) message(hwnd win32.HWND, msg uint32, wparam, lparam uintptr) uintptr {
	switch msg {
	case win32.WM_COMMAND:
		if id := win32.LoWord(wparam); id == win32.IDOK || id == win32.IDCANCEL {
			d.open = false
			return 0
		}
		switch win32.LoWord(wparam) {
		case idViewerAll:
			d.refilter(d.report, view.LogAll)
			return 0
		case idViewerWarnings:
			d.refilter(d.report, view.LogWarnings)
			return 0
		case idViewerOpen:
			d.openFile()
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
