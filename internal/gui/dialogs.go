package gui

import (
	"RestoreSafe/internal/gui/view"
	"RestoreSafe/internal/gui/widget"
	"RestoreSafe/internal/gui/win32"
	"RestoreSafe/internal/security/cryptox"

	"golang.org/x/sys/windows"
)

// credentialClass is the window class of the credential dialogs; the GUI
// tests find them by it.
const credentialClass = "RestoreSafeInputDialog"

// Sizes of the credential dialogs, in DIPs.
const (
	credentialWidth  = 440
	credentialMargin = 18
	fieldLabelHeight = 20
	codeLineHeight   = 40
	dialogButton     = 96
)

// Control IDs of the credential dialogs.
const (
	idCredentialOK     = win32.IDOK
	idCredentialCancel = 470
)

// credentialDialog is the open credential dialog (spec 9); the GUI shows
// at most one at a time.
type credentialDialog struct {
	win      *dialogWindow
	edits    []win32.HWND
	fields   []view.Field
	values   [][]byte
	code     []win32.HWND
	codeFont windows.Handle
	noCancel bool
	ok, open bool
}

var activeCredential *credentialDialog

// runCredentialDialog shows v modal to the main window and returns the
// field values, or ok=false when it was cancelled. The caller zeroes
// masked values after use.
func (a *app) runCredentialDialog(v view.CredentialDialog) (values [][]byte, ok bool) {
	t := a.theme
	s := t.Scale
	pal := t.Palette
	win, err := newDialogWindow(t, a.hwnd, credentialClass, v.Title)
	if err != nil {
		return nil, false
	}
	d := &credentialDialog{win: win, fields: v.Fields, open: true, noCancel: v.Cancel == ""}
	activeCredential = d
	win.onCommand = func(id uint16) {
		switch id {
		case idCredentialOK:
			d.close(true)
		case idCredentialCancel, win32.IDCANCEL:
			d.close(false)
		}
	}
	win.defID = func() uint16 { return idCredentialOK }

	panel := win.panel
	st := newStack(t, panel, s.Px(credentialWidth-2*credentialMargin))
	st.para(v.Intro, widget.TextBody, pal.Text, view.GlyphNone)
	if v.Hint != "" {
		st.gap(4)
		st.para(v.Hint, widget.TextSmall, pal.TextSecondary, view.GlyphNone)
	}
	if len(v.CodeLines) > 0 {
		st.gap(12)
		font, err := a.codeFont()
		if err == nil {
			d.codeFont = font
		}
		for _, line := range v.CodeLines {
			// A static control: the code can be neither selected nor copied
			// (spec 13.3).
			h, _ := win32.CreateWindow(0, "STATIC", line, win32.WS_CHILD|win32.WS_VISIBLE|win32.SS_NOPREFIX|win32.SS_CENTER, 0, 0, 0, 0, panel.HWND(), 0)
			panel.Adopt(h)
			if font != 0 {
				win32.SetFont(h, font)
			}
			d.code = append(d.code, h)
			st.row(codeLineHeight, cell{hwnd: h, fill: true})
		}
		st.gap(8)
	}
	for _, f := range v.Fields {
		st.gap(10)
		st.row(fieldLabelHeight, cell{hwnd: panel.Label(f.Label, widget.TextSmall, pal.Text), fill: true})
		style := uint32(win32.WS_TABSTOP | win32.WS_BORDER | win32.ES_AUTOHSCROLL)
		if f.Masked {
			style |= win32.ES_PASSWORD
		}
		e, _ := win32.CreateWindow(0, "EDIT", "", win32.WS_CHILD|win32.WS_VISIBLE|style, 0, 0, 0, 0, panel.HWND(), 0)
		panel.Adopt(e)
		win32.SetFont(e, t.Fonts.Get(widget.TextBody))
		win32.SetAccessibleName(e, f.Label)
		d.edits = append(d.edits, e)
		st.row(widget.EditHeight, cell{hwnd: e, fill: true})
	}
	if v.Error != "" {
		st.gap(8)
		st.para(v.Error, widget.TextSmall, pal.Error, view.GlyphError)
	}
	if v.Note != "" {
		st.gap(10)
		st.para(v.Note, widget.TextSmall, pal.TextSecondary, view.GlyphNone)
	}

	okButton := panel.PrimaryButton(v.OK, idCredentialOK)
	var cancel win32.HWND
	if v.Cancel != "" {
		cancel = panel.Button(v.Cancel, idCredentialCancel)
	}
	margin := s.Px(credentialMargin)
	buttonsH := s.Px(widget.ButtonHeight)
	w := s.Px(credentialWidth)
	h := margin + st.height() + 2*margin + buttonsH
	win.resize(w, h, true)
	st.place(margin, margin)
	row := widget.NewArea(s, win32.Rect{Left: margin, Top: h - margin - buttonsH, Right: w - margin, Bottom: h - margin})
	if cancel != 0 {
		win32.SetWindowPos(cancel, row.RightPx(max(buttonWidth(t, cancel), s.Px(dialogButton))))
		row.Right(8)
	}
	win32.SetWindowPos(okButton, row.RightPx(max(buttonWidth(t, okButton), s.Px(dialogButton))))

	a.modal = win.hwnd
	win32.Enable(a.hwnd, false)
	win32.ShowWindow(win.hwnd, win32.SW_SHOWNORMAL)
	if len(d.edits) > 0 {
		win32.SetFocus(d.edits[0])
	} else {
		win32.SetFocus(okButton)
	}

	var msg win32.Msg
	for d.open {
		more, err := win32.GetMessage(&msg)
		if err != nil || !more {
			// WM_QUIT: end the dialog and let the main loop see it.
			win32.PostQuitMessage(int32(msg.WParam))
			d.close(false)
			break
		}
		if !win32.IsDialogMessage(win.hwnd, &msg) {
			win32.TranslateAndDispatch(&msg)
		}
	}
	// Enable the owner before the dialog disappears, so the activation
	// returns to it and not to another application.
	win32.Enable(a.hwnd, true)
	win.destroy()
	win32.DeleteObject(d.codeFont)
	activeCredential = nil
	a.modal = 0
	return d.values, d.ok
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
// displayed code is overwritten, so the controls do not keep it.
func (d *credentialDialog) close(ok bool) {
	if !d.open {
		return
	}
	if d.noCancel {
		ok = true
	}
	for _, c := range d.code {
		win32.OverwriteText(c)
	}
	if ok {
		for i, e := range d.edits {
			if d.fields[i].Masked {
				d.values = append(d.values, win32.ReadSecret(e))
			} else {
				d.values = append(d.values, []byte(win32.Text(e)))
			}
		}
	} else {
		// Clear masked fields so the controls do not keep a typed secret.
		for i, e := range d.edits {
			if d.fields[i].Masked {
				cryptox.ZeroBytes(win32.ReadSecret(e))
			}
		}
	}
	d.ok = ok
	d.open = false
}

// closeModal cancels the open credential dialog, if any (e.g. the
// operation was cancelled while it waited for an answer).
func (a *app) closeModal() {
	if d := activeCredential; d != nil {
		d.close(false)
	}
}

// rgb returns a COLORREF.
func rgb(r, g, b byte) uint32 { return uint32(r) | uint32(g)<<8 | uint32(b)<<16 }

// taskDialog shows a task dialog owned by owner and returns the chosen
// button and radio button (IDCANCEL when it was closed).
func (a *app) taskDialog(owner win32.HWND, d win32.TaskDialog) (button, radio int32) {
	if d.Title == "" {
		d.Title = "RestoreSafe"
	}
	button, radio, err := d.Show(owner)
	if err != nil {
		return win32.IDCANCEL, 0
	}
	return button, radio
}

// confirm asks c in a warning task dialog owned by owner; No is the
// default. It reports whether the user chose Yes.
func (a *app) confirm(owner win32.HWND, c view.Confirm) bool {
	button, _ := a.taskDialog(owner, win32.TaskDialog{
		Instruction: c.Instruction,
		Content:     c.Content,
		Icon:        win32.TD_WARNING_ICON,
		Buttons:     []win32.TaskButton{{ID: win32.IDOK, Text: c.Yes}, {ID: win32.IDCANCEL, Text: c.No}},
		Default:     win32.IDCANCEL,
	})
	return button == win32.IDOK
}
