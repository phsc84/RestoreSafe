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
	codeLineHeight   = 32
	dialogButton     = 96
)

// Control IDs of the credential dialogs.
const (
	idCredentialOK     = win32.IDOK
	idCredentialCancel = 470
	idCredentialLink   = 471
	idCredentialCopy   = 472
)

// credentialDialog is the open credential dialog (spec 9); the GUI shows
// at most one at a time.
type credentialDialog struct {
	win      *dialogWindow
	edits    []win32.HWND
	fields   []view.Field
	values   [][]byte
	code     win32.HWND
	codeFont windows.Handle
	noCancel bool
	ok, open bool
	// linked is set when the user chose the link.
	linked bool
}

var activeCredential *credentialDialog

// credentialAnswer is how the user closed a credential dialog.
type credentialAnswer struct {
	// values are the fields, read when ok; the caller zeroes masked ones.
	values [][]byte
	ok     bool
	// link is set when the user chose the dialog's link.
	link bool
}

// runCredentialDialog shows v modal to the main window until the user
// answers.
func (a *app) runCredentialDialog(v view.CredentialDialog) credentialAnswer {
	owner := a.dialogOwner()
	t := a.theme
	if dw := dialogWindows[owner]; dw != nil {
		t = dw.theme // the restore wizard, at its scale
	}
	win, err := newDialogWindow(t, owner, credentialClass, v.Title)
	if err != nil {
		return credentialAnswer{}
	}
	t = win.theme
	s := t.Scale
	pal := t.Palette
	d := &credentialDialog{win: win, fields: v.Fields, open: true, noCancel: v.Cancel == ""}
	activeCredential = d
	var copyButton win32.HWND
	win.onCommand = func(id uint16) {
		switch id {
		case idCredentialOK:
			d.close(true)
		case idCredentialCancel, win32.IDCANCEL:
			d.close(false)
		case idCredentialLink:
			d.linked = true
			d.close(false)
		case idCredentialCopy:
			// Into a password manager, say; out of the clipboard history.
			if win32.CopySecretText(win.hwnd, v.Copy) == nil {
				win32.SetText(copyButton, view.ButtonCopied)
			}
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
	if v.Code != "" {
		st.gap(12)
		font, err := codeFont(uint32(s))
		if err == nil {
			d.codeFont = font
		}
		// A static control: the code is copied only with the Copy button
		// (spec 13.3).
		h, _ := win32.CreateWindow(0, "STATIC", v.Code, win32.WS_CHILD|win32.WS_VISIBLE|win32.SS_NOPREFIX|win32.SS_CENTER, 0, 0, 0, 0, panel.HWND(), 0)
		panel.Adopt(h)
		if font != 0 {
			win32.SetFont(h, font)
		}
		d.code = h
		st.row(codeLineHeight, cell{hwnd: h, fill: true})
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
	if v.Link != "" {
		st.gap(10)
		w, _ := t.Fonts.Measure(v.Link, widget.TextSmall)
		st.row(stackLineHeight, cell{hwnd: panel.Link(v.Link, idCredentialLink), px: w + s.Px(linkPadding)})
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
	if v.Copy != "" {
		copyButton = panel.Button(view.ButtonCopy, idCredentialCopy)
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
	if copyButton != 0 {
		r := row.Rest()
		r.Right = r.Left + max(buttonWidth(t, copyButton), s.Px(dialogButton))
		win32.SetWindowPos(copyButton, r)
	}

	// A DPI change scales the controls in place: rebuilding would lose
	// what was typed.
	win.onDpi = func(old widget.Scale) { d.rescale(old, win.theme) }
	a.modal = win.hwnd
	win32.Enable(owner, false)
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
	win32.Enable(owner, true)
	win.destroy()
	win32.DeleteObject(d.codeFont)
	activeCredential = nil
	a.modal = 0
	return credentialAnswer{values: d.values, ok: d.ok, link: d.linked}
}

// codeFont creates the font for a displayed code at dpi: monospaced, bold,
// and one and a half times the message font's size, so a recovery code
// fits on one line. The caller deletes it.
func codeFont(dpi uint32) (windows.Handle, error) {
	lf, err := win32.MessageFont(dpi)
	if err != nil {
		return 0, err
	}
	lf.Height = lf.Height * 3 / 2
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
	if d.code != 0 {
		win32.OverwriteText(d.code)
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

// confirmInfo asks c in an information task dialog owned by the main
// window; Yes is the default. It reports whether the user chose Yes.
func (a *app) confirmInfo(c view.Confirm) bool {
	button, _ := a.taskDialog(a.dialogOwner(), win32.TaskDialog{
		Instruction: c.Instruction,
		Content:     c.Content,
		Icon:        win32.TD_INFORMATION_ICON,
		Buttons:     []win32.TaskButton{{ID: win32.IDCANCEL, Text: c.No}, {ID: win32.IDOK, Text: c.Yes}},
		Default:     win32.IDOK,
	})
	return button == win32.IDOK
}

// dialogOwner is the window questions and confirmations are modal to: the
// restore wizard while it is open, the main window otherwise.
func (a *app) dialogOwner() win32.HWND {
	if a.wizard != nil {
		return a.wizard.win.hwnd
	}
	return a.hwnd
}

// rescale moves and sizes the dialog's controls from the scale old to the
// theme's, and gives the edits and the code their fonts at it.
func (d *credentialDialog) rescale(old widget.Scale, t *widget.Theme) {
	ratio := func(v int32) int32 { return v * int32(t.Scale) / int32(old) }
	for _, c := range win32.ChildWindows(d.win.panel.HWND()) {
		r := win32.ChildRect(c)
		win32.SetWindowPos(c, win32.Rect{Left: ratio(r.Left), Top: ratio(r.Top), Right: ratio(r.Right), Bottom: ratio(r.Bottom)})
	}
	for _, e := range d.edits {
		win32.SetFont(e, t.Fonts.Get(widget.TextBody))
	}
	if d.code != 0 {
		if font, err := codeFont(uint32(t.Scale)); err == nil {
			win32.SetFont(d.code, font)
			win32.DeleteObject(d.codeFont)
			d.codeFont = font
		}
	}
}
