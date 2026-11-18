package commands

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/copera/copera-cli/internal/build"
	"github.com/copera/copera-cli/internal/exitcodes"
	"github.com/spf13/cobra"
)

//go:embed connect_skill/SKILL.md
var connectSkill string

var connectAllowedCommands = []string{
	"auth status",
	"designs check",
	"designs status",
	"designs diff",
	"designs log",
	"designs list",
	"designs get",
	"designs versions",
	"designs guide",
	"designs screenshot",
}

type connectTool struct {
	name      string
	binaries  []string
	directory string
	override  string
}

var connectTools = []connectTool{
	{"claude-code", []string{"claude"}, ".claude", "CLAUDE_CONFIG_DIR"},
	{"codex", []string{"codex"}, ".codex", "CODEX_HOME"},
	{"cursor", []string{"agent", "cursor-agent"}, ".cursor", ""},
	{"opencode", []string{"opencode"}, filepath.Join(".config", "opencode"), ""},
}

func findConnectTool(name string) (connectTool, error) {
	if name == "claude" {
		name = "claude-code"
	}
	for _, tool := range connectTools {
		if tool.name == name {
			return tool, nil
		}
	}
	return connectTool{}, exitcodes.Newf(exitcodes.Usage, "unknown tool %q; use claude-code, codex, cursor, or opencode", name)
}

func (t connectTool) detected(home string) bool {
	for _, binary := range t.binaries {
		if _, err := exec.LookPath(binary); err == nil {
			return true
		}
	}
	for _, path := range []string{filepath.Join(home, t.directory), os.Getenv(t.override)} {
		if filepath.IsAbs(path) {
			if info, err := os.Stat(path); err == nil && info.IsDir() {
				return true
			}
		}
	}
	return false
}

func (t connectTool) configDir(home string) (string, error) {
	dir := os.Getenv(t.override)
	if dir == "" {
		return filepath.Join(home, t.directory), nil
	}
	if !filepath.IsAbs(dir) {
		return "", exitcodes.Newf(exitcodes.Usage, "%s must be an absolute user configuration path", t.override)
	}
	dir = filepath.Clean(dir)
	if cwd, err := os.Getwd(); err == nil && cwd != home && cwd != filepath.Dir(cwd) {
		if relative, err := filepath.Rel(cwd, dir); err == nil && filepath.IsLocal(relative) {
			return "", exitcodes.Newf(exitcodes.Usage, "%s points inside the working directory: %s", t.override, cwd)
		}
	}
	for parent := dir; parent != home && parent != filepath.Dir(parent); parent = filepath.Dir(parent) {
		for _, marker := range []string{".git", filepath.Join(".copera", "design.json")} {
			if _, err := os.Lstat(filepath.Join(parent, marker)); err == nil {
				return "", exitcodes.Newf(exitcodes.Usage, "%s points inside a project: %s", t.override, parent)
			}
		}
	}
	return dir, nil
}

func connectSkillPath(dir string) string {
	return filepath.Join(dir, "skills", connectSkillDir, "SKILL.md")
}

func (t connectTool) skill(home string) (string, []byte, error) {
	shared := connectSkillPath(filepath.Join(home, ".agents"))
	if t.name != "claude-code" {
		return shared, []byte(connectSkill), nil
	}
	dir, err := t.configDir(home)
	if err != nil {
		return "", nil, err
	}
	path := connectSkillPath(dir)
	if strings.EqualFold(path, shared) {
		return "", nil, exitcodes.Newf(exitcodes.Conflict, "Claude Code needs its own skill path: %s", path)
	}
	var allowed strings.Builder
	allowed.WriteString("allowed-tools:")
	for _, command := range connectAllowedCommands {
		fmt.Fprintf(&allowed, " Bash(copera %s *)", command)
	}
	text := strings.Replace(connectSkill, "\n---\n", "\n"+allowed.String()+"\n---\n", 1)
	return path, []byte(text), nil
}

func (t connectTool) ownsInstallPath(home, path string) bool {
	if !filepath.IsAbs(path) || path != filepath.Clean(path) {
		return false
	}
	shared := connectSkillPath(filepath.Join(home, ".agents"))
	if t.name == "claude-code" {
		return !strings.EqualFold(path, shared) && path == connectSkillPath(filepath.Dir(filepath.Dir(filepath.Dir(path))))
	}
	if path == shared {
		return true
	}
	return t.name == "codex" && path == filepath.Join(filepath.Dir(filepath.Dir(path)), "rules", "copera.rules")
}

