package commands_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/copera/copera-cli/internal/api"
	"github.com/copera/copera-cli/internal/output"
	"github.com/copera/copera-cli/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDesignInitAdoptPublishableFiles(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /artifacts/guide": designGuideResponse,
	}.Handler())
	setupHome(t, srv.URL)
	t.Run("refused paths", func(t *testing.T) {
		dir := t.TempDir()
		offenders := []struct {
			reason string
			paths  []string
		}{
			{"the builder owns build configuration", []string{
				"src/Package.JSON", "src/tsconfig.json", "src/JSConfig.json", "src/VITE.CONFIG.MTS",
				"src/tailwind.config.cts", "src/postcss.config.cjs",
			}},
			{"looks like a secret or a database", []string{
				"id_rsa.pub", "id_ed25519-test", "server.PEM", "backup.sql", "data.sqlite", "data.db",
				"site.key", "site.p12", "site.pfx", "site.keystore", "credentials/private.txt",
			}},
			{"only letters, digits, dots, underscores and hyphens", []string{"with space.html", "ação.html", "src/@app.js"}},
			{"packages are not uploaded", []string{"node_modules/first.txt"}},
		}
		writePublishFixture(t, filepath.Join(dir, "index.html"), nil)
		for _, group := range offenders {
			for _, name := range group.paths {
				writePublishFixture(t, filepath.Join(dir, name), nil)
			}
		}
		res := testutil.RunCommand(t, []string{"designs", "init", dir, "--adopt", "--kind", "web_app"}, "")
		assert.Equal(t, 2, res.ExitCode, res.Stderr)
		var refusal output.StructuredError
		require.NoError(t, json.Unmarshal([]byte(res.Stderr), &refusal))
		for _, group := range offenders {
			for _, name := range group.paths {
				if strings.HasPrefix(name, "credentials/") || strings.HasPrefix(name, "node_modules/") {
					name = filepath.ToSlash(filepath.Dir(name))
				}
				assert.Contains(t, refusal.Message, fmt.Sprintf("%q: %s", name, group.reason))
			}
		}
		assert.True(t, strings.HasSuffix(refusal.Message, "Copy the files you want published into a clean folder and adopt that folder."))
		assert.NotContains(t, refusal.Message, "Remove")
		assert.NoDirExists(t, filepath.Join(dir, ".copera"))
	})
	t.Run("populated metadata", func(t *testing.T) {
		dir := t.TempDir()
		writePublishFixture(t, filepath.Join(dir, "index.html"), nil)
		extra := filepath.Join(dir, ".copera", "guide", "EXTRA.md")
		writePublishFixture(t, extra, []byte("existing guide"))
		res := testutil.RunCommand(t, []string{"designs", "init", dir, "--adopt", "--kind", "web_app"}, "")
		assert.Equal(t, 5, res.ExitCode, res.Stderr)
		assert.Contains(t, res.Stderr, ".copera")
		assert.Equal(t, "existing guide", readFile(t, extra))
		assert.NoFileExists(t, filepath.Join(dir, ".copera", "design.json"))
	})
	t.Run("legacy paths", func(t *testing.T) {
		dir := t.TempDir()
		writePublishFixture(t, filepath.Join(dir, ".copera", "design.json"), []byte(`{"kind":"website","runtime":"static_web"}`))
		writePublishFixture(t, filepath.Join(dir, "with space.html"), []byte("<title>Legacy</title>"))
		res := testutil.RunCommand(t, []string{"designs", "status", dir, "--json"}, "")
		assert.Zero(t, res.ExitCode, res.Stderr)
		assert.Contains(t, res.Stdout, "with space.html")
	})
	t.Run("publish list", func(t *testing.T) {
		populate := func(dir string) {
			for _, name := range []string{"index.html", "app.js", "base.css", "image.png", ".git/config", "AGENTS.md"} {
				writePublishFixture(t, filepath.Join(dir, name), nil)
			}
			require.NoError(t, os.Mkdir(filepath.Join(dir, ".copera"), 0o755))
			for i := range 39 {
				writePublishFixture(t, filepath.Join(dir, fmt.Sprintf("copy-%02d.txt", i)), nil)
			}
		}
		args := []string{"designs", "init", "", "--adopt", "--kind", "web_app"}
		args[2] = t.TempDir()
		populate(args[2])
		res := testutil.RunCommand(t, append(args, "--output", "plain"), "")
		require.Zero(t, res.ExitCode, res.Stderr)
		for _, line := range []string{"Pages (1): index.html", "Scripts (1): app.js", "Styles (1): base.css", "Other text (39):", "Assets (1)", "and 3 more"} {
			assert.Contains(t, res.Stdout, line)
		}
		assert.Contains(t, res.Stdout, "copy-36.txt")
		assert.NotContains(t, res.Stdout, "copy-37.txt")
		assert.NotContains(t, res.Stdout, ".git/config")
		assert.NotContains(t, res.Stdout, "AGENTS.md")

		args[2] = t.TempDir()
		populate(args[2])
		res = testutil.RunCommand(t, append(args, "--json"), "")
		require.Zero(t, res.ExitCode, res.Stderr)
		var report struct {
			Pages, Scripts, Styles, OtherText, Assets []string
			FileCounts                                map[string]int `json:"fileCounts"`
		}
		require.NoError(t, json.Unmarshal([]byte(res.Stdout), &report))
		assert.Equal(t, []string{"index.html"}, report.Pages)
		assert.Equal(t, []string{"app.js"}, report.Scripts)
		assert.Equal(t, []string{"base.css"}, report.Styles)
		assert.Equal(t, []string{"image.png"}, report.Assets)
		assert.Len(t, report.OtherText, 39)
		assert.Contains(t, report.OtherText, "copy-38.txt")
		assert.Equal(t, map[string]int{"pages": 1, "scripts": 1, "styles": 1, "otherText": 39, "assets": 1}, report.FileCounts)
	})
}

