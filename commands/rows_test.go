package commands_test

import (
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/copera/copera-cli/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupHomeWithBoard(t *testing.T, apiURL string) {
	t.Helper()
	home := t.TempDir()
	testutil.WriteTempConfigAt(t, filepath.Join(home, ".copera.toml"), `
[profiles.default]
token = "tok_test_fake"
board_id = "board1"
table_id = "table1"

[api]
base_url = "`+apiURL+`"
`)
	testutil.SetEnv(t, "HOME", home)
}

// ── tables list ──────────────────────────────────────────────────────────────

func TestTablesList_JSON(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /board/board1/tables": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, []map[string]any{
				{
					"_id": "t1", "name": "Tasks", "board": "board1",
					"createdAt": "2025-01-01T00:00:00Z", "updatedAt": "2025-06-01T00:00:00Z",
					"columns": []map[string]any{
						{"columnId": "c1", "label": "Status", "type": "STATUS", "order": 0},
					},
				},
			})
		},
	}.Handler())
	setupHomeWithBoard(t, srv.URL)

	res := testutil.RunCommand(t, []string{"tables", "list", "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)

	var tables []map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &tables))
	assert.Len(t, tables, 1)
	assert.Equal(t, "Tasks", tables[0]["name"])
}

func TestTablesList_HumanOutput(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /board/board1/tables": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, []map[string]any{
				{
					"_id": "t1", "name": "Tasks", "board": "board1",
					"createdAt": "2025-01-01T00:00:00Z", "updatedAt": "2025-06-01T00:00:00Z",
					"columns": []map[string]any{
						{"columnId": "c1", "label": "Status", "type": "STATUS", "order": 0},
						{"columnId": "c2", "label": "Title", "type": "TEXT", "order": 1},
					},
				},
			})
		},
	}.Handler())
	setupHomeWithBoard(t, srv.URL)

	res := testutil.RunCommand(t, []string{"tables", "list", "--output", "table"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Contains(t, res.Stdout, "ID:      t1")
	assert.Contains(t, res.Stdout, "Name:    Tasks")
	assert.Contains(t, res.Stdout, "Columns: 2")
}

func TestTablesList_BoardFromFlag(t *testing.T) {
	var capturedPath string
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /board/flagboard/tables": func(w http.ResponseWriter, r *http.Request) {
			capturedPath = r.URL.Path
			testutil.RespondJSON(w, http.StatusOK, []map[string]any{})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"tables", "list", "--board", "flagboard", "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Equal(t, "/board/flagboard/tables", capturedPath)
}

func TestTablesList_MissingBoard(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"tables", "list"}, "")
	assert.Equal(t, 2, res.ExitCode)
}

// ── tables get ───────────────────────────────────────────────────────────────

func TestTablesGet_ColumnsDisplayed(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /board/board1/table/t1": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"_id": "t1", "name": "Tasks", "board": "board1",
				"createdAt": "2025-01-01T00:00:00Z", "updatedAt": "2025-06-01T00:00:00Z",
				"columns": []map[string]any{
					{"columnId": "c1", "label": "Status", "type": "STATUS", "order": 0,
						"options": []map[string]any{
							{"optionId": "o1", "label": "Todo", "color": "GRAY", "statusGroup": "TODO"},
							{"optionId": "o2", "label": "Done", "color": "GREEN", "statusGroup": "DONE"},
						}},
					{"columnId": "c2", "label": "Title", "type": "TEXT", "order": 1},
				},
			})
		},
	}.Handler())
	setupHomeWithBoard(t, srv.URL)

	res := testutil.RunCommand(t, []string{"tables", "get", "t1", "--output", "table"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Contains(t, res.Stdout, "Columns:")
	assert.Contains(t, res.Stdout, "STATUS")
	assert.Contains(t, res.Stdout, "Todo")
	assert.Contains(t, res.Stdout, "Title")
}

// ── rows list ────────────────────────────────────────────────────────────────

func TestRowsList_JSON(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /board/board1/table/table1/rows": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, []map[string]any{
				{
					"_id": "r1", "rowId": 1, "owner": "user1", "table": "table1", "board": "board1",
					"createdAt": "2025-01-01T00:00:00Z", "updatedAt": "2025-06-01T00:00:00Z",
					"columns": []map[string]any{
						{"columnId": "c1", "value": "Todo"},
					},
				},
			})
		},
	}.Handler())
	setupHomeWithBoard(t, srv.URL)

	res := testutil.RunCommand(t, []string{"rows", "list", "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)

	var rows []map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &rows))
	assert.Len(t, rows, 1)
}

