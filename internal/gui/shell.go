package gui

import (
	"RestoreSafe/internal/gui/flow"
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
	backups  *backupsPage
	settings *settingsPage
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
	if a.shell.backups, err = newBackupsPage(a); err != nil {
		return err
	}
	if a.shell.settings, err = newSettingsPage(a); err != nil {
		return err
	}
	a.refreshShell()
	return nil
}

// pagePanels returns the panels of the navigation's pages, in order.
func (a *app) pagePanels() []*widget.Panel {
	return []*widget.Panel{a.shell.overview.panel, a.shell.backups.panel, a.shell.settings.panel}
}

// showPage shows a page of the navigation.
func (a *app) showPage(page int) {
	a.page = page
	for i, p := range a.pagePanels() {
		p.Show(i == page)
	}
	a.shell.sidebar.Select(page)
	a.refreshInfo()
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
	a.shell.settings.update()
	a.refreshActivity()
	a.refreshInfo()
	a.layout()
}

// refreshInfo shows the right part of the status bar: the free space on
// the Overview, the runs and their size on Backups.
func (a *app) refreshInfo() {
	info := a.shell.overview.view.Status
	if a.page == view.PageBackups {
		info = a.shell.backups.view.Status
	}
	win32.SetText(a.shell.info, info)
}

// restyleShell applies the theme's fonts after a DPI change.
func (a *app) restyleShell() {
	a.shell.status.Restyle()
	win32.Invalidate(a.shell.sidebar.HWND())
	a.shell.overview.restyle()
	a.shell.backups.restyle()
	a.shell.backups.update()
	a.shell.settings.restyle()
}

// shortcut handles the keyboard shortcuts of spec 3.2; it reports whether
// it handled the key.
func (a *app) shortcut(vk uintptr) bool {
	if a.modal != 0 || a.plan != nil || a.wizard != nil {
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
			a.startOperation(opRequest{op: flow.OpBackup})
		}
	case view.ActionRestore:
		b := a.shell.backups
		if len(b.chosen()) > 0 {
			a.openWizard(b.selRun, b.selSet)
		}
	case view.ActionOpenRestored:
		if r := a.machine.Current(); r != nil && r.Restore != nil {
			a.open(r.Restore.Destination, false)
		}
	case view.ActionVerify:
		if sets := a.shell.backups.chosen(); len(sets) > 0 {
			a.verifyWhat = a.shell.backups.bar.What
			a.startOperation(opRequest{op: flow.OpVerify, sets: sets})
		}
	case view.ActionCheckAgain:
		a.startCheck()
	case view.ActionCheckDetails:
		if a.snapshot != nil {
			a.showDetails(a.hwnd, view.DetailsTitle, a.snapshot.Check.Report())
		}
	case view.ActionShowInBackups:
		a.showPage(view.PageBackups)
	case view.ActionOpenSettings:
		a.showPage(view.PageSettings)
	case view.ActionEditConfig:
		a.open(a.opts.ConfigPath, true)
	case view.ActionCancel:
		a.confirmCancel()
	case view.ActionShowLog:
		a.showRunLog()
	case view.ActionShowDetails:
		a.showResultDetails()
	case view.ActionDismiss:
		a.dismiss()
	case view.ActionReload:
		a.reload()
	case view.ActionOpenBackupDir:
		a.open(a.backupDir, false)
	}
}

// focusPage puts the keyboard focus on the shown page's first action.
func (a *app) focusPage() {
	switch a.page {
	case view.PageOverview:
		a.shell.overview.focus()
	case view.PageBackups:
		a.shell.backups.focus()
	default:
		win32.SetFocus(a.shell.sidebar.HWND())
	}
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

// paletteChanged follows Windows into or out of high contrast: the pages
// are built again in the new colors.
func (a *app) paletteChanged() {
	p := widget.CurrentPalette()
	if p == a.theme.Palette || a.shell.sidebar == nil {
		return
	}
	a.theme.Palette = p
	for _, h := range []win32.HWND{a.shell.sidebar.HWND(), a.shell.status.HWND(), a.shell.overview.panel.HWND(), a.shell.backups.panel.HWND(), a.shell.settings.panel.HWND()} {
		win32.DestroyWindow(h)
	}
	a.shell = shell{}
	if err := a.createShell(); err != nil {
		return
	}
	a.showPage(a.page)
	a.refreshShell()
	a.focusPage()
}
