//go:build windows

package fsx

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// QueryFreeSpaceBytes returns available free bytes for the filesystem containing path.
func QueryFreeSpaceBytes(path string) (uint64, error) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, fmt.Errorf("Failed to encode path: %w", err)
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