func TestRowsList_MissingTable(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{}.Handler())
	// config has board_id but no table_id
	home := t.TempDir()
	testutil.WriteTempConfigAt(t, filepath.Join(home, ".copera.toml"), `
[profiles.default]
token = "tok_test_fake"
board_id = "board1"

[api]
base_url = "`+srv.URL+`"
`)
	testutil.SetEnv(t, "HOME", home)

	res := testutil.RunCommand(t, []string{"rows", "list"}, "")
	assert.Equal(t, 2, res.ExitCode)
}

// ── rows get ─────────────────────────────────────────────────────────────────

func TestRowsGet_HumanOutput(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /board/board1/table/table1/row/r1": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"_id": "r1", "rowId": 42, "owner": "user1", "table": "table1", "board": "board1",
				"createdAt": "2025-01-01T00:00:00Z", "updatedAt": "2025-06-01T00:00:00Z",
				"columns": []map[string]any{
					{"columnId": "c1", "value": "opt_inprog"},
					{"columnId": "c2", "value": "Fix the bug"},
					{"columnId": "c3",
						"value":     []any{"row1", "row2"},
						"linkValue": []any{"Task Alpha", "Task Beta"},
					},
				},
			})
		},
		"GET /board/board1/table/table1": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"_id": "table1", "name": "Tasks", "board": "board1",
				"createdAt": "2025-01-01T00:00:00Z", "updatedAt": "2025-06-01T00:00:00Z",
				"columns": []map[string]any{
					{"columnId": "c1", "label": "Status", "type": "STATUS", "order": 0,
						"options": []map[string]any{
							{"optionId": "opt_inprog", "label": "In Progress", "color": "BLUE", "statusGroup": "IN_PROGRESS"},
						}},
					{"columnId": "c2", "label": "Title", "type": "TEXT", "order": 1},
					{"columnId": "c3", "label": "Related Tasks", "type": "LINK", "order": 2},
				},
			})
		},
	}.Handler())
	setupHomeWithBoard(t, srv.URL)

	res := testutil.RunCommand(t, []string{"rows", "get", "r1", "--output", "table"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Contains(t, res.Stdout, "ID:      r1")
	assert.Contains(t, res.Stdout, "Row#:    42")
	assert.Contains(t, res.Stdout, "Status: In Progress")
	assert.Contains(t, res.Stdout, "Title: Fix the bug")
	assert.Contains(t, res.Stdout, "Related Tasks: Task Alpha, Task Beta")
}

// ── rows create ──────────────────────────────────────────────────────────────

func TestRowsCreate_JSON(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"POST /board/board1/table/table1/row": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"_id": "new1", "rowId": 99, "owner": "user1", "table": "table1", "board": "board1",
				"createdAt": "2025-01-01T00:00:00Z", "updatedAt": "2025-06-01T00:00:00Z",
				"columns": []map[string]any{{"columnId": "c1", "value": "test"}},
			})
		},
	}.Handler())
	setupHomeWithBoard(t, srv.URL)

	res := testutil.RunCommand(t, []string{
		"rows", "create", "--json",
		"--data", `{"columns":[{"columnId":"c1","value":"test"}]}`,
	}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)

	var row map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &row))
	assert.Equal(t, "new1", row["_id"])
}

func TestRowsCreate_Stdin(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"POST /board/board1/table/table1/row": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"_id": "new2", "rowId": 100, "owner": "user1", "table": "table1", "board": "board1",
				"createdAt": "2025-01-01T00:00:00Z", "updatedAt": "2025-06-01T00:00:00Z",
				"columns": []map[string]any{},
			})
		},
	}.Handler())
	setupHomeWithBoard(t, srv.URL)

	stdin := `{"columns":[{"columnId":"c1","value":"piped"}]}`
	res := testutil.RunCommand(t, []string{"rows", "create", "--json"}, stdin)
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
}

func TestRowsCreate_MissingData(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{}.Handler())
	setupHomeWithBoard(t, srv.URL)

	res := testutil.RunCommand(t, []string{"rows", "create"}, "")
	assert.Equal(t, 2, res.ExitCode)
}

// ── rows update ─────────────────────────────────────────────────────────────

func TestRowsUpdate_JSON(t *testing.T) {
	var capturedBody []byte
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"PATCH /board/board1/table/table1/row/r1": func(w http.ResponseWriter, r *http.Request) {
			capturedBody, _ = io.ReadAll(r.Body)
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"_id": "r1", "rowId": 42, "owner": "user1", "table": "table1", "board": "board1",
				"createdAt": "2025-01-01T00:00:00Z", "updatedAt": "2025-06-15T00:00:00Z",
				"columns": []map[string]any{{"columnId": "c1", "value": "updated"}},
			})
		},
	}.Handler())
	setupHomeWithBoard(t, srv.URL)

	res := testutil.RunCommand(t, []string{
		"rows", "update", "r1", "--json",
		"--data", `{"columns":[{"columnId":"c1","value":"updated"}]}`,
	}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)

	var row map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &row))
	assert.Equal(t, "r1", row["_id"])

	var body map[string]any
	require.NoError(t, json.Unmarshal(capturedBody, &body))
	cols := body["columns"].([]any)
	assert.Len(t, cols, 1)
}

