package gui

import "RestoreSafe/internal/win32"

// scale converts device-independent pixels (1/96 inch) to pixels at dpi.
type scale uint32

// px returns dip scaled to the DPI, rounded to the nearest pixel.
func (s scale) px(dip int32) int32 {
	return (dip*int32(s) + 48) / 96
}

// rect returns a rectangle from DIP coordinates.
func (s scale) rect(x, y, w, h int32) win32.Rect {
	return win32.Rect{Left: s.px(x), Top: s.px(y), Right: s.px(x + w), Bottom: s.px(y + h)}
}

// Sizes of the main window and its controls, in DIPs.
const (
	windowMinWidth   = 720
	windowMinHeight  = 520
	windowWidth      = 900
	windowHeight     = 640
	margin           = 16
	gap              = 8
	rowHeight        = 24
	buttonHeight     = 30
	labelWidth       = 100
	smallButtonWidth = 90
	actionWidth      = 150
	noteHeight       = 20
	reportPadding    = 8
)

// homeLayout places the home screen's controls in a client area of the
// given size in pixels.
type homeLayout struct {
	configLabel, configPath, configOpen win32.Rect
	backupLabel, backupPath, backupOpen win32.Rect
	report                              win32.Rect
	blocked                             win32.Rect
	backup, restore, verify, recheck    win32.Rect
}

func layoutHome(s scale, width, height int32) homeLayout {
	// Work in DIPs, then scale; the client size arrives in pixels.
	w := width * 96 / int32(s)
	h := height * 96 / int32(s)
	var l homeLayout

	pathX := int32(margin + labelWidth + gap)
	openX := w - margin - smallButtonWidth
	pathW := max(openX-gap-pathX, 0)
	row := func(y int32) (label, path, open win32.Rect) {
		return s.rect(margin, y, labelWidth, rowHeight),
			s.rect(pathX, y, pathW, rowHeight),
			s.rect(openX, y-1, smallButtonWidth, rowHeight+2)
	}
	l.configLabel, l.configPath, l.configOpen = row(margin)
	l.backupLabel, l.backupPath, l.backupOpen = row(margin + rowHeight + gap)

	buttonsY := h - margin - buttonHeight
	blockedY := buttonsY - gap - noteHeight
	reportY := int32(margin + 2*(rowHeight+gap) + gap)
	l.report = s.rect(margin, reportY, w-2*margin, max(blockedY-gap-reportY, 0))
	l.blocked = s.rect(margin, blockedY, w-2*margin, noteHeight)

	l.backup = s.rect(margin, buttonsY, actionWidth, buttonHeight)
	l.restore = s.rect(margin+actionWidth+gap, buttonsY, actionWidth, buttonHeight)
	l.verify = s.rect(margin+2*(actionWidth+gap), buttonsY, actionWidth, buttonHeight)
	l.recheck = s.rect(w-margin-smallButtonWidth-20, buttonsY, smallButtonWidth+20, buttonHeight)
	return l
}

// Sizes of the operation screen, in DIPs.
const (
	opTitleHeight    = 28
	opDetailHeight   = 20
	opProgressHeight = 16
	opButtonWidth    = 180
	opButtons        = 4
)

// operationLayout places the operation screen's controls.
type operationLayout struct {
	title, detail, progress win32.Rect
	report, log             win32.Rect
	buttons                 [opButtons]win32.Rect
}

// layoutOperation lays out the operation screen: heading, detail line,
// progress bar (when shown), the report and the log (each alone takes the
// whole content area; together the report gets two thirds), and a row of
// buttons.
func layoutOperation(s scale, width, height int32, showReport, showLog, showProgress bool) operationLayout {
	w := width * 96 / int32(s)
	h := height * 96 / int32(s)
	var l operationLayout
	y := int32(margin)
	l.title = s.rect(margin, y, w-2*margin, opTitleHeight)
	y += opTitleHeight
	l.detail = s.rect(margin, y, w-2*margin, opDetailHeight)
	y += opDetailHeight + gap
	if showProgress {
		l.progress = s.rect(margin, y, w-2*margin, opProgressHeight)
		y += opProgressHeight + gap
	}
	buttonsY := h - margin - buttonHeight
	content := max(buttonsY-gap-y, 0)
	if showReport {
		reportH := content
		if showLog {
			reportH = content * 2 / 3
		}
		l.report = s.rect(margin, y, w-2*margin, reportH)
		y += reportH + gap
		content = max(content-reportH-gap, 0)
	}
	l.log = s.rect(margin, y, w-2*margin, content)
	// Buttons are narrower when the window is too small for all of them.
	bw := min(int32(opButtonWidth), (w-2*margin-(opButtons-1)*gap)/opButtons)
	for i := range l.buttons {
		l.buttons[i] = s.rect(margin+int32(i)*(bw+gap), buttonsY, bw, buttonHeight)
	}
	return l
}
