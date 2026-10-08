package gui

import (
	"RestoreSafe/internal/gui/win32"

	"golang.org/x/sys/windows"
)

// The one way a window procedure finds its Go object (refactoring 2.0
// RF-34): every top-level window RestoreSafe creates (the main window, the
// dialog windows, the details viewer) is in handlers from right after
// CreateWindow until it is destroyed. Window procedures run on the UI
// thread only, so the map needs no lock. Messages that arrive while a
// window is created, before it is in the map, get the default handling.

// windowHandler handles the messages of one window.
type windowHandler interface {
	message(hwnd win32.HWND, msg uint32, wparam, lparam uintptr) uintptr
}

var (
	handlers = map[win32.HWND]windowHandler{}
	// classes are the window classes registered so far; procPtr is the
	// callback of windowProc, created once.
	classes = map[string]bool{}
	procPtr uintptr
)

// windowProc is the window procedure of every class RestoreSafe registers.
func windowProc(hwnd win32.HWND, msg uint32, wparam, lparam uintptr) uintptr {
	if h := handlers[hwnd]; h != nil {
		return h.message(hwnd, msg, wparam, lparam)
	}
	return win32.DefWindowProc(hwnd, msg, wparam, lparam)
}

// registerClass registers the window class wc once, with windowProc as its
// window procedure and the default instance, cursor and background unless
// wc sets them.
func registerClass(wc win32.WndClassEx, class string) error {
	if classes[class] {
		return nil
	}
	if procPtr == 0 {
		procPtr = windows.NewCallback(windowProc)
	}
	wc.WndProc = procPtr
	wc.ClassName = win32.UTF16(class)
	if wc.Instance == 0 {
		wc.Instance = win32.ModuleHandle()
	}
	if wc.Cursor == 0 {
		wc.Cursor = win32.ArrowCursor()
	}
	if wc.Background == 0 {
		wc.Background = win32.SysColorBrush(win32.COLOR_WINDOW)
	}
	if err := win32.RegisterClass(&wc); err != nil {
		return err
	}
	classes[class] = true
	return nil
}
