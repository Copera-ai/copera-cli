package commands_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/copera/copera-cli/commands"
	"github.com/copera/copera-cli/internal/exitcodes"
	"github.com/copera/copera-cli/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	sharedSkillDigest  = "a209aca598aa3de7f7df9020c0b75e594bb3abcbc46baddd2a40486b2e12bf20"
	claudeSkillDigest  = "9e72c07a40f9fb1583dc8869fe2f572bc09614c71f71ddffba00ee959881b9a8"
	claudeAllowedTools = "allowed-tools: Bash(copera auth status *) Bash(copera designs check *) Bash(copera designs status *) Bash(copera designs diff *) Bash(copera designs log *) Bash(copera designs list *) Bash(copera designs get *) Bash(copera designs versions *) Bash(copera designs guide *) Bash(copera designs screenshot *)"
	codexRules         = `prefix_rule(pattern=["copera", "auth", "status"], decision="allow")
prefix_rule(pattern=["copera", "designs", "check"], decision="allow")
prefix_rule(pattern=["copera", "designs", "status"], decision="allow")
prefix_rule(pattern=["copera", "designs", "diff"], decision="allow")
prefix_rule(pattern=["copera", "designs", "log"], decision="allow")
prefix_rule(pattern=["copera", "designs", "list"], decision="allow")
prefix_rule(pattern=["copera", "designs", "get"], decision="allow")
prefix_rule(pattern=["copera", "designs", "versions"], decision="allow")
prefix_rule(pattern=["copera", "designs", "guide"], decision="allow")
prefix_rule(pattern=["copera", "designs", "screenshot"], decision="allow")
`
)

type connectTestFile struct {
	Path   string `json:"path"`
	Action string `json:"action"`
}

type connectTestPlan struct {
	Tool     string            `json:"tool"`
	Detected bool              `json:"detected"`
	DryRun   bool              `json:"dry_run"`
	Files    []connectTestFile `json:"files"`
}

func setupConnectTest(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	envHome := t.TempDir()
	t.Setenv("HOME", envHome)
	t.Setenv("USERPROFILE", envHome)
	t.Setenv("PATH", filepath.Join(home, "no-binaries"))
	t.Setenv("CI", "true")
	testutil.UnsetEnv(t, "CLAUDE_CONFIG_DIR")
	testutil.UnsetEnv(t, "CODEX_HOME")
	testutil.UnsetEnv(t, "COPERA_CLI_AUTH_TOKEN")
	testutil.UnsetEnv(t, "COPERA_PROFILE")
	return home
}

func userSkillPath(home, toolDir string) string {
	return filepath.Join(home, toolDir, "skills", "copera-designs", "SKILL.md")
}

func connectRecordPath(home string) string {
	return filepath.Join(home, ".copera", "connect.json")
}

func runConnectTest(t *testing.T, home string, wantCode int, args ...string) testutil.Result {
	t.Helper()
	var stdout, stderr bytes.Buffer
	err := commands.ExecuteWithWriters(append(args, "--json"), strings.NewReader(""), &stdout, &stderr, commands.ExecOpts{HomeDir: home})
	if wantCode == exitcodes.OK {
		require.NoError(t, err, "stdout: %s\nstderr: %s", stdout.String(), stderr.String())
	} else {
		var exitErr *exitcodes.ExitError
		require.ErrorAs(t, err, &exitErr)
		require.Equal(t, wantCode, exitErr.Code, "stdout: %s\nstderr: %s", stdout.String(), stderr.String())
	}
	return testutil.Result{Stdout: stdout.String(), Stderr: stderr.String()}
}