func TestDesignInitAdoptRefusalLimit(t *testing.T) {
	for _, tc := range []struct {
		name   string
		paths  int
		create func(int) string
	}{
		{"packages", 300, func(i int) string { return fmt.Sprintf("node_modules/pkg-%03d/package.json", i) }},
		{"independent refusals", 42, func(i int) string { return fmt.Sprintf("item-%02d.key", i) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writePublishFixture(t, filepath.Join(dir, "index.html"), nil)
			for i := range tc.paths {
				writePublishFixture(t, filepath.Join(dir, tc.create(i)), nil)
			}
			res := testutil.RunCommand(t, []string{"designs", "init", dir, "--adopt", "--kind", "web_app"}, "")
			require.Equal(t, 2, res.ExitCode)
			var refusal output.StructuredError
			require.NoError(t, json.Unmarshal([]byte(res.Stderr), &refusal))
			if tc.name == "packages" {
				assert.Equal(t, 1, strings.Count(refusal.Message, "node_modules"))
				assert.False(t, strings.Contains(refusal.Message, "pkg-000"))
				assert.False(t, strings.Contains(refusal.Message, "and 1 more"))
			} else {
				assert.Equal(t, 40, strings.Count(refusal.Message, ".key"))
				assert.True(t, strings.Contains(refusal.Message, "and 2 more"))
			}
		})
	}
}

