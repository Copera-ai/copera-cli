package commands_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/copera/copera-cli/internal/api"
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

func TestTablesList_Query(t *testing.T) {
	var capturedQuery url.Values
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /board/board1/tables": func(w http.ResponseWriter, r *http.Request) {
			capturedQuery = r.URL.Query()
			testutil.RespondJSON(w, http.StatusOK, []map[string]any{})
		},
	}.Handler())
	setupHomeWithBoard(t, srv.URL)

	res := testutil.RunCommand(t, []string{"tables", "list", "--query", "tasks", "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Equal(t, "tasks", capturedQuery.Get("q"))
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

func TestRowsList_QueryFilterAndSort(t *testing.T) {
	var capturedQuery string
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /board/board1/table/table1/rows": func(w http.ResponseWriter, r *http.Request) {
			capturedQuery = r.URL.RawQuery
			testutil.RespondJSON(w, http.StatusOK, []map[string]any{})
		},
	}.Handler())
	setupHomeWithBoard(t, srv.URL)

	filter := `{"match":"and","conditions":[{"column_id":"c1","operator":"contains","value":"foo"}]}`
	res := testutil.RunCommand(t, []string{
		"rows", "list", "--json",
		"--query", "oauth",
		"--filter", filter,
		"--sort", "c1:asc,c2:desc",
	}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)

	values, err := url.ParseQuery(capturedQuery)
	require.NoError(t, err)
	assert.Equal(t, "oauth", values.Get("q"))
	assert.Equal(t, filter, values.Get("filter"))
	assert.Equal(t, "c1:asc,c2:desc", values.Get("sort"))
}

func TestRowsList_InvalidFilterJSON(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{}.Handler())
	setupHomeWithBoard(t, srv.URL)

	res := testutil.RunCommand(t, []string{
		"rows", "list", "--filter", "{not json",
	}, "")
	assert.NotEqual(t, 0, res.ExitCode)
	assert.Contains(t, res.Stderr, "valid JSON")
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
				"description": "Legacy row body",
				"createdAt":   "2025-01-01T00:00:00Z", "updatedAt": "2025-06-01T00:00:00Z",
				"columns": []map[string]any{
					{"columnId": "c1", "value": "opt_inprog"},
					{"columnId": "c2", "value": "Fix the bug"},
					{"columnId": "c3",
						"value":     []any{"row1", "row2"},
						"linkValue": []any{"Task Alpha", "Task Beta"},
					},
					{"columnId": "c4", "value": "Modern description preview"},
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
					{"columnId": "c4", "label": "Description", "type": "DESCRIPTION", "order": 3},
				},
			})
		},
	}.Handler())
	setupHomeWithBoard(t, srv.URL)

	res := testutil.RunCommand(t, []string{"rows", "get", "r1", "--output", "table"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Contains(t, res.Stdout, "ID:      r1")
	assert.Contains(t, res.Stdout, "Row#:    42")
	assert.Contains(t, res.Stdout, "Description (legacy): Legacy row body")
	assert.Contains(t, res.Stdout, "Status: In Progress")
	assert.Contains(t, res.Stdout, "Title: Fix the bug")
	assert.Contains(t, res.Stdout, "Related Tasks: Task Alpha, Task Beta")
	assert.Contains(t, res.Stdout, "Description (column: c4, type: DESCRIPTION): Modern description preview")
	assert.Contains(t, res.Stdout, "DESCRIPTION/RICH TEXT columns use rows column-content/update-column-content with --column")
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

// ── rows description ────────────────────────────────────────────────────────

func TestRowsDescription_HumanOutput(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /board/board1/table/table1/row/r1/md": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"content": "# Heading\n\nSome markdown body.",
			})
		},
	}.Handler())
	setupHomeWithBoard(t, srv.URL)

	res := testutil.RunCommand(t, []string{"rows", "description", "r1", "--output", "table"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Contains(t, res.Stdout, "# Heading")
	assert.Contains(t, res.Stdout, "Some markdown body.")
}

func TestRowsDescription_JSON(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /board/board1/table/table1/row/r1/md": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"content": "raw markdown",
			})
		},
	}.Handler())
	setupHomeWithBoard(t, srv.URL)

	res := testutil.RunCommand(t, []string{"rows", "description", "r1", "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	var out map[string]string
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &out))
	assert.Equal(t, "raw markdown", out["content"])
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