func connectCodexRules() []byte {
	var rules strings.Builder
	for _, command := range connectAllowedCommands {
		words := strings.Split(command, " ")
		fmt.Fprintf(&rules, "prefix_rule(pattern=[%q, %q, %q], decision=\"allow\")\n", "copera", words[0], words[1])
	}
	return []byte(rules.String())
}

type connectResult struct {
	Tool        string            `json:"tool"`
	Detected    bool              `json:"detected"`
	Connected   bool              `json:"connected"`
	DryRun      bool              `json:"dry_run,omitempty"`
	Files       []connectFilePlan `json:"files"`
	PreApproval string            `json:"pre_approval,omitempty"`
	NextStep    string            `json:"next_step,omitempty"`
	SignIn      string            `json:"sign_in,omitempty"`
}

func newConnectCmd(cli *CLI) *cobra.Command {
	var yes, dryRun, list bool
	cmd := &cobra.Command{
		Use:   "connect [tool]",
		Short: "Install a user-level Copera Designs skill or list supported tools",
		Args:  connectArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if list && len(args) != 0 {
				return reportConnectError(cli, exitcodes.Newf(exitcodes.Usage, "--list does not take a tool"))
			}
			home, err := connectHome(cli.homeDir)
			if err != nil {
				return reportConnectError(cli, err)
			}
			if len(args) == 0 {
				return listConnectTools(cli, home)
			}
			tool, err := findConnectTool(args[0])
			if err != nil {
				return reportConnectError(cli, err)
			}
			return runConnect(cli, home, tool, yes, dryRun)
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show the plan without writing files")
	cmd.Flags().BoolVar(&list, "list", false, "List detected and connected tools")
	return cmd
}

func newDisconnectCmd(cli *CLI) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "disconnect <tool>",
		Short: "Remove the Copera Designs skill and rules owned by Copera",
		Args:  connectArgs(1, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			home, err := connectHome(cli.homeDir)
			if err != nil {
				return reportConnectError(cli, err)
			}
			tool, err := findConnectTool(args[0])
			if err != nil {
				return reportConnectError(cli, err)
			}
			return runDisconnect(cli, home, tool, yes)
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation")
	return cmd
}

func connectArgs(min, max int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := cobra.RangeArgs(min, max)(cmd, args); err != nil {
			return exitcodes.New(exitcodes.Usage, err)
		}
		return nil
	}
}

func connectHome(home string) (string, error) {
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return "", err
		}
	}
	if !filepath.IsAbs(home) {
		return "", fmt.Errorf("home directory must be an absolute path: %s", home)
	}
	return filepath.Clean(home), nil
}

