package commands_test

import (
	"net/http"
	"testing"

	"github.com/copera/copera-cli/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDesignsAlias(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /artifacts/{$}": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"edges": []map[string]any{{"cursor": "1", "node": artifactFixture()}},
			})
		},
	}.Handler())
	setupHome(t, srv.URL)
	canonical := testutil.RunCommand(t, []string{"designs", "list", "--json"}, "")
	require.Equal(t, 0, canonical.ExitCode, canonical.Stderr)
	alias := testutil.RunCommand(t, []string{"artifacts", "list", "--json"}, "")
	require.Equal(t, 0, alias.ExitCode, alias.Stderr)
	assert.Equal(t, canonical.Stdout, alias.Stdout)
	assert.Contains(t, canonical.Stdout, artID)
}

func TestDesignsOpenAndLog(t *testing.T) {
	link := "https://app.copera.ai/acme/artifacts/" + artID
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /artifacts/" + artID + "/handoff": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"artifact": map[string]string{"id": artID, "url": link},
			})
		},
		"GET /artifacts/" + artID + "/versions": func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("after") == "next" {
				testutil.RespondJSON(w, http.StatusOK, map[string]any{"edges": []map[string]any{
					{"node": map[string]any{"version": 3, "createdAt": "2026-10-01T10:00:00Z"}},
				}})
				return
			}
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"edges": []map[string]any{{"node": map[string]any{
					"version": 4, "createdAt": "2026-10-02T10:00:00Z", "origin": "public_api",
					"authorKind": "user", "tool": "claude_code", "changeSummary": "Add contact page",
				}}},
				"hasNextPage": true, "endCursor": "next",
			})
		},
	}.Handler())
	setupHome(t, srv.URL)
	opened := testutil.RunCommand(t, []string{"designs", "open", link, "--json"}, "")
	require.Equal(t, 0, opened.ExitCode, opened.Stderr)
	assert.JSONEq(t, `{"url":"`+link+`"}`, opened.Stdout)

	log := testutil.RunCommand(t, []string{"designs", "log", artID, "--json"}, "")
	require.Equal(t, 0, log.ExitCode, log.Stderr)
	assert.JSONEq(t, `[
		{"version":4,"date":"2026-10-02T10:00:00Z","origin":"public_api","authorKind":"user","tool":"claude_code","changeSummary":"Add contact page"},
		{"version":3,"date":"2026-10-01T10:00:00Z","origin":null,"authorKind":null,"tool":null,"changeSummary":null}
	]`, log.Stdout)

	table := testutil.RunCommand(t, []string{"designs", "log", artID, "--output", "table"}, "")
	require.Equal(t, 0, table.ExitCode, table.Stderr)
	for _, value := range []string{"VERSION", "DATE", "ORIGIN", "AUTHOR", "TOOL", "CHANGE SUMMARY", "4", "2026-10-02T10:00:00Z", "public_api", "user", "claude_code", "Add contact page"} {
		assert.Contains(t, table.Stdout, value)
	}
}

func TestDesignsGuide(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /artifacts/guide": func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "landing_page", r.URL.Query().Get("kind"))
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"version": 1, "kind": "website", "markdown": "# Author a Design\n", "facts": map[string]any{},
			})
		},
	}.Handler())
	setupHome(t, srv.URL)
	res := testutil.RunCommand(t, []string{"designs", "guide", "--kind", "landing_page", "--output", "plain"}, "")
	require.Equal(t, 0, res.ExitCode, res.Stderr)
	assert.Equal(t, "# Author a Design\n", res.Stdout)
}
