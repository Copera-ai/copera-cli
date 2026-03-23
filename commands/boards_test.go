package commands_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/copera/copera-cli/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── boards list ──────────────────────────────────────────────────────────────

func TestBoardsList_JSON(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /board/list-boards": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, []map[string]any{
				{"_id": "board1", "name": "Alpha", "description": "First board", "createdAt": "2025-01-01T00:00:00Z", "updatedAt": "2025-06-01T00:00:00Z"},
				{"_id": "board2", "name": "Beta", "description": "", "createdAt": "2025-02-01T00:00:00Z", "updatedAt": "2025-07-01T00:00:00Z"},
			})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"boards", "list", "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)

	var boards []map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &boards))
	assert.Len(t, boards, 2)
	assert.Equal(t, "Alpha", boards[0]["name"])
}

func TestBoardsList_HumanOutput(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /board/list-boards": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, []map[string]any{
				{"_id": "board1", "name": "Alpha", "description": "First board", "createdAt": "2025-01-01T00:00:00Z", "updatedAt": "2025-06-01T00:00:00Z"},
				{"_id": "board2", "name": "Beta", "description": "", "createdAt": "2025-02-01T00:00:00Z", "updatedAt": "2025-07-01T00:00:00Z"},
			})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"boards", "list", "--output", "table"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Contains(t, res.Stdout, "ID:          board1")
	assert.Contains(t, res.Stdout, "Name:        Alpha")
	assert.Contains(t, res.Stdout, "Description: First board")
	assert.Contains(t, res.Stdout, "ID:          board2")
	assert.Contains(t, res.Stdout, "Name:        Beta")
}

func TestBoardsList_BasesAlias(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /board/list-boards": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, []map[string]any{})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"bases", "list", "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
}

// ── boards get ───────────────────────────────────────────────────────────────

func TestBoardsGet_JSON(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /board/board1": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"_id": "board1", "name": "Alpha", "description": "First board",
				"createdAt": "2025-01-01T00:00:00Z", "updatedAt": "2025-06-01T00:00:00Z",
			})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"boards", "get", "board1", "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)

	var board map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &board))
	assert.Equal(t, "Alpha", board["name"])
}

func TestBoardsGet_HumanOutput(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /board/board1": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"_id": "board1", "name": "Alpha", "description": "Desc here",
				"createdAt": "2025-01-01T00:00:00Z", "updatedAt": "2025-06-01T00:00:00Z",
			})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"boards", "get", "board1", "--output", "table"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Contains(t, res.Stdout, "ID:          board1")
	assert.Contains(t, res.Stdout, "Name:        Alpha")
	assert.Contains(t, res.Stdout, "Description: Desc here")
}

func TestBoardsGet_MissingToken(t *testing.T) {
	home := t.TempDir()
	testutil.WriteTempConfigAt(t, home+"/.copera.toml", `
[profiles.default]
`)
	testutil.SetEnv(t, "HOME", home)

	res := testutil.RunCommand(t, []string{"boards", "get", "board1"}, "")
	assert.Equal(t, 4, res.ExitCode)
}