func runConnect(cli *CLI, home string, tool connectTool, yes, dryRun bool) error {
	result := connectResult{Tool: tool.name, Detected: tool.detected(home), DryRun: dryRun}
	if tool.name == "cursor" || tool.name == "opencode" {
		result.PreApproval = fmt.Sprintf("Pre-approval is unavailable for %s.", tool.name)
	}
	record, recordFile, recordErr := loadConnectRecord(home)
	entry, connected := record.Tools[tool.name]
	result.Connected = connected

	path, skill, err := tool.skill(home)
	if err != nil {
		return reportConnectError(cli, err)
	}
	result.Files = append(result.Files, connectFilePlan{Path: path, after: skill})
	if tool.name == "codex" {
		dir, err := tool.configDir(home)
		if err != nil {
			return reportConnectError(cli, err)
		}
		result.Files = append(result.Files, connectFilePlan{
			Path: filepath.Join(dir, "rules", "copera.rules"), after: connectCodexRules(),
		})
	}

	var problem error
	previousFiles := slices.Clone(entry.Files)
	for i := range result.Files {
		file := &result.Files[i]
		before, err := readConnectFile(home, file.Path)
		file.before = before
		switch {
		case err != nil:
			file.Action = "conflict"
			problem = errors.Join(problem, fmt.Errorf("%w: %s: %w", errConnectUnsafePath, file.Path, err))
		case before == nil:
			file.Action = "new"
		case !record.owns(file.Path, sha256Hex(before)):
			file.Action = "conflict"
			problem = errors.Join(problem, fmt.Errorf("%w: %s is not owned by Copera or was edited", errConnectUnsafePath, file.Path))
		case bytes.Equal(before, file.after):
			file.Action = "unchanged"
		default:
			file.Action = "update"
		}
		if file.Action == "conflict" {
			continue
		}
		owned := connectedFile{Path: file.Path, SHA256: sha256Hex(file.after)}
		record.updateSharedDigest(owned)
		i := slices.IndexFunc(entry.Files, func(file connectedFile) bool { return file.Path == owned.Path })
		if i == -1 {
			entry.Files = append(entry.Files, owned)
		} else {
			entry.Files[i] = owned
		}
	}
	if recordErr != nil {
		recordFile.Action = "conflict"
		result.Files = append(result.Files, recordFile)
		problem = errors.Join(problem, recordErr)
	} else if problem == nil {
		changed := !connected || entry.Version != build.Version || !slices.Equal(previousFiles, entry.Files)
		if changed {
			entry.Version, entry.ConnectedAt = build.Version, time.Now().UTC()
			record.Tools[tool.name] = entry
		}
		recordFile, err = planConnectRecord(recordFile, record, changed)
		if err != nil {
			return reportConnectError(cli, err)
		}
		result.Files = append(result.Files, recordFile)
	}
	if err := printConnectPlan(cli, result, !dryRun && problem == nil); err != nil {
		return err
	}
	if dryRun {
		return nil
	}
	if problem != nil {
		return reportConnectError(cli, problem)
	}
	ok, err := confirmConnectChange(cli, yes, "Install the Copera Designs skill?")
	if err != nil || !ok {
		if cli.Printer.IsJSON() {
			return errors.Join(err, cli.Printer.PrintJSON(result))
		}
		return err
	}
	if err := applyConnectPlan(home, result.Files); err != nil {
		return reportConnectError(cli, err)
	}
	result.Connected = true
	result.NextStep = fmt.Sprintf("Start %s and ask for a Copera Design.", tool.name)
	if cfg, err := cli.LoadConfig(); err != nil || cfg.RequireToken() != nil {
		result.SignIn = "Sign in first: copera auth login"
	}
	if cli.Printer.IsJSON() {
		return cli.Printer.PrintJSON(result)
	}
	cli.Printer.PrintLine(fmt.Sprintf("Connected %s.", tool.name))
	if result.SignIn != "" {
		cli.Printer.PrintLine(result.SignIn)
	}
	cli.Printer.PrintLine(result.NextStep)
	return nil
}

func printConnectPlan(cli *CLI, result connectResult, applying bool) error {
	printLine := cli.Printer.PrintLine
	if cli.Printer.IsJSON() {
		if !applying {
			return cli.Printer.PrintJSON(result)
		}
		printLine = func(line string) { fmt.Fprintln(cli.Printer.Err, line) }
	}
	if !result.Detected {
		printLine(fmt.Sprintf("%s was not detected.", result.Tool))
	}
	for _, file := range result.Files {
		printLine(fmt.Sprintf("%s  %s", file.Action, file.Path))
	}
	if result.PreApproval != "" {
		printLine(result.PreApproval)
	}
	return nil
}

func confirmConnectChange(cli *CLI, yes bool, prompt string) (bool, error) {
	if yes {
		return true, nil
	}
	if cli.IsNonInteractive() || !stderrIsTTY(cli.Printer.Out) {
		return false, reportConnectError(cli, exitcodes.Newf(exitcodes.Usage, "pass --yes to proceed non-interactively"))
	}
	fmt.Fprintf(cli.Printer.Err, "%s [y/N]: ", prompt)
	var answer string
	fmt.Fscanln(cli.Stdin, &answer)
	if strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes") {
		return true, nil
	}
	cli.Printer.Info("Aborted.")
	return false, nil
}

type connectToolStatus struct {
	Tool        string `json:"tool"`
	Detected    bool   `json:"detected"`
	Connected   bool   `json:"connected"`
	SkillStatus string `json:"skill_status"`
}

