package view

import (
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
