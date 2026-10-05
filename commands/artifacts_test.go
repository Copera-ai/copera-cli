package commands_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/copera/copera-cli/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	artID  = "aaaaaaaaaaaaaaaaaaaaaaaa"
	verID  = "bbbbbbbbbbbbbbbbbbbbbbbb"
	ver2ID = "cccccccccccccccccccccccc"
)

func artifactVersionFixture(id string, number int) map[string]any {
	return map[string]any{
		"id": id, "artifactId": artID, "version": number,
		"runtime": "vite_react", "entrypoint": "src/main.tsx",
		"contentDigest": strings.Repeat("a", 64),
		"authoringManifest": map[string]any{
			"schemaVersion": 1, "defaultPageId": "home",
			"pages": []map[string]any{{
				"id": "home", "title": "Home", "entrypoint": "src/main.tsx", "kind": "web_app",
				"surface": "web", "defaultViewport": map[string]any{"width": 1440, "height": 900}, "scaffoldVersion": "1",
			}},
			"sharedDesign": map[string]any{"version": 1, "tokensPath": "design/tokens.css"},
			"assets":       []map[string]any{},
		},
		"files": []map[string]any{
			{"path": "src/main.tsx", "mediaType": "text/tsx", "contentDigest": strings.Repeat("b", 64), "utf8Bytes": 2048},
		},
		"createdAt": "2026-10-01T10:00:00Z",
	}
}

func artifactFixture() map[string]any {
	return map[string]any{
		"id": artID, "title": "Launch Site", "description": "Marketing site",
		"currentDraftVersionId": verID, "headRevision": 4, "canEdit": true,
		"currentVersion": artifactVersionFixture(verID, 4),
		"preview":        map[string]any{"status": "ready", "url": "https://preview.example/a"},
	}
}

// ── artifacts list ───────────────────────────────────────────────────────────

func TestArtifactsList_JSONAndQuery(t *testing.T) {
	var q url.Values
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /artifacts/{$}": func(w http.ResponseWriter, r *http.Request) {
			q = r.URL.Query()
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"edges":       []map[string]any{{"cursor": "c1", "node": artifactFixture()}},
				"endCursor":   "c1",
				"hasNextPage": true,
			})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"artifacts", "list", "--scope", "shared", "--search", "launch", "--limit", "5", "--after", "c0", "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Equal(t, "shared", q.Get("scope"))
	assert.Equal(t, "launch", q.Get("search"))
	assert.Equal(t, "5", q.Get("limit"))
	assert.Equal(t, "c0", q.Get("after"))

	var out struct {
		Edges []struct {
			Node struct {
				ID    string `json:"id"`
				Title string `json:"title"`
			} `json:"node"`
		} `json:"edges"`
		HasNextPage bool `json:"hasNextPage"`
	}
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &out))
	require.Len(t, out.Edges, 1)
	assert.Equal(t, "Launch Site", out.Edges[0].Node.Title)
	assert.True(t, out.HasNextPage)
}

func TestArtifactsList_Human(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /artifacts/{$}": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"edges":       []map[string]any{{"cursor": "c1", "node": artifactFixture()}},
				"endCursor":   "c1",
				"hasNextPage": true,
			})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"artifacts", "list", "--output", "table"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Contains(t, res.Stdout, "Launch Site")
	assert.Contains(t, res.Stdout, artID)
	assert.Contains(t, res.Stderr, "--after c1")
}

func TestArtifactsList_InvalidFlags(t *testing.T) {
	setupHome(t, "http://127.0.0.1:1")
	for _, args := range [][]string{
		{"artifacts", "list", "--scope", "everyone"},
		{"artifacts", "list", "--limit", "51"},
		{"artifacts", "versions", artID, "--limit", "-1"},
	} {
		res := testutil.RunCommand(t, args, "")
		assert.Equal(t, 2, res.ExitCode, "args %v stderr: %s", args, res.Stderr)
	}
}

// ── artifacts get ────────────────────────────────────────────────────────────

