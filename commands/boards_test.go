package commands_test

import (
	"encoding/json"
	"net/http"
	"net/url"
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

func TestBoardsList_Query(t *testing.T) {
	var capturedQuery url.Values
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /board/list-boards": func(w http.ResponseWriter, r *http.Request) {
			capturedQuery = r.URL.Query()
			testutil.RespondJSON(w, http.StatusOK, []map[string]any{})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"boards", "list", "--query", "roadmap", "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Equal(t, "roadmap", capturedQuery.Get("q"))
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

// ── boards create ─────────────────────────────────────────────────────────────

func TestBoardsCreate_JSON(t *testing.T) {
	var captured map[string]any
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"POST /board": func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&captured)
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"_id": "b100", "name": "Roadmap", "description": "Plans", "defaultTableId": "t100",
			})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{
		"boards", "create", "--name", "Roadmap", "--description", "Plans", "--table-name", "Tasks", "--json",
	}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Equal(t, "Roadmap", captured["name"])
	assert.Equal(t, "Plans", captured["description"])
	assert.Equal(t, "Tasks", captured["tableName"])

	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &out))
	assert.Equal(t, "b100", out["_id"])
	assert.Equal(t, "t100", out["defaultTableId"])
}

func TestBoardsCreate_Human(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"POST /board": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"_id": "b100", "name": "Roadmap", "defaultTableId": "t100",
			})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"boards", "create", "--name", "Roadmap", "--output", "table"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Contains(t, res.Stdout, "ID:              b100")
	assert.Contains(t, res.Stdout, "Name:            Roadmap")
	assert.Contains(t, res.Stdout, "Default table:   t100")
}

func TestBoardsCreate_MissingName(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"boards", "create"}, "")
	assert.Equal(t, 2, res.ExitCode)
	assert.Contains(t, res.Stderr, "board name is required")
}

// ── boards update ─────────────────────────────────────────────────────────────

func TestBoardsUpdate_JSON(t *testing.T) {
	var captured map[string]any
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"PATCH /board/b100": func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&captured)
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"_id": "b100", "name": "Renamed", "description": "New desc",
			})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{
		"boards", "update", "b100", "--name", "Renamed", "--description", "New desc", "--json",
	}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Equal(t, "Renamed", captured["name"])
	assert.Equal(t, "New desc", captured["description"])
	_, hasIcon := captured["icon"]
	assert.False(t, hasIcon, "icon should be omitted when not passed")

	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &out))
	assert.Equal(t, "Renamed", out["name"])
}

func TestBoardsUpdate_NothingToUpdate(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"boards", "update", "b100"}, "")
	assert.Equal(t, 2, res.ExitCode)
	assert.Contains(t, res.Stderr, "nothing to update")
}

// ── boards delete ─────────────────────────────────────────────────────────────

func TestBoardsDelete_Force(t *testing.T) {
	var hit bool
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"DELETE /board/b100": func(w http.ResponseWriter, r *http.Request) {
			hit = true
			testutil.RespondJSON(w, http.StatusOK, map[string]any{"success": true})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"boards", "delete", "b100", "--force", "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.True(t, hit)
	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &out))
	assert.Equal(t, true, out["deleted"])
}

func TestBoardsDelete_ConfirmYes(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"DELETE /board/b100": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{"success": true})
		},
	}.Handler())
	setupHome(t, srv.URL)
	testutil.SetEnv(t, "CI", "")

	res := testutil.RunCommand(t, []string{"boards", "delete", "b100", "--output", "table"}, "y\n")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Contains(t, res.Stderr, "Board deleted.")
}

func TestBoardsDelete_RequiresForceNonInteractive(t *testing.T) {
	var hit bool
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"DELETE /board/b100": func(w http.ResponseWriter, r *http.Request) {
			hit = true
			testutil.RespondJSON(w, http.StatusOK, map[string]any{"success": true})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"boards", "delete", "b100", "--no-input"}, "")
	assert.Equal(t, 2, res.ExitCode)
	assert.Contains(t, res.Stderr, "confirmation_required")
	assert.False(t, hit)
}

// ── boards participants ─────────────────────────────────────────────────────

func TestBoardsParticipantsAdd_JSON(t *testing.T) {
	var captured map[string]any
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"POST /board/b100/participants": func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&captured)
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"boardId": "b100", "participantUserIds": []string{"u001", "u002"},
			})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{
		"boards", "participants", "add", "--board", "b100",
		"--participant", "u001", "--participant", "u002", "--json",
	}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	ids := captured["participantIds"].([]any)
	assert.Len(t, ids, 2)

	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &out))
	assert.Equal(t, "b100", out["boardId"])
}

func TestBoardsParticipantsAdd_MissingParticipant(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"boards", "participants", "add", "--board", "b100"}, "")
	assert.Equal(t, 2, res.ExitCode)
	assert.Contains(t, res.Stderr, "at least one participant is required")
}