func TestConnectInstallsSkills(t *testing.T) {
	for _, tt := range []struct {
		tool, skillDir, digest string
	}{
		{"claude-code", ".claude", claudeSkillDigest},
		{"codex", ".agents", sharedSkillDigest},
		{"cursor", ".agents", sharedSkillDigest},
		{"opencode", ".agents", sharedSkillDigest},
	} {
		t.Run(tt.tool, func(t *testing.T) {
			home := setupConnectTest(t)
			project := t.TempDir()
			t.Chdir(project)
			const fakeToken = "cp_pat_fake_connect_test_token"
			t.Setenv("COPERA_CLI_AUTH_TOKEN", fakeToken)
			res := runConnectTest(t, home, exitcodes.OK, "connect", tt.tool, "--yes")
			assert.Contains(t, res.Stdout, "Start ")
			assert.NotContains(t, res.Stdout+res.Stderr, fakeToken)

			skill := userSkillPath(home, tt.skillDir)
			content := readFile(t, skill)
			assert.Equal(t, tt.digest, digestOf(content))
			lines := strings.SplitN(content, "\n", 6)
			require.Len(t, lines, 6)
			assert.Equal(t, "---", lines[0])
			assert.Equal(t, "name: copera-designs", lines[1])
			assert.True(t, strings.HasPrefix(lines[2], "description: "))
			if tt.tool == "claude-code" {
				assert.Equal(t, claudeAllowedTools, lines[3])
			} else {
				assert.NotContains(t, content, "allowed-tools:")
			}

			record := connectRecordPath(home)
			expected := []string{skill, record}
			if tt.tool == "codex" {
				rules := filepath.Join(home, ".codex", "rules", "copera.rules")
				assert.Equal(t, codexRules, readFile(t, rules))
				expected = append(expected, rules)
			}
			assert.Contains(t, readFile(t, record), tt.digest)
			var files []string
			require.NoError(t, filepath.WalkDir(home, func(path string, entry os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if !entry.IsDir() {
					files = append(files, path)
				}
				return nil
			}))
			assert.ElementsMatch(t, expected, files)
			for _, file := range files {
				assert.NotContains(t, readFile(t, file), fakeToken)
				if runtime.GOOS != "windows" {
					info, err := os.Stat(file)
					require.NoError(t, err)
					assert.Equal(t, os.FileMode(0o644), info.Mode().Perm())
				}
			}
			entries, err := os.ReadDir(project)
			require.NoError(t, err)
			assert.Empty(t, entries)
		})
	}
}

func TestConnectDryRunAndConfirmation(t *testing.T) {
	t.Run("dry run", func(t *testing.T) {
		home := setupConnectTest(t)
		res := runConnectTest(t, home, exitcodes.OK, "connect", "cursor", "--dry-run")
		var plan connectTestPlan
		require.NoError(t, json.Unmarshal([]byte(res.Stdout), &plan))
		assert.True(t, plan.DryRun)
		assert.False(t, plan.Detected)
		assert.Contains(t, plan.Files, connectTestFile{Path: userSkillPath(home, ".agents"), Action: "new"})
		assert.Contains(t, plan.Files, connectTestFile{Path: connectRecordPath(home), Action: "new"})
		entries, err := os.ReadDir(home)
		require.NoError(t, err)
		assert.Empty(t, entries)
	})

	t.Run("no confirmation", func(t *testing.T) {
		home := setupConnectTest(t)
		res := runConnectTest(t, home, exitcodes.Usage, "connect", "cursor")
		assert.Contains(t, res.Stderr, "--yes")
		entries, err := os.ReadDir(home)
		require.NoError(t, err)
		assert.Empty(t, entries)
	})
}