func TestDesignInitAdoptHiddenFiles(t *testing.T) {
	const html = "<title>Site</title>"
	files := []api.ArtifactContentFile{{Path: "index.html", MediaType: "text/html", Content: html}}
	var checks, creates atomic.Int32
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /artifacts/guide": designGuideResponse,
		"GET /workspace/info": func(w http.ResponseWriter, _ *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, api.Workspace{ID: designWorkspaceID})
		},
		"GET /artifacts/design-systems/defaults": func(w http.ResponseWriter, _ *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, api.ArtifactSystemDefaults{})
		},
		"POST /artifacts/check": func(w http.ResponseWriter, r *http.Request) {
			checks.Add(1)
			var input api.ArtifactCheckInput
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&input))
			assert.Equal(t, files, input.Files)
			testutil.RespondJSON(w, http.StatusOK, map[string]bool{"ok": true})
		},
		"POST /artifacts/": func(w http.ResponseWriter, r *http.Request) {
			creates.Add(1)
			reader, err := r.MultipartReader()
			if !assert.NoError(t, err) {
				w.WriteHeader(http.StatusUnsupportedMediaType)
				return
			}
			project, err := reader.NextPart()
			if !assert.NoError(t, err) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			assert.Equal(t, "project", project.FormName())
			var input api.ArtifactCreateInput
			assert.NoError(t, json.NewDecoder(project).Decode(&input))
			assert.Equal(t, files, input.Project.Files)
			_, err = reader.NextPart()
			assert.ErrorIs(t, err, io.EOF)
			testutil.RespondJSON(w, http.StatusOK, designPullFixture("index.html", html, 1))
		},
	}.Handler())
	setupHome(t, srv.URL)
	dir := t.TempDir()
	writePublishFixture(t, filepath.Join(dir, "index.html"), []byte(html))
	hidden := []string{".gitignore", ".DS_Store", ".nojekyll", ".htaccess", ".env", ".well-known/hidden.png", "src/.nested"}
	for _, name := range hidden {
		writePublishFixture(t, filepath.Join(dir, name), []byte("private"))
	}
	for i := range 15 {
		writePublishFixture(t, filepath.Join(dir, "tmp", fmt.Sprintf(".extra-%02d", i)), []byte("private"))
	}

	res := testutil.RunCommand(t, []string{"designs", "init", dir, "--adopt", "--kind", "web_app", "--output", "plain"}, "")
	require.Zero(t, res.ExitCode, res.Stderr)
	assert.Contains(t, res.Stdout, "Not published (hidden)")
	for _, name := range hidden {
		if strings.HasPrefix(name, ".well-known/") {
			name = ".well-known"
		}
		assert.Equal(t, 1, strings.Count(res.Stdout, name), name)
	}
	assert.NotContains(t, res.Stdout, "hidden.png")
	assert.Contains(t, res.Stdout, ".extra-12")
	assert.NotContains(t, res.Stdout, ".extra-13")
	assert.Contains(t, res.Stdout, "and 2 more")

	writePublishFixture(t, filepath.Join(dir, "src", ".added"), []byte("private"))
	res = testutil.RunCommand(t, []string{"designs", "check", dir, "--json"}, "")
	require.Zero(t, res.ExitCode, res.Stderr)
	res = testutil.RunCommand(t, []string{"designs", "push", dir, "-m", "Publish", "--no-check", "--json"}, "")
	require.Zero(t, res.ExitCode, res.Stderr)
	assert.EqualValues(t, 1, checks.Load())
	assert.EqualValues(t, 1, creates.Load())
}

func TestDesignInitAdoptRejectsNewCredential(t *testing.T) {
	var checks atomic.Int32
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /artifacts/guide": designGuideResponse,
		"POST /artifacts/check": func(w http.ResponseWriter, _ *http.Request) {
			checks.Add(1)
			testutil.RespondJSON(w, http.StatusOK, map[string]bool{"ok": true})
		},
	}.Handler())
	setupHome(t, srv.URL)
	dir := t.TempDir()
	writePublishFixture(t, filepath.Join(dir, "index.html"), []byte("<title>Site</title>"))
	res := testutil.RunCommand(t, []string{"designs", "init", dir, "--adopt", "--kind", "web_app"}, "")
	require.Zero(t, res.ExitCode, res.Stderr)
	writePublishFixture(t, filepath.Join(dir, "id_rsa"), []byte("private"))
	res = testutil.RunCommand(t, []string{"designs", "check", dir, "--json"}, "")
	assert.NotZero(t, res.ExitCode)
	assert.Contains(t, res.Stderr, "id_rsa")
	assert.Contains(t, res.Stderr, "looks like a secret or a database")
	assert.Zero(t, checks.Load())
}

func TestDesignPlainInitKeepsDotFile(t *testing.T) {
	var checks atomic.Int32
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /artifacts/guide": designGuideResponse,
		"GET /artifacts/design-systems/defaults": func(w http.ResponseWriter, _ *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, api.ArtifactSystemDefaults{})
		},
		"POST /artifacts/check": func(w http.ResponseWriter, r *http.Request) {
			checks.Add(1)
			var input api.ArtifactCheckInput
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&input))
			assert.Contains(t, input.Files, api.ArtifactContentFile{Path: ".nojekyll", MediaType: "text/plain"})
			testutil.RespondJSON(w, http.StatusOK, map[string]bool{"ok": true})
		},
	}.Handler())
	setupHome(t, srv.URL)
	dir := t.TempDir()
	res := testutil.RunCommand(t, []string{"designs", "init", dir, "--kind", "web_app", "--no-system"}, "")
	require.Zero(t, res.ExitCode, res.Stderr)
	writePublishFixture(t, filepath.Join(dir, "index.html"), []byte("<title>Site</title>"))
	writePublishFixture(t, filepath.Join(dir, ".nojekyll"), nil)
	res = testutil.RunCommand(t, []string{"designs", "check", dir, "--json"}, "")
	require.Zero(t, res.ExitCode, res.Stderr)
	assert.EqualValues(t, 1, checks.Load())
}

