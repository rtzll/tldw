package internal

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/mattn/go-isatty"
	"github.com/schollz/progressbar/v3"
)

// UIManager handles all user interface concerns (progress, verbose output, prompts)
type UIManager interface {
	// Progress bars
	NewProgressBar(total int, description string) ProgressBar
	NewSharedProgressBar(total int, description string) ProgressBar
	NewSpinner(description string) ProgressBar

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
	Advance() // For spinners
}

// AutoSpinner interface for spinners that advance themselves
type AutoSpinner interface {
	ProgressBar
	Stop() // Stop auto-advancement
}

// StandardUIManager handles normal UI operations
type StandardUIManager struct {
	verbose bool
	quiet   bool
}

func NewUIManager(verbose, quiet bool) UIManager {
	// If stdout is not a TTY (e.g., output is piped), treat it as quiet mode
	// to prevent spinners and progress bars from appearing in piped output
	if !isatty.IsTerminal(os.Stdout.Fd()) {
		quiet = true
	}

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

func (ui *StandardUIManager) NewSpinner(description string) ProgressBar {
	if ui.quiet {
		return &SilentProgressBar{bar: progressbar.DefaultSilent(-1)}
	}

	bar := progressbar.NewOptions(-1,
		progressbar.OptionSetDescription(description),
		progressbar.OptionSpinnerType(11), // Nice Braille dots spinner
		progressbar.OptionSetWidth(30),
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

func (v *VisibleProgressBar) Advance() {
	v.bar.Add(1)
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

func (s *SilentProgressBar) Advance() {
	s.bar.Add(1)
}

// NoOpProgressBar implements a no-operation progress bar for when status is disabled
type NoOpProgressBar struct{}

func (n *NoOpProgressBar) Set(current int)             {}
func (n *NoOpProgressBar) Describe(description string) {}
func (n *NoOpProgressBar) Finish()                     {}
func (n *NoOpProgressBar) Advance()                    {}

// AutoAdvancingSpinner implements a spinner that advances itself in a background goroutine
type AutoAdvancingSpinner struct {
	bar    *progressbar.ProgressBar
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
}

func NewAutoAdvancingSpinner(description string, quiet bool) AutoSpinner {
	if quiet {
		return &NoOpAutoSpinner{}
	}

	ctx, cancel := context.WithCancel(context.Background())

	bar := progressbar.NewOptions(-1,
		progressbar.OptionSetDescription(description),
		progressbar.OptionSpinnerType(11),
		progressbar.OptionSetWidth(30),
		progressbar.OptionClearOnFinish(),
		progressbar.OptionSetTheme(progressbar.Theme{
			Saucer:        "=",
			SaucerHead:    ">",
			SaucerPadding: " ",
			BarStart:      "[",
			BarEnd:        "]",
		}))

	spinner := &AutoAdvancingSpinner{
		bar:    bar,
		ctx:    ctx,
		cancel: cancel,
		done:   make(chan struct{}),
	}

	// Start auto-advancement
	go spinner.autoAdvance()

	return spinner
}

func (a *AutoAdvancingSpinner) autoAdvance() {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	defer close(a.done)

	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			a.bar.Add(1)
		}
	}
}

func (a *AutoAdvancingSpinner) Set(current int) {
	a.bar.Set(current)
}

func (a *AutoAdvancingSpinner) Describe(description string) {
	a.bar.Describe(description)
}

func (a *AutoAdvancingSpinner) Advance() {
	// No-op since we auto-advance
}

func (a *AutoAdvancingSpinner) Stop() {
	a.cancel()
	<-a.done // Wait for goroutine to finish
}

func (a *AutoAdvancingSpinner) Finish() {
	a.Stop()
	a.bar.Finish()
}

// NoOpAutoSpinner implements AutoSpinner as no-ops
type NoOpAutoSpinner struct{}

func (n *NoOpAutoSpinner) Set(current int)             {}
func (n *NoOpAutoSpinner) Describe(description string) {}
func (n *NoOpAutoSpinner) Finish()                     {}
func (n *NoOpAutoSpinner) Advance()                    {}
func (n *NoOpAutoSpinner) Stop()                       {}
