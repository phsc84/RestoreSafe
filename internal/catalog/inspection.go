package catalog

import (
	"RestoreSafe/internal/util"
	"fmt"
	"os"
	"sort"
)

// InspectBackupParts validates split-part continuity and returns part count and total size.
func InspectBackupParts(targetDir string, entry util.BackupEntry) (int, int64, error) {
	entries, err := os.ReadDir(targetDir)
	if err != nil {
		return 0, 0, err
	}

	type partInfo struct {
		seq  int
		size int64
	}

	parts := make([]partInfo, 0)
	for _, dirEntry := range entries {
		parsedEntry, seq, ok := util.ParsePartFileName(dirEntry.Name())
		if !ok {
			continue
		}
		if parsedEntry != entry {
			continue
		}

		info, err := dirEntry.Info()
		if err != nil {
			return len(parts), 0, fmt.Errorf("Failed to inspect part file %q: %w. Remedy: Check file/folder permissions.", dirEntry.Name(), err)
		}
		parts = append(parts, partInfo{seq: seq, size: info.Size()})
	}

	if len(parts) == 0 {
		return 0, 0, fmt.Errorf("No part files found. Remedy: Ensure the .enc files are present in target_folder.")
	}

	sort.Slice(parts, func(i, j int) bool {
		return parts[i].seq < parts[j].seq
	})

	var totalSize int64
	for i, part := range parts {
		totalSize += part.size
		expectedSeq := i + 1
		if part.seq != expectedSeq {
			return len(parts), totalSize, fmt.Errorf("Missing part file %03d. Remedy: Restore the missing .enc part or create a new backup.", expectedSeq)
		}
	}

	return len(parts), totalSize, nil
}
