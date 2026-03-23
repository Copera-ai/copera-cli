package commands

import (
	"fmt"

	"github.com/copera/copera-cli/internal/cache"
	"github.com/spf13/cobra"
)

func newCacheCmd(cli *CLI) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache",
		Short: "Manage local cache",
	}
	cmd.AddCommand(
		newCacheStatusCmd(cli),
		newCacheCleanCmd(cli),
	)
	return cmd
}

func newCacheStatusCmd(cli *CLI) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show cache size and location",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _ := cli.LoadConfig()
			dir := ""
			if cfg != nil {
				dir = cfg.Cache.Dir
			}

			info := cache.Stat(dir)

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(map[string]any{
					"path":  info.Path,
					"files": info.Files,
					"bytes": info.Bytes,
				})
			}

			cli.Printer.PrintLine(fmt.Sprintf("Path:  %s", info.Path))
			cli.Printer.PrintLine(fmt.Sprintf("Files: %d", info.Files))
			cli.Printer.PrintLine(fmt.Sprintf("Size:  %s", cache.FormatSize(info.Bytes)))
			return nil
		},
	}
}

func newCacheCleanCmd(cli *CLI) *cobra.Command {
	return &cobra.Command{
		Use:   "clean",
		Short: "Remove all cached data",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _ := cli.LoadConfig()
			dir := ""
			if cfg != nil {
				dir = cfg.Cache.Dir
			}

			info := cache.Stat(dir)
			if err := cache.Clean(dir); err != nil {
				cli.Printer.PrintError("cache_error", err.Error(), "", false)
				return err
			}

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(map[string]any{
					"cleaned": true,
					"files":   info.Files,
					"bytes":   info.Bytes,
				})
			}

			cli.Printer.Info("Removed %d files (%s).", info.Files, cache.FormatSize(info.Bytes))
			return nil
		},
	}
}
