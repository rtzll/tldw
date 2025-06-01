package main

import (
	"os"

	"github.com/rtzll/tldw/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
