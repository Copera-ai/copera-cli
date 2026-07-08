package commands_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/copera/copera-cli/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func authWhoamiRoutes() testutil.MockRoutes {
	return testutil.MockRoutes{
		"GET /workspace/info": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"_id": "ws001", "name": "Acme Inc", "slug": "acme-inc",
				"seats": 42, "createdAt": "2024-01-15T10:00:00Z", "updatedAt": "2025-01-20T14:00:00Z",
			})
		},
		"GET /workspace/me": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"_id": "u001", "name": "Mike Johnson", "email": "mike@acme.com",
				"title": "Engineer", "type": "user", "status": "online",
				"active": true, "createdAt": "2024-03-10T08:00:00Z",
			})
		},
	}
}

func TestAuthWhoami_Human(t *testing.T) {
	srv := testutil.NewMockServer(t, authWhoamiRoutes().Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"auth", "whoami", "--output", "table"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Contains(t, res.Stdout, "Mike Johnson")
	assert.Contains(t, res.Stdout, "mike@acme.com")
	assert.Contains(t, res.Stdout, "u001")
	assert.Contains(t, res.Stdout, "Acme Inc")
}

func TestAuthWhoami_JSON(t *testing.T) {
	srv := testutil.NewMockServer(t, authWhoamiRoutes().Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"auth", "whoami", "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)

	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &out))
	assert.Equal(t, "Mike Johnson", out["name"])
	assert.Equal(t, "mike@acme.com", out["email"])
	assert.Equal(t, "u001", out["user_id"])
	assert.Equal(t, "Acme Inc", out["workspace"])
	assert.Equal(t, "acme-inc", out["slug"])
	assert.Contains(t, out, "profile")
	assert.Contains(t, out, "token")
}
