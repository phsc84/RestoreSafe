package backup

import (
	"RestoreSafe/internal/util"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type backupSourcePlan struct {
	Resolved       string
	normalizedPath string // cached result of normalizedSourcePathKey(Resolved)
	BackupName     string
	Warning        string
	Skip           bool
	Err            error
}

func planBackupSources(sourceFolders []string, exeDir string) []backupSourcePlan {
	result := make([]backupSourcePlan, 0, len(sourceFolders))
	for _, src := range sourceFolders {
		resolved := util.ResolveDir(src, exeDir)
		status := backupSourcePlan{Resolved: resolved, normalizedPath: normalizedSourcePathKey(resolved)}

		info, err := os.Stat(resolved)
		if err != nil {
			status.Err = fmt.Errorf("Not found or inaccessible: %w. Remedy: Check the path in config.yaml and use forward slashes on Windows (e.g. C:/Users/Name/Documents).", err)
			result = append(result, status)
			continue
		}
		if !info.IsDir() {
			status.Err = fmt.Errorf("Path is not a directory. Remedy: Provide a folder path, not a file path.")
			result = append(result, status)
			continue
		}
		if _, err := os.ReadDir(resolved); err != nil {
			status.Err = fmt.Errorf("Directory not readable: %w. Remedy: Check permissions and ensure this user can read the folder.", err)
		}

		result = append(result, status)
	}
	markIdenticalSourceDuplicates(result)
	assignSourceBackupNames(result)
	return result
}

func markIdenticalSourceDuplicates(sources []backupSourcePlan) {
	seenByPath := make(map[string]int)
	for i := range sources {
		if sources[i].Err != nil {
			continue
		}

		pathKey := sources[i].normalizedPath
		if firstIndex, exists := seenByPath[pathKey]; exists {
			sources[i].Skip = true
			sources[i].Warning = fmt.Sprintf("identical duplicate of %s; this entry will be skipped", sources[firstIndex].Resolved)
			continue
		}

		seenByPath[pathKey] = i
	}
}

func normalizedSourcePathKey(path string) string {
	cleaned := filepath.Clean(path)
	cleaned = strings.ReplaceAll(cleaned, "/", "\\")
	return strings.ToLower(cleaned)
}

func assignSourceBackupNames(sources []backupSourcePlan) {
	grouped := groupSourcesByBasename(sources)
	assignNamesByGroup(sources, grouped)
	fillMissingBackupNames(sources)
}

// groupSourcesByBasename maps each unique base folder name to the indices of valid (non-error, non-skipped) sources that share it.
func groupSourcesByBasename(sources []backupSourcePlan) map[string][]int {
	grouped := make(map[string][]int)
	for i, source := range sources {
		if source.Err != nil || source.Skip {
			continue
		}
		baseName := util.FolderBaseName(source.Resolved)
		grouped[baseName] = append(grouped[baseName], i)
	}
	return grouped
}

// assignNamesByGroup assigns BackupNames from the grouped index map.
// Unique base names are used directly; duplicate base names get a path-alias suffix.
// Alias collisions mark both sources with an error.
func assignNamesByGroup(sources []backupSourcePlan, grouped map[string][]int) {
	for baseName, indices := range grouped {
		if len(indices) == 1 {
			sources[indices[0]].BackupName = baseName
			continue
		}

		aliasOwners := make(map[string]int)
		for _, index := range indices {
			pathAlias := sourceAliasFromFullPath(sources[index].Resolved)
			backupName := fmt.Sprintf("%s__%s", baseName, pathAlias)
			sources[index].BackupName = backupName

			if ownerIndex, exists := aliasOwners[backupName]; exists {
				ownerPath := sources[ownerIndex].Resolved
				currentPath := sources[index].Resolved
				errText := fmt.Sprintf("backup name alias collision: %s and %s both resolve to %q; adjust one source path to avoid ambiguity", ownerPath, currentPath, backupName)
				sources[ownerIndex].Err = errors.New(errText)
				sources[index].Err = errors.New(errText)
				continue
			}

			aliasOwners[backupName] = index
		}
	}
}

// fillMissingBackupNames ensures every source has a BackupName set.
// Skipped sources inherit the name of their non-skipped counterpart; others fall back to base folder name.
func fillMissingBackupNames(sources []backupSourcePlan) {
	nameByPath := make(map[string]string)
	for i := range sources {
		if sources[i].Err != nil || sources[i].Skip {
			continue
		}
		if sources[i].BackupName == "" {
			sources[i].BackupName = util.FolderBaseName(sources[i].Resolved)
		}
		nameByPath[sources[i].normalizedPath] = sources[i].BackupName
	}

	for i := range sources {
		if sources[i].BackupName != "" {
			continue
		}
		if sources[i].Skip {
			if name, exists := nameByPath[sources[i].normalizedPath]; exists {
				sources[i].BackupName = name
				continue
			}
		}
		sources[i].BackupName = util.FolderBaseName(sources[i].Resolved)
	}
}

func sourceAliasFromFullPath(path string) string {
	parts := pathHintParts(path)
	return aliasFromParts(parts)
}

func pathHintParts(path string) []string {
	cleaned := filepath.Clean(path)
	volumeName := filepath.VolumeName(cleaned)
	volume := strings.TrimSuffix(volumeName, ":")

	withoutVolume := strings.TrimPrefix(cleaned, volumeName)
	withoutVolume = strings.TrimLeft(withoutVolume, "\\/")

	rawSegments := strings.FieldsFunc(withoutVolume, func(r rune) bool {
		return r == '\\' || r == '/'
	})
	if len(rawSegments) > 0 {
		rawSegments = rawSegments[:len(rawSegments)-1]
	}

	parts := make([]string, 0, len(rawSegments)+1)
	for _, segment := range rawSegments {
		if normalized := sanitizeAliasPart(segment); normalized != "" {
			parts = append(parts, normalized)
		}
	}

	if normalized := sanitizeAliasPart(volume); normalized != "" {
		parts = append(parts, normalized)
	}

	if len(parts) == 0 {
		return []string{"source"}
	}

	return parts
}

func aliasFromParts(parts []string) string {
	alias := strings.Join(parts, "-")
	if alias == "" {
		return "source"
	}
	return alias
}

func sanitizeAliasPart(part string) string {
	trimmed := strings.TrimSpace(part)
	if trimmed == "" {
		return ""
	}

	var b strings.Builder
	b.Grow(len(trimmed))

	for _, r := range trimmed {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			for _, by := range []byte(string(r)) {
				b.WriteString(fmt.Sprintf("~%02X~", by))
			}
		}
	}

	if b.Len() == 0 {
		return "source"
	}

	return b.String()
}
