// Package view computes what the user interface shows, as plain Go without
// Win32: one function per page and dialog turns the snapshot, the plans and
// the state of an operation into a view (texts, icons, enabled states and
// actions). Every user-visible string is in strings.go and every number and
// date is formatted by format.go (GUI spec 3.6). The Win32 pages only render
// views, so what the user sees is tested here.
package view
