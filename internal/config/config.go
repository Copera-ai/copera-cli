// Package config loads and resolves CLI configuration from multiple sources.
// Commands always read from a *Config — they never call viper directly.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/viper"
)

// Config holds all resolved configuration for one CLI invocation.
type Config struct {
	// Active profile name
	Profile string

	// Resolved from active profile
	Token     string
	BoardID   string
	TableID   string
	RowID     string
	ChannelID string
	DocID     string

	// Where the token came from — used by 'auth status'
	TokenSource TokenSource

	// Global settings (apply across all profiles)
	Output OutputConfig
	Cache  CacheConfig
	API    APIConfig
}

// TokenSource describes where the resolved token came from.
type TokenSource struct {
	Kind string // "env" | "flag" | "file" | ""
	Path string // set when Kind == "file"
}

func (ts TokenSource) String() string {
	switch ts.Kind {
	case "env":
		return "environment variable COPERA_CLI_AUTH_TOKEN"
	case "flag":
		return "--token flag"
	case "file":
		return ts.Path
	default:
		return "not set"
	}
}

// OutputConfig mirrors the [output] TOML section.
type OutputConfig struct {
	Format string
	Color  string
}

// CacheConfig mirrors the [cache] TOML section.
type CacheConfig struct {
	Dir string
	TTL time.Duration
}

// APIConfig mirrors the [api] TOML section.
type APIConfig struct {
	BaseURL string
	Timeout time.Duration
}

// LoadOpts controls how configuration is resolved.
// Zero values produce sensible defaults (uses os.Getwd and os.UserHomeDir).
type LoadOpts struct {
	// FlagProfile is the --profile flag value; overrides COPERA_PROFILE env var.
	FlagProfile string
	// FlagToken is the --token flag value; overrides config files but not env var.
	FlagToken string
	// CWD overrides os.Getwd() — useful in tests.
	CWD string
	// HomeDir overrides os.UserHomeDir() — useful in tests.
	HomeDir string
}

// Load reads config files (lowest → highest priority), merges them, resolves
// the active profile, and applies env var / flag token overrides.
func Load(opts LoadOpts) (*Config, error) {
	cwd, homeDir, err := resolveDirs(opts)
	if err != nil {
		return nil, err
	}

	v := newViper()

	// Config files merged in order: home → cwd → cwd-local (highest wins)
	filePaths := []string{
		filepath.Join(homeDir, ".copera.toml"),
		filepath.Join(cwd, ".copera.toml"),
		filepath.Join(cwd, ".copera.local.toml"),
	}

	for _, path := range filePaths {
		if err := mergeFile(v, path); err != nil {
			return nil, fmt.Errorf("config: reading %s: %w", path, err)
		}
	}

	// Resolve active profile
	profileName, explicit := resolveProfileName(opts.FlagProfile, v.GetString("default_profile"))

	// If the profile was explicitly requested, verify it exists
	if explicit && profileName != "default" {
		if v.Get("profiles."+profileName) == nil {
			return nil, &ProfileNotFoundError{Profile: profileName}
		}
	}

	cfg := buildConfig(v, profileName)

	// Token resolution: env var > --token flag > profile config
	if token := os.Getenv("COPERA_CLI_AUTH_TOKEN"); token != "" {
		cfg.Token = token
		cfg.TokenSource = TokenSource{Kind: "env"}
	} else if opts.FlagToken != "" {
		cfg.Token = opts.FlagToken
		cfg.TokenSource = TokenSource{Kind: "flag"}
	} else if cfg.Token != "" {
		cfg.TokenSource = TokenSource{
			Kind: "file",
			Path: findTokenFile(v, profileName, filePaths),
		}
	}

	return cfg, nil
}

// RequireToken returns an error if cfg has no token set.
// Commands call this before making any API request.
func (c *Config) RequireToken() error {
	if c.Token == "" {
		return &MissingTokenError{}
	}
	return nil
}

// --- helpers -----------------------------------------------------------------

