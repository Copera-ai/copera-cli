package commands

import (
	"fmt"

	"github.com/copera/copera-cli/internal/build"
	"github.com/spf13/cobra"
)

func newVersionCmd(cli *CLI) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show version information",
		RunE: func(cmd *cobra.Command, args []string) error {
			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(map[string]string{
					"version":        build.Version,
					"build_time":     build.Time,
					"schema_version": "1",
				})
			}
			fmt.Fprintf(cli.Printer.Out, "copera %s (built %s)\n", build.Version, build.Time)
			return nil
		},
	}
}