func TestRowsDescriptionHelpClarifiesLegacyVsColumnContent(t *testing.T) {
	res := testutil.RunCommand(t, []string{"rows", "update-description", "--help"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Contains(t, res.Stdout, "legacy row-level markdown description")
	assert.Contains(t, res.Stdout, "does not update RICH TEXT / Description columns")
	assert.Contains(t, res.Stdout, "rows update-column-content")
}

// ── rows column-content ─────────────────────────────────────────────────────

func TestRowsColumnContent_Human(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /board/board1/table/table1/row/r1/column/col1/md": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"content": "# Cell heading\n\nRich text body.",
			})
		},
	}.Handler())
	setupHomeWithBoard(t, srv.URL)

	res := testutil.RunCommand(t, []string{"rows", "column-content", "r1", "--column", "col1", "--output", "table"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Contains(t, res.Stdout, "# Cell heading")
	assert.Contains(t, res.Stdout, "Rich text body.")
}

func TestRowsColumnContent_JSON(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /board/board1/table/table1/row/r1/column/col1/md": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"content": "raw cell markdown",
			})
		},
	}.Handler())
	setupHomeWithBoard(t, srv.URL)

	res := testutil.RunCommand(t, []string{"rows", "column-content", "r1", "--column", "col1", "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	var out map[string]string
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &out))
	assert.Equal(t, "raw cell markdown", out["content"])
}

func TestRowsColumnContent_MissingColumn(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{}.Handler())
	setupHomeWithBoard(t, srv.URL)

	res := testutil.RunCommand(t, []string{"rows", "column-content", "r1"}, "")
	assert.NotEqual(t, 0, res.ExitCode)
	assert.Contains(t, res.Stderr, "column")
}

// ── rows update-column-content ──────────────────────────────────────────────

func TestRowsUpdateColumnContent_ReadsStdin(t *testing.T) {
	var capturedBody map[string]string
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"POST /board/board1/table/table1/row/r1/column/col1/md": func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&capturedBody)
			w.WriteHeader(http.StatusAccepted)
		},
	}.Handler())
	setupHomeWithBoard(t, srv.URL)

	res := testutil.RunCommand(t, []string{"rows", "update-column-content", "r1", "--column", "col1"}, "# Cell body")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Equal(t, "replace", capturedBody["operation"])
	assert.Equal(t, "# Cell body", capturedBody["content"])
}

func TestRowsUpdateColumnContent_AppendOperation(t *testing.T) {
	var capturedBody map[string]string
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"POST /board/board1/table/table1/row/r1/column/col1/md": func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&capturedBody)
			w.WriteHeader(http.StatusAccepted)
		},
	}.Handler())
	setupHomeWithBoard(t, srv.URL)

	res := testutil.RunCommand(t, []string{"rows", "update-column-content", "r1", "--column", "col1", "--operation", "append", "--content", "appended chunk"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Equal(t, "append", capturedBody["operation"])
	assert.Equal(t, "appended chunk", capturedBody["content"])
}

func TestRowsUpdateColumnContent_MissingColumn(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{}.Handler())
	setupHomeWithBoard(t, srv.URL)

	res := testutil.RunCommand(t, []string{"rows", "update-column-content", "r1", "--content", "x"}, "")
	assert.NotEqual(t, 0, res.ExitCode)
	assert.Contains(t, res.Stderr, "column")
}

