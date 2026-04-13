package commands_test

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/copera/copera-cli/internal/cache"
	"github.com/copera/copera-cli/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// treeResp builds a mock tree API response using the real shape.
func treeResp(nodes []map[string]any) map[string]any {
	return map[string]any{"root": nodes, "totalDocs": len(nodes), "truncated": false}
}

func setupHome(t *testing.T, apiURL string) string {
	t.Helper()
	home := t.TempDir()
	testutil.WriteTempConfigAt(t, filepath.Join(home, ".copera.toml"), `
[profiles.default]
token = "tok_test_fake"

[api]
base_url = "`+apiURL+`"
`)
	testutil.SetEnv(t, "HOME", home)
	return home
}

func setupHomeWithCache(t *testing.T, apiURL, cacheDir string) {
	t.Helper()
	home := t.TempDir()
	testutil.WriteTempConfigAt(t, filepath.Join(home, ".copera.toml"), `
[profiles.default]
token = "tok_test_fake"

[api]
base_url = "`+apiURL+`"

[cache]
dir = "`+cacheDir+`"
ttl = "1h"
`)
	testutil.SetEnv(t, "HOME", home)
}

// ── tree ──────────────────────────────────────────────────────────────────────

func TestDocsTree_JSON(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /docs/tree": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, treeResp([]map[string]any{
				{"_id": "aaa111", "title": "Root Doc", "hasChildren": false},
				{"_id": "bbb222", "title": "Child Doc", "hasChildren": true},
			}))
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"docs", "tree", "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)

	var nodes []map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &nodes))
	assert.Len(t, nodes, 2)
	assert.Equal(t, "Root Doc", nodes[0]["title"])
}

func TestDocsTree_HumanFormat(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /docs/tree": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, treeResp([]map[string]any{
				{"_id": "aaa111", "title": "Alpha", "hasChildren": true, "children": []map[string]any{
					{"_id": "ccc333", "title": "Child One", "hasChildren": false},
					{"_id": "ddd444", "title": "Child Two", "hasChildren": false},
				}},
				{"_id": "bbb222", "title": "Beta", "hasChildren": false},
			}))
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"docs", "tree", "--output", "table"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Contains(t, res.Stdout, "Alpha")
	assert.Contains(t, res.Stdout, "Beta")
	assert.Contains(t, res.Stdout, "aaa111")
	assert.Contains(t, res.Stdout, "Child One")
	assert.Contains(t, res.Stdout, "Child Two")
}

func TestDocsTree_ParentFlag(t *testing.T) {
	var capturedParent string
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /docs/tree": func(w http.ResponseWriter, r *http.Request) {
			capturedParent = r.URL.Query().Get("parentId")
			testutil.RespondJSON(w, http.StatusOK, treeResp([]map[string]any{
				{"_id": "child1", "title": "Child", "hasChildren": false},
			}))
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"docs", "tree", "--parent", "parentXYZ", "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Equal(t, "parentXYZ", capturedParent)
}

// ── search ────────────────────────────────────────────────────────────────────

func TestDocsSearch_PassesParams(t *testing.T) {
	var capturedQuery string
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /docs/search": func(w http.ResponseWriter, r *http.Request) {
			capturedQuery = r.URL.Query().Get("q")
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"hits": []map[string]any{
					{
						"_id":       "doc1",
						"title":     "Match",
						"highlight": map[string]any{"title": "<em>match</em>", "mdBody": ""},
						"createdAt": "1766424420119",
						"updatedAt": "1766424420119",
					},
				},
				"totalHits": 1,
			})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"docs", "search", "runbook", "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Equal(t, "runbook", capturedQuery)

	var hits []map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &hits))
	assert.Len(t, hits, 1)
}

func TestDocsSearch_ListOutput(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /docs/search": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"hits": []map[string]any{
					{
						"_id":   "doc1",
						"title": "Deploy Runbook",
						"parents": []map[string]any{
							{"_id": "par1", "title": "Engineering"},
							{"_id": "par2", "title": "Ops"},
						},
						"highlight": map[string]any{"title": "deploy title match", "mdBody": "deploy the service"},
						"createdAt": "1766424420119",
						"updatedAt": "1766424420119",
					},
				},
				"totalHits": 1,
			})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"docs", "search", "deploy", "--output", "table"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Contains(t, res.Stdout, "ID:        doc1")
	assert.Contains(t, res.Stdout, "Title:     Deploy Runbook")
	assert.Contains(t, res.Stdout, "Match:     deploy the service")
	assert.Contains(t, res.Stdout, "Ancestors: Engineering (par1) > Ops (par2)")
}

// ── content + cache ───────────────────────────────────────────────────────────

