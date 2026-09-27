package gui

import (
	"RestoreSafe/internal/gui/win32"
	"RestoreSafe/internal/security/cryptox"

	"golang.org/x/sys/windows"
)

// inputField is one edit field of an input dialog.
type inputField struct {
	label  string
	masked bool // password field: read with win32.ReadSecret
}

// inputDialog is a modal dialog with a heading, an optional message, one or
// more edit fields, and OK/Cancel.
type inputDialog struct {
	title   string
	heading string
	message string // shown under the heading
	// messageIsError shows the message in red (the previous attempt failed).
	messageIsError bool
	fields         []inputField
	okText         string
	// code is shown large in a monospaced font, e.g. a recovery code. It is a
	// static control, so it cannot be selected or copied, and it is
	// overwritten when the dialog closes.
	code string
	// note is explanatory text below the code.
	note string
	// noCancel shows only the OK button; closing the dialog counts as OK.
	noCancel bool
}

// dialogState is the open input dialog; the GUI shows at most one at a time.
type dialogState struct {
	hwnd           win32.HWND
	heading        win32.HWND
	message        win32.HWND
	edits          []win32.HWND
	fields         []inputField
	values         [][]byte
	code           win32.HWND
	codeFont       windows.Handle
	noCancel       bool
	ok, open       bool
	messageIsError bool
}

var (
	activeDialog      *dialogState
	dialogClassExists bool
)

const dialogClass = "RestoreSafeInputDialog"

// Sizes of the input dialog, in DIPs.
const (
	dialogWidth    = 440
	dialogMargin   = 16
	headingHeight  = 24
	messageHeight  = 54
	codeHeight     = 76
	codeNoteHeight = 84
	fieldLabel     = 20
	fieldHeight    = 26
	dialogButton   = 88
)

// runInputDialog shows d modal to the main window and returns the field
// values, or ok=false when it was cancelled. The caller zeroes masked
// values after use.
func (a *app) runInputDialog(d inputDialog) (values [][]byte, ok bool) {
	if !dialogClassExists {
		wc := win32.WndClassEx{
			WndProc:    windows.NewCallback(dialogProc),
			Instance:   win32.ModuleHandle(),
			Cursor:     win32.ArrowCursor(),
			Background: win32.SysColorBrush(win32.COLOR_WINDOW),
			ClassName:  win32.UTF16(dialogClass),
		}
		if err := win32.RegisterClass(&wc); err != nil {
			return nil, false
		}
		dialogClassExists = true
	}

	s := scale(a.dpi)
	height := int32(dialogMargin + headingHeight + gap)
	if d.message != "" {
		height += messageHeight + gap
	}
	if d.code != "" {
		height += codeHeight + gap
	}
	if d.note != "" {
		height += codeNoteHeight + gap
	}
	height += int32(len(d.fields)) * (fieldLabel + fieldHeight + gap)
	height += gap + buttonHeight + dialogMargin

	const style = win32.WS_POPUP | win32.WS_CAPTION | win32.WS_SYSMENU
	const exStyle = win32.WS_EX_DLGMODALFRAME | win32.WS_EX_CONTROLPARENT
	frame := win32.WindowRectForClient(win32.Rect{Right: s.px(dialogWidth), Bottom: s.px(height)}, style, exStyle, a.dpi)
	owner := win32.WindowRect(a.hwnd)
	x := owner.Left + (owner.Width()-frame.Width())/2
	y := owner.Top + (owner.Height()-frame.Height())/3

	hwnd, err := win32.CreateWindow(exStyle, dialogClass, d.title, style, x, y, frame.Width(), frame.Height(), a.hwnd, 0)
	if err != nil {
		return nil, false
	}
	ds := &dialogState{hwnd: hwnd, fields: d.fields, open: true, messageIsError: d.messageIsError, noCancel: d.noCancel}
	activeDialog = ds
	a.modal = hwnd

	child := func(class, text string, st uint32, r win32.Rect, id uintptr) win32.HWND {
		c, _ := win32.CreateWindow(0, class, text, win32.WS_CHILD|win32.WS_VISIBLE|st, r.Left, r.Top, r.Width(), r.Height(), hwnd, id)
		win32.SetFont(c, a.font)
		return c
	}
	w := int32(dialogWidth - 2*dialogMargin)
	yy := int32(dialogMargin)
	ds.heading = child("STATIC", d.heading, win32.SS_NOPREFIX, s.rect(dialogMargin, yy, w, headingHeight), 0)
	win32.SetFont(ds.heading, a.boldFont)
	yy += headingHeight + gap
	if d.message != "" {
		ds.message = child("STATIC", d.message, win32.SS_NOPREFIX, s.rect(dialogMargin, yy, w, messageHeight), 0)
		yy += messageHeight + gap
	}
	if d.code != "" {
		ds.code = child("STATIC", d.code, win32.SS_NOPREFIX|win32.SS_CENTER, s.rect(dialogMargin, yy, w, codeHeight), 0)
		if font, err := a.codeFont(); err == nil {
			ds.codeFont = font
			win32.SetFont(ds.code, font)
		}
		yy += codeHeight + gap
	}
	if d.note != "" {
		child("STATIC", d.note, win32.SS_NOPREFIX, s.rect(dialogMargin, yy, w, codeNoteHeight), 0)
		yy += codeNoteHeight + gap
	}
	for _, f := range d.fields {
		child("STATIC", f.label, win32.SS_NOPREFIX, s.rect(dialogMargin, yy, w, fieldLabel), 0)
		yy += fieldLabel
		st := uint32(win32.WS_TABSTOP | win32.WS_BORDER | win32.ES_AUTOHSCROLL)
		if f.masked {
			st |= win32.ES_PASSWORD
		}
		ds.edits = append(ds.edits, child("EDIT", "", st, s.rect(dialogMargin, yy, w, fieldHeight), 0))
		yy += fieldHeight + gap
	}
	yy += gap
	okText := d.okText
	if okText == "" {
		okText = "OK"
	}
	var okButton win32.HWND
	if d.noCancel {
		width := int32(2*dialogButton + gap)
		okButton = child("BUTTON", okText, win32.WS_TABSTOP|win32.BS_DEFPUSHBUTTON, s.rect(dialogWidth-dialogMargin-width, yy, width, buttonHeight), win32.IDOK)
	} else {
		okButton = child("BUTTON", okText, win32.WS_TABSTOP|win32.BS_DEFPUSHBUTTON, s.rect(dialogWidth-dialogMargin-2*dialogButton-gap, yy, dialogButton, buttonHeight), win32.IDOK)
		child("BUTTON", "Cancel", win32.WS_TABSTOP|win32.BS_PUSHBUTTON, s.rect(dialogWidth-dialogMargin-dialogButton, yy, dialogButton, buttonHeight), win32.IDCANCEL)
	}

	win32.Enable(a.hwnd, false)
	win32.ShowWindow(hwnd, win32.SW_SHOWNORMAL)
	if len(ds.edits) > 0 {
		win32.SetFocus(ds.edits[0])
	} else {
		win32.SetFocus(okButton)
	}

	var msg win32.Msg
	for ds.open {
		more, err := win32.GetMessage(&msg)
		if err != nil || !more {
			// WM_QUIT: end the dialog and let the main loop see it.
			win32.PostQuitMessage(int32(msg.WParam))
			ds.close(false)
			break
		}
		if !win32.IsDialogMessage(hwnd, &msg) {
			win32.TranslateAndDispatch(&msg)
		}
	}
	// Re-enable the owner before the dialog disappears, so the activation
	// returns to it and not to another application.
	win32.Enable(a.hwnd, true)
	win32.SetForeground(a.hwnd)
	win32.DestroyWindow(hwnd)
	win32.DeleteObject(ds.codeFont)
	activeDialog = nil
	a.modal = 0
	return ds.values, ds.ok
}

