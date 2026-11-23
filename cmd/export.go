package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/rtzll/tldw/internal/atomicfile"
	"github.com/spf13/cobra"
)

func addOutputFlags(cmd *cobra.Command) {
	cmd.Flags().StringP("output", "o", "-", "Write to a new file, or - for stdout")
	cmd.Flags().Bool("force", false, "Replace an existing regular output file")
}

func exportResult(cmd *cobra.Command, produce func() ([]byte, error)) error {
	path, _ := cmd.Flags().GetString("output")
	if path == "" {
		return usageError{errors.New("--output requires a path or - for stdout")}
	}
	if path == "-" {
		data, err := produce()
		if err != nil {
			return err
		}
		_, err = cmd.OutOrStdout().Write(append(data, '\n'))
		return err
	}

	force, _ := cmd.Flags().GetBool("force")
	mode := os.FileMode(0600)
	info, err := os.Lstat(path)
	switch {
	case err == nil:
		if !info.Mode().IsRegular() {
			return fmt.Errorf("output %q must be a regular file", path)
		}
		if !force {
			return outputExists(path)
		}
		mode = info.Mode().Perm()
	case !errors.Is(err, os.ErrNotExist):
		return err
	}

	err = atomicfile.Publish(path, mode, force && info != nil, func(w io.Writer) error {
		data, err := produce()
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	})
	if errors.Is(err, os.ErrExist) {
		return outputExists(path)
	}
	return err
}

func outputExists(path string) error {
	return fmt.Errorf("output %q already exists. Use --force to replace it", path)
}
