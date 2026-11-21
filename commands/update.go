package commands

import (
	"fmt"
	"os"
	"strings"

	"github.com/copera/copera-cli/internal/build"
	"github.com/copera/copera-cli/internal/exitcodes"
	"github.com/copera/copera-cli/internal/updater"
	"github.com/spf13/cobra"
)

type updateResult struct {
	Current string `json:"current"`
	Target  string `json:"target"`
	Action  string `json:"action"`
}

func newUpdateCmd(cli *CLI) *cobra.Command {
	var flagVersion string
	var flagForce bool
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update copera to the latest version",
		RunE: func(cmd *cobra.Command, args []string) error {
			if version, resumed, err := updater.ResumeInstall(); resumed {
				return printUpdateResult(cli, version, "updated", err)
			}
			if build.Version == "dev" {
				if cli.Printer.IsJSON() {
					return printUpdateResult(cli, "", "", fmt.Errorf("update is unavailable for development builds"))
				}
				cli.Printer.Info("Running a development build — update is not available.")
				return nil
			}

			version := strings.TrimPrefix(flagVersion, "v")
			if version == "" {
				latest, err := updater.LatestVersion(cmd.Context())
				if err != nil {
					return printUpdateResult(cli, "", "", err)
				}
				version = latest
				if version == strings.TrimPrefix(build.Version, "v") {
					return printUpdateResult(cli, version, "up_to_date", nil)
				}
			}

			stdin, _ := cli.Stdin.(*os.File)
			interactive := !cli.IsNonInteractive() && !cli.Printer.IsJSON() &&
				stdin != nil && stderrIsTTY(stdin) && stderrIsTTY(cli.Printer.Out)
			if !cli.Printer.IsJSON() {
				cli.Printer.PrintLine(fmt.Sprintf("Updating copera: v%s → v%s", build.Version, version))
			}

			if !flagForce && interactive {
				fmt.Fprintf(cli.Printer.Out, "Continue? [Y/n]: ")
				var ans string
				fmt.Fscanln(cli.Stdin, &ans)
				if ans != "" && ans != "y" && ans != "Y" {
					cli.Printer.Info("Update cancelled.")
					return nil
				}
			}

			return printUpdateResult(cli, version, "updated", updater.Update(cmd.Context(), version, interactive))
		},
	}

	cmd.Flags().StringVar(&flagVersion, "version", "", "Update to a specific version (e.g. 1.2.0)")
	cmd.Flags().BoolVar(&flagForce, "force", false, "Skip confirmation prompt")
	return cmd
}

func printUpdateResult(cli *CLI, version, action string, err error) error {
	if err != nil {
		cli.Printer.PrintError("update_failed", err.Error(), "", false)
		return exitcodes.New(exitcodes.Error, nil)
	}
	if cli.Printer.IsJSON() {
		return cli.Printer.PrintJSON(updateResult{Current: build.Version, Target: version, Action: action})
	}
	if action == "up_to_date" {
		cli.Printer.Info("Already up to date (v%s).", build.Version)
	} else {
		cli.Printer.Info("Updated to v%s.", version)
	}
	return nil
}
