package plan

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/phsc84/restoresafe/internal/format/naming"
	"github.com/phsc84/restoresafe/internal/fsx"
)

// Source is one configured source directory, resolved and checked.
type Source struct {
	// Resolved is the absolute path.
	Resolved       string
	normalizedPath string // cached result of fsx.NormalizePathKey(Resolved)
	// BackupName is the directory name in the backup file names, with an
	// alias when several sources share a base name.
	BackupName string
	// Warning is set for an identical duplicate of an earlier source.
	Warning string
	// Skip is set for an identical duplicate; it is not backed up again.
	Skip bool
	// Err is set when the directory cannot be backed up.
	Err error
}

// ResolveSources resolves the configured source directories (relative paths
// against exeDir), checks them, marks identical duplicates, and assigns the
// backup names.
func ResolveSources(sourceDirectories []string, exeDir string) []Source {
	result := make([]Source, 0, len(sourceDirectories))
	for _, src := range sourceDirectories {
		resolved := fsx.ResolveDir(src, exeDir)
		status := Source{Resolved: resolved, normalizedPath: fsx.NormalizePathKey(resolved)}

		status.Err = fsx.ValidateSourceDirectory(resolved)
		result = append(result, status)
	}
	markIdenticalSourceDuplicates(result)
	assignSourceBackupNames(result)
	return result
}

func markIdenticalSourceDuplicates(sources []Source) {
	seenByPath := make(map[string]int)
	for i := range sources {
		if sources[i].Err != nil {
			continue
		}

		pathKey := sources[i].normalizedPath
		if firstIndex, exists := seenByPath[pathKey]; exists {
			sources[i].Skip = true
			sources[i].Warning = fmt.Sprintf(fsx.SourceDuplicateWarningFmt, sources[firstIndex].Resolved)
			continue
		}

		seenByPath[pathKey] = i
	}
}

func assignSourceBackupNames(sources []Source) {
	grouped := groupSourcesByBasename(sources)
	assignNamesByGroup(sources, grouped)
	fillMissingBackupNames(sources)
}

// groupSourcesByBasename maps each unique base directory name to the indices of valid (non-error, non-skipped) sources that share it.
func groupSourcesByBasename(sources []Source) map[string][]int {
	grouped := make(map[string][]int)
	for i, source := range sources {
		if source.Err != nil || source.Skip {
			continue
		}
		baseName := naming.DirectoryBaseName(source.Resolved)
		grouped[baseName] = append(grouped[baseName], i)
	}
	return grouped
}

// assignNamesByGroup assigns BackupNames from the grouped index map.
// Unique base names are used directly; duplicate base names get a path-alias suffix.
// Alias collisions mark both sources with an error.
func assignNamesByGroup(sources []Source, grouped map[string][]int) {
	for baseName, indices := range grouped {
		if len(indices) == 1 {
			sources[indices[0]].BackupName = baseName
			continue
		}

		aliasOwners := make(map[string]int)
		for _, index := range indices {
			pathAlias := sourceAliasFromFullPath(sources[index].Resolved)
			backupName := fmt.Sprintf("%s__from__%s", baseName, pathAlias)
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
// Skipped sources inherit the name of their non-skipped counterpart; others fall back to base directory name.
func fillMissingBackupNames(sources []Source) {
	nameByPath := make(map[string]string)
	for i := range sources {
		if sources[i].Err != nil || sources[i].Skip {
			continue
		}
		if sources[i].BackupName == "" {
			sources[i].BackupName = naming.DirectoryBaseName(sources[i].Resolved)
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
		sources[i].BackupName = naming.DirectoryBaseName(sources[i].Resolved)
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
	if normalized := sanitizeAliasPart(volume); normalized != "" {
		parts = append(parts, normalized)
	}

	for _, segment := range rawSegments {
		if normalized := sanitizeAliasPart(segment); normalized != "" {
			parts = append(parts, normalized)
		}
	}

	if len(parts) == 0 {
		return []string{"source"}
	}

	return parts
}

func aliasFromParts(parts []string) string {
	alias := strings.Join(parts, "_")
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