func listConnectTools(cli *CLI, home string) error {
	record, _, _ := loadConnectRecord(home)
	rows := make([]connectToolStatus, 0, len(connectTools))
	for _, tool := range connectTools {
		_, connected := record.Tools[tool.name]
		row := connectToolStatus{Tool: tool.name, Detected: tool.detected(home), Connected: connected, SkillStatus: "missing"}
		path, current, err := tool.skill(home)
		if err == nil {
			installed, err := readConnectFile(home, path)
			switch {
			case err != nil:
				row.SkillStatus = "changed"
			case installed == nil:
			case !record.owns(path, sha256Hex(installed)):
				row.SkillStatus = "changed"
			case bytes.Equal(installed, current):
				row.SkillStatus = "current"
			default:
				row.SkillStatus = "older"
			}
		}
		rows = append(rows, row)
	}
	if cli.Printer.IsJSON() {
		return cli.Printer.PrintJSON(rows)
	}
	cli.Printer.PrintLine(fmt.Sprintf("%-12s %-12s %-12s %s", "TOOL", "DETECTED", "CONNECTED", "SKILL"))
	for _, row := range rows {
		cli.Printer.PrintLine(fmt.Sprintf("%-12s %-12t %-12t %s", row.Tool, row.Detected, row.Connected, row.SkillStatus))
	}
	return nil
}

type disconnectResult struct {
	Tool      string            `json:"tool"`
	Connected bool              `json:"connected"`
	Files     []connectFilePlan `json:"files"`
}

func runDisconnect(cli *CLI, home string, tool connectTool, yes bool) error {
	record, recordFile, err := loadConnectRecord(home)
	if err != nil {
		return reportConnectError(cli, err)
	}
	result := disconnectResult{Tool: tool.name, Files: []connectFilePlan{}}
	entry, connected := record.Tools[tool.name]
	if !connected {
		if cli.Printer.IsJSON() {
			return cli.Printer.PrintJSON(result)
		}
		cli.Printer.PrintLine(fmt.Sprintf("%s is not connected.", tool.name))
		return nil
	}
	for _, file := range entry.Files {
		if !tool.ownsInstallPath(home, file.Path) {
			return reportConnectError(cli, fmt.Errorf("%w: recorded path %s", errConnectUnsafePath, file.Path))
		}
		before, err := readConnectFile(home, file.Path)
		if err != nil {
			return reportConnectError(cli, err)
		}
		planned := connectFilePlan{Path: file.Path, before: before}
		switch {
		case before == nil:
			planned.Action = "missing"
		case sha256Hex(before) != file.SHA256:
			planned.Action = "changed"
		case record.shared(file.Path, tool.name):
			planned.Action = "shared"
		default:
			planned.Action = "removed"
		}
		result.Files = append(result.Files, planned)
	}
	ok, err := confirmConnectChange(cli, yes, "Remove the Copera Designs connection for "+tool.name+"?")
	if err != nil || !ok {
		if cli.Printer.IsJSON() {
			return errors.Join(err, cli.Printer.PrintJSON(disconnectResult{
				Tool: tool.name, Connected: true, Files: []connectFilePlan{},
			}))
		}
		return err
	}
	delete(record.Tools, tool.name)
	recordFile, err = planConnectRecord(recordFile, record, true)
	if err != nil {
		return reportConnectError(cli, err)
	}
	if err := applyConnectPlan(home, append(result.Files, recordFile)); err != nil {
		return reportConnectError(cli, err)
	}

	if cli.Printer.IsJSON() {
		return cli.Printer.PrintJSON(result)
	}
	for _, file := range result.Files {
		cli.Printer.PrintLine(fmt.Sprintf("%s  %s", file.Action, file.Path))
	}
	cli.Printer.PrintLine(fmt.Sprintf("Disconnected %s.", tool.name))
	return nil
}

func reportConnectError(cli *CLI, err error) error {
	var exit *exitcodes.ExitError
	if !errors.As(err, &exit) {
		code := exitcodes.Error
		if errors.Is(err, errConnectUnsafePath) {
			code = exitcodes.Conflict
		}
		exit = &exitcodes.ExitError{Code: code, Err: err}
	}
	kind := "connection_error"
	switch exit.Code {
	case exitcodes.Conflict:
		kind = "conflict"
	case exitcodes.Usage:
		kind = "invalid_input"
	}
	cli.Printer.PrintError(kind, err.Error(), "", false)
	return exit
}
