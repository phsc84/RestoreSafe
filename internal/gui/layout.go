package gui

import (
	"RestoreSafe/internal/gui/widget"
	"RestoreSafe/internal/gui/win32"
)

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

func layoutHome(s widget.Scale, width, height int32) homeLayout {
	// Work in DIPs, then scale; the client size arrives in pixels.
	w := width * 96 / int32(s)
	h := height * 96 / int32(s)
	var l homeLayout

	pathX := int32(margin + labelWidth + gap)
	openX := w - margin - smallButtonWidth
	pathW := max(openX-gap-pathX, 0)
	row := func(y int32) (label, path, open win32.Rect) {
		return s.Rect(margin, y, labelWidth, rowHeight),
			s.Rect(pathX, y, pathW, rowHeight),
			s.Rect(openX, y-1, smallButtonWidth, rowHeight+2)
	}
	l.configLabel, l.configPath, l.configOpen = row(margin)
	l.backupLabel, l.backupPath, l.backupOpen = row(margin + rowHeight + gap)

	buttonsY := h - margin - buttonHeight
	blockedY := buttonsY - gap - noteHeight
	reportY := int32(margin + 2*(rowHeight+gap) + gap)
	l.report = s.Rect(margin, reportY, w-2*margin, max(blockedY-gap-reportY, 0))
	l.blocked = s.Rect(margin, blockedY, w-2*margin, noteHeight)

	l.backup = s.Rect(margin, buttonsY, actionWidth, buttonHeight)
	l.restore = s.Rect(margin+actionWidth+gap, buttonsY, actionWidth, buttonHeight)
	l.verify = s.Rect(margin+2*(actionWidth+gap), buttonsY, actionWidth, buttonHeight)
	l.recheck = s.Rect(w-margin-smallButtonWidth-20, buttonsY, smallButtonWidth+20, buttonHeight)
	return l
}

// Sizes of the operation screen, in DIPs.
const (
	opTitleHeight    = 28
	opDetailHeight   = 20
	opProgressHeight = 16
	opButtonWidth    = 180
	opButtons        = 4
	browseWidth      = 110
	checkHeight      = 24
	destNoteHeight   = 40
)

// opContent is what the content area of the operation screen shows.
type opContent int

const (
	contentLog          opContent = iota // running and result screens
	contentReport                        // preflight
	contentReportAndLog                  // result after a blocking preflight
	contentTree                          // backup selection
	contentDestination                   // restore destination
)

// operationLayout places the operation screen's controls; rectangles of
// controls the content mode does not show are empty.
type operationLayout struct {
	title, detail, progress win32.Rect
	report, log, tree       win32.Rect
	destLabel, destEdit     win32.Rect
	destBrowse, destCheck   win32.Rect
	destNote                win32.Rect
	buttons                 [opButtons]win32.Rect
}

// layoutOperation lays out the operation screen: heading, detail line,
// progress bar (when shown), the content area, and a row of buttons. In the
// content area, the report and the log each alone take all of it; together
// the report gets two thirds.
func layoutOperation(s widget.Scale, width, height int32, content opContent, showProgress bool) operationLayout {
	w := width * 96 / int32(s)
	h := height * 96 / int32(s)
	var l operationLayout
	y := int32(margin)
	l.title = s.Rect(margin, y, w-2*margin, opTitleHeight)
	y += opTitleHeight
	l.detail = s.Rect(margin, y, w-2*margin, opDetailHeight)
	y += opDetailHeight + gap
	if showProgress {
		l.progress = s.Rect(margin, y, w-2*margin, opProgressHeight)
		y += opProgressHeight + gap
	}
	buttonsY := h - margin - buttonHeight
	area := max(buttonsY-gap-y, 0)
	full := s.Rect(margin, y, w-2*margin, area)

	switch content {
	case contentLog:
		l.log = full
	case contentReport:
		l.report = full
	case contentReportAndLog:
		reportH := area * 2 / 3
		l.report = s.Rect(margin, y, w-2*margin, reportH)
		l.log = s.Rect(margin, y+reportH+gap, w-2*margin, max(area-reportH-gap, 0))
	case contentTree:
		l.tree = full
	case contentDestination:
		l.destLabel = s.Rect(margin, y, w-2*margin, fieldLabel)
		y += fieldLabel
		l.destEdit = s.Rect(margin, y, max(w-2*margin-gap-browseWidth, 0), fieldHeight)
		l.destBrowse = s.Rect(w-margin-browseWidth, y, browseWidth, fieldHeight)
		y += fieldHeight + gap
		l.destCheck = s.Rect(margin, y, w-2*margin, checkHeight)
		y += checkHeight + gap
		l.destNote = s.Rect(margin, y, w-2*margin, destNoteHeight)
	}

	// Buttons are narrower when the window is too small for all of them.
	bw := min(int32(opButtonWidth), (w-2*margin-(opButtons-1)*gap)/opButtons)
	for i := range l.buttons {
		l.buttons[i] = s.Rect(margin+int32(i)*(bw+gap), buttonsY, bw, buttonHeight)
	}
	return l
}
