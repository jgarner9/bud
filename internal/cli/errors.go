package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
)

// Exit codes: 1 when something failed, 2 when the command line was wrong
const (
	ExitFailure = 1
	ExitUsage   = 2
)

// UsageError means the command line was wrong, not that something failed
type UsageError struct {
	Err     error
	Printed bool // the flag package already wrote it to stderr
}

func (e *UsageError) Error() string { return e.Err.Error() }
func (e *UsageError) Unwrap() error { return e.Err }

func usageErrorf(format string, a ...any) error {
	return &UsageError{Err: fmt.Errorf(format, a...)}
}

// ExitCode reports err on stderr, once and prefixed with "bud:", and returns the exit code
func ExitCode(err error, stderr io.Writer) int {
	var usage *UsageError
	switch {
	case err == nil, errors.Is(err, flag.ErrHelp):
		// -h/--help already printed usage, so it's a clean exit, not a failure
		return 0
	case errors.As(err, &usage):
		if !usage.Printed {
			fmt.Fprintf(stderr, "bud: %v\n", usage.Err)
			fmt.Fprintln(stderr, "run 'bud --help' for usage")
		}
		return ExitUsage
	default:
		fmt.Fprintf(stderr, "bud: %v\n", err)
		return ExitFailure
	}
}
