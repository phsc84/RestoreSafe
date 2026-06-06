package util

// AppVersion is the RestoreSafe application version. The build stamps it into
// cmd/main.go via -ldflags, and main copies it here at startup so any package
// can record it without threading the value through call signatures.
//
// It is written as the first line of a freshly created log file (see NewLogger)
// so the tool version that produced a backup can be identified later from the
// log that travels alongside the backup. Defaults to "dev" for un-stamped builds.
var AppVersion = "dev"
