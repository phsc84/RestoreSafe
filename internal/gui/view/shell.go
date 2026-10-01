package view

import (
	"RestoreSafe/internal/workflow/health"
	"fmt"
)

// Pages of the main window, in the order of the navigation.
const (
	PageOverview = iota
	PageBackups
	PageSettings
)

// Navigation returns the names of the pages, in order.
func Navigation() []string { return []string{navOverview, navBackups, navSettings} }

// Title returns the window title: the version, and the configuration file
// when it is not the default config.yaml (spec 3.2).
func Title(version, configName string) string {
	if configName == "" || configName == "config.yaml" {
		return fmt.Sprintf(titleFormat, appName, version)
	}
	return fmt.Sprintf(titleWithConfig, appName, version, configName)
}

// Activity is the left part of the status bar.
func Activity(checking bool) string {
	if checking {
		return statusChecking
	}
	return statusReady
}

// BackupsInterim is the Backups page until the run list replaces it
// (plan phase 7): restore and verify through the first GUI's screens.
type BackupsInterim struct {
	Title, Line     string
	Restore, Verify Button
}

// BackupsInterimOf computes the interim Backups page.
func BackupsInterimOf(s *health.Snapshot) BackupsInterim {
	enabled := s != nil && len(s.Runs) > 0 && !s.Check.BlocksRestoreOrVerify()
	v := BackupsInterim{
		Title:   navBackups,
		Line:    backupsInterimLine,
		Restore: Button{Text: buttonRestoreOld, Action: ActionRestore, Enabled: enabled},
		Verify:  Button{Text: buttonVerifyOld, Action: ActionVerify, Enabled: enabled},
	}
	if s != nil && len(s.Runs) == 0 {
		v.Line = lastBackupNone
	}
	return v
}

// SettingsInterim is the Settings page until its cards replace it (plan
// phase 9): the configuration file and the backup directory.
type SettingsInterim struct {
	Title                  string
	ConfigLabel, Config    string
	BackupDirLabel, Folder string
	Edit, Open             Button
	Line                   string
}

// SettingsInterimOf computes the interim Settings page.
func SettingsInterimOf(configPath, backupDir string) SettingsInterim {
	return SettingsInterim{
		Title:          navSettings,
		ConfigLabel:    settingsConfigFile,
		Config:         configPath,
		BackupDirLabel: settingsBackupDir,
		Folder:         backupDir,
		Edit:           Button{Text: buttonEditConfigFile, Action: ActionEditConfig, Enabled: true},
		Open:           Button{Text: buttonOpenFolder, Action: ActionOpenBackupDir, Enabled: true},
		Line:           settingsRestartLine,
	}
}

// DetailsTitle is the title of the Check details dialog.
const DetailsTitle = detailsTitle
