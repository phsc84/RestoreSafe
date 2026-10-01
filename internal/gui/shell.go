package gui

import (
	"RestoreSafe/internal/gui/view"
	"RestoreSafe/internal/gui/widget"
	"RestoreSafe/internal/gui/win32"
)

// shell is the new interface around the pages: the sidebar, the status bar
// and the pages (spec 3.2).
type shell struct {
	sidebar  *widget.Sidebar
	status   *widget.Panel
	activity win32.HWND
	info     win32.HWND
	overview *overviewPage
	backups  *backupsInterim
	settings *settingsInterim
}

// sidebarGlyphs are the icons of the pages, in the order of view.Navigation.
var sidebarGlyphs = []widget.Glyph{widget.GlyphHome, widget.GlyphHistory, widget.GlyphSettings}

func (a *app) createShell() error {
	t := a.theme
	var items []widget.SidebarItem
	for i, name := range view.Navigation() {
		items = append(items, widget.SidebarItem{Glyph: sidebarGlyphs[i], Text: name})
	}
	sidebar, err := widget.NewSidebar(t, a.hwnd, idSidebar, items)
	if err != nil {
		return err
	}
	sidebar.OnSelect = a.showPage
	a.shell.sidebar = sidebar

	status, err := widget.NewPanel(t, a.hwnd, idStatus, widget.PanelStyle{Back: t.Palette.SurfaceAlt})
	if err != nil {
		return err
	}
	a.shell.status = status
	a.shell.activity = status.Label(view.Activity(true), widget.TextCaption, t.Palette.TextSecondary)
	a.shell.info = status.RightLabel("", widget.TextCaption, t.Palette.TextSecondary)

	if a.shell.overview, err = newOverviewPage(a); err != nil {
		return err
	}
	if a.shell.backups, err = newBackupsInterim(a); err != nil {
		return err
	}
	if a.shell.settings, err = newSettingsInterim(a); err != nil {
		return err
	}
	a.refreshShell()
	return nil
}

// pagePanels returns the panels of the navigation's pages, in order.
func (a *app) pagePanels() []*widget.Panel {
	return []*widget.Panel{a.shell.overview.panel, a.shell.backups.panel, a.shell.settings.panel}
}

// showPage shows page: a page of the navigation, or the operation screen.
func (a *app) showPage(page int) {
	a.page = page
	shellShown := page != pageOperation
	setShown(a.shell.sidebar.HWND(), shellShown)
	a.shell.status.Show(shellShown)
	for i, p := range a.pagePanels() {
		p.Show(i == page)
	}
	if shellShown {
		a.shell.sidebar.Select(page)
		win32.SetText(a.hwnd, a.title())
	}
	a.applyOpVisibility()
	a.layout()
}

// layoutShell places the sidebar, the status bar and the shown page.
func (a *app) layoutShell(client win32.Rect) {
	area := widget.NewArea(a.theme.Scale, client)
	win32.SetWindowPos(a.shell.status.HWND(), area.Bottom(widget.StatusBarHeight))
	win32.SetWindowPos(a.shell.sidebar.HWND(), area.Left(widget.SidebarWidth))
	content := area.Rest()
	for _, p := range a.pagePanels() {
		win32.SetWindowPos(p.HWND(), content)
	}
	status := widget.NewArea(a.theme.Scale, win32.ClientRect(a.shell.status.HWND()))
	status.Inset(12, 0, 12, 0)
	halves := status.Columns(12, 1, 1)
	win32.SetWindowPos(a.shell.activity, halves[0].Rest())
	win32.SetWindowPos(a.shell.info, halves[1].Rest())
	a.shell.overview.layout()
	a.shell.backups.layout()
	a.shell.settings.layout()
}

// refreshShell shows the current snapshot on the pages and the status bar.
func (a *app) refreshShell() {
	if a.shell.overview == nil {
		return
	}
	a.shell.overview.update()
	a.shell.backups.update()
	win32.SetText(a.shell.activity, view.Activity(a.checking))
	win32.SetText(a.shell.info, a.shell.overview.view.Status)
	a.layout()
}

// restyleShell applies the theme's fonts after a DPI change.
func (a *app) restyleShell() {
	a.shell.status.Restyle()
	win32.Invalidate(a.shell.sidebar.HWND())
	a.shell.overview.restyle()
	a.shell.backups.panel.Restyle()
	a.shell.settings.panel.Restyle()
}

// shortcut handles the keyboard shortcuts of spec 3.2; it reports whether
// it handled the key.
func (a *app) shortcut(vk uintptr) bool {
	if a.page == pageOperation || a.modal != 0 {
		return false
	}
	ctrl := win32.KeyDown(win32.VK_CONTROL)
	switch {
	case ctrl && vk >= '1' && vk <= '3':
		a.showPage(int(vk - '1'))
	case ctrl && vk == 'B':
		a.do(view.ActionBackUp)
	case vk == win32.VK_F5:
		a.do(view.ActionCheckAgain)
	default:
		return false
	}
	return true
}

// do runs the action of a button or link.
func (a *app) do(action view.Action) {
	switch action {
	case view.ActionBackUp:
		if a.snapshot != nil && !a.snapshot.Check.BlocksBackup() {
			a.startOperation(opBackup)
		}
	case view.ActionRestore:
		a.startOperation(opRestore)
	case view.ActionVerify:
		a.startOperation(opVerify)
	case view.ActionCheckAgain:
		a.startCheck()
	case view.ActionCheckDetails:
		if a.snapshot != nil {
			a.showDetails(view.DetailsTitle, a.snapshot.Check.Report())
		}
	case view.ActionShowInBackups:
		a.showPage(view.PageBackups)
	case view.ActionOpenSettings:
		a.showPage(view.PageSettings)
	case view.ActionEditConfig:
		a.open(a.opts.ConfigPath, true)
	case view.ActionOpenBackupDir:
		a.open(a.backupDir, false)
	}
}

// focusPage puts the keyboard focus on the shown page's first action.
func (a *app) focusPage() {
	if a.page == view.PageOverview {
		a.shell.overview.focus()
		return
	}
	win32.SetFocus(a.shell.sidebar.HWND())
}

// actions maps the control IDs of a page's buttons and links to actions.
type actions map[uint16]view.Action

// button creates a button for b on p and records its action; primary makes
// it the accent-filled button.
func (m actions) button(p *widget.Panel, b view.Button, id uint16, primary bool) win32.HWND {
	var h win32.HWND
	if primary {
		h = p.PrimaryButton(b.Text, uintptr(id))
	} else {
		h = p.Button(b.Text, uintptr(id))
	}
	win32.Enable(h, b.Enabled)
	m[id] = b.Action
	return h
}

// link creates a link for b on p and records its action.
func (m actions) link(p *widget.Panel, b view.Button, id uint16) win32.HWND {
	h := p.Link(b.Text, uintptr(id))
	win32.Enable(h, b.Enabled)
	m[id] = b.Action
	return h
}
