package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"syscall"

	"github.com/rtzll/tldw/internal/tldw"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// The command has already emitted the usable result and identified skipped items.
var errIncomplete = errors.New("incomplete result")

type usageError struct{ error }

func (e usageError) Unwrap() error { return e.error }

// Cobra calls argument validation before command setup.
func arguments(validate cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := validate(cmd, args); err != nil {
			return usageError{err}
		}
		return nil
	}
}

// A bad option can stop pflag before a later --json. Reuse the same flag
// grammar to recover only the requested error format, without executing anything.
func flagError(cmd *cobra.Command, err error) error {
	if format := cmd.Flags().Lookup("json"); format != nil {
		flags := pflag.NewFlagSet(cmd.Name(), pflag.ContinueOnError)
		flags.AddFlagSet(cmd.Flags())
		flags.ParseErrorsAllowlist.UnknownFlagsHandling = pflag.IgnoreUnknownFlag
		_ = flags.ParseAll(os.Args[1:], func(flag *pflag.Flag, value string) error {
			if flag == format {
				return format.Value.Set(value)
			}
			return nil
		})
	}
	return usageError{err}
}

type signalError struct{ signal syscall.Signal }

func (e signalError) Error() string { return "canceled by " + e.signal.String() }

type failure struct {
	Status  string `json:"status"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

func classify(err error) (int, failure) {
	code, status, reason := 1, "error", "operation_failed"
	var sig signalError
	var usage usageError
	switch {
	case errors.As(err, &sig):
		code, reason = 128+int(sig.signal), "canceled"
	case errors.Is(err, context.Canceled), errors.Is(err, errConsentDeclined):
		code, reason = 130, "canceled"
	case errors.Is(err, errConsentRequired):
		code, status, reason = 2, "action_required", "confirmation_required"
	case errors.As(err, &usage):
		code, status, reason = 2, "action_required", "invalid_arguments"
	case errors.Is(err, context.DeadlineExceeded):
		reason = "timeout"
	case errors.Is(err, tldw.ErrRateLimited):
		reason = "rate_limited"
	}
	return code, failure{status, reason, err.Error()}
}

func reportFailure(cmd *cobra.Command, err error) int {
	if errors.Is(err, errIncomplete) {
		return 1
	}
	code, result := classify(err)
	jsonOutput, _ := cmd.Flags().GetBool("json")
	if jsonOutput || cmd.Name() == "metadata" {
		_ = json.NewEncoder(cmd.ErrOrStderr()).Encode(result)
	} else {
		cmd.PrintErrln("Error:", result.Message)
	}
	return code
}