func TestRowsColumnContentHelpClarifiesModernDescriptionColumns(t *testing.T) {
	res := testutil.RunCommand(t, []string{"rows", "update-column-content", "--help"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Contains(t, res.Stdout, "RICH TEXT / Description column cell")
	assert.Contains(t, res.Stdout, "different from the fixed legacy row description")
	assert.Contains(t, res.Stdout, "rows update-description")
}

// ── rows attachments download ───────────────────────────────────────────────

func TestRowsAttachmentsDownload_WritesBinaryFile(t *testing.T) {
	var authHeader string
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /board/board1/table/table1/row/r1/column/file_col/file/file1/download": func(w http.ResponseWriter, r *http.Request) {
			authHeader = r.Header.Get("Authorization")
			w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''contract%20final.txt")
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("contract bytes"))
		},
	}.Handler())
	setupHomeWithBoard(t, srv.URL)

	dest := filepath.Join(t.TempDir(), "downloaded.txt")
	res := testutil.RunCommand(t, []string{
		"rows", "attachments", "download", "r1",
		"--column", "file_col",
		"--file", "file1",
		"-o", dest,
		"--json",
	}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Equal(t, "Bearer tok_test_fake", authHeader)

	body, err := os.ReadFile(dest)
	require.NoError(t, err)
	assert.Equal(t, "contract bytes", string(body))

	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &out))
	assert.Equal(t, "contract final.txt", out["file"])
	assert.Equal(t, float64(len("contract bytes")), out["size"])
	assert.Equal(t, dest, out["path"])
}

func TestRowsAttachmentsDownload_MissingColumnOrFile(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{}.Handler())
	setupHomeWithBoard(t, srv.URL)

	missingColumn := testutil.RunCommand(t, []string{
		"rows", "attachments", "download", "r1",
		"--file", "file1",
	}, "")
	assert.Equal(t, 2, missingColumn.ExitCode)
	assert.Contains(t, missingColumn.Stderr, "column")

	missingFile := testutil.RunCommand(t, []string{
		"rows", "attachments", "download", "r1",
		"--column", "file_col",
	}, "")
	assert.Equal(t, 2, missingFile.ExitCode)
	assert.Contains(t, missingFile.Stderr, "file")
}

func TestRowsAttachmentsDownload_APIError(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /board/board1/table/table1/row/r1/column/file_col/file/file1/download": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusForbidden, map[string]any{
				"code":    "FORBIDDEN",
				"message": "No file access",
			})
		},
	}.Handler())
	setupHomeWithBoard(t, srv.URL)

	res := testutil.RunCommand(t, []string{
		"rows", "attachments", "download", "r1",
		"--column", "file_col",
		"--file", "file1",
		"--json",
	}, "")
	assert.Equal(t, 4, res.ExitCode)
	assert.Contains(t, res.Stderr, "FORBIDDEN: No file access")
}

// ── rows comment ────────────────────────────────────────────────────────────

func sampleCommentResponse(id string) map[string]any {
	return map[string]any{
		"_id":         id,
		"content":     "hello world",
		"contentType": "text/html",
		"visibility":  "internal",
		"author": map[string]any{
			"_id": "u1", "name": "Alice", "picture": "", "email": "alice@example.com",
		},
		"createdAt": "2025-06-01T00:00:00Z",
		"updatedAt": "2025-06-01T00:00:00Z",
	}
}

func sampleCommentResponseWithAttachment(id string) map[string]any {
	cmt := sampleCommentResponse(id)
	cmt["attachment"] = map[string]any{
		"fileId":   "file1",
		"name":     "contract.pdf",
		"mimeType": "application/pdf",
		"size":     1234,
	}
	return cmt
}

func TestRowsComment_JSON(t *testing.T) {
	var capturedBody map[string]string
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"POST /board/board1/table/table1/row/r1/comment": func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&capturedBody)
			testutil.RespondJSON(w, http.StatusOK, sampleCommentResponse("c1"))
		},
	}.Handler())
	setupHomeWithBoard(t, srv.URL)

	res := testutil.RunCommand(t, []string{
		"rows", "comment", "r1", "--json",
		"--content", "hello world",
		"--visibility", "external",
	}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)

	assert.Equal(t, "hello world", capturedBody["content"])
	assert.Equal(t, "external", capturedBody["visibility"])

	var cmt map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &cmt))
	assert.Equal(t, "c1", cmt["_id"])
	assert.Equal(t, "hello world", cmt["content"])
}

