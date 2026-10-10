//go:build windows

package fsx

import (
	"fmt"

	"github.com/phsc84/restoresafe/internal/problem"

	"golang.org/x/sys/windows"
)

// QueryFreeSpaceBytes returns available free bytes for the filesystem containing path.
func QueryFreeSpaceBytes(path string) (uint64, error) {
	free, _, err := QueryDiskSpace(path)
	return free, err
}

// QueryDiskSpace returns the free bytes available to this user and the total
// size of the filesystem containing path.
func QueryDiskSpace(path string) (free, total uint64, err error) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, fmt.Errorf("Failed to encode path: %w", err)
	}

	var totalNumberOfFreeBytes uint64
	err = windows.GetDiskFreeSpaceEx(pathPtr, &free, &total, &totalNumberOfFreeBytes)
	if err != nil {
		return 0, 0, problem.Errorf("Failed to query free space for %q: %w.", path, err).WithRemedy("Check drive availability and access rights.")
	}
	return free, total, nil
}
