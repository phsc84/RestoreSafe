package operation

import (
	"RestoreSafe/internal/util"
	"fmt"
)

// SourceValidationStatus is startup-health metadata for one configured source folder.
type SourceValidationStatus struct {
	Resolved string
	Warning  string
	Skip     bool
	Err      error
}

// InspectSourceFoldersForValidation resolves and validates configured source folders
// for startup diagnostics without depending on backup workflow internals.
func InspectSourceFoldersForValidation(sourceFolders []string, exeDir string) []SourceValidationStatus {
	statuses := make([]SourceValidationStatus, 0, len(sourceFolders))
	for _, src := range sourceFolders {
		resolved := util.ResolveDir(src, exeDir)
		status := SourceValidationStatus{Resolved: resolved}

		status.Err = util.ValidateSourceDirectory(resolved)
		statuses = append(statuses, status)
	}

	markSourceValidationDuplicates(statuses)
	return statuses
}

func markSourceValidationDuplicates(statuses []SourceValidationStatus) {
	seenByPath := make(map[string]int)
	for i := range statuses {
		if statuses[i].Err != nil {
			continue
		}

		pathKey := util.NormalizePathKey(statuses[i].Resolved)
		if firstIndex, exists := seenByPath[pathKey]; exists {
			statuses[i].Skip = true
			statuses[i].Warning = fmt.Sprintf("identical duplicate of %s; this entry will be skipped", statuses[firstIndex].Resolved)
			continue
		}

		seenByPath[pathKey] = i
	}
}

