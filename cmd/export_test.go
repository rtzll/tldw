package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
)

func TestExportGuardsDestinationBeforeAndDuringAcquisition(t *testing.T) {
	path := filepath.Join(t.TempDir(), "result")
	cmd := &cobra.Command{}
	addOutputFlags(cmd)
	if err := cmd.Flags().Set("output", path); err != nil {
		t.Fatal(err)
	}
	err := exportResult(cmd, func() ([]byte, error) {
		if err := os.WriteFile(path, []byte("concurrent"), 0600); err != nil {
			t.Fatal(err)
		}
		return []byte("new"), nil
	})
	if err == nil {
		t.Fatal("overwrote concurrent export")
	}
	err = exportResult(cmd, func() ([]byte, error) { t.Fatal("started work on an occupied destination"); return nil, nil })
	if err == nil {
		t.Fatal("accepted existing file")
	}
	if err := cmd.Flags().Set("force", "true"); err != nil {
		t.Fatal(err)
	}
	failed := errors.New("fetch failed")
	if err := exportResult(cmd, func() ([]byte, error) { return nil, failed }); !errors.Is(err, failed) {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "concurrent" {
		t.Fatalf("%q, %v", got, err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 {
		t.Fatalf("staging files: %v, %v", entries, err)
	}
	if err := cmd.Flags().Set("output", filepath.Join(path, "missing")); err != nil {
		t.Fatal(err)
	}
	if err := exportResult(cmd, func() ([]byte, error) { t.Fatal("fetched for an invalid path"); return nil, nil }); err == nil {
		t.Fatal("accepted invalid parent")
	}
}