func resolveDirs(opts LoadOpts) (cwd, homeDir string, err error) {
	cwd = opts.CWD
	if cwd == "" {
		if cwd, err = os.Getwd(); err != nil {
			return "", "", fmt.Errorf("config: working directory: %w", err)
		}
	}
	homeDir = opts.HomeDir
	if homeDir == "" {
		if homeDir, err = os.UserHomeDir(); err != nil {
			return "", "", fmt.Errorf("config: home directory: %w", err)
		}
	}
	return cwd, homeDir, nil
}

func newViper() *viper.Viper {
	v := viper.New()
	v.SetConfigType("toml")
	v.SetDefault("default_profile", "default")
	v.SetDefault("output.format", "auto")
	v.SetDefault("output.color", "auto")
	v.SetDefault("cache.ttl", "1h")
	v.SetDefault("api.base_url", "https://api.copera.ai/public/v1")
	v.SetDefault("api.timeout", "30s")
	return v
}

func mergeFile(v *viper.Viper, path string) error {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil // missing file is fine
	}
	if err != nil {
		return err
	}
	defer f.Close()
	return v.MergeConfig(f)
}

// resolveProfileName returns the active profile name and whether it was
// explicitly set (true) or fell back implicitly (false).
// Priority: --profile flag > COPERA_PROFILE env > default_profile config key > "default"
func resolveProfileName(flagProfile, configDefault string) (name string, explicit bool) {
	if flagProfile != "" {
		return flagProfile, true
	}
	if env := os.Getenv("COPERA_PROFILE"); env != "" {
		return env, true
	}
	if configDefault != "" && configDefault != "default" {
		return configDefault, true
	}
	return "default", false
}

func buildConfig(v *viper.Viper, profileName string) *Config {
	p := "profiles." + profileName + "."

	cacheTTL, _ := time.ParseDuration(v.GetString("cache.ttl"))
	if cacheTTL == 0 {
		cacheTTL = time.Hour
	}
	apiTimeout, _ := time.ParseDuration(v.GetString("api.timeout"))
	if apiTimeout == 0 {
		apiTimeout = 30 * time.Second
	}

	return &Config{
		Profile:   profileName,
		Token:     v.GetString(p + "token"),
		BoardID:   v.GetString(p + "board_id"),
		TableID:   v.GetString(p + "table_id"),
		RowID:     v.GetString(p + "row_id"),
		ChannelID: v.GetString(p + "channel_id"),
		DocID:     v.GetString(p + "doc_id"),
		Output: OutputConfig{
			Format: v.GetString("output.format"),
			Color:  v.GetString("output.color"),
		},
		Cache: CacheConfig{
			Dir: v.GetString("cache.dir"),
			TTL: cacheTTL,
		},
		API: APIConfig{
			BaseURL: v.GetString("api.base_url"),
			Timeout: apiTimeout,
		},
	}
}

// findTokenFile returns the highest-priority file that contains a token for
// the given profile — used to show the source in 'auth status'.
func findTokenFile(v *viper.Viper, profileName string, paths []string) string {
	key := "profiles." + profileName + ".token"
	// Walk paths highest → lowest; first file with the key wins display
	for i := len(paths) - 1; i >= 0; i-- {
		fv := viper.New()
		fv.SetConfigType("toml")
		if f, err := os.Open(paths[i]); err == nil {
			_ = fv.ReadConfig(f)
			f.Close()
			if fv.GetString(key) != "" {
				return paths[i]
			}
		}
	}
	return ""
}

// --- error types -------------------------------------------------------------

// ProfileNotFoundError is returned when an explicitly requested profile
// does not exist in any config file.
type ProfileNotFoundError struct {
	Profile string
}

func (e *ProfileNotFoundError) Error() string {
	return fmt.Sprintf("profile %q not found in any config file", e.Profile)
}

// MissingTokenError is returned by RequireToken when no token is configured.
type MissingTokenError struct{}

func (e *MissingTokenError) Error() string {
	return "no API token found — run 'copera auth login' or set COPERA_CLI_AUTH_TOKEN"
}