func TestRowsComment_Stdin(t *testing.T) {
	var capturedBody map[string]string
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"POST /board/board1/table/table1/row/r1/comment": func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&capturedBody)
			testutil.RespondJSON(w, http.StatusOK, sampleCommentResponse("c2"))
		},
	}.Handler())
	setupHomeWithBoard(t, srv.URL)

	res := testutil.RunCommand(t, []string{"rows", "comment", "r1", "--json"}, "from stdin")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Equal(t, "from stdin", capturedBody["content"])
	assert.Equal(t, "internal", capturedBody["visibility"])
}

func TestRowsComment_MissingContent(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{}.Handler())
	setupHomeWithBoard(t, srv.URL)

	res := testutil.RunCommand(t, []string{"rows", "comment", "r1"}, "")
	assert.Equal(t, 2, res.ExitCode)
}

func TestRowsComment_InvalidVisibility(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{}.Handler())
	setupHomeWithBoard(t, srv.URL)

	res := testutil.RunCommand(t, []string{
		"rows", "comment", "r1", "--content", "hi", "--visibility", "secret",
	}, "")
	assert.Equal(t, 2, res.ExitCode)
}

func TestRowsComment_HumanOutput(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"POST /board/board1/table/table1/row/r1/comment": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, sampleCommentResponse("c3"))
		},
	}.Handler())
	setupHomeWithBoard(t, srv.URL)

	res := testutil.RunCommand(t, []string{
		"rows", "comment", "r1", "--content", "hi", "--output", "table",
	}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Contains(t, res.Stdout, "ID:         c3")
	assert.Contains(t, res.Stdout, "Alice <alice@example.com>")
	assert.Contains(t, res.Stdout, "Visibility: internal")
	assert.Contains(t, res.Stdout, "hello world")
}

// ── rows comments ───────────────────────────────────────────────────────────

func TestRowsComments_JSON(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /board/board1/table/table1/row/r1/comments": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"items": []map[string]any{
					sampleCommentResponseWithAttachment("c1"),
					sampleCommentResponse("c2"),
				},
				"pageInfo": map[string]any{
					"endCursor":       nil,
					"startCursor":     nil,
					"hasNextPage":     false,
					"hasPreviousPage": false,
				},
			})
		},
	}.Handler())
	setupHomeWithBoard(t, srv.URL)

	res := testutil.RunCommand(t, []string{"rows", "comments", "r1", "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)

	var page api.CommentsPage
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &page))
	require.Len(t, page.Items, 2)
	require.NotNil(t, page.Items[0].Attachment)
	assert.Equal(t, "file1", page.Items[0].Attachment.FileID)
	assert.Equal(t, "contract.pdf", page.Items[0].Attachment.Name)
	assert.Equal(t, "application/pdf", page.Items[0].Attachment.MimeType)
	assert.Equal(t, int64(1234), page.Items[0].Attachment.Size)
	assert.Nil(t, page.Items[1].Attachment)
}

func TestRowsComments_QueryParams(t *testing.T) {
	var capturedQuery string
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /board/board1/table/table1/row/r1/comments": func(w http.ResponseWriter, r *http.Request) {
			capturedQuery = r.URL.RawQuery
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"items": []map[string]any{},
				"pageInfo": map[string]any{
					"endCursor": nil, "startCursor": nil,
					"hasNextPage": false, "hasPreviousPage": false,
				},
			})
		},
	}.Handler())
	setupHomeWithBoard(t, srv.URL)

	res := testutil.RunCommand(t, []string{
		"rows", "comments", "r1", "--json",
		"--after", "cur=X+y",
		"--visibility", "external",
	}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)

	values, err := url.ParseQuery(capturedQuery)
	require.NoError(t, err)
	assert.Equal(t, "cur=X+y", values.Get("after"))
	assert.Equal(t, "external", values.Get("visibility"))
	assert.Equal(t, "", values.Get("before"))
}

