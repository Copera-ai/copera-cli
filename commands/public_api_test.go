package commands_test

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/copera/copera-cli/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTempoCreateMergesDataAndFields(t *testing.T) {
	var body map[string]any
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"POST /time/calendars": func(w http.ResponseWriter, r *http.Request) {
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			testutil.RespondJSON(w, http.StatusOK, map[string]any{"id": "calendar1"})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{
		"time", "calendars", "create", "--data", `{"name":"Old","timezone":"UTC"}`,
		"--field", "name=Focus", "--field", "roles={\"owner\":true}", "--json",
	}, "")
	require.Equal(t, 0, res.ExitCode, res.Stderr)
	assert.Equal(t, "Focus", body["name"])
	assert.Equal(t, "UTC", body["timezone"])
	assert.Equal(t, map[string]any{"owner": true}, body["roles"])
}

func TestStructuredCommandReadsDataFromFile(t *testing.T) {
	var body map[string]any
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"POST /time/shelf-items": func(w http.ResponseWriter, r *http.Request) {
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			testutil.RespondJSON(w, http.StatusOK, map[string]any{"id": "item1"})
		},
	}.Handler())
	setupHome(t, srv.URL)
	path := filepath.Join(t.TempDir(), "item.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"title":"From file","color":"blue"}`), 0o600))

	res := testutil.RunCommand(t, []string{
		"tempo", "shelf", "create", "--data", "@" + path,
		"--field", "title=Overridden", "--json",
	}, "")
	require.Equal(t, 0, res.ExitCode, res.Stderr)
	assert.Equal(t, "Overridden", body["title"])
	assert.Equal(t, "blue", body["color"])
}

func TestAgendaCreateReadsBodyFromStdin(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"POST /agenda/events": func(w http.ResponseWriter, r *http.Request) {
			data, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			assert.JSONEq(t, `{"title":"Planning","startDate":"2026-09-01T10:00:00Z","participants":[]}`, string(data))
			testutil.RespondJSON(w, http.StatusCreated, map[string]any{"id": "event1"})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"agenda", "events", "create", "--data", "-", "--json"},
		`{"title":"Planning","startDate":"2026-09-01T10:00:00Z","participants":[]}`)
	require.Equal(t, 0, res.ExitCode, res.Stderr)
}

func TestDestructiveEndpointRequiresForceWhenNonInteractive(t *testing.T) {
	called := false
	srv := testutil.NewMockServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		testutil.RespondJSON(w, http.StatusOK, map[string]any{"success": true})
	}))
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"agenda", "events", "delete", "event1", "--no-input", "--json"}, "")
	assert.Equal(t, 2, res.ExitCode)
	assert.Contains(t, res.Stderr, "confirmation_required")
	assert.False(t, called)
}

func TestBookingCancelWithForceCallsLifecycleEndpoint(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"POST /bookings/booking1/cancel": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{"status": "CANCELLED"})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"bookings", "cancel", "booking1", "--force", "--json"}, "")
	require.Equal(t, 0, res.ExitCode, res.Stderr)
	assert.Contains(t, res.Stdout, "CANCELLED")
}

func TestInboxAttachmentRejectsFilesOver25MiB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.bin")
	require.NoError(t, os.WriteFile(path, nil, 0o600))
	require.NoError(t, os.Truncate(path, 25*1024*1024+1))
	setupHome(t, "http://127.0.0.1:1")

	res := testutil.RunCommand(t, []string{"inbox", "attachments", "upload", "inbox1", "--file", path, "--json"}, "")
	assert.Equal(t, 2, res.ExitCode)
	assert.Contains(t, res.Stderr, "exceeds 25 MiB")
}

func TestRowsListPaginationIsOptIn(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /board/b1/table/t1/rows": func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "5", r.URL.Query().Get("offset"))
			assert.Equal(t, "10", r.URL.Query().Get("limit"))
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"rows": []any{}, "total": 12, "offset": 5, "limit": 10, "hasMore": false,
			})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"rows", "list", "--board", "b1", "--table", "t1", "--offset", "5", "--limit", "10", "--json"}, "")
	require.Equal(t, 0, res.ExitCode, res.Stderr)
	assert.Contains(t, res.Stdout, `"total": 12`)
}
