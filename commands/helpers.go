package commands

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

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
	client := api.New(cfg.API.BaseURL, cfg.Token, cfg.API.Timeout)
	if cli.flags.verbose {
		client.SetVerbose(cli.Printer.Err)
	}
	return client, cfg, nil
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

// newWorkspaceCache returns a workspace metadata cache (used for slug lookup).
func newWorkspaceCache(cli *CLI, cfg *config.Config) *cache.Cache {
	if cli.CacheStore != nil {
		return cache.NewWithStore(cli.CacheStore, cfg.Cache.TTL)
	}
	return cache.NewWorkspaceCache(cfg.Cache.Dir, cfg.Cache.TTL)
}

// tokenCacheKey returns a short, stable key derived from the API token,
// so workspace slugs from different tokens never collide on disk.
func tokenCacheKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return "slug-" + hex.EncodeToString(sum[:8])
}

// resolveWorkspaceSlug returns the active workspace slug, hitting the cache
// first and falling back to GET /workspace/info. Returns "" on any failure
// so URL construction can be skipped silently.
func resolveWorkspaceSlug(ctx context.Context, cli *CLI, client *api.Client, cfg *config.Config) string {
	key := tokenCacheKey(cfg.Token)
	wc := newWorkspaceCache(cli, cfg)
	if slug, ok := wc.Get(key); ok && slug != "" {
		return slug
	}
	ws, err := client.WorkspaceInfo(ctx)
	if err != nil || ws == nil || ws.Slug == "" {
		return ""
	}
	wc.Set(key, ws.Slug)
	return ws.Slug
}

// ── Resource URL builders ────────────────────────────────────────────────────

func webBase(cfg *config.Config) string {
	return strings.TrimRight(cfg.API.WebURL, "/")
}

func docURL(cfg *config.Config, slug, docID string) string {
	if slug == "" || docID == "" || cfg.API.WebURL == "" {
		return ""
	}
	return fmt.Sprintf("%s/%s/docs/%s", webBase(cfg), slug, docID)
}

func driveItemURL(cfg *config.Config, slug, itemType, itemID string) string {
	if slug == "" || itemID == "" || cfg.API.WebURL == "" {
		return ""
	}
	seg := "c"
	if itemType == "folder" {
		seg = "f"
	}
	return fmt.Sprintf("%s/%s/drive/%s/%s", webBase(cfg), slug, seg, itemID)
}

func boardURL(cfg *config.Config, slug, boardID string) string {
	if slug == "" || boardID == "" || cfg.API.WebURL == "" {
		return ""
	}
	return fmt.Sprintf("%s/%s/boards/%s", webBase(cfg), slug, boardID)
}

func tableURL(cfg *config.Config, slug, boardID, tableID string) string {
	if slug == "" || boardID == "" || tableID == "" || cfg.API.WebURL == "" {
		return ""
	}
	return fmt.Sprintf("%s/%s/boards/%s/%s", webBase(cfg), slug, boardID, tableID)
}

func rowURL(cfg *config.Config, slug, boardID, tableID, rowID string) string {
	if slug == "" || boardID == "" || tableID == "" || rowID == "" || cfg.API.WebURL == "" {
		return ""
	}
	return fmt.Sprintf("%s/%s/boards/%s/%s/v/%s", webBase(cfg), slug, boardID, tableID, rowID)
}
