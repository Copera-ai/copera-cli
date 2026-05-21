package commands_test

import (
	"encoding/json"
	"net/http"
	"os"
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

// ── tables export ───────────────────────────────────────────────────────────

func TestTablesExport_Inline(t *testing.T) {
	var capturedBody map[string]any
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"POST /board/board1/table/table1/export": func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&capturedBody)
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"fileName": "tasks.csv",
				"mimeType": "text/csv",
				"payload":  "id,title\n1,hello\n",
				"rowCount": 1,
				"columns":  []map[string]any{},
				"rows":     []map[string]any{},
			})
		},
	}.Handler())
	setupHomeWithBoard(t, srv.URL)

	res := testutil.RunCommand(t, []string{
		"tables", "export", "table1",
		"--view", "view1", "--format", "CSV",
		"--output", "table",
	}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)

	assert.Equal(t, "board1", capturedBody["boardId"])
	assert.Equal(t, "view1", capturedBody["viewId"])
	assert.Equal(t, "CSV", capturedBody["format"])
	assert.Contains(t, res.Stdout, "id,title")
	assert.Contains(t, res.Stderr, "Export complete (1 rows, tasks.csv).")
}

func TestTablesExport_AsyncJob(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"POST /board/board1/table/table1/export": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"asyncJob": map[string]any{
					"jobId":     "job_abc",
					"status":    "queued",
					"format":    "PDF",
					"expiresAt": "2026-01-01T00:00:00Z",
				},
			})
		},
	}.Handler())
	setupHomeWithBoard(t, srv.URL)

	res := testutil.RunCommand(t, []string{
		"tables", "export", "table1",
		"--view", "view1", "--format", "PDF",
		"--force-async", "--output", "table",
	}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Contains(t, res.Stdout, "Job:    job_abc")
	assert.Contains(t, res.Stdout, "Status: queued")
	assert.Contains(t, res.Stderr, "Export queued.")
}

func TestTablesExport_MissingView(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{}.Handler())
	setupHomeWithBoard(t, srv.URL)

	res := testutil.RunCommand(t, []string{
		"tables", "export", "table1", "--format", "CSV",
	}, "")
	assert.Equal(t, 2, res.ExitCode)
	assert.Contains(t, res.Stderr, "--view is required")
}

func TestTablesExport_InvalidFormat(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{}.Handler())
	setupHomeWithBoard(t, srv.URL)

	res := testutil.RunCommand(t, []string{
		"tables", "export", "table1", "--view", "view1", "--format", "PARQUET",
	}, "")
	assert.Equal(t, 2, res.ExitCode)
	assert.Contains(t, res.Stderr, "invalid format")
}

func TestTablesExport_WriteToFile(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"POST /board/board1/table/table1/export": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"fileName": "tasks.csv",
				"mimeType": "text/csv",
				"payload":  "id,title\n1,hello\n",
				"rowCount": 1,
				"columns":  []map[string]any{},
				"rows":     []map[string]any{},
			})
		},
	}.Handler())
	setupHomeWithBoard(t, srv.URL)

	out := t.TempDir() + "/export.csv"
	res := testutil.RunCommand(t, []string{
		"tables", "export", "table1",
		"--view", "view1", "--format", "CSV",
		"-o", out,
		"--output", "table",
	}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)

	data, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Equal(t, "id,title\n1,hello\n", string(data))
}
