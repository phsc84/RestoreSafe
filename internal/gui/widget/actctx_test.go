package widget

import (
	"path/filepath"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// actCtx is an ACTCTXW.
type actCtx struct {
	size              uint32
	flags             uint32
	source            *uint16
	processorArch     uint16
	langID            uint16
	assemblyDirectory *uint16
	resourceName      *uint16
	applicationName   *uint16
	module            windows.Handle
}

var (
	kernel32             = windows.NewLazySystemDLL("kernel32.dll")
	procCreateActCtxW    = kernel32.NewProc("CreateActCtxW")
	procActivateActCtx   = kernel32.NewProc("ActivateActCtx")
	procDeactivateActCtx = kernel32.NewProc("DeactivateActCtx")
	procReleaseActCtx    = kernel32.NewProc("ReleaseActCtx")
)

// useCommonControls6 makes the calling OS thread use common controls 6 (and
// with it SysLink) until the test ends, as the application manifest does
// for RestoreSafe.exe: test binaries have no manifest. The caller must have
// locked the goroutine to its thread.
func useCommonControls6(t *testing.T) {
	t.Helper()
	manifest, err := filepath.Abs(filepath.Join("..", "..", "..", "build", "windows", "RestoreSafe.manifest"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := actCtx{source: windows.StringToUTF16Ptr(manifest)}
	ctx.size = uint32(unsafe.Sizeof(ctx))
	h, _, err := procCreateActCtxW.Call(uintptr(unsafe.Pointer(&ctx)))
	if windows.Handle(h) == windows.InvalidHandle {
		t.Fatalf("CreateActCtxW(%s): %v", manifest, err)
	}
	var cookie uintptr
	if r, _, err := procActivateActCtx.Call(h, uintptr(unsafe.Pointer(&cookie))); r == 0 {
		t.Fatalf("ActivateActCtx: %v", err)
	}
	t.Cleanup(func() {
		procDeactivateActCtx.Call(0, cookie)
		procReleaseActCtx.Call(h)
	})
}
