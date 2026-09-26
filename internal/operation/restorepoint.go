package operation

import (
	"RestoreSafe/internal/archive"
	"RestoreSafe/internal/container"
	"RestoreSafe/internal/manifest"
	"RestoreSafe/internal/util"
	"fmt"
	"io"
)

// ProcessRestorePoint restores the restore point of set into destDir, or, with
// verifyOnly, checks it without writing anything. Every file is checked
// against its manifest hash. It returns the target manifest so the caller can
// report skipped files.
func ProcessRestorePoint(set *container.Set, master []byte, destDir string, verifyOnly bool, log *util.Logger) (*manifest.Manifest, error) {
	if set.Header.IsDiff() {
		return nil, fmt.Errorf("Restoring differential backups is not supported yet.")
	}
	keys, err := set.SectionKeys(master)
	if err != nil {
		return nil, err
	}
	defer keys.Zero()

	m, _, err := set.ReadManifest(keys)
	if err != nil {
		return nil, err
	}

	r := archive.NewRestorer(m, destDir, verifyOnly)
	if err := r.CreateDirectories(); err != nil {
		return nil, err
	}

	verb, prefix := "decrypted", "Extraction"
	if verifyOnly {
		verb, prefix = "verified", "Verification"
	}
	err = RunSectionPipeline(set, keys, log, set.Header.DirectoryName, verb, prefix, func(tarStream io.Reader) error {
		return r.ExtractSection(tarStream, archive.DecideOwn(m))
	})
	if err != nil {
		return nil, err
	}
	if err := r.Finish(); err != nil {
		for _, p := range r.MissingFiles() {
			log.WarnLogOnly("  Missing in backup data: %s", p)
		}
		return nil, err
	}
	return m, nil
}

// VerifyOwnData checks a set's own data section against its manifest: every
// file stored in the set is decrypted and hashed. For a differential this
// covers the new and changed files without reading the full backup, which is
// what verify_after_backup needs right after writing it.
func VerifyOwnData(set *container.Set, master []byte, log *util.Logger) (*manifest.Manifest, error) {
	keys, err := set.SectionKeys(master)
	if err != nil {
		return nil, err
	}
	defer keys.Zero()
	m, _, err := set.ReadManifest(keys)
	if err != nil {
		return nil, err
	}
	r := archive.NewRestorer(m, "", true)
	r.ExpectOwnContentOnly()
	err = RunSectionPipeline(set, keys, log, set.Header.DirectoryName, "verified", "Verification", func(tarStream io.Reader) error {
		return r.ExtractSection(tarStream, archive.DecideOwn(m))
	})
	if err != nil {
		return nil, err
	}
	if err := r.Finish(); err != nil {
		return nil, err
	}
	return m, nil
}

// ReportSkippedFiles logs the files that could not be read during the backup
// and are therefore missing from the restore point. It returns their count.
func ReportSkippedFiles(m *manifest.Manifest, directoryName string, log *util.Logger) int {
	skipped := archive.SkippedFiles(m)
	if len(skipped) == 0 {
		return 0
	}
	log.Warn("  [%s] %d file(s) are not in this backup because they could not be read during backup:", directoryName, len(skipped))
	for _, p := range skipped {
		log.Warn("    %s", p)
	}
	return len(skipped)
}