func TestDesignInitAdoptTerminalLine(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{"GET /artifacts/guide": designGuideResponse}.Handler())
	setupHome(t, srv.URL)
	const title = "Sample\x1b[8m\u202E\u034f Title"
	dir := t.TempDir()
	writePublishFixture(t, filepath.Join(dir, "index.html"), []byte(`<title>Sample&#27;[8m&#8238;&#847; Title</title><script src="https://example.test/a.js&#10;marker&#13;&#27;"></script>`))
	res := testutil.RunCommand(t, []string{"designs", "init", dir, "--adopt", "--kind", "web_app", "--output", "plain"}, "")
	require.Zero(t, res.ExitCode, res.Stderr)
	assert.NotContains(t, res.Stdout, "\nmarker")
	assert.NotContains(t, res.Stdout, "\x1b")
	assert.NotContains(t, res.Stdout, "\r")
	assert.NotContains(t, res.Stdout, title)
	assert.Contains(t, res.Stdout, `https://example.test/a.js\nmarker\r\x1b`)
	assert.Contains(t, res.Stdout, `Title: Sample\x1b[8m\u202e\u034f Title`)
}

func TestDesignInitAdoptStorageReferences(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{"GET /artifacts/guide": designGuideResponse}.Handler())
	setupHome(t, srv.URL)
	for _, tc := range []struct {
		name, sdk, script string
		scopes            []string
		fallback          bool
	}{
		{"alias", "", "const s = window.coperaArtifact.storage; s.local.get()", []string{"local"}, false},
		{"minified", "window.coperaArtifact", "b=a.storage,c=b.local,d=b.session", []string{"local", "session"}, false},
		{"across scripts", "const sdk = window.coperaArtifact", "sdk.storage.session.get()", []string{"session"}, false},
		{"fallback", "window.coperaArtifact", "const store = sdk.storage; unrelated.session.get()", []string{"local"}, true},
		{"storage identifier suffix", "window.coperaArtifact", "sdk.storage$metadata", nil, false},
		{"scope identifier suffix", "window.coperaArtifact", "sdk.storage.session$metadata", []string{"local"}, true},
		{"no sdk", "", "const store = other.storage; store.session.get()", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writePublishFixture(t, filepath.Join(dir, "index.html"), nil)
			writePublishFixture(t, filepath.Join(dir, "sdk.js"), []byte(tc.sdk))
			writePublishFixture(t, filepath.Join(dir, "app.js"), []byte(tc.script))
			res := testutil.RunCommand(t, []string{"designs", "init", dir, "--adopt", "--kind", "web_app", "--output", "plain"}, "")
			require.Zero(t, res.ExitCode, res.Stderr)
			var state struct {
				Capabilities []api.ArtifactProjectCapability `json:"capabilities"`
			}
			require.NoError(t, json.Unmarshal([]byte(readFile(t, filepath.Join(dir, ".copera", "design.json"))), &state))
			if tc.scopes == nil {
				assert.Empty(t, state.Capabilities)
				assert.Contains(t, res.Stdout, "Storage: none")
				return
			}
			require.Len(t, state.Capabilities, 1)
			require.NotNil(t, state.Capabilities[0].Scopes)
			assert.Equal(t, tc.scopes, *state.Capabilities[0].Scopes)
			assert.Contains(t, res.Stdout, "Storage: declared ("+strings.Join(tc.scopes, ", ")+")")
			assert.Equal(t, tc.fallback, strings.Contains(res.Stdout, "defaulted to local"))
		})
	}
}

