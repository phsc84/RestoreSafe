package gui

import (
	"RestoreSafe/internal/gui/view"
	"RestoreSafe/internal/gui/widget"
	"RestoreSafe/internal/gui/win32"
	"path/filepath"
)

// The Settings page until its final version replaces it (plan phase 9).

// Control IDs of the interim page.
const (
	idSettingsEdit = 501 + iota
	idSettingsOpen
)

// Sizes of the interim page, in DIPs.
const (
	titleHeight        = 30
	interimLabelWidth  = 140
	interimActionWidth = 160
	interimLines       = 24
)

// settingsInterim shows the configuration file and the backup directory.
type settingsInterim struct {
	a                      *app
	panel                  *widget.Panel
	acts                   actions
	title                  win32.HWND
	configLabel, config    win32.HWND
	backupLabel, backupDir win32.HWND
	edit, open, line       win32.HWND
}

func newSettingsInterim(a *app) (*settingsInterim, error) {
	t := a.theme
	panel, err := widget.NewPanel(t, a.hwnd, idPage+view.PageSettings, widget.PanelStyle{Back: t.Palette.Surface})
	if err != nil {
		return nil, err
	}
	v := view.SettingsInterimOf(filepath.Clean(a.opts.ConfigPath), filepath.Clean(a.backupDir))
	s := &settingsInterim{a: a, panel: panel, acts: actions{}}
	s.title = panel.Label(v.Title, widget.TextTitle, t.Palette.Text)
	s.configLabel = panel.Label(v.ConfigLabel, widget.TextStrong, t.Palette.Text)
	s.config = panel.Label(v.Config, widget.TextBody, t.Palette.Text)
	s.edit = s.acts.button(panel, v.Edit, idSettingsEdit, false)
	s.backupLabel = panel.Label(v.BackupDirLabel, widget.TextStrong, t.Palette.Text)
	s.backupDir = panel.Label(v.Folder, widget.TextBody, t.Palette.Text)
	s.open = s.acts.button(panel, v.Open, idSettingsOpen, false)
	s.line = panel.Label(v.Line, widget.TextSmall, t.Palette.TextSecondary)
	panel.OnCommand = func(id, code uint16) {
		if action, ok := s.acts[id]; ok && code == win32.BN_CLICKED {
			a.do(action)
		}
	}
	return s, nil
}

func (s *settingsInterim) layout() {
	sc := s.a.theme.Scale
	area := widget.NewArea(sc, win32.ClientRect(s.panel.HWND()))
	area.Inset(widget.ContentPaddingX, widget.ContentPaddingY, widget.ContentPaddingX, widget.ContentPaddingY)
	win32.SetWindowPos(s.title, area.Top(titleHeight))
	for _, row := range [][3]win32.HWND{{s.configLabel, s.config, s.edit}, {s.backupLabel, s.backupDir, s.open}} {
		area.Top(widget.CardGap)
		r := widget.NewArea(sc, area.Top(widget.ButtonHeight))
		win32.SetWindowPos(row[0], r.Left(interimLabelWidth))
		win32.SetWindowPos(row[2], r.Right(interimActionWidth))
		r.Right(8)
		win32.SetWindowPos(row[1], r.Rest())
	}
	area.Top(widget.CardGap)
	win32.SetWindowPos(s.line, area.Top(interimLines))
}
