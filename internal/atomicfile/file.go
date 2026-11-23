// Package atomicfile publishes complete files from the destination directory.
package atomicfile

import (
	"errors"
	"io"
	"os"
	"path/filepath"
)

// Replace commits with a rename after writing, syncing, and closing the staged file.
func Replace(path string, data []byte, mode os.FileMode) error {
	return Publish(path, mode, true, func(w io.Writer) error { _, err := w.Write(data); return err })
}

// Create publishes a new file without overwriting a concurrent creator.
func Create(path string, data []byte, mode os.FileMode) error {
	return Publish(path, mode, false, func(w io.Writer) error { _, err := w.Write(data); return err })
}

// Publish opens a private staging file before calling write. The destination
// changes only after write, sync, and close have all succeeded.
func Publish(path string, mode os.FileMode, replace bool, write func(io.Writer) error) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".tldw-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if err = write(f); err == nil {
		err = f.Chmod(mode)
	}
	if err == nil {
		err = f.Sync()
	}
	if err := errors.Join(err, f.Close()); err != nil {
		return err
	}
	if replace {
		return os.Rename(f.Name(), path)
	}
	return os.Link(f.Name(), path)
}
