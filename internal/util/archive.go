package util

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// WriteTar walks srcDir and writes all files as a TAR stream to w.
// File paths inside the archive are relative to srcDir.
// Any provided exclude directories are skipped.
func WriteTar(w io.Writer, srcDir string, excludeDirs ...string) error {
	tw := tar.NewWriter(w)
	defer tw.Close()

	srcDir = filepath.Clean(srcDir)

	exs := make([]string, 0, len(excludeDirs))
	for _, e := range excludeDirs {
		if e == "" {
			continue
		}
		ce := filepath.Clean(e)
		rel, relErr := filepath.Rel(srcDir, ce)
		if relErr != nil || strings.HasPrefix(rel, "..") {
			continue
		}
		exs = append(exs, ce)
	}

	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return fmt.Errorf("Failed to scan source folder at %q: %w. Remedy: Check source-folder readability and permissions.", path, err)
		}

		for _, ex := range exs {
			rel, relErr := filepath.Rel(ex, path)
			if relErr == nil && rel != "" && !strings.HasPrefix(rel, "..") {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return fmt.Errorf("Failed to compute relative path: %w. Remedy: Verify source path accessibility and path validity.", err)
		}
		rel = filepath.ToSlash(rel)

		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return fmt.Errorf("Failed to create TAR header for %q: %w. Remedy: Check file metadata accessibility.", path, err)
		}
		hdr.Name = rel

		if err := tw.WriteHeader(hdr); err != nil {
			return fmt.Errorf("Failed to write TAR header for %q: %w. Remedy: Check destination write permissions and free disk space.", path, err)
		}

		if info.IsDir() || !info.Mode().IsRegular() {
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("Failed to open file %q: %w. Remedy: Check file readability and permissions.", path, err)
		}

		if _, err := io.Copy(tw, f); err != nil {
			f.Close() //nolint:errcheck
			return fmt.Errorf("Failed to copy file content %q: %w. Remedy: Check file readability and destination write permissions.", path, err)
		}

		if err := f.Close(); err != nil {
			return fmt.Errorf("Failed to close file %q: %w. Remedy: Retry; if it persists, check file-system health.", path, err)
		}

		return nil
	})
}

// ExtractTar reads a TAR stream from r and extracts all entries to destDir.
func ExtractTar(r io.Reader, destDir string) error {
	tr := tar.NewReader(r)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("Failed to read TAR entry: %w. Remedy: Check password/challenge and .enc part completeness.", err)
		}

		if err := validateTarPath(hdr.Name); err != nil {
			return err
		}

		target := filepath.Join(destDir, filepath.FromSlash(hdr.Name))
		if !strings.HasPrefix(filepath.Clean(target)+string(os.PathSeparator), filepath.Clean(destDir)+string(os.PathSeparator)) {
			return fmt.Errorf("Invalid path in archive (path traversal): %q. Remedy: Do not use this backup; use only unmodified, trusted backup files.", hdr.Name)
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o750); err != nil {
				return fmt.Errorf("Failed to create folder %q: %w. Remedy: Check write permissions in the restore destination.", target, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
				return fmt.Errorf("Failed to create parent folder: %w. Remedy: Check write permissions in the restore destination.", err)
			}
			if err := writeArchiveFile(target, tr); err != nil {
				return err
			}
		}
	}

	return nil
}

// ValidateTar verifies that all TAR headers and regular-file payloads can be consumed.
func ValidateTar(r io.Reader) error {
	tr := tar.NewReader(r)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("Failed to read TAR entry: %w. Remedy: Check password/challenge and .enc part completeness.", err)
		}

		if err := validateTarPath(hdr.Name); err != nil {
			return err
		}

		if hdr.Typeflag == tar.TypeReg {
			if _, err := io.Copy(io.Discard, tr); err != nil {
				return fmt.Errorf("Failed to read TAR entry payload %q: %w. Remedy: Check .enc part completeness and create a new backup if needed.", hdr.Name, err)
			}
		}
	}

	return nil
}

func writeArchiveFile(target string, r io.Reader) error {
	f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		return fmt.Errorf("Failed to create archive file %q: %w. Remedy: Check write permissions in the restore destination.", target, err)
	}
	defer f.Close()

	if _, err := io.Copy(f, r); err != nil {
		return fmt.Errorf("Failed to write file content %q: %w. Remedy: Check free disk space and write permissions.", target, err)
	}
	return nil
}

func validateTarPath(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("Invalid path in archive: empty TAR entry name. Remedy: Do not use this backup; use only unmodified, trusted backup files.")
	}

	normalized := strings.ReplaceAll(name, "\\", "/")
	cleaned := path.Clean(normalized)
	if strings.HasPrefix(cleaned, "/") || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return fmt.Errorf("Invalid path in archive (path traversal): %q. Remedy: Do not use this backup; use only unmodified, trusted backup files.", name)
	}
	if strings.Contains(cleaned, ":") {
		return fmt.Errorf("Invalid path in archive (absolute path): %q. Remedy: Do not use this backup; use only unmodified, trusted backup files.", name)
	}
	if vol := filepath.VolumeName(filepath.FromSlash(cleaned)); vol != "" {
		return fmt.Errorf("Invalid path in archive (absolute path): %q. Remedy: Do not use this backup; use only unmodified, trusted backup files.", name)
	}

	return nil
}