func TestBoardsParticipantsRemove_Force(t *testing.T) {
	var hit bool
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"DELETE /board/b100/participant/u001": func(w http.ResponseWriter, r *http.Request) {
			hit = true
			testutil.RespondJSON(w, http.StatusOK, map[string]any{"success": true})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{
		"boards", "participants", "remove", "u001", "--board", "b100", "--force", "--json",
	}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.True(t, hit)
	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &out))
	assert.Equal(t, true, out["removed"])
}

// ── tables create/update/delete ─────────────────────────────────────────────

func TestTablesCreate_JSON(t *testing.T) {
	var captured map[string]any
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"POST /board/board1/table": func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&captured)
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"_id": "tb1", "name": "Tasks", "boardId": "board1",
			})
		},
	}.Handler())
	setupHomeWithBoard(t, srv.URL)

	res := testutil.RunCommand(t, []string{"tables", "create", "--name", "Tasks", "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Equal(t, "Tasks", captured["name"])

	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &out))
	assert.Equal(t, "tb1", out["_id"])
	assert.Equal(t, "board1", out["boardId"])
}

func TestTablesCreate_MissingName(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{}.Handler())
	setupHomeWithBoard(t, srv.URL)

	res := testutil.RunCommand(t, []string{"tables", "create"}, "")
	assert.Equal(t, 2, res.ExitCode)
	assert.Contains(t, res.Stderr, "table name is required")
}

func TestTablesUpdate_JSON(t *testing.T) {
	var captured map[string]any
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"PATCH /board/board1/table/tb1": func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&captured)
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"_id": "tb1", "name": "Renamed", "boardId": "board1",
			})
		},
	}.Handler())
	setupHomeWithBoard(t, srv.URL)

	res := testutil.RunCommand(t, []string{"tables", "update", "tb1", "--name", "Renamed", "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Equal(t, "Renamed", captured["name"])
}

func TestTablesDelete_Force(t *testing.T) {
	var hit bool
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"DELETE /board/board1/table/tb1": func(w http.ResponseWriter, r *http.Request) {
			hit = true
			testutil.RespondJSON(w, http.StatusOK, map[string]any{"success": true})
		},
	}.Handler())
	setupHomeWithBoard(t, srv.URL)

	res := testutil.RunCommand(t, []string{"tables", "delete", "tb1", "--force", "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.True(t, hit)
}

func TestTablesDelete_RequiresForceNonInteractive(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"DELETE /board/board1/table/tb1": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{"success": true})
		},
	}.Handler())
	setupHomeWithBoard(t, srv.URL)

	res := testutil.RunCommand(t, []string{"tables", "delete", "tb1", "--no-input"}, "")
	assert.Equal(t, 2, res.ExitCode)
	assert.Contains(t, res.Stderr, "confirmation_required")
}

// ── tables columns ──────────────────────────────────────────────────────────

func TestTablesColumnsAdd_JSON(t *testing.T) {
	var captured map[string]any
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"POST /board/board1/table/table1/column": func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&captured)
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"columnId": "col1", "label": "Priority", "type": "STATUS",
				"options": []map[string]any{
					{"optionId": "o1", "label": "Low", "color": "green"},
					{"optionId": "o2", "label": "High", "color": "red"},
				},
			})
		},
	}.Handler())
	setupHomeWithBoard(t, srv.URL)

	res := testutil.RunCommand(t, []string{
		"tables", "columns", "add", "--label", "Priority", "--type", "status",
		"--option", "Low:green", "--option", "High:red", "--json",
	}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	// type is uppercased before send
	assert.Equal(t, "STATUS", captured["type"])
	assert.Equal(t, "Priority", captured["label"])
	opts := captured["options"].([]any)
	assert.Len(t, opts, 2)
	first := opts[0].(map[string]any)
	assert.Equal(t, "Low", first["label"])
	assert.Equal(t, "green", first["color"])

	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &out))
	assert.Equal(t, "col1", out["columnId"])
}

func TestTablesColumnsAdd_InvalidType(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{}.Handler())
	setupHomeWithBoard(t, srv.URL)

	res := testutil.RunCommand(t, []string{
		"tables", "columns", "add", "--label", "X", "--type", "BOGUS",
	}, "")
	assert.Equal(t, 2, res.ExitCode)
	assert.Contains(t, res.Stderr, "invalid column type")
}

func TestTablesColumnsUpdate_JSON(t *testing.T) {
	var captured map[string]any
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"PATCH /board/board1/table/table1/column/col1": func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&captured)
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"columnId": "col1", "label": "Stage", "type": "STATUS",
			})
		},
	}.Handler())
	setupHomeWithBoard(t, srv.URL)

	res := testutil.RunCommand(t, []string{
		"tables", "columns", "update", "col1", "--label", "Stage", "--add-option", "Blocked:red", "--json",
	}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Equal(t, "Stage", captured["label"])
	add := captured["addOptions"].([]any)
	assert.Len(t, add, 1)
}

func TestTablesColumnsUpdate_NothingToUpdate(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{}.Handler())
	setupHomeWithBoard(t, srv.URL)

	res := testutil.RunCommand(t, []string{"tables", "columns", "update", "col1"}, "")
	assert.Equal(t, 2, res.ExitCode)
	assert.Contains(t, res.Stderr, "nothing to update")
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
