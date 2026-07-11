package internal

import (
	"fmt"
	"os"
	"path/filepath"
)

func CleanupTempDir(tempDir string) error {
	if _, err := os.Stat(tempDir); os.IsNotExist(err) {
		return nil
	}
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		return fmt.Errorf("reading temp directory: %w", err)
	}
	for _, entry := range entries {
		path := filepath.Join(tempDir, entry.Name())
		if err := os.Remove(path); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to remove temporary file %s: %v\n", path, err)
		}
	}
	if err := os.Remove(tempDir); err != nil {
		fmt.Fprintf(os.Stderr, "Note: could not remove temp directory %s: %v\n", tempDir, err)
	}
	return nil
}

func FileExists(filename string) bool {
	_, err := os.Stat(filename)
	return !os.IsNotExist(err)
}

func EnsureDirs(dirs ...string) error {
	for _, dir := range dirs {
		if !FileExists(dir) {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}
		}
	}
	return nil
}
