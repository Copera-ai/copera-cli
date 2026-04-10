package commands

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"io"

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

// loginPasteModeSentinel is the value `cmd.Flags().Lookup("token")` returns
// when the user passed bare `--token` with no value. It's a sentinel that
// cannot collide with a real token (which always starts with `cp_`).
const loginPasteModeSentinel = "__paste_mode__"

// loginMode selects between the three sub-flows of `copera auth login`.
type loginMode int

const (
	// loginModeBrowser is the default: print the URL, try to open the
	// browser, then prompt for the token paste. This is the new PAT-
	// friendly flow.
	loginModeBrowser loginMode = iota

	// loginModePasteOnly skips the browser and goes straight to the
	// masked paste prompt — identical to the legacy interactive flow.
	// Triggered by `--token` with no value. Useful in WSL/SSH where the
	// user already has a token in hand and doesn't want us to launch
	// xdg-open.
	loginModePasteOnly

	// loginModeDirect saves the flag value directly. No browser, no
	// prompts. Triggered by `--token=<value>`. Useful for scripts, CI,
	// and LLM agents.
	loginModeDirect
)

func newAuthLoginCmd(cli *CLI) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Set up credentials (opens the browser by default)",
		Long: `Authenticate the Copera CLI.

Default behavior:
  copera auth login                 Opens your browser to the OAuth-style PAT
                                    creation page, then waits for you to paste
                                    the generated token back at the prompt.

Escape hatches:
  copera auth login --token=VALUE   Save VALUE directly. No browser, no prompts
                                    (beyond profile/save-location in interactive
                                    mode). Ideal for scripts, CI, and agents.

  copera auth login --token         Skip the browser and drop straight into
                                    the masked paste prompt (same UX as the
                                    old CLI). Useful in WSL/SSH where you
                                    already have a token.

Non-interactive fallback:
  COPERA_CLI_AUTH_TOKEN=<token>     Set this environment variable and any
                                    command (including 'auth login') picks it
                                    up without touching the token files.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := resolveLoginMode(cmd)

			// The only mode that tolerates non-interactive terminals is
			// loginModeDirect — the token arrives via a flag so we don't
			// need TTY input. Browser and paste modes both need a prompt,
			// so gate them behind the interactivity check.
			if mode != loginModeDirect && cli.IsNonInteractive() {
				cli.Printer.PrintError(
					"non_interactive",
					"auth login requires an interactive terminal",
					"Use 'copera auth login --token=<value>' or set COPERA_CLI_AUTH_TOKEN",
					false,
				)
				return exitcodes.Newf(exitcodes.Usage,
					"use --token=<value>, set COPERA_CLI_AUTH_TOKEN, or run this command in a terminal")
			}

			switch mode {
			case loginModeDirect:
				token := cmd.Flag("token").Value.String()
				return runAuthLoginDirect(cli, token)
			case loginModePasteOnly:
				return runAuthLoginPasteOnly(cli)
			default:
				return runAuthLoginBrowser(cli)
			}
		},
	}

	// Local --token flag shadows the persistent --token from root.
	// Cobra's Command.mergePersistentFlags() only copies a parent's
	// persistent flag into a child's flag set when the child doesn't
	// already have a flag of the same name — so this registration wins
	// within the `auth login` scope and the root's binding (which would
	// otherwise send the token through cli.flags.token) is bypassed here.
	cmd.Flags().String(
		"token",
		"",
		"Save this token directly (skips browser). Use bare --token for paste-only mode.",
	)
	cmd.Flags().Lookup("token").NoOptDefVal = loginPasteModeSentinel

	return cmd
}

// resolveLoginMode inspects the `--token` flag state and maps it to one of
// the three login sub-flows.
func resolveLoginMode(cmd *cobra.Command) loginMode {
	f := cmd.Flag("token")
	if f == nil || !f.Changed {
		return loginModeBrowser
	}
	if f.Value.String() == loginPasteModeSentinel {
		return loginModePasteOnly
	}
	return loginModeDirect
}

// runAuthLoginBrowser is the default login flow: print the URL, attempt to
// open the browser, then prompt the user to paste the token back.
func runAuthLoginBrowser(cli *CLI) error {
	r := bufio.NewReader(cli.Stdin)

	// Pull the web URL from config so we point at the right host
	// (prod vs sandbox vs user override). A missing token here is
	// expected — we're literally in the process of setting one — so
	// swallow the MissingTokenError.
	webURL := resolveWebURL(cli)
	loginURL := strings.TrimRight(webURL, "/") + "/oauth/cli"

	// ALWAYS print the URL first. In WSL/SSH/headless/containers the
	// browser launch may succeed silently without doing anything useful,
	// or fail silently — either way, the user's only reliable fallback
	// is to read the URL from the terminal and open it themselves.
	fmt.Fprintln(cli.Printer.Out)
	fmt.Fprintln(cli.Printer.Out, "To authenticate the Copera CLI, open this URL in your browser:")
	fmt.Fprintln(cli.Printer.Out)
	fmt.Fprintf(cli.Printer.Out, "    %s\n", loginURL)
	fmt.Fprintln(cli.Printer.Out)
	fmt.Fprintln(cli.Printer.Out, "Select a workspace, create a token, then paste it below.")
	fmt.Fprintln(cli.Printer.Out)

	// Best-effort browser launch — intentionally ignore the error.
	_ = auth.OpenURL(loginURL)

	return promptTokenAndSave(cli, r)
}

// runAuthLoginPasteOnly skips the browser URL banner and drops straight
// into the profile/paste/save-location prompts. Equivalent to the legacy
// `copera auth login` flow.
func runAuthLoginPasteOnly(cli *CLI) error {
	r := bufio.NewReader(cli.Stdin)
	return promptTokenAndSave(cli, r)
}

// runAuthLoginDirect saves the given token verbatim. Skips the browser
// launch AND the masked paste prompt — the caller already has the token.
// In interactive terminals we still ask for the profile name and save
// location; in non-interactive we use safe defaults (`default` profile,
// `~/.copera.toml`).
func runAuthLoginDirect(cli *CLI, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		cli.Printer.PrintError("invalid_input", "token cannot be empty", "", false)
		return exitcodes.Newf(exitcodes.Usage, "token cannot be empty")
	}

	vals := config.ProfileValues{Token: token}

	if cli.IsNonInteractive() {
		// Non-interactive + --token=<value>: save to home with default profile.
		homeDir, err := os.UserHomeDir()
		if err != nil {
			cli.Printer.PrintError("home_dir_error", err.Error(), "", false)
			return exitcodes.New(exitcodes.Error, err)
		}
		savePath := filepath.Join(homeDir, ".copera.toml")
		if err := config.WriteProfile(savePath, "default", vals); err != nil {
			cli.Printer.PrintError("write_error", err.Error(), "", false)
			return exitcodes.New(exitcodes.Error, err)
		}
		cli.Printer.Info("✓ Credentials saved to %s (profile: default)", savePath)
		cli.Printer.Info("  Token: %s", auth.MaskToken(token))
		return nil
	}

	// Interactive: still ask for profile name + save location so users
	// can distinguish multiple accounts on the same machine.
	r := bufio.NewReader(cli.Stdin)

	profileName := promptProfileName(cli, r)
	savePath, choice := promptSaveLocation(cli, r)

	if err := config.WriteProfile(savePath, profileName, vals); err != nil {
		cli.Printer.PrintError("write_error", err.Error(), "", false)
		return exitcodes.New(exitcodes.Error, err)
	}

	if choice == "2" {
		cwd, _ := os.Getwd()
		offerGitignore(r, cli, cwd)
	}

	cli.Printer.Info("✓ Credentials saved to %s (profile: %s)", savePath, profileName)
	cli.Printer.Info("  Token: %s", auth.MaskToken(token))
	return nil
}

// promptTokenAndSave runs the shared "profile name → masked paste → save
// location → write → .gitignore" sequence used by both browser and
// paste-only flows.
func promptTokenAndSave(cli *CLI, r *bufio.Reader) error {
	// Masked paste prompt — ask for the token FIRST so users can paste
	// immediately after copying from the browser.
	token, err := readMaskedInput(cli.Printer.Out, "API token: ")
	if err != nil {
		// Fallback for non-TTY stdin (piped token in test environments).
		fmt.Fprintf(cli.Printer.Out, "API token: ")
		tokenInput, _ := r.ReadString('\n')
		token = strings.TrimSpace(tokenInput)
	}
	if token == "" {
		cli.Printer.PrintError("invalid_input", "token cannot be empty", "", false)
		return exitcodes.Newf(exitcodes.Usage, "token cannot be empty")
	}

	profileName := promptProfileName(cli, r)

	vals := config.ProfileValues{Token: token}

	savePath, choice := promptSaveLocation(cli, r)

	if err := config.WriteProfile(savePath, profileName, vals); err != nil {
		cli.Printer.PrintError("write_error", err.Error(), "", false)
		return exitcodes.New(exitcodes.Error, err)
	}

	if choice == "2" {
		cwd, _ := os.Getwd()
		offerGitignore(r, cli, cwd)
	}

	cli.Printer.Info("✓ Credentials saved to %s (profile: %s)", savePath, profileName)
	cli.Printer.Info("  Token: %s", auth.MaskToken(token))
	return nil
}

func promptProfileName(cli *CLI, r *bufio.Reader) string {
	fmt.Fprintf(cli.Printer.Out, "Profile name [default]: ")
	profileInput, _ := r.ReadString('\n')
	profileName := strings.TrimSpace(profileInput)
	if profileName == "" {
		profileName = "default"
	}
	return profileName
}

func promptSaveLocation(cli *CLI, r *bufio.Reader) (savePath, choice string) {
	homeDir, _ := os.UserHomeDir()
	cwd, _ := os.Getwd()

	fmt.Fprintln(cli.Printer.Out, "\nWhere should credentials be saved?")
	fmt.Fprintln(cli.Printer.Out, "  1) ~/.copera.toml           (personal, all projects)")
	fmt.Fprintln(cli.Printer.Out, "  2) .copera.local.toml       (this project only, add to .gitignore)")
	fmt.Fprintln(cli.Printer.Out, "  3) .copera.toml             (this project, committed — no tokens!)")
	fmt.Fprintf(cli.Printer.Out, "Choice [1]: ")

	choiceInput, _ := r.ReadString('\n')
	choice = strings.TrimSpace(choiceInput)

	switch choice {
	case "2":
		savePath = filepath.Join(cwd, ".copera.local.toml")
	case "3":
		savePath = filepath.Join(cwd, ".copera.toml")
	default:
		savePath = filepath.Join(homeDir, ".copera.toml")
	}
	return savePath, choice
}

// resolveWebURL returns the Copera web app base URL (not the REST API) for
// the current profile. It loads config silently and tolerates any error —
// `auth login` should never fail just because the user has a malformed
// config or no token yet (the whole point of login is to SET a token). On
// any error or missing value we fall back to the prod default.
// readMaskedInput prints the prompt, reads from stdin char by char in raw
// terminal mode, echoing '*' for each character. On Enter it rewrites the
// line with the masked representation (e.g. "*******ab1f"). Returns the
// clear-text token string.
func readMaskedInput(w io.Writer, prompt string) (string, error) {
	fd := int(os.Stdin.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return "", err
	}
	defer term.Restore(fd, oldState)

	fmt.Fprint(w, prompt)

	var buf []byte
	b := make([]byte, 1)
	for {
		if _, err := os.Stdin.Read(b); err != nil {
			return "", err
		}
		switch {
		case b[0] == '\n' || b[0] == '\r':
			token := strings.TrimSpace(string(buf))
			fmt.Fprint(w, "\r\n")
			return token, nil
		case b[0] == 127 || b[0] == 8: // backspace / delete
			if len(buf) > 0 {
				buf = buf[:len(buf)-1]
				fmt.Fprint(w, "\b \b")
			}
		case b[0] == 3: // Ctrl-C
			fmt.Fprint(w, "\r\n")
			return "", fmt.Errorf("interrupted")
		case b[0] >= 32: // printable
			buf = append(buf, b[0])
			fmt.Fprint(w, "*")
		}
	}
}

func resolveWebURL(cli *CLI) string {
	cfg, _ := cli.LoadConfig()
	if cfg != nil && cfg.API.WebURL != "" {
		return cfg.API.WebURL
	}
	return "https://app.copera.ai"
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
