package commands_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/copera/copera-cli/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── workspace info ──────────────────────────────────────────────────────────

func TestWorkspaceInfo_JSON(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /workspace/info": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"_id": "ws001", "name": "Acme Inc", "slug": "acme-inc",
				"description": "Test workspace", "seats": 42,
				"createdAt": "2024-01-15T10:00:00Z", "updatedAt": "2025-01-20T14:00:00Z",
			})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"workspace", "info", "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)

	var info map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &info))
	assert.Equal(t, "Acme Inc", info["name"])
	assert.Equal(t, "acme-inc", info["slug"])
	assert.Equal(t, float64(42), info["seats"])
}

func TestWorkspaceInfo_Human(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /workspace/info": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"_id": "ws001", "name": "Acme Inc", "slug": "acme-inc",
				"description": "Test workspace", "seats": 42,
				"createdAt": "2024-01-15T10:00:00Z", "updatedAt": "2025-01-20T14:00:00Z",
			})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"workspace", "info", "--output", "table"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Contains(t, res.Stdout, "Name:")
	assert.Contains(t, res.Stdout, "Acme Inc")
	assert.Contains(t, res.Stdout, "Slug:")
	assert.Contains(t, res.Stdout, "acme-inc")
	assert.Contains(t, res.Stdout, "Seats:")
	assert.Contains(t, res.Stdout, "42")
}

// ── workspace members ───────────────────────────────────────────────────────

func TestWorkspaceMembers_JSON(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /workspace/members": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"members": []map[string]any{
					{
						"_id": "u001", "name": "Mike Johnson", "email": "mike@acme.com",
						"title": "Engineer", "type": "user", "status": "online",
						"active": true, "createdAt": "2024-03-10T08:00:00Z",
					},
					{
						"_id": "u002", "name": "Jane Smith", "email": "jane@acme.com",
						"title": "Designer", "type": "user", "status": "offline",
						"active": true, "createdAt": "2024-04-01T09:00:00Z",
					},
				},
				"total": 2, "limit": 25, "offset": 0,
			})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"workspace", "members", "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)

	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &out))
	members := out["members"].([]any)
	assert.Len(t, members, 2)
}

func TestWorkspaceMembers_Human(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /workspace/members": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"members": []map[string]any{
					{
						"_id": "u001", "name": "Mike Johnson", "email": "mike@acme.com",
						"title": "Engineer", "type": "user", "status": "online",
						"active": true, "createdAt": "2024-03-10T08:00:00Z",
					},
					{
						"_id": "u002", "name": "Jane Smith", "email": "jane@acme.com",
						"title": "Designer", "type": "user", "status": "offline",
						"active": true, "createdAt": "2024-04-01T09:00:00Z",
					},
				},
				"total": 2, "limit": 25, "offset": 0,
			})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"workspace", "members", "--output", "table"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Contains(t, res.Stdout, "Name:")
	assert.Contains(t, res.Stdout, "Mike Johnson")
	assert.Contains(t, res.Stdout, "Email:")
	assert.Contains(t, res.Stdout, "mike@acme.com")
	assert.Contains(t, res.Stdout, "Type:")
	assert.Contains(t, res.Stdout, "Jane Smith")
	assert.Contains(t, res.Stdout, "jane@acme.com")
}

func TestWorkspaceMembers_QueryParam(t *testing.T) {
	var capturedQuery, capturedLimit, capturedOffset string
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /workspace/members": func(w http.ResponseWriter, r *http.Request) {
			capturedQuery = r.URL.Query().Get("q")
			capturedLimit = r.URL.Query().Get("limit")
			capturedOffset = r.URL.Query().Get("offset")
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"members": []map[string]any{
					{
						"_id": "u001", "name": "Mike Johnson", "email": "mike@acme.com",
						"title": "Engineer", "type": "user", "status": "online",
						"active": true, "createdAt": "2024-03-10T08:00:00Z",
					},
				},
				"total": 1, "limit": 10, "offset": 5,
			})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"workspace", "members", "--query", "mike", "--limit", "10", "--offset", "5", "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Equal(t, "mike", capturedQuery)
	assert.Equal(t, "10", capturedLimit)
	assert.Equal(t, "5", capturedOffset)
}

func TestWorkspaceMembers_Empty(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /workspace/members": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"members": []map[string]any{},
				"total":   0, "limit": 25, "offset": 0,
			})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"workspace", "members", "--output", "table"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Contains(t, res.Stderr, "No members found")
}

// ── workspace teams ─────────────────────────────────────────────────────────

func TestWorkspaceTeams_JSON(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /workspace/teams": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"teams": []map[string]any{
					{
						"_id": "t001", "name": "Engineering", "main": false,
						"participants": []string{"u001", "u002", "u003"},
						"createdBy":    "u001",
						"createdAt":    "2024-01-01T00:00:00Z", "updatedAt": "2025-01-01T00:00:00Z",
					},
					{
						"_id": "t002", "name": "Design", "main": true,
						"participants": []string{"u004", "u005"},
						"createdBy":    "u004",
						"createdAt":    "2024-02-01T00:00:00Z", "updatedAt": "2025-02-01T00:00:00Z",
					},
				},
				"total": 2, "limit": 25, "offset": 0,
			})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"workspace", "teams", "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)

	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &out))
	teams := out["teams"].([]any)
	assert.Len(t, teams, 2)
}

func TestWorkspaceTeams_Human(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /workspace/teams": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"teams": []map[string]any{
					{
						"_id": "t001", "name": "Engineering", "main": false,
						"participants": []string{"u001", "u002", "u003"},
						"createdBy":    "u001",
						"createdAt":    "2024-01-01T00:00:00Z", "updatedAt": "2025-01-01T00:00:00Z",
					},
					{
						"_id": "t002", "name": "Design", "main": true,
						"participants": []string{"u004", "u005"},
						"createdBy":    "u004",
						"createdAt":    "2024-02-01T00:00:00Z", "updatedAt": "2025-02-01T00:00:00Z",
					},
				},
				"total": 2, "limit": 25, "offset": 0,
			})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"workspace", "teams", "--output", "table"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Contains(t, res.Stdout, "Name:")
	assert.Contains(t, res.Stdout, "Engineering")
	assert.Contains(t, res.Stdout, "Members:")
	assert.Contains(t, res.Stdout, "Design")
}
