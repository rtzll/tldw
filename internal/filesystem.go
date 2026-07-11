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
