// Package buildinfo holds the application version stamped by the build.
package buildinfo

// Version is the RestoreSafe application version. build.bat stamps it with
// -ldflags "-X RestoreSafe/internal/buildinfo.Version=<version>" from
// build/versioninfo.json, so any package can record it without threading the
// value through call signatures.
//
// It is written as the first line of a freshly created log file (see NewLogger)
// so the tool version that produced a backup can be identified later from the
// log that travels alongside the backup. Defaults to "dev" for un-stamped builds.
var Version = "dev"
