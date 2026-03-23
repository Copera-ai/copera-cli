package exitcodes

import "fmt"

// Exit code constants. These are the only valid exit codes for the CLI.
const (
	OK          = 0
	Error       = 1
	Usage       = 2
	NotFound    = 3
	AuthFailure = 4
	Conflict    = 5
	RateLimit   = 6
)

// ExitError wraps an error with a semantic exit code. Commands return this;
// main.go calls os.Exit based on the embedded code.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return fmt.Sprintf("exit %d", e.Code)
}

func (e *ExitError) Unwrap() error { return e.Err }

// New wraps err with the given exit code.
func New(code int, err error) *ExitError {
	return &ExitError{Code: code, Err: err}
}

// Newf creates an ExitError with a formatted message.
func Newf(code int, format string, args ...any) *ExitError {
	return &ExitError{Code: code, Err: fmt.Errorf(format, args...)}
}
