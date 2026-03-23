package testutil

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/copera/copera-cli/commands"
	"github.com/copera/copera-cli/internal/cache"
	"github.com/copera/copera-cli/internal/exitcodes"
)

// Result holds the captured output of a CLI command run in a test.
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// RunCommand executes the CLI root command with the given args and optional
// stdin content. It captures stdout and stderr without calling os.Exit.
// All cache I/O uses an in-memory store — no disk access.
func RunCommand(t *testing.T, args []string, stdin string) Result {
	t.Helper()
	return RunCommandWithStore(t, args, stdin, cache.NewMemStore())
}

// RunCommandWithStore is like RunCommand but accepts a shared cache store,
// so multiple calls within one test can observe each other's cache writes.
func RunCommandWithStore(t *testing.T, args []string, stdin string, store cache.Store) Result {
	t.Helper()

	var outBuf, errBuf bytes.Buffer
	var stdinReader io.Reader = strings.NewReader(stdin)

	err := commands.ExecuteWithWriters(args, stdinReader, &outBuf, &errBuf, commands.ExecOpts{
		CacheStore: store,
	})

	code := 0
	if err != nil {
		var exitErr *exitcodes.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.Code
		} else {
			code = exitcodes.Error
		}
	}

	return Result{
		Stdout:   outBuf.String(),
		Stderr:   errBuf.String(),
		ExitCode: code,
	}
}
