package utils

import (
	"crypto/rand"
	"fmt"
	"log"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func GenerateRandomID(length int) (string, error) {
	const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	result := make([]byte, length)
	for i := range result {
		num, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			return "", err
		}
		result[i] = charset[num.Int64()]
	}
	return string(result), nil
}

func CompareDirectoryPaths(path1, path2 string) (bool, error) {
	if len(strings.TrimSpace(path1)) == 0 {
		log.Printf("Temp directory not set, using backup directory.")
		return true, nil
	} else {
		// Clean and get absolute paths: handles relative paths, ".", "..", to ensure consistent comparison.
		absPath1, err := filepath.Abs(path1)
		if err != nil {
			return false, fmt.Errorf("error getting absolute path for %s: %w", path1, err)
		}
		absPath2, err := filepath.Abs(path2)
		if err != nil {
			return false, fmt.Errorf("error getting absolute path for %s: %w", path2, err)
		}
		//EvalSymlinks to resolve symbolic links
		absPath1, err = filepath.EvalSymlinks(absPath1)
		if err != nil {
			return false, fmt.Errorf("error resolving symlinks for %s: %w", path1, err)
		}
		absPath2, err = filepath.EvalSymlinks(absPath2)
		if err != nil {
			return false, fmt.Errorf("error resolving symlinks for %s: %w", path2, err)
		}
		return absPath1 == absPath2, nil
	}
}

func SortFilesByModTime(files []os.FileInfo) []os.FileInfo {
	sort.Slice(
		files, func(i, j int) bool {
			return files[i].ModTime().Before(files[j].ModTime())
		},
	)
	return files
}
