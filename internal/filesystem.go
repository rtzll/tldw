package internal

import (
	"fmt"
	"os"
)

func CleanupTempDir(tempDir string) error {
	if err := os.RemoveAll(tempDir); err != nil {
		return fmt.Errorf("removing temporary directory: %w", err)
	}
	return nil
}

func EnsureDirs(dirs ...string) error {
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("creating directory %q: %w", dir, err)
		}
	}
	return nil
}
