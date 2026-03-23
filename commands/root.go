// Package commands contains all CLI subcommands.
// Use ExecuteWithWriters for tests; Execute for production.
package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/copera/copera-cli/internal/build"
	"github.com/copera/copera-cli/internal/cache"
	"github.com/copera/copera-cli/internal/config"
	"github.com/copera/copera-cli/internal/exitcodes"
	"github.com/copera/copera-cli/internal/output"
	"github.com/copera/copera-cli/internal/updater"
	"github.com/spf13/cobra"
)

// CLI holds shared state threaded through all subcommands.
// It is created once per invocation in newRootCmd and passed to subcommand
// constructors via closure — subcommands never reach for package-level globals.
type CLI struct {
	Printer    *output.Printer
	Stdin      io.Reader
	CacheStore cache.Store // nil = use disk (default); set to MemStore in tests

	flags struct {
		token   string
		profile string
		json    bool
		output  string
		quiet   bool
		noInput bool
	}
}

// IsNonInteractive reports whether interactive prompts should be suppressed.
func (c *CLI) IsNonInteractive() bool {
	return c.flags.noInput || os.Getenv("CI") == "true"
}

// LoadConfig resolves configuration for the current invocation.
// Commands call this to get the active profile, token, and default resource IDs.
func (c *CLI) LoadConfig() (*config.Config, error) {
	return config.Load(config.LoadOpts{
		FlagProfile: c.flags.profile,
		FlagToken:   c.flags.token,
	})
}

func newRootCmd(stdin io.Reader, stdout, stderr io.Writer) (*cobra.Command, *CLI) {
	cli := &CLI{Stdin: stdin}

	cmd := &cobra.Command{
		Use:   "copera",
		Short: "Copera CLI — manage boards, docs, and messaging",
		Long: `Copera CLI wraps the Copera public API.

Get started:
  copera auth login           Set up credentials interactively
  export COPERA_CLI_AUTH_TOKEN=<token>   Or use an environment variable

Documentation: https://developers.copera.ai/`,
		SilenceUsage:  true,
		SilenceErrors: true,
		// PersistentPreRunE wires the Printer so every subcommand gets it.
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			format, err := output.ParseFormat(cli.flags.output)
			if err != nil {
				return exitcodes.New(exitcodes.Usage, err)
			}
			if cli.flags.json {
				format = output.FormatJSON
			}
			cli.Printer = output.New(format, stdout, stderr, cli.flags.quiet)
			return nil
		},
	}

	pf := cmd.PersistentFlags()
	pf.StringVar(&cli.flags.token, "token", "", "API token (overrides config and COPERA_CLI_AUTH_TOKEN)")
	pf.StringVar(&cli.flags.profile, "profile", "", `Config profile to use (default: "default")`)
	pf.BoolVar(&cli.flags.json, "json", false, "Force JSON output")
	pf.StringVar(&cli.flags.output, "output", "auto", "Output format: auto|json|table|plain")
	pf.BoolVarP(&cli.flags.quiet, "quiet", "q", false, "Suppress informational messages")
	pf.BoolVar(&cli.flags.noInput, "no-input", false, "Disable all interactive prompts")

	// Register subcommands
	cmd.AddCommand(
		newVersionCmd(cli),
		newAuthCmd(cli),
		newDocsCmd(cli),
		newBoardsCmd(cli),
		newTablesCmd(cli),
		newRowsCmd(cli),
		newChannelsCmd(cli),
		newCacheCmd(cli),
		newUpdateCmd(cli),
	)

	// Background version check — non-blocking, stderr only, suppressed in JSON/CI/quiet
	var updateResult *updater.CheckResult
	var updateOnce sync.Once
	cmd.PersistentPostRun = func(cmd *cobra.Command, args []string) {
		updateOnce.Do(func() {
			if cli.flags.json || cli.flags.quiet || os.Getenv("CI") == "true" ||
				os.Getenv("COPERA_NO_UPDATE_CHECK") == "1" || build.Version == "dev" {
				return
			}
			updateResult = updater.CheckVersion(context.Background(), cache.DefaultDir())
			if updateResult != nil && updateResult.HasUpdate {
				fmt.Fprintf(stderr, "\nA new version of copera is available: v%s → v%s\n", updateResult.Current, updateResult.Latest)
				fmt.Fprintf(stderr, "Run 'copera update' to upgrade.\n")
			}
		})
	}

	return cmd, cli
}

// Execute runs the CLI reading from os.Args, os.Stdin, writing to os.Stdout/Stderr.
func Execute() error {
	return ExecuteWithWriters(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
}

// ExecOpts holds optional overrides for CLI execution (used by tests).
type ExecOpts struct {
	CacheStore cache.Store
}

// ExecuteWithWriters runs the CLI with injected args and I/O — used by tests.
func ExecuteWithWriters(args []string, stdin io.Reader, stdout, stderr io.Writer, opts ...ExecOpts) error {
	cmd, cli := newRootCmd(stdin, stdout, stderr)
	if len(opts) > 0 && opts[0].CacheStore != nil {
		cli.CacheStore = opts[0].CacheStore
	}
	cmd.SetArgs(args)
	cmd.SetIn(stdin)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)

	if err := cmd.Execute(); err != nil {
		// Cobra wraps unknown commands in its own error type; preserve exit code.
		var exitErr *exitcodes.ExitError
		if errors.As(err, &exitErr) {
			return exitErr
		}
		return exitcodes.New(exitcodes.Error, err)
	}
	return nil
}
