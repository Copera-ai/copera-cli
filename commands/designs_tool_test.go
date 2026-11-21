package commands

import (
	"os"
	"testing"
)

func TestDesignsTerminalLine(t *testing.T) {
	for _, tc := range []struct{ text, want string }{
		{"Café — Menü", "Café — Menü"},
		{"日本語", "日本語"},
		{"می\u200cروم", "می\u200cروم"},
		{"👩\u200d💻", "👩\u200d💻"},
		{"❤️", "❤️"},
		{"a\u00a0b\u3000c", "a b c"},
		{"\x1b\a\r\n\u202e\u2066\u2069", `\x1b\a\r\n\u202e\u2066\u2069`},
	} {
		if got := designsTerminalLine(tc.text); got != tc.want {
			t.Errorf("designsTerminalLine(%q) = %q, want %q", tc.text, got, tc.want)
		}
	}
}

func TestDesignTool(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, key := range []string{"CLAUDECODE", "CODEX_THREAD_ID", "CODEX_SESSION_ID", "CODEX_HOME", "CURSOR_AGENT", "OPENCODE", "COPERA_DESIGN_TOOL"} {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}

	type toolTest struct {
		name string
		env  map[string]string
		want string
	}
	tests := []toolTest{
		{"fallback", nil, "copera_cli"},
		{"Codex config only", map[string]string{"CODEX_HOME": home}, "copera_cli"},
		{"Claude wins", map[string]string{"CLAUDECODE": "", "CODEX_THREAD_ID": "thread"}, "claude_code"},
		{"Codex thread wins", map[string]string{"CODEX_THREAD_ID": "", "CURSOR_AGENT": "1"}, "codex"},
		{"Codex session", map[string]string{"CODEX_SESSION_ID": "session"}, "codex"},
		{"Cursor wins", map[string]string{"CURSOR_AGENT": "1", "OPENCODE": "1"}, "cursor"},
		{"empty Cursor and set OpenCode", map[string]string{"CURSOR_AGENT": "", "OPENCODE": ""}, "opencode"},
		{"invalid override falls back", map[string]string{"COPERA_DESIGN_TOOL": "claude-code", "OPENCODE": "1"}, "opencode"},
	}
	for _, tool := range []string{"claude_code", "codex", "cursor", "opencode", "gemini_cli", "windsurf", "copera_cli", "other"} {
		tests = append(tests, toolTest{"override " + tool, map[string]string{"COPERA_DESIGN_TOOL": tool, "CLAUDECODE": "1"}, tool})
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for key, value := range tc.env {
				t.Setenv(key, value)
			}
			if got := designTool(); got != tc.want {
				t.Errorf("designTool() = %q, want %q", got, tc.want)
			}
		})
	}
}