func TestDocsContent_CachesOnSecondCall(t *testing.T) {
	fetchCount := 0
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /docs/doc1/md": func(w http.ResponseWriter, r *http.Request) {
			fetchCount++
			testutil.RespondJSON(w, http.StatusOK, map[string]any{"content": "# Hello"})
		},
	}.Handler())

	cacheDir := t.TempDir()
	setupHomeWithCache(t, srv.URL, cacheDir)

	store := cache.NewMemStore()
	res1 := testutil.RunCommandWithStore(t, []string{"docs", "content", "doc1"}, "", store)
	require.Equal(t, 0, res1.ExitCode, "stderr: %s", res1.Stderr)
	assert.Contains(t, res1.Stdout, "# Hello")
	assert.Equal(t, 1, fetchCount)

	res2 := testutil.RunCommandWithStore(t, []string{"docs", "content", "doc1"}, "", store)
	require.Equal(t, 0, res2.ExitCode)
	assert.Contains(t, res2.Stdout, "# Hello")
	assert.Equal(t, 1, fetchCount, "expected no second API call (cache hit)")
}

func TestDocsContent_NoCacheBypassesCache(t *testing.T) {
	fetchCount := 0
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /docs/doc1/md": func(w http.ResponseWriter, r *http.Request) {
			fetchCount++
			testutil.RespondJSON(w, http.StatusOK, map[string]any{"content": "# Fresh"})
		},
	}.Handler())

	cacheDir := t.TempDir()
	setupHomeWithCache(t, srv.URL, cacheDir)

	store := cache.NewMemStore()
	testutil.RunCommandWithStore(t, []string{"docs", "content", "doc1"}, "", store)
	testutil.RunCommandWithStore(t, []string{"docs", "content", "doc1", "--no-cache"}, "", store)
	assert.Equal(t, 2, fetchCount, "--no-cache should bypass cache")
}

// ── update ────────────────────────────────────────────────────────────────────

func TestDocsUpdate_ReadsStdin(t *testing.T) {
	var capturedBody map[string]string
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"POST /docs/doc1/md": func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&capturedBody)
			w.WriteHeader(http.StatusAccepted)
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"docs", "update", "doc1"}, "# Updated content")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Equal(t, "replace", capturedBody["operation"])
	assert.Equal(t, "# Updated content", capturedBody["content"])
}

func TestDocsUpdate_AppendOperation(t *testing.T) {
	var capturedBody map[string]string
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"POST /docs/doc1/md": func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&capturedBody)
			w.WriteHeader(http.StatusAccepted)
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"docs", "update", "doc1", "--operation", "append", "--content", "new section"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Equal(t, "append", capturedBody["operation"])
	assert.Equal(t, "new section", capturedBody["content"])
}

// ── delete ────────────────────────────────────────────────────────────────────

func TestDocsDelete_RequiresForce_WhenNonInteractive(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{}.Handler())
	setupHome(t, srv.URL)
	testutil.SetEnv(t, "CI", "true")

	res := testutil.RunCommand(t, []string{"docs", "delete", "doc1"}, "")
	assert.Equal(t, 2, res.ExitCode)
	assert.Contains(t, res.Stderr, "--force")
}

func TestDocsDelete_WithForce(t *testing.T) {
	deleted := false
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"DELETE /docs/doc1": func(w http.ResponseWriter, r *http.Request) {
			deleted = true
			testutil.RespondJSON(w, http.StatusOK, map[string]any{"success": true})
		},
	}.Handler())
	setupHome(t, srv.URL)
	testutil.SetEnv(t, "CI", "true")

	res := testutil.RunCommand(t, []string{"docs", "delete", "doc1", "--force"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.True(t, deleted)
}

// ── auth / validation ─────────────────────────────────────────────────────────

func TestDocsMissingToken(t *testing.T) {
	home := t.TempDir()
	testutil.WriteTempConfigAt(t, filepath.Join(home, ".copera.toml"), "[profiles.default]\n")
	testutil.SetEnv(t, "HOME", home)
	testutil.UnsetEnv(t, "COPERA_CLI_AUTH_TOKEN")

	res := testutil.RunCommand(t, []string{"docs", "tree"}, "")
	assert.Equal(t, 4, res.ExitCode)
}

func TestDocsGet_MissingID(t *testing.T) {
	home := t.TempDir()
	testutil.WriteTempConfigAt(t, filepath.Join(home, ".copera.toml"), `
[profiles.default]
token = "tok_test_fake"
`)
	testutil.SetEnv(t, "HOME", home)

	res := testutil.RunCommand(t, []string{"docs", "get"}, "")
	assert.Equal(t, 2, res.ExitCode)
}

// ── docs metadata ───────────────────────────────────────────────────────────

func TestDocsMetadata_Title(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"PATCH /docs/doc1": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"_id": "doc1", "title": "New Title", "starred": false,
				"createdAt": "2025-01-01T00:00:00Z", "updatedAt": "2025-06-15T00:00:00Z",
			})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"docs", "metadata", "doc1", "--title", "New Title", "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)

	var doc map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &doc))
	assert.Equal(t, "New Title", doc["title"])
}

func TestDocsMetadata_NoFlags(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"docs", "metadata", "doc1"}, "")
	assert.Equal(t, 2, res.ExitCode)
}
