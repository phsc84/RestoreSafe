//go:build windows

package util

import (
	"fmt"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// QueryFreeSpaceBytes returns available free bytes for the filesystem containing path.
func QueryFreeSpaceBytes(path string) (uint64, error) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, fmt.Errorf("Failed to encode path: %w. Remedy: Check the path format and use a valid Windows path.", err)
	}

	var freeBytesAvailable uint64
	var totalNumberOfBytes uint64
	var totalNumberOfFreeBytes uint64

	err = windows.GetDiskFreeSpaceEx(pathPtr, &freeBytesAvailable, &totalNumberOfBytes, &totalNumberOfFreeBytes)
	if err != nil {
		return 0, fmt.Errorf("Failed to query free space for %q: %w. Remedy: Check drive availability and access rights.", path, err)
	}

	return freeBytesAvailable, nil
}

// IsNetworkVolume reports whether path is located on a network-backed volume.
// It recognizes UNC shares and mapped network drives.
func IsNetworkVolume(path string) bool {
	volume := filepath.VolumeName(filepath.Clean(path))
	if volume == "" {
		return false
	}

	if strings.HasPrefix(volume, `\\`) || strings.HasPrefix(volume, "//") {
		return true
	}

	root := volume
	if strings.HasSuffix(root, ":") {
		root += `\`
	}

	rootPtr, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return false
	}

	return windows.GetDriveType(rootPtr) == windows.DRIVE_REMOTE
}