func TestRowsComments_Empty(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /board/board1/table/table1/row/r1/comments": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"items": []map[string]any{},
				"pageInfo": map[string]any{
					"endCursor": nil, "startCursor": nil,
					"hasNextPage": false, "hasPreviousPage": false,
				},
			})
		},
	}.Handler())
	setupHomeWithBoard(t, srv.URL)

	res := testutil.RunCommand(t, []string{"rows", "comments", "r1", "--output", "table"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Contains(t, res.Stderr, "No comments found.")
}

func TestRowsComments_HumanOutput(t *testing.T) {
	endCursor := "next-page-cursor"
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /board/board1/table/table1/row/r1/comments": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"items": []map[string]any{sampleCommentResponse("c1")},
				"pageInfo": map[string]any{
					"endCursor": endCursor, "startCursor": nil,
					"hasNextPage": true, "hasPreviousPage": false,
				},
			})
		},
	}.Handler())
	setupHomeWithBoard(t, srv.URL)

	res := testutil.RunCommand(t, []string{"rows", "comments", "r1", "--output", "table"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Contains(t, res.Stdout, "ID:         c1")
	assert.Contains(t, res.Stdout, "Alice <alice@example.com>")
	assert.Contains(t, res.Stdout, "hello world")
	assert.Contains(t, res.Stderr, "--after "+endCursor)
}

// ── rows comments attachments download ─────────────────────────────────────

func TestRowsCommentAttachmentsDownload_WritesBinaryFile(t *testing.T) {
	var authHeader string
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /board/board1/table/table1/row/r1/comment/c1/file/file1/download": func(w http.ResponseWriter, r *http.Request) {
			authHeader = r.Header.Get("Authorization")
			w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''comment%20file.txt")
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("comment attachment bytes"))
		},
	}.Handler())
	setupHomeWithBoard(t, srv.URL)

	dest := filepath.Join(t.TempDir(), "comment-file.txt")
	res := testutil.RunCommand(t, []string{
		"rows", "comments", "attachments", "download", "r1",
		"--comment", "c1",
		"--file", "file1",
		"-o", dest,
		"--json",
	}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Equal(t, "Bearer tok_test_fake", authHeader)

	body, err := os.ReadFile(dest)
	require.NoError(t, err)
	assert.Equal(t, "comment attachment bytes", string(body))

	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &out))
	assert.Equal(t, "comment file.txt", out["file"])
	assert.Equal(t, float64(len("comment attachment bytes")), out["size"])
	assert.Equal(t, dest, out["path"])
}

func TestRowsCommentAttachmentsDownload_MissingCommentOrFile(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{}.Handler())
	setupHomeWithBoard(t, srv.URL)

	missingComment := testutil.RunCommand(t, []string{
		"rows", "comments", "attachments", "download", "r1",
		"--file", "file1",
	}, "")
	assert.Equal(t, 2, missingComment.ExitCode)
	assert.Contains(t, missingComment.Stderr, "comment")

	missingFile := testutil.RunCommand(t, []string{
		"rows", "comments", "attachments", "download", "r1",
		"--comment", "c1",
	}, "")
	assert.Equal(t, 2, missingFile.ExitCode)
	assert.Contains(t, missingFile.Stderr, "file")
}

func TestRowsCommentAttachmentsDownload_APIError(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /board/board1/table/table1/row/r1/comment/c1/file/file1/download": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusForbidden, map[string]any{
				"code":    "FORBIDDEN",
				"message": "No comment attachment access",
			})
		},
	}.Handler())
	setupHomeWithBoard(t, srv.URL)

	res := testutil.RunCommand(t, []string{
		"rows", "comments", "attachments", "download", "r1",
		"--comment", "c1",
		"--file", "file1",
		"--json",
	}, "")
	assert.Equal(t, 4, res.ExitCode)
	assert.Contains(t, res.Stderr, "FORBIDDEN: No comment attachment access")
}
