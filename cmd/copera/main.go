package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/copera/copera-cli/commands"
	"github.com/copera/copera-cli/internal/exitcodes"
)

func main() {
	if err := commands.Execute(); err != nil {
		// Print the error message if not already printed by the command.
		// Structured errors are written to stderr by the command itself;
		// this handles unexpected errors that bubble up without being formatted.
		var exitErr *exitcodes.ExitError
		if errors.As(err, &exitErr) {
			if exitErr.Err != nil {
				fmt.Fprintln(os.Stderr, exitErr.Err.Error())
			}
			os.Exit(exitErr.Code)
		}
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(exitcodes.Error)
	}
}
