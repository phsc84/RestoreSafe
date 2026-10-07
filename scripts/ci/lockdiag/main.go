// Command lockdiag is a temporary CI diagnostic: can a file held with share
// mode 0 still be opened on the runner, and by which kind of open?
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

func main() {
	path := filepath.Join(os.TempDir(), "lockdiag.bin")
	os.WriteFile(path, []byte("data"), 0o600)
	p, _ := windows.UTF16PtrFromString(path)
	h, err := windows.CreateFile(p, windows.GENERIC_READ, 0, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	fmt.Printf("lock: %v\n", err)
	defer windows.CloseHandle(h)

	f, err := os.Open(path)
	fmt.Printf("os.Open: %v\n", err)
	if err == nil {
		buf := make([]byte, 4)
		n, rerr := f.Read(buf)
		fmt.Printf("os.Open read: %d %v\n", n, rerr)
		f.Close()
	}
	for _, c := range []struct {
		name  string
		flags uint32
	}{{"plain", windows.FILE_ATTRIBUTE_NORMAL}, {"backup", windows.FILE_FLAG_BACKUP_SEMANTICS}} {
		h2, err := windows.CreateFile(p, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, c.flags, 0)
		fmt.Printf("CreateFile %s: %v\n", c.name, err)
		if err == nil {
			windows.CloseHandle(h2)
		}
	}
	var vol [64]uint16
	var fsName [64]uint16
	root, _ := windows.UTF16PtrFromString(filepath.VolumeName(path) + `\`)
	err = windows.GetVolumeInformation(root, &vol[0], 64, nil, nil, nil, &fsName[0], 64)
	fmt.Printf("temp %s on %s (%v)\n", path, windows.UTF16ToString(fsName[:]), err)
}
