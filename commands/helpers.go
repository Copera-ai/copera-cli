package commands

import (
	"errors"
	"fmt"

	"github.com/copera/copera-cli/internal/api"
	"github.com/copera/copera-cli/internal/cache"
	"github.com/copera/copera-cli/internal/config"
	"github.com/copera/copera-cli/internal/exitcodes"
)

// requireAPIClient loads config, verifies a token is set, and returns an API client.
func requireAPIClient(cli *CLI) (*api.Client, *config.Config, error) {
	cfg, err := cli.LoadConfig()
	if err != nil {
		return nil, nil, handleConfigErr(cli, err)
	}
	if err := cfg.RequireToken(); err != nil {
		cli.Printer.PrintError("auth_required",
			"no API token configured",
			"Run 'copera auth login' or set COPERA_CLI_AUTH_TOKEN",
			false)
		return nil, nil, exitcodes.New(exitcodes.AuthFailure, err)
	}
	return api.New(cfg.API.BaseURL, cfg.Token, cfg.API.Timeout), cfg, nil
}

func handleConfigErr(cli *CLI, err error) error {
	var pnf *config.ProfileNotFoundError
	if errors.As(err, &pnf) {
		cli.Printer.PrintError("unknown_profile", err.Error(),
			fmt.Sprintf("Run 'copera auth login' to create profile %q", pnf.Profile), false)
		return exitcodes.New(exitcodes.Usage, err)
	}
	cli.Printer.PrintError("config_error", err.Error(), "", false)
	return exitcodes.New(exitcodes.Error, err)
}

func apiError(cli *CLI, err error) error {
	var apiErr *api.APIError
	if errors.As(err, &apiErr) {
		cli.Printer.PrintError("api_error", apiErr.Error(), "", false)
		return exitcodes.New(apiErr.ExitCode(), err)
	}
	cli.Printer.PrintError("request_failed", err.Error(), "", false)
	return exitcodes.New(exitcodes.Error, err)
}

// resolveID returns the first non-empty value from args[0], flagValue, or configDefault.
// name is used in the error message when all are empty.
func resolveID(args []string, flagValue, configDefault, name string) (string, error) {
	if len(args) > 0 && args[0] != "" {
		return args[0], nil
	}
	if flagValue != "" {
		return flagValue, nil
	}
	if configDefault != "" {
		return configDefault, nil
	}
	return "", fmt.Errorf("%s required", name)
}

// newDocCache returns a doc content cache using cli.CacheStore if set, otherwise disk.
func newDocCache(cli *CLI, cfg *config.Config) *cache.Cache {
	if cli.CacheStore != nil {
		return cache.NewWithStore(cli.CacheStore, cfg.Cache.TTL)
	}
	return cache.New(cfg.Cache.Dir, cfg.Cache.TTL)
}

// newTableCache returns a table schema cache using cli.CacheStore if set, otherwise disk.
func newTableCache(cli *CLI, cfg *config.Config) *cache.TableCache {
	if cli.CacheStore != nil {
		return cache.NewTableCacheWithStore(cli.CacheStore, cfg.Cache.TTL)
	}
	return cache.NewTableCache(cfg.Cache.Dir, cfg.Cache.TTL)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
