package operation

import (
	"RestoreSafe/internal/util"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

		info, err := os.Stat(resolved)
		if err != nil {
			status.Err = fmt.Errorf("Not found or inaccessible: %w. Remedy: Check the path in config.yaml and use forward slashes on Windows (e.g. C:/Users/Name/Documents).", err)
			statuses = append(statuses, status)
			continue
		}
		if !info.IsDir() {
			status.Err = fmt.Errorf("Path is not a directory. Remedy: Provide a folder path, not a file path.")
			statuses = append(statuses, status)
			continue
		}
		if _, err := os.ReadDir(resolved); err != nil {
			status.Err = fmt.Errorf("Directory not readable: %w. Remedy: Check permissions and ensure this user can read the folder.", err)
		}

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

		pathKey := normalizeSourceValidationPath(statuses[i].Resolved)
		if firstIndex, exists := seenByPath[pathKey]; exists {
			statuses[i].Skip = true
			statuses[i].Warning = fmt.Sprintf("identical duplicate of %s; this entry will be skipped", statuses[firstIndex].Resolved)
			continue
		}

		seenByPath[pathKey] = i
	}
}

func normalizeSourceValidationPath(path string) string {
	cleaned := filepath.Clean(path)
	cleaned = strings.ReplaceAll(cleaned, "/", "\\")
	return strings.ToLower(cleaned)
}