func TestArtifactsGet_JSON(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /artifacts/" + artID: func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, artifactFixture())
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"artifacts", "get", artID, "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	var out struct {
		ID             string `json:"id"`
		CurrentVersion struct {
			ID    string `json:"id"`
			Files []struct {
				Path string `json:"path"`
			} `json:"files"`
		} `json:"currentVersion"`
	}
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &out))
	assert.Equal(t, artID, out.ID)
	assert.Equal(t, verID, out.CurrentVersion.ID)
	require.Len(t, out.CurrentVersion.Files, 1)
	assert.Equal(t, "src/main.tsx", out.CurrentVersion.Files[0].Path)
}

func TestArtifactsGet_Human(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /artifacts/" + artID: func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, artifactFixture())
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"artifacts", "get", artID, "--output", "table"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	for _, want := range []string{"Title:       Launch Site", "Runtime:     vite_react", "home  Home", "src/main.tsx  (2.0 KB)", "https://preview.example/a"} {
		assert.Contains(t, res.Stdout, want)
	}
}

func TestArtifactsGet_Errors(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		code      string
		wantExit  int
		wantInErr string
	}{
		{"not found", http.StatusNotFound, "NOT_FOUND", 3, "NOT_FOUND"},
		{"flag off", http.StatusForbidden, "FORBIDDEN", 4, "Artifacts enabled"},
		{"bad request", http.StatusBadRequest, "INVALID_PARAMS", 1, "INVALID_PARAMS"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := testutil.NewMockServer(t, testutil.MockRoutes{
				"GET /artifacts/" + artID: func(w http.ResponseWriter, r *http.Request) {
					testutil.RespondError(w, tc.status, tc.code, "nope")
				},
			}.Handler())
			setupHome(t, srv.URL)

			res := testutil.RunCommand(t, []string{"artifacts", "get", artID, "--json"}, "")
			assert.Equal(t, tc.wantExit, res.ExitCode)
			assert.Contains(t, res.Stderr, tc.wantInErr)
			assert.Empty(t, res.Stdout)
		})
	}
}

// ── artifacts versions ───────────────────────────────────────────────────────

func TestArtifactsVersions_JSONAndHuman(t *testing.T) {
	var q url.Values
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /artifacts/" + artID + "/versions": func(w http.ResponseWriter, r *http.Request) {
			q = r.URL.Query()
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"edges": []map[string]any{
					{"cursor": "c2", "node": artifactVersionFixture(ver2ID, 5)},
					{"cursor": "c1", "node": artifactVersionFixture(verID, 4)},
				},
				"hasNextPage": false,
			})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"artifacts", "versions", artID, "--limit", "2", "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Equal(t, "2", q.Get("limit"))
	var out struct {
		Edges []struct {
			Node struct {
				Version int `json:"version"`
			} `json:"node"`
		} `json:"edges"`
	}
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &out))
	require.Len(t, out.Edges, 2)
	assert.Equal(t, 5, out.Edges[0].Node.Version)

	res = testutil.RunCommand(t, []string{"artifacts", "versions", artID, "--output", "table"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Contains(t, res.Stdout, "Version:  5")
	assert.Contains(t, res.Stdout, ver2ID)
}

// ── artifacts source ─────────────────────────────────────────────────────────

func sourcePageResp(path, text string, start, end int, cursor string, eof bool) map[string]any {
	var next any
	if cursor != "" {
		next = cursor
	}
	return map[string]any{
		"artifactId": artID, "versionId": verID, "path": path, "mediaType": "text/tsx",
		"contentDigest": strings.Repeat("b", 64), "projectionDigest": strings.Repeat("c", 64),
		"text": text, "startOffset": start, "endOffset": end, "nextCursor": next, "eof": eof,
	}
}

func TestArtifactsSource_DefaultsToCurrentEntrypointAndPagesUntilEOF(t *testing.T) {
	var reads []url.Values
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /artifacts/" + artID: func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, artifactFixture())
		},
		"GET /artifacts/" + artID + "/versions/" + verID + "/source": func(w http.ResponseWriter, r *http.Request) {
			reads = append(reads, r.URL.Query())
			if r.URL.Query().Get("cursor") == "" {
				testutil.RespondJSON(w, http.StatusOK, sourcePageResp("src/main.tsx", "const a = 1;\n", 0, 13, "next", false))
				return
			}
			testutil.RespondJSON(w, http.StatusOK, sourcePageResp("src/main.tsx", "export {};", 13, 23, "", true))
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"artifacts", "source", artID, "--output", "plain"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Equal(t, "const a = 1;\nexport {};", res.Stdout)
	require.Len(t, reads, 2)
	assert.Equal(t, "src/main.tsx", reads[0].Get("path"))
	assert.Equal(t, "next", reads[1].Get("cursor"))
}

func TestArtifactsSource_ExplicitVersionAndPathJSON(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /artifacts/" + artID + "/versions/" + ver2ID + "/source": func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "src/App.tsx", r.URL.Query().Get("path"))
			resp := sourcePageResp("src/App.tsx", "app", 0, 3, "", true)
			resp["versionId"] = ver2ID
			testutil.RespondJSON(w, http.StatusOK, resp)
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"artifacts", "source", artID, "--version", ver2ID, "--path", "src/App.tsx", "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	var out struct {
		VersionID  string `json:"versionId"`
		Path       string `json:"path"`
		Text       string `json:"text"`
		ByteLength int    `json:"byteLength"`
	}
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &out))
	assert.Equal(t, ver2ID, out.VersionID)
	assert.Equal(t, "app", out.Text)
	assert.Equal(t, 3, out.ByteLength)
}

