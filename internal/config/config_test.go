package config_test

import (
	"path/filepath"
	"testing"

	"github.com/copera/copera-cli/internal/config"
	"github.com/copera/copera-cli/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- profile resolution ------------------------------------------------------

func TestDefaultProfileUsedWhenNothingSet(t *testing.T) {
	dir := t.TempDir()
	testutil.WriteTempConfigAt(t, filepath.Join(dir, ".copera.toml"), `
[profiles.default]
token = "tok_default"
`)
	cfg, err := config.Load(config.LoadOpts{CWD: dir, HomeDir: dir})
	require.NoError(t, err)
	assert.Equal(t, "default", cfg.Profile)
	assert.Equal(t, "tok_default", cfg.Token)
}

func TestFlagProfileSelectsCorrectSection(t *testing.T) {
	dir := t.TempDir()
	testutil.WriteTempConfigAt(t, filepath.Join(dir, ".copera.toml"), `
[profiles.default]
token = "tok_default"

[profiles.work]
token      = "tok_work"
board_id   = "board_work"
channel_id = "chan_work"
`)
	cfg, err := config.Load(config.LoadOpts{CWD: dir, HomeDir: dir, FlagProfile: "work"})
	require.NoError(t, err)
	assert.Equal(t, "work", cfg.Profile)
	assert.Equal(t, "tok_work", cfg.Token)
	assert.Equal(t, "board_work", cfg.BoardID)
	assert.Equal(t, "chan_work", cfg.ChannelID)
}

func TestEnvProfileSelectsCorrectSection(t *testing.T) {
	dir := t.TempDir()
	testutil.SetEnv(t, "COPERA_PROFILE", "work")
	testutil.WriteTempConfigAt(t, filepath.Join(dir, ".copera.toml"), `
[profiles.default]
token = "tok_default"
[profiles.work]
token = "tok_work"
`)
	cfg, err := config.Load(config.LoadOpts{CWD: dir, HomeDir: dir})
	require.NoError(t, err)
	assert.Equal(t, "work", cfg.Profile)
	assert.Equal(t, "tok_work", cfg.Token)
}

func TestFlagProfileOverridesEnvProfile(t *testing.T) {
	dir := t.TempDir()
	testutil.SetEnv(t, "COPERA_PROFILE", "work")
	testutil.WriteTempConfigAt(t, filepath.Join(dir, ".copera.toml"), `
[profiles.work]
token = "tok_work"
[profiles.staging]
token = "tok_staging"
`)
	cfg, err := config.Load(config.LoadOpts{CWD: dir, HomeDir: dir, FlagProfile: "staging"})
	require.NoError(t, err)
	assert.Equal(t, "staging", cfg.Profile)
	assert.Equal(t, "tok_staging", cfg.Token)
}

func TestUnknownExplicitProfileReturnsError(t *testing.T) {
	dir := t.TempDir()
	testutil.WriteTempConfigAt(t, filepath.Join(dir, ".copera.toml"), `
[profiles.default]
token = "tok_default"
`)
	_, err := config.Load(config.LoadOpts{CWD: dir, HomeDir: dir, FlagProfile: "nonexistent"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nonexistent")

	var pnf *config.ProfileNotFoundError
	require.ErrorAs(t, err, &pnf)
	assert.Equal(t, "nonexistent", pnf.Profile)
}

// Missing default profile is not an error — user just hasn't configured yet.
func TestMissingDefaultProfileReturnsEmptyConfig(t *testing.T) {
	dir := t.TempDir()
	cfg, err := config.Load(config.LoadOpts{CWD: dir, HomeDir: dir})
	require.NoError(t, err)
	assert.Equal(t, "default", cfg.Profile)
	assert.Empty(t, cfg.Token)
}

// --- token resolution --------------------------------------------------------

func TestEnvVarTokenOverridesProfileToken(t *testing.T) {
	dir := t.TempDir()
	testutil.SetEnv(t, "COPERA_CLI_AUTH_TOKEN", "tok_from_env")
	testutil.WriteTempConfigAt(t, filepath.Join(dir, ".copera.toml"), `
[profiles.default]
token = "tok_from_file"
`)
	cfg, err := config.Load(config.LoadOpts{CWD: dir, HomeDir: dir})
	require.NoError(t, err)
	assert.Equal(t, "tok_from_env", cfg.Token)
	assert.Equal(t, "env", cfg.TokenSource.Kind)
}

func TestFlagTokenOverridesProfileToken(t *testing.T) {
	dir := t.TempDir()
	testutil.UnsetEnv(t, "COPERA_CLI_AUTH_TOKEN")
	testutil.WriteTempConfigAt(t, filepath.Join(dir, ".copera.toml"), `
[profiles.default]
token = "tok_from_file"
`)
	cfg, err := config.Load(config.LoadOpts{CWD: dir, HomeDir: dir, FlagToken: "tok_from_flag"})
	require.NoError(t, err)
	assert.Equal(t, "tok_from_flag", cfg.Token)
	assert.Equal(t, "flag", cfg.TokenSource.Kind)
}

func TestMissingTokenRequireTokenReturnsError(t *testing.T) {
	dir := t.TempDir()
	testutil.UnsetEnv(t, "COPERA_CLI_AUTH_TOKEN")
	cfg, err := config.Load(config.LoadOpts{CWD: dir, HomeDir: dir})
	require.NoError(t, err)
	assert.Empty(t, cfg.Token)

	err = cfg.RequireToken()
	require.Error(t, err)
	var missing *config.MissingTokenError
	assert.ErrorAs(t, err, &missing)
}

// --- file precedence ---------------------------------------------------------

func TestLocalFileOverridesCwdFile(t *testing.T) {
	dir := t.TempDir()
	testutil.UnsetEnv(t, "COPERA_CLI_AUTH_TOKEN")
	testutil.WriteTempConfigAt(t, filepath.Join(dir, ".copera.toml"), `
[profiles.default]
token    = "tok_committed"
board_id = "board_committed"
`)
	testutil.WriteTempConfigAt(t, filepath.Join(dir, ".copera.local.toml"), `
[profiles.default]
token = "tok_local"
`)
	cfg, err := config.Load(config.LoadOpts{CWD: dir, HomeDir: dir})
	require.NoError(t, err)
	assert.Equal(t, "tok_local", cfg.Token)
	// board_id from .copera.toml is preserved — local only overrides token
	assert.Equal(t, "board_committed", cfg.BoardID)
}

func TestCwdFileOverridesHomeFile(t *testing.T) {
	homeDir := t.TempDir()
	cwdDir := t.TempDir()
	testutil.UnsetEnv(t, "COPERA_CLI_AUTH_TOKEN")
	testutil.WriteTempConfigAt(t, filepath.Join(homeDir, ".copera.toml"), `
[profiles.default]
token    = "tok_home"
board_id = "board_home"
`)
	testutil.WriteTempConfigAt(t, filepath.Join(cwdDir, ".copera.toml"), `
[profiles.default]
token = "tok_cwd"
`)
	cfg, err := config.Load(config.LoadOpts{CWD: cwdDir, HomeDir: homeDir})
	require.NoError(t, err)
	assert.Equal(t, "tok_cwd", cfg.Token)
	// board_id from home file is preserved
	assert.Equal(t, "board_home", cfg.BoardID)
}

// --- global settings ---------------------------------------------------------

func TestGlobalOutputSettingsApplyAcrossProfiles(t *testing.T) {
	dir := t.TempDir()
	testutil.WriteTempConfigAt(t, filepath.Join(dir, ".copera.toml"), `
[output]
format = "json"
color  = "never"

[profiles.default]
token = "tok_x"

[profiles.work]
token = "tok_work"
`)
	for _, profile := range []string{"default", "work"} {
		cfg, err := config.Load(config.LoadOpts{CWD: dir, HomeDir: dir, FlagProfile: profile})
		require.NoError(t, err, "profile: %s", profile)
		assert.Equal(t, "json", cfg.Output.Format, "profile: %s", profile)
		assert.Equal(t, "never", cfg.Output.Color, "profile: %s", profile)
	}
}

func TestDefaultsPopulatedWhenFieldsAbsent(t *testing.T) {
	dir := t.TempDir()
	cfg, err := config.Load(config.LoadOpts{CWD: dir, HomeDir: dir})
	require.NoError(t, err)
	assert.Equal(t, "auto", cfg.Output.Format)
	assert.Equal(t, "auto", cfg.Output.Color)
	assert.Equal(t, "https://api.copera.ai/public/v1", cfg.API.BaseURL)
}

// --- no config files ---------------------------------------------------------

func TestNoConfigFilesReturnsDefaultConfig(t *testing.T) {
	dir := t.TempDir()
	testutil.UnsetEnv(t, "COPERA_CLI_AUTH_TOKEN")
	testutil.UnsetEnv(t, "COPERA_PROFILE")

	cfg, err := config.Load(config.LoadOpts{CWD: dir, HomeDir: dir})
	require.NoError(t, err)
	assert.Equal(t, "default", cfg.Profile)
	assert.Empty(t, cfg.Token)
	assert.NotEmpty(t, cfg.API.BaseURL)
}

// --- write profile -----------------------------------------------------------

func TestWriteProfileCreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".copera.toml")

	err := config.WriteProfile(path, "default", config.ProfileValues{
		Token:   "tok_new",
		BoardID: "board_new",
	})
	require.NoError(t, err)

	cfg, err := config.Load(config.LoadOpts{CWD: dir, HomeDir: dir})
	require.NoError(t, err)
	assert.Equal(t, "tok_new", cfg.Token)
	assert.Equal(t, "board_new", cfg.BoardID)
}

func TestWriteProfilePreservesOtherProfiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".copera.toml")

	require.NoError(t, config.WriteProfile(path, "default", config.ProfileValues{Token: "tok_default"}))
	require.NoError(t, config.WriteProfile(path, "work", config.ProfileValues{Token: "tok_work"}))

	cfg, err := config.Load(config.LoadOpts{CWD: dir, HomeDir: dir, FlagProfile: "default"})
	require.NoError(t, err)
	assert.Equal(t, "tok_default", cfg.Token)

	cfg, err = config.Load(config.LoadOpts{CWD: dir, HomeDir: dir, FlagProfile: "work"})
	require.NoError(t, err)
	assert.Equal(t, "tok_work", cfg.Token)
}

func TestDeleteTokenRemovesTokenOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".copera.toml")

	require.NoError(t, config.WriteProfile(path, "default", config.ProfileValues{
		Token:   "tok_to_delete",
		BoardID: "board_keep",
	}))

	require.NoError(t, config.DeleteToken(path, "default"))

	testutil.UnsetEnv(t, "COPERA_CLI_AUTH_TOKEN")
	cfg, err := config.Load(config.LoadOpts{CWD: dir, HomeDir: dir})
	require.NoError(t, err)
	assert.Empty(t, cfg.Token)
	assert.Equal(t, "board_keep", cfg.BoardID)
}

