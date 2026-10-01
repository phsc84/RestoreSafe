// Package flow runs operations for the user interface without Win32: the
// bridge between the worker goroutine that runs a workflow and the UI
// thread. It is plain Go, so its tests need no window.
package flow