func TestArtifactsSource_MissingFileIsAnError(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /artifacts/" + artID + "/versions/" + verID + "/source": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondError(w, http.StatusBadRequest, "NOT_FOUND", "File not found in the pinned version")
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"artifacts", "source", artID, "--version", verID, "--path", "nope.js", "--json"}, "")
	assert.Equal(t, 1, res.ExitCode)
	assert.Contains(t, res.Stderr, "File not found")
}

// ── artifacts design ─────────────────────────────────────────────────────────

func TestArtifactsDesign_DefaultVersion(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /artifacts/" + artID: func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, artifactFixture())
		},
		"GET /artifacts/" + artID + "/versions/" + verID + "/design": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"schemaVersion": 1, "sourceArtifactId": artID, "sourceVersionId": verID,
				"designDigest": strings.Repeat("d", 64), "runtime": "vite_react",
				"files":        []map[string]any{{"path": "design/tokens.css", "mediaType": "text/css", "content": ":root{}"}},
				"dependencies": []map[string]any{{"name": "react", "version": "19.0.0"}},
				"sharedDesign": map[string]any{"version": 1, "tokensPath": "design/tokens.css"},
				"assets":       []map[string]any{{"id": "logo", "path": "assets/logo.svg", "mediaType": "image/svg+xml", "contentDigest": strings.Repeat("e", 64), "usage": "logo"}},
			})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"artifacts", "design", artID, "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	var out struct {
		DesignDigest string `json:"designDigest"`
		Files        []struct {
			Content string `json:"content"`
		} `json:"files"`
	}
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &out))
	assert.Equal(t, strings.Repeat("d", 64), out.DesignDigest)
	require.Len(t, out.Files, 1)
	assert.Equal(t, ":root{}", out.Files[0].Content)

	res = testutil.RunCommand(t, []string{"artifacts", "design", artID, "--output", "table"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	for _, want := range []string{"Tokens:    design/tokens.css", "react@19.0.0", "assets/logo.svg  (logo, image/svg+xml)"} {
		assert.Contains(t, res.Stdout, want)
	}
}

// ── artifacts starters ───────────────────────────────────────────────────────

func TestArtifactsStarters(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /artifacts/starters": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{"starters": []map[string]any{{
				"id": "minimal", "title": "Minimal", "runtime": "vite_react",
				"files":        []map[string]any{{"path": "index.html", "mediaType": "text/html", "content": "<html></html>"}},
				"dependencies": []map[string]any{},
				"sharedDesign": map[string]any{"version": 1},
			}}})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"artifacts", "starters", "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	var out struct {
		Starters []struct {
			ID string `json:"id"`
		} `json:"starters"`
	}
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &out))
	require.Len(t, out.Starters, 1)
	assert.Equal(t, "minimal", out.Starters[0].ID)

	res = testutil.RunCommand(t, []string{"artifacts", "starters", "--output", "table"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Contains(t, res.Stdout, "Title:    Minimal")
}