func TestConnectPreservesUnownedAndEditedSkills(t *testing.T) {
	const edits = "edited by the person\n"
	t.Run("unowned", func(t *testing.T) {
		home := setupConnectTest(t)
		skill := userSkillPath(home, ".agents")
		require.NoError(t, os.MkdirAll(filepath.Dir(skill), 0o755))
		require.NoError(t, os.WriteFile(skill, []byte(edits), 0o644))
		res := runConnectTest(t, home, exitcodes.Conflict, "connect", "codex", "--yes")
		assert.Contains(t, res.Stderr, "SKILL.md")
		assert.Equal(t, edits, readFile(t, skill))
		assert.NoFileExists(t, filepath.Join(home, ".codex", "rules", "copera.rules"))
		assert.NoFileExists(t, connectRecordPath(home))
	})

	t.Run("edited", func(t *testing.T) {
		home := setupConnectTest(t)
		skill := userSkillPath(home, ".agents")
		runConnectTest(t, home, exitcodes.OK, "connect", "opencode", "--yes")
		require.NoError(t, os.WriteFile(skill, []byte(edits), 0o644))
		res := runConnectTest(t, home, exitcodes.OK, "disconnect", "opencode", "--yes")
		var result struct{ Files []connectTestFile }
		require.NoError(t, json.Unmarshal([]byte(res.Stdout), &result))
		assert.Contains(t, result.Files, connectTestFile{Path: skill, Action: "changed"})
		assert.Equal(t, edits, readFile(t, skill))
	})
}

func TestDisconnectSharedSkill(t *testing.T) {
	home := setupConnectTest(t)
	skill := userSkillPath(home, ".agents")
	rules := filepath.Join(home, ".codex", "rules", "copera.rules")
	runConnectTest(t, home, exitcodes.OK, "connect", "codex", "--yes")
	runConnectTest(t, home, exitcodes.OK, "connect", "cursor", "--yes")
	runConnectTest(t, home, exitcodes.OK, "disconnect", "codex", "--yes")
	assert.FileExists(t, skill)
	assert.NoFileExists(t, rules)
	runConnectTest(t, home, exitcodes.OK, "disconnect", "cursor", "--yes")
	assert.NoDirExists(t, filepath.Dir(skill))
	assert.DirExists(t, filepath.Dir(filepath.Dir(skill)))
	runConnectTest(t, home, exitcodes.OK, "disconnect", "cursor", "--yes")
}

func TestConnectRefusesSymlinkedSkillFolder(t *testing.T) {
	home := setupConnectTest(t)
	skill := userSkillPath(home, ".agents")
	folder := filepath.Dir(skill)
	require.NoError(t, os.MkdirAll(filepath.Dir(folder), 0o755))
	outside := t.TempDir()
	if err := os.Symlink(outside, folder); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlink unavailable: %v", err)
		}
		require.NoError(t, err)
	}
	res := runConnectTest(t, home, exitcodes.Conflict, "connect", "cursor", "--yes")
	assert.Contains(t, res.Stderr, "symlink")
	assert.NoFileExists(t, filepath.Join(outside, "SKILL.md"))
	assert.NoFileExists(t, connectRecordPath(home))
}

func TestConnectRespectsToolDirectories(t *testing.T) {
	home := setupConnectTest(t)
	claudeDir := filepath.Join(home, "custom-claude")
	codexDir := filepath.Join(home, "custom-codex")
	t.Setenv("CLAUDE_CONFIG_DIR", claudeDir)
	t.Setenv("CODEX_HOME", codexDir)
	require.NoError(t, os.Mkdir(claudeDir, 0o755))
	require.NoError(t, os.Mkdir(codexDir, 0o755))
	defaultRules := filepath.Join(codexDir, "rules", "default.rules")
	require.NoError(t, os.Mkdir(filepath.Dir(defaultRules), 0o755))
	require.NoError(t, os.WriteFile(defaultRules, []byte("# user rules\n"), 0o644))
	res := runConnectTest(t, home, exitcodes.OK, "connect", "claude", "--yes")
	var plan connectTestPlan
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &plan))
	assert.Equal(t, "claude-code", plan.Tool)
	assert.True(t, plan.Detected)
	res = runConnectTest(t, home, exitcodes.OK, "connect", "codex", "--yes")
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &plan))
	assert.True(t, plan.Detected)
	assert.FileExists(t, userSkillPath(home, "custom-claude"))
	assert.FileExists(t, userSkillPath(home, ".agents"))
	assert.FileExists(t, filepath.Join(codexDir, "rules", "copera.rules"))
	assert.Equal(t, "# user rules\n", readFile(t, defaultRules))
	assert.NoDirExists(t, filepath.Join(home, ".claude"))
	assert.NoDirExists(t, filepath.Join(home, ".codex"))
}