// codeFont creates the font for a displayed code: monospaced, bold, and
// about twice the message font's size. The caller deletes it.
func (a *app) codeFont() (windows.Handle, error) {
	lf, err := win32.MessageFont(a.dpi)
	if err != nil {
		return 0, err
	}
	lf.Height *= 2
	lf.Weight = win32.FW_BOLD
	lf.FaceName = [32]uint16{}
	copy(lf.FaceName[:], windows.StringToUTF16("Consolas"))
	return win32.CreateFont(&lf)
}

// close ends the dialog; with ok, the field values are read first. A
// displayed code is overwritten, so the control does not keep it.
func (ds *dialogState) close(ok bool) {
	if !ds.open {
		return
	}
	if ds.noCancel {
		ok = true
	}
	if ds.code != 0 {
		win32.OverwriteText(ds.code)
	}
	if ok {
		for i, e := range ds.edits {
			if ds.fields[i].masked {
				ds.values = append(ds.values, win32.ReadSecret(e))
			} else {
				ds.values = append(ds.values, []byte(win32.Text(e)))
			}
		}
	} else {
		// Clear masked fields so the controls do not keep a typed secret.
		for i, e := range ds.edits {
			if ds.fields[i].masked {
				cryptox.ZeroBytes(win32.ReadSecret(e))
			}
		}
	}
	ds.ok = ok
	ds.open = false
}

func dialogProc(hwnd win32.HWND, msg uint32, wparam, lparam uintptr) uintptr {
	ds := activeDialog
	if ds == nil || ds.hwnd != hwnd {
		return win32.DefWindowProc(hwnd, msg, wparam, lparam)
	}
	switch msg {
	case win32.WM_COMMAND:
		switch win32.LoWord(wparam) {
		case win32.IDOK:
			ds.close(true)
			return 0
		case win32.IDCANCEL:
			ds.close(false)
			return 0
		}
	case win32.WM_CLOSE:
		ds.close(false)
		return 0
	case win32.WM_CTLCOLORSTATIC:
		win32.SetBkModeTransparent(wparam)
		if win32.HWND(lparam) == ds.message && ds.message != 0 && ds.messageIsError {
			win32.SetTextColor(wparam, rgb(196, 30, 30))
		}
		return uintptr(win32.SysColorBrush(win32.COLOR_WINDOW))
	}
	return win32.DefWindowProc(hwnd, msg, wparam, lparam)
}

// closeModal cancels the open input dialog, if any (e.g. the operation was
// cancelled while it waited for an answer).
func (a *app) closeModal() {
	if ds := activeDialog; ds != nil {
		ds.close(false)
	}
}

// rgb returns a COLORREF.
func rgb(r, g, b byte) uint32 { return uint32(r) | uint32(g)<<8 | uint32(b)<<16 }

// taskDialog shows a task dialog owned by the main window and returns the
// chosen button and radio button (IDCANCEL when it was closed).
func (a *app) taskDialog(d win32.TaskDialog) (button, radio int32) {
	if d.Title == "" {
		d.Title = "RestoreSafe"
	}
	button, radio, err := d.Show(a.hwnd)
	if err != nil {
		return win32.IDCANCEL, 0
	}
	return button, radio
}
