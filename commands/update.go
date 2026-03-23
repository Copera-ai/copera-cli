package commands

import (
	"context"
	"fmt"

	"github.com/copera/copera-cli/internal/build"
	"github.com/copera/copera-cli/internal/cache"
	"github.com/copera/copera-cli/internal/updater"
	"github.com/spf13/cobra"
)

func newUpdateCmd(cli *CLI) *cobra.Command {
	var flagVersion string
	var flagForce bool

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update copera to the latest version",
		RunE: func(cmd *cobra.Command, args []string) error {
			if build.Version == "dev" {
				cli.Printer.Info("Running a development build — update is not available.")
				return nil
			}

			version := flagVersion
			if version == "" {
				result := updater.CheckVersion(context.Background(), cache.DefaultDir())
				if result == nil || !result.HasUpdate {
					cli.Printer.Info("Already up to date (v%s).", build.Version)
					return nil
				}
				version = result.Latest
			}

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(map[string]any{
					"current": build.Version,
					"target":  version,
					"action":  "updating",
				})
			}

			cli.Printer.PrintLine(fmt.Sprintf("Updating copera: v%s → v%s", build.Version, version))

			if !flagForce && !cli.IsNonInteractive() {
				fmt.Fprintf(cli.Printer.Out, "Continue? [Y/n]: ")
				var ans string
				fmt.Fscanln(cli.Stdin, &ans)
				if ans != "" && ans != "y" && ans != "Y" {
					cli.Printer.Info("Update cancelled.")
					return nil
				}
			}

			if err := updater.Update(context.Background(), version); err != nil {
				cli.Printer.PrintError("update_failed", err.Error(),
					"Try downloading manually from https://cli.copera.ai", false)
				return err
			}

			cli.Printer.Info("Updated to v%s.", version)
			return nil
		},
	}

	cmd.Flags().StringVar(&flagVersion, "version", "", "Update to a specific version (e.g. 1.2.0)")
	cmd.Flags().BoolVar(&flagForce, "force", false, "Skip confirmation prompt")
	return cmd
}
