package internal

import (
	"os"

	"github.com/mattn/go-isatty"
	"github.com/schollz/progressbar/v3"
)

// Spinner is the status surface used while generating summaries.
type Spinner interface {
	Describe(description string)
	Finish()
}

// NewSpinner creates a terminal spinner, or a no-op spinner when output is
// redirected and terminal animation would corrupt piped output.
func NewSpinner(description string) Spinner {
	if !isatty.IsTerminal(os.Stdout.Fd()) {
		return NoOpSpinner{}
	}
	bar := progressbar.NewOptions(-1,
		progressbar.OptionSetDescription(description),
		progressbar.OptionSpinnerType(11),
		progressbar.OptionSetWidth(30),
		progressbar.OptionClearOnFinish(),
	)
	return terminalSpinner{bar: bar}
}

type terminalSpinner struct {
	bar *progressbar.ProgressBar
}

func (spinner terminalSpinner) Describe(description string) {
	spinner.bar.Describe(description)
}

func (spinner terminalSpinner) Finish() {
	_ = spinner.bar.Finish()
}

type NoOpSpinner struct{}

func (NoOpSpinner) Describe(string) {}
func (NoOpSpinner) Finish()         {}
