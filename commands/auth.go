package commands

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/copera/copera-cli/internal/auth"
	"github.com/copera/copera-cli/internal/config"
	"github.com/copera/copera-cli/internal/exitcodes"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newAuthCmd(cli *CLI) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage authentication",
	}
	cmd.AddCommand(
		newAuthLoginCmd(cli),
		newAuthStatusCmd(cli),
		newAuthLogoutCmd(cli),
	)
	return cmd
}

// auth login ─────────────────────────────────────────────────────────────────

func newAuthLoginCmd(cli *CLI) *cobra.Command {
	return &cobra.Command{
		Use:   "login",
		Short: "Set up credentials interactively",
		Long: `Set up API credentials interactively.

For non-interactive environments (CI, agents), set the environment variable:
  export COPERA_CLI_AUTH_TOKEN=<your-token>`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if cli.IsNonInteractive() {
				cli.Printer.PrintError(
					"non_interactive",
					"auth login requires an interactive terminal",
					"Set COPERA_CLI_AUTH_TOKEN environment variable instead",
					false,
				)
				return exitcodes.Newf(exitcodes.Usage,
					"set COPERA_CLI_AUTH_TOKEN or run this command in a terminal")
			}
			return runAuthLogin(cli)
		},
	}
}

func runAuthLogin(cli *CLI) error {
	r := bufio.NewReader(cli.Stdin)

	// 1. Profile name
	fmt.Fprintf(cli.Printer.Out, "Profile name [default]: ")
	profileInput, _ := r.ReadString('\n')
	profileName := strings.TrimSpace(profileInput)
	if profileName == "" {
		profileName = "default"
	}

	// 2. Token (masked)
	fmt.Fprintf(cli.Printer.Out, "API token: ")
	tokenBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(cli.Printer.Out) // newline after hidden input
	if err != nil {
		// Fallback for non-TTY stdin (e.g. piped token in test environments)
		fmt.Fprintf(cli.Printer.Out, "API token: ")
		tokenInput, _ := r.ReadString('\n')
		tokenBytes = []byte(strings.TrimSpace(tokenInput))
	}
	token := strings.TrimSpace(string(tokenBytes))
	if token == "" {
		cli.Printer.PrintError("invalid_input", "token cannot be empty", "", false)
		return exitcodes.Newf(exitcodes.Usage, "token cannot be empty")
	}

	vals := config.ProfileValues{Token: token}

	// 3. Save location
	homeDir, _ := os.UserHomeDir()
	cwd, _ := os.Getwd()

	fmt.Fprintln(cli.Printer.Out, "\nWhere should credentials be saved?")
	fmt.Fprintln(cli.Printer.Out, "  1) ~/.copera.toml           (personal, all projects)")
	fmt.Fprintln(cli.Printer.Out, "  2) .copera.local.toml       (this project only, add to .gitignore)")
	fmt.Fprintln(cli.Printer.Out, "  3) .copera.toml             (this project, committed — no tokens!)")
	fmt.Fprintf(cli.Printer.Out, "Choice [1]: ")

	choiceInput, _ := r.ReadString('\n')
	choice := strings.TrimSpace(choiceInput)

	var savePath string
	switch choice {
	case "2":
		savePath = filepath.Join(cwd, ".copera.local.toml")
	case "3":
		savePath = filepath.Join(cwd, ".copera.toml")
	default:
		savePath = filepath.Join(homeDir, ".copera.toml")
	}

	// 5. Write config
	if err := config.WriteProfile(savePath, profileName, vals); err != nil {
		cli.Printer.PrintError("write_error", err.Error(), "", false)
		return exitcodes.New(exitcodes.Error, err)
	}

	// 6. Offer to add .copera.local.toml to .gitignore
	if choice == "2" {
		offerGitignore(r, cli, cwd)
	}

	cli.Printer.Info("✓ Credentials saved to %s (profile: %s)", savePath, profileName)
	cli.Printer.Info("  Token: %s", auth.MaskToken(token))
	return nil
}

