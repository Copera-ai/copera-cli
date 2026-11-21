package commands_test

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/copera/copera-cli/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDesignInitAdoptKeepsSource(t *testing.T) {
	const font = "https://fonts.googleapis.com/css2?family=Inter"
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /artifacts/guide": func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("topic") == "" {
				designGuideResponse(w, r)
				return
			}
			assert.Equal(t, "existing-app", r.URL.Query().Get("topic"))
			testutil.RespondJSON(w, http.StatusOK, json.RawMessage(`{"markdown":"# Existing app","facts":{}}`))
		},
	}.Handler())
	setupHome(t, srv.URL)
	dir := t.TempDir()
	source := map[string]string{
		"index.html": "<title>Site</title><link rel=stylesheet href=" + font + ">",
		"other.html": "<title>Another page</title>",
		"site.js":    "const sdk = window.coperaArtifact; sdk.storage.local.get('a');",
		"AGENTS.md":  "Existing agent notes\n",
	}
	for name, content := range source {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
	}
	res := testutil.RunCommand(t, []string{"designs", "init", dir, "--adopt", "--kind", "web_app", "--title", "A New Site", "--json"}, "")
	require.Equal(t, 0, res.ExitCode, res.Stderr)
	var report struct {
		Entrypoint string   `json:"entrypoint"`
		Pages      []string `json:"pages"`
		Links      []struct {
			URL  string `json:"url"`
			Host string `json:"host"`
		} `json:"outsideLinks"`
		Storage bool   `json:"storageDeclared"`
		Title   string `json:"title"`
		Next    string `json:"nextCommand"`
	}
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &report))
	assert.Equal(t, "index.html", report.Entrypoint)
	assert.Equal(t, []string{"index.html", "other.html"}, report.Pages)
	require.Len(t, report.Links, 1)
	assert.Equal(t, font, report.Links[0].URL)
	assert.Equal(t, "fonts.googleapis.com", report.Links[0].Host)
	assert.True(t, report.Storage)
	assert.Equal(t, "A New Site", report.Title)
	assert.Contains(t, report.Next, "copera designs vendor --all")
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, len(source)+1)
	for name, content := range source {
		assert.Equal(t, content, readFile(t, filepath.Join(dir, name)), name)
	}
	assert.Equal(t, "# Existing app", readFile(t, filepath.Join(dir, ".copera", "guide", "README.md")))
	var state struct {
		Title        string          `json:"title"`
		Capabilities json.RawMessage `json:"capabilities"`
	}
	require.NoError(t, json.Unmarshal([]byte(readFile(t, filepath.Join(dir, ".copera", "design.json"))), &state))
	assert.Equal(t, "A New Site", state.Title)
	assert.JSONEq(t, `[{"kind":"storage","scopes":["local"],"namespace":"a-new-site","schemaVersion":1,"migration":{"strategy":"reset"}}]`, string(state.Capabilities))

	again := testutil.RunCommand(t, []string{"designs", "init", dir, "--adopt", "--kind", "web_app"}, "")
	assert.Equal(t, 5, again.ExitCode, again.Stderr)
	assert.Contains(t, again.Stderr, "already a Design")
	assert.Contains(t, again.Stderr, "push")
	assert.Contains(t, again.Stderr, "status")
}

func TestDesignInitAdoptEntryStorageAndLegacyGuide(t *testing.T) {
	for _, tc := range []struct {
		name, entry, code, scope string
		pages                    []string
	}{
		{"only page", "", "", "", []string{"main.html"}},
		{"explicit nested", "pages/other.html", "<script>coperaArtifact.storage.session.get('a')</script>", "session", []string{"index.html", "pages/other.html"}},
		{"explicit storage bindings", "", "<script>const { session } = coperaArtifact.storage; coperaArtifact.storage['local'].get('a');</script>", "local, session", []string{"index.html"}},
		{"index", "", "localStorage.getItem('a');", "", []string{"index.html", "main.html"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := testutil.NewMockServer(t, testutil.MockRoutes{
				"GET /artifacts/guide": func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Query().Get("topic") == "existing-app" {
						message := "Unsupported authoring guide topic"
						if tc.name == "only page" {
							message = "Invalid query"
						}
						testutil.RespondJSON(w, http.StatusBadRequest, map[string]string{"error": message, "code": "BAD_REQUEST"})
						return
					}
					designGuideResponse(w, r)
				},
			}.Handler())
			setupHome(t, srv.URL)
			dir := t.TempDir()
			args := []string{"designs", "init", dir, "--adopt", "--kind", "web_app", "--output", "plain"}
			want := tc.pages[0]
			if tc.entry != "" {
				args = append(args, "--entry", tc.entry)
				want = tc.entry
			}
			for _, name := range tc.pages {
				target := filepath.Join(dir, name)
				require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
				require.NoError(t, os.WriteFile(target, []byte(tc.code), 0o644))
			}
			res := testutil.RunCommand(t, args, "")
			require.Equal(t, 0, res.ExitCode, res.Stderr)
			assert.Contains(t, res.Stdout, "Entry: "+want)
			assert.Contains(t, res.Stdout, "copera designs check")
			if tc.scope == "" {
				assert.Contains(t, res.Stdout, "Storage: none")
			} else {
				assert.Contains(t, res.Stdout, "Storage: declared ("+tc.scope+")")
			}
			assert.Equal(t, "# Authoring guide\n", readFile(t, filepath.Join(dir, ".copera", "guide", "README.md")))
		})
	}
}

func TestDesignInitAdoptRefusals(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "one.html"), nil, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "two.html"), nil, 0o644))
	base := []string{"designs", "init", dir, "--kind", "web_app"}
	for _, tc := range []struct {
		flags    []string
		exitCode int
		text     string
	}{
		{nil, 5, "--adopt"},
		{[]string{"--adopt"}, 2, "--entry"},
		{[]string{"--adopt", "--entry", "missing.html"}, 2, "missing.html"},
		{[]string{"--adopt", "--runtime", "react"}, 2, "--runtime static"},
		{[]string{"--adopt", "--template", "review"}, 2, "--template"},
		{[]string{"--adopt", "--system", "brand"}, 2, "--system"},
		{[]string{"--adopt", "--kit", "none"}, 2, "--kit"},
	} {
		res := testutil.RunCommand(t, append(append([]string{}, base...), tc.flags...), "")
		assert.Equal(t, tc.exitCode, res.ExitCode, res.Stderr)
		assert.Contains(t, res.Stderr, tc.text)
	}
	for _, name := range []string{"node_modules", ".vscode"} {
		require.NoError(t, os.Mkdir(filepath.Join(dir, name), 0o755))
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "with space.html"), nil, 0o644))
	res := testutil.RunCommand(t, append(base, "--adopt", "--entry", "one.html"), "")
	assert.NotEqual(t, 0, res.ExitCode, res.Stderr)
	for _, want := range []string{"node_modules", ".vscode", "with space.html", "Remove", "clean folder"} {
		assert.Contains(t, res.Stderr, want)
	}
	_, err := os.Stat(filepath.Join(dir, ".copera"))
	assert.True(t, os.IsNotExist(err))
}
