package job

import (
	"RestoreSafe/internal/format/catalog"
	"RestoreSafe/internal/format/naming"
	"RestoreSafe/internal/problem"
	"fmt"
)

// SelectSets returns the inventory entries of the requested backup sets, in
// request order. Every requested set must be in the inventory and complete:
// only complete sets are restore points.
func SelectSets(infos []catalog.SetInfo, requested []naming.BackupEntry) ([]catalog.SetInfo, error) {
	if len(requested) == 0 {
		return nil, problem.New("No backup chosen.").WithRemedy("Choose the backup to use.")
	}
	byEntry := make(map[naming.BackupEntry]catalog.SetInfo, len(infos))
	for _, info := range infos {
		byEntry[info.Entry] = info
	}
	out := make([]catalog.SetInfo, 0, len(requested))
	for _, entry := range requested {
		info, ok := byEntry[entry]
		if !ok {
			return nil, problem.Errorf("Backup %s is no longer in the backup directory.", entry.String()).WithRemedy("Check the backup directory, then choose the backup again.")
		}
		if !info.Complete() {
			return nil, fmt.Errorf("Backup %s cannot be used: %v", entry.String(), info.Err)
		}
		out = append(out, info)
	}
	return out, nil
}
