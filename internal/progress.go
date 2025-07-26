package internal

import (
	"fmt"

	"github.com/schollz/progressbar/v3"
)

// UIManager handles all user interface concerns (progress, verbose output, prompts)
type UIManager interface {
	// Progress bars
	NewProgressBar(total int, description string) ProgressBar
	NewSharedProgressBar(total int, description string) ProgressBar

	// Verbose output
	Verbose(format string, args ...interface{})

	// Status messages
	Printf(format string, args ...interface{})
	Println(args ...interface{})
}

// ProgressBar interface abstracts progress bar operations
type ProgressBar interface {
	Set(current int)
	Describe(description string)
	Finish()
}

// StandardUIManager handles normal UI operations
type StandardUIManager struct {
	verbose bool
	quiet   bool
}

func NewUIManager(verbose, quiet bool) UIManager {
	return &StandardUIManager{
		verbose: verbose,
		quiet:   quiet,
	}
}

// Progress Bar Methods
func (ui *StandardUIManager) NewProgressBar(total int, description string) ProgressBar {
	if ui.quiet {
		return &SilentProgressBar{bar: progressbar.DefaultSilent(int64(total))}
	}

	bar := progressbar.NewOptions(total,
		progressbar.OptionSetDescription(description),
		progressbar.OptionSetWidth(30),
		progressbar.OptionShowCount(),
		progressbar.OptionSetPredictTime(false),
		progressbar.OptionClearOnFinish(),
		progressbar.OptionSetTheme(progressbar.Theme{
			Saucer:        "=",
			SaucerHead:    ">",
			SaucerPadding: " ",
			BarStart:      "[",
			BarEnd:        "]",
		}))
	return &VisibleProgressBar{bar: bar}
}

func (ui *StandardUIManager) NewSharedProgressBar(total int, description string) ProgressBar {
	return ui.NewProgressBar(total, description)
}

// Verbose Output Methods
func (ui *StandardUIManager) Verbose(format string, args ...interface{}) {
	if ui.verbose {
		fmt.Printf(format, args...)
	}
}

// Status Message Methods
func (ui *StandardUIManager) Printf(format string, args ...interface{}) {
	if !ui.quiet {
		fmt.Printf(format, args...)
	}
}

func (ui *StandardUIManager) Println(args ...interface{}) {
	if !ui.quiet {
		fmt.Println(args...)
	}
}

// VisibleProgressBar wraps the actual progress bar
type VisibleProgressBar struct {
	bar *progressbar.ProgressBar
}

func (v *VisibleProgressBar) Set(current int) {
	v.bar.Set(current)
}

func (v *VisibleProgressBar) Describe(description string) {
	v.bar.Describe(description)
}

func (v *VisibleProgressBar) Finish() {
	v.bar.Finish()
}

// SilentProgressBar implements a silent progress bar
type SilentProgressBar struct {
	bar *progressbar.ProgressBar
}

func (s *SilentProgressBar) Set(current int) {
	s.bar.Set(current)
}

func (s *SilentProgressBar) Describe(description string) {
	// Do nothing for silent mode
}

func (s *SilentProgressBar) Finish() {
	s.bar.Finish()
}
