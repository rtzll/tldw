// Package atomicfile publishes complete files from the destination directory.
package atomicfile

import (
	"errors"
	"os"
	"path/filepath"
)

// Replace commits with a rename after writing, syncing, and closing the staged file.
func Replace(path string, data []byte, mode os.FileMode) error {
	return publish(path, data, mode, os.Rename)
}

// Create publishes a new file without overwriting a concurrent creator.
func Create(path string, data []byte, mode os.FileMode) error {
	return publish(path, data, mode, os.Link)
}

func publish(path string, data []byte, mode os.FileMode, commit func(string, string) error) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".tldw-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	if err := errors.Join(err, f.Close()); err != nil {
		return err
	}
	return commit(f.Name(), path)
}