func TestConnectPlansUpgradeAndListsSkillStatus(t *testing.T) {
	home := setupConnectTest(t)
	skill := userSkillPath(home, ".agents")
	record := connectRecordPath(home)
	require.NoError(t, os.Mkdir(filepath.Join(home, ".cursor"), 0o755))
	runConnectTest(t, home, exitcodes.OK, "connect", "cursor", "--yes")
	current := readFile(t, skill)
	originalRecord := readFile(t, record)
	older := strings.Replace(current, "# Copera Design Artifacts", "# Older Copera Design Artifacts", 1)
	require.NotEqual(t, current, older)
	require.NoError(t, os.WriteFile(skill, []byte(older), 0o644))

	type status struct {
		Tool        string `json:"tool"`
		Detected    bool   `json:"detected"`
		Connected   bool   `json:"connected"`
		SkillStatus string `json:"skill_status"`
	}
	checkList := func(wantSkillStatus string, args ...string) {
		t.Helper()
		res := runConnectTest(t, home, exitcodes.OK, args...)
		var list []status
		require.NoError(t, json.Unmarshal([]byte(res.Stdout), &list))
		assert.ElementsMatch(t, []status{
			{Tool: "claude-code", SkillStatus: "missing"},
			{Tool: "codex", SkillStatus: wantSkillStatus},
			{Tool: "cursor", Detected: true, Connected: true, SkillStatus: wantSkillStatus},
			{Tool: "opencode", SkillStatus: wantSkillStatus},
		}, list)
	}
	checkList("changed", "connect")
	require.NoError(t, os.WriteFile(record, []byte(strings.ReplaceAll(originalRecord, digestOf(current), digestOf(older))), 0o644))
	checkList("older", "connect", "--list")
	res := runConnectTest(t, home, exitcodes.OK, "connect", "cursor", "--dry-run")
	var plan connectTestPlan
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &plan))
	assert.Contains(t, plan.Files, connectTestFile{Path: skill, Action: "update"})
	assert.Equal(t, older, readFile(t, skill))

	runConnectTest(t, home, exitcodes.OK, "connect", "cursor", "--yes")
	assert.Equal(t, current, readFile(t, skill))
	checkList("current", "connect", "--list")
	res = runConnectTest(t, home, exitcodes.OK, "connect", "cursor", "--dry-run")
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &plan))
	assert.Contains(t, plan.Files, connectTestFile{Path: skill, Action: "unchanged"})
}

func TestConnectLockLeftByAKilledRun(t *testing.T) {
	home := setupConnectTest(t)
	lock := filepath.Join(home, ".copera", ".connect-lock")
	require.NoError(t, os.MkdirAll(lock, 0o755))

	// A fresh lock is another run at work: nothing is written.
	runConnectTest(t, home, exitcodes.Conflict, "connect", "claude-code", "--yes")
	_, err := os.Stat(userSkillPath(home, ".claude"))
	require.ErrorIs(t, err, os.ErrNotExist)

	// A lock older than any real run was left behind and must not block for ever.
	old := time.Now().Add(-10 * time.Minute)
	require.NoError(t, os.Chtimes(lock, old, old))
	runConnectTest(t, home, exitcodes.OK, "connect", "claude-code", "--yes")
	_, err = os.Stat(userSkillPath(home, ".claude"))
	require.NoError(t, err)
	_, err = os.Stat(lock)
	require.ErrorIs(t, err, os.ErrNotExist)
}