func offerGitignore(r *bufio.Reader, cli *CLI, cwd string) {
	gitignorePath := filepath.Join(cwd, ".gitignore")
	entry := ".copera.local.toml"

	// Check if already present
	if data, err := os.ReadFile(gitignorePath); err == nil {
		if strings.Contains(string(data), entry) {
			return
		}
	}

	fmt.Fprintf(cli.Printer.Out, "\nAdd .copera.local.toml to .gitignore? [Y/n]: ")
	ans, _ := r.ReadString('\n')
	ans = strings.TrimSpace(strings.ToLower(ans))
	if ans == "" || ans == "y" || ans == "yes" {
		f, err := os.OpenFile(gitignorePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err == nil {
			fmt.Fprintln(f, entry)
			f.Close()
			cli.Printer.Info("  Added .copera.local.toml to .gitignore")
		}
	}
}

// auth status ─────────────────────────────────────────────────────────────────

func newAuthStatusCmd(cli *CLI) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show current authentication status",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := cli.LoadConfig()
			if err != nil {
				return authConfigError(cli, err)
			}

			type statusOut struct {
				Profile     string `json:"profile"`
				TokenSource string `json:"token_source"`
				Token       string `json:"token"`
				Configured  bool   `json:"configured"`
			}
			out := statusOut{
				Profile:     cfg.Profile,
				TokenSource: cfg.TokenSource.String(),
				Configured:  cfg.Token != "",
			}
			if cfg.Token != "" {
				out.Token = auth.MaskToken(cfg.Token)
			}

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(out)
			}

			if !out.Configured {
				cli.Printer.Info("Not authenticated. Run 'copera auth login' or set COPERA_CLI_AUTH_TOKEN.")
				return nil
			}
			cli.Printer.PrintLine(fmt.Sprintf("Profile:  %s", out.Profile))
			cli.Printer.PrintLine(fmt.Sprintf("Token:    %s", out.Token))
			cli.Printer.PrintLine(fmt.Sprintf("Source:   %s", out.TokenSource))
			return nil
		},
	}
}

// auth logout ─────────────────────────────────────────────────────────────────

func newAuthLogoutCmd(cli *CLI) *cobra.Command {
	var flagForce bool
	cmd := &cobra.Command{
		Use:   "logout",
		Short: "Remove stored credentials",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := cli.LoadConfig()
			if err != nil {
				return authConfigError(cli, err)
			}

			if cfg.TokenSource.Kind != "file" {
				cli.Printer.PrintError(
					"no_stored_token",
					"no token stored in a config file for this profile",
					"Token may be set via COPERA_CLI_AUTH_TOKEN or --token flag",
					false,
				)
				return exitcodes.Newf(exitcodes.Usage, "no stored token to remove")
			}

			if !flagForce && !cli.IsNonInteractive() {
				r := bufio.NewReader(cli.Stdin)
				fmt.Fprintf(cli.Printer.Out,
					"Remove token for profile %q from %s? [y/N]: ",
					cfg.Profile, cfg.TokenSource.Path)
				ans, _ := r.ReadString('\n')
				if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(ans)), "y") {
					cli.Printer.Info("Aborted.")
					return nil
				}
			}

			if err := config.DeleteToken(cfg.TokenSource.Path, cfg.Profile); err != nil {
				cli.Printer.PrintError("write_error", err.Error(), "", false)
				return exitcodes.New(exitcodes.Error, err)
			}

			cli.Printer.Info("Token removed from %s (profile: %s)", cfg.TokenSource.Path, cfg.Profile)
			return nil
		},
	}
	cmd.Flags().BoolVar(&flagForce, "force", false, "Skip confirmation prompt")
	return cmd
}

// --- helpers -----------------------------------------------------------------

func authConfigError(cli *CLI, err error) error {
	var pnf *config.ProfileNotFoundError
	if errors.As(err, &pnf) {
		cli.Printer.PrintError(
			"unknown_profile",
			err.Error(),
			fmt.Sprintf("Run 'copera auth login' to create profile %q", pnf.Profile),
			false,
		)
		return exitcodes.New(exitcodes.Usage, err)
	}
	cli.Printer.PrintError("config_error", err.Error(), "", false)
	return exitcodes.New(exitcodes.Error, err)
}