func TestRowsUpdate_Stdin(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"PATCH /board/board1/table/table1/row/r1": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"_id": "r1", "rowId": 42, "owner": "user1", "table": "table1", "board": "board1",
				"createdAt": "2025-01-01T00:00:00Z", "updatedAt": "2025-06-15T00:00:00Z",
				"columns": []map[string]any{},
			})
		},
	}.Handler())
	setupHomeWithBoard(t, srv.URL)

	stdin := `{"columns":[{"columnId":"c1","value":"piped"}]}`
	res := testutil.RunCommand(t, []string{"rows", "update", "r1", "--json"}, stdin)
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
}

// ── rows delete ─────────────────────────────────────────────────────────────

func TestRowsDelete_Force(t *testing.T) {
	var deleteCalled bool
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"DELETE /board/board1/table/table1/row/r1": func(w http.ResponseWriter, r *http.Request) {
			deleteCalled = true
			w.WriteHeader(http.StatusNoContent)
		},
	}.Handler())
	setupHomeWithBoard(t, srv.URL)

	res := testutil.RunCommand(t, []string{"rows", "delete", "r1", "--force", "--output", "table"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.True(t, deleteCalled)
	assert.Contains(t, res.Stderr, "Row deleted.")
}

func TestRowsDelete_JSON(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"DELETE /board/board1/table/table1/row/r1": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		},
	}.Handler())
	setupHomeWithBoard(t, srv.URL)

	res := testutil.RunCommand(t, []string{"rows", "delete", "r1", "--force", "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)

	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &out))
	assert.Equal(t, true, out["deleted"])
}

// ── rows authenticate ───────────────────────────────────────────────────────

func TestRowsAuthenticate_JSON(t *testing.T) {
	var capturedBody []byte
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"POST /board/board1/table/table1/row/authenticate": func(w http.ResponseWriter, r *http.Request) {
			capturedBody, _ = io.ReadAll(r.Body)
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"_id": "r1", "rowId": 42, "owner": "user1", "table": "table1", "board": "board1",
				"createdAt": "2025-01-01T00:00:00Z", "updatedAt": "2025-06-01T00:00:00Z",
				"columns": []map[string]any{},
			})
		},
	}.Handler())
	setupHomeWithBoard(t, srv.URL)

	res := testutil.RunCommand(t, []string{
		"rows", "authenticate", "--json",
		"--identifier-column", "col_email",
		"--identifier-value", "mike@acme.com",
		"--password-column", "col_pass",
		"--password-value", "secret",
	}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)

	var body map[string]any
	require.NoError(t, json.Unmarshal(capturedBody, &body))
	assert.Equal(t, "col_email", body["identifierColumnId"])
	assert.Equal(t, "mike@acme.com", body["identifierColumnValue"])
	assert.Equal(t, "col_pass", body["passwordColumnId"])
	assert.Equal(t, "secret", body["passwordColumnValue"])
}

func TestRowsAuthenticate_MissingFlags(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{}.Handler())
	setupHomeWithBoard(t, srv.URL)

	res := testutil.RunCommand(t, []string{"rows", "authenticate"}, "")
	assert.Equal(t, 2, res.ExitCode)
}

// ── rows update-description ─────────────────────────────────────────────────

func TestRowsUpdateDescription_ReadsStdin(t *testing.T) {
	var capturedBody map[string]string
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"POST /board/board1/table/table1/row/r1/md": func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&capturedBody)
			w.WriteHeader(http.StatusAccepted)
		},
	}.Handler())
	setupHomeWithBoard(t, srv.URL)

	res := testutil.RunCommand(t, []string{"rows", "update-description", "r1"}, "# Updated description")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Equal(t, "replace", capturedBody["operation"])
	assert.Equal(t, "# Updated description", capturedBody["content"])
}

func TestRowsUpdateDescription_AppendOperation(t *testing.T) {
	var capturedBody map[string]string
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"POST /board/board1/table/table1/row/r1/md": func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&capturedBody)
			w.WriteHeader(http.StatusAccepted)
		},
	}.Handler())
	setupHomeWithBoard(t, srv.URL)

	res := testutil.RunCommand(t, []string{"rows", "update-description", "r1", "--operation", "append", "--content", "new section"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Equal(t, "append", capturedBody["operation"])
	assert.Equal(t, "new section", capturedBody["content"])
}
