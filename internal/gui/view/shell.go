package view

import (
	"fmt"
)

// Pages of the main window, in the order of the navigation.
const (
	PageCreate = iota
	PageRestore
	PageSettings
	// PageYubiKey is at the bottom of the sidebar, shown while YubiKeyVisible
	// (GUI spec YK-1).
	PageYubiKey
)

// Navigation returns the names of the pages, in order.
func Navigation() []string { return []string{navCreate, navRestore, navSettings, navYubiKey} }

// Title returns the window title: the version, and the configuration file
// when it is not the default config.yaml (GUI spec 3.2).
func Title(version, configName string) string {
	if configName == "" || configName == "config.yaml" {
		return fmt.Sprintf(titleFormat, appName, version)
	}
	return fmt.Sprintf(titleWithConfig, appName, version, configName)
}
