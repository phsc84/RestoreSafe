package operation

import (
	"RestoreSafe/internal/util"
	"fmt"
)

// SourceValidationStatus is startup-health metadata for one configured source directory.
type SourceValidationStatus struct {
	Resolved string
	Warning  string
	Skip     bool
	Err      error
}

// InspectSourceDirectoriesForValidation resolves and validates configured source directories
// for startup diagnostics without depending on backup workflow internals.
func InspectSourceDirectoriesForValidation(sourceDirectories []string, exeDir string) []SourceValidationStatus {
	statuses := make([]SourceValidationStatus, 0, len(sourceDirectories))
	for _, src := range sourceDirectories {
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