func TestDesignInitAdoptGuideFallbackAndSystemDecline(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			const entry = "<title>App</title>"
			srv := testutil.NewMockServer(t, testutil.MockRoutes{
				"GET /artifacts/guide": func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Query().Get("topic") == "existing-app" && status != http.StatusOK {
						testutil.RespondJSON(w, status, map[string]string{"error": "Guide unavailable"})
						return
					}
					designGuideResponse(w, r)
				},
				"GET /artifacts/design-systems/defaults": func(w http.ResponseWriter, _ *http.Request) {
					id := verID
					testutil.RespondJSON(w, http.StatusOK, api.ArtifactSystemDefaults{WorkspaceSystemID: &id})
				},
				"GET /workspace/info": func(w http.ResponseWriter, _ *http.Request) {
					testutil.RespondJSON(w, http.StatusOK, api.Workspace{ID: designWorkspaceID})
				},
				"POST /artifacts/": func(w http.ResponseWriter, r *http.Request) {
					var input api.ArtifactCreateInput
					assert.NoError(t, json.Unmarshal([]byte(r.FormValue("project")), &input))
					assert.True(t, input.NoDesignSystem)
					testutil.RespondJSON(w, http.StatusCreated, designPullFixture("index.html", entry, 1))
				},
			}.Handler())
			setupHome(t, srv.URL)
			dir := filepath.Join(t.TempDir(), "app")
			if status == http.StatusOK {
				res := testutil.RunCommand(t, []string{"designs", "init", dir, "--kind", "web_app", "--no-system", "--json"}, "")
				require.Zero(t, res.ExitCode, res.Stderr)
				writePublishFixture(t, filepath.Join(dir, "index.html"), []byte(entry))
				res = testutil.RunCommand(t, []string{"designs", "push", dir, "-m", "Publish", "--no-check", "--json"}, "")
				require.Zero(t, res.ExitCode, res.Stderr)
				dir = filepath.Join(t.TempDir(), "adopted")
			}
			writePublishFixture(t, filepath.Join(dir, "index.html"), []byte(entry))
			res := testutil.RunCommand(t, []string{"designs", "init", dir, "--adopt", "--kind", "web_app", "--json"}, "")
			require.Zero(t, res.ExitCode, res.Stderr)
			assert.Equal(t, "# Authoring guide\n", readFile(t, filepath.Join(dir, ".copera", "guide", "README.md")))
			res = testutil.RunCommand(t, []string{"designs", "push", dir, "-m", "Publish", "--no-check", "--json"}, "")
			require.Zero(t, res.ExitCode, res.Stderr)
		})
	}
}

func TestDesignInitAdoptGroupsOutsideLinks(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{"GET /artifacts/guide": designGuideResponse}.Handler())
	setupHome(t, srv.URL)
	const font = "https://fonts.googleapis.com/css2?family=Inter"
	for _, format := range []string{"plain", "json"} {
		t.Run(format, func(t *testing.T) {
			dir := t.TempDir()
			for _, name := range []string{"index.html", "about.html"} {
				writePublishFixture(t, filepath.Join(dir, name), []byte(`<link rel="stylesheet" href="`+font+`"><link rel="stylesheet" href="`+font+`">`))
			}
			res := testutil.RunCommand(t, []string{"designs", "init", dir, "--adopt", "--kind", "web_app", "--output", format}, "")
			require.Zero(t, res.ExitCode, res.Stderr)
			assert.Equal(t, 1, strings.Count(res.Stdout, font))
			if format == "plain" {
				assert.Contains(t, res.Stdout, "about.html, index.html")
				return
			}
			var report struct {
				Links []struct {
					URL, Host string
					Files     []string
				} `json:"outsideLinks"`
			}
			require.NoError(t, json.Unmarshal([]byte(res.Stdout), &report))
			require.Len(t, report.Links, 1)
			assert.Equal(t, font, report.Links[0].URL)
			assert.Equal(t, "fonts.googleapis.com", report.Links[0].Host)
			assert.Equal(t, []string{"about.html", "index.html"}, report.Links[0].Files)
		})
	}
}

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
	for _, want := range []string{"node_modules", "with space.html", "clean folder"} {
		assert.Contains(t, res.Stderr, want)
	}
	assert.NotContains(t, res.Stderr, ".vscode")
	_, err := os.Stat(filepath.Join(dir, ".copera"))
	assert.True(t, os.IsNotExist(err))
}
