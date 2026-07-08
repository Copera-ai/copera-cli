package commands_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/copera/copera-cli/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── meeting-notes list ────────────────────────────────────────────────────────

func TestMeetingNotesList_JSON(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /meeting-notes": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"notes": []map[string]any{
					{
						"_id": "n0000000000000000000001", "title": "Weekly Sync", "status": "COMPLETED",
						"startedAt": "2025-05-01T10:00:00Z", "endedAt": "2025-05-01T10:30:00Z", "hasSummary": true,
					},
				},
			})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"meeting-notes", "list", "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)

	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &out))
	notes := out["notes"].([]any)
	assert.Len(t, notes, 1)
}

func TestMeetingNotesList_Human(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /meeting-notes": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"notes": []map[string]any{
					{
						"_id": "n0000000000000000000001", "title": "Weekly Sync", "status": "COMPLETED",
						"startedAt": "2025-05-01T10:00:00Z", "endedAt": "2025-05-01T10:30:00Z", "hasSummary": true,
					},
				},
			})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"meeting-notes", "list", "--output", "table"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Contains(t, res.Stdout, "Title:")
	assert.Contains(t, res.Stdout, "Weekly Sync")
	assert.Contains(t, res.Stdout, "Status:")
	assert.Contains(t, res.Stdout, "COMPLETED")
}

func TestMeetingNotesList_QueryParams(t *testing.T) {
	var q, limit, before string
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /meeting-notes": func(w http.ResponseWriter, r *http.Request) {
			q = r.URL.Query().Get("q")
			limit = r.URL.Query().Get("limit")
			before = r.URL.Query().Get("before")
			testutil.RespondJSON(w, http.StatusOK, map[string]any{"notes": []map[string]any{}})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{
		"meeting-notes", "list",
		"--query", "sync", "--limit", "5", "--before", "2025-05-01T00:00:00Z", "--json",
	}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Equal(t, "sync", q)
	assert.Equal(t, "5", limit)
	assert.Equal(t, "2025-05-01T00:00:00Z", before)
}

func TestMeetingNotesList_Empty(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /meeting-notes": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{"notes": []map[string]any{}})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"meeting-notes", "list", "--output", "table"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Contains(t, res.Stderr, "No meeting notes found")
}

// ── meeting-notes get ─────────────────────────────────────────────────────────

func TestMeetingNotesGet_JSON(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /meeting-notes/n0000000000000000000001": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"_id": "n0000000000000000000001", "title": "Weekly Sync", "status": "COMPLETED",
				"startedAt": "2025-05-01T10:00:00Z", "hasSummary": true,
				"summary": "Discussed the roadmap.", "userNotes": "action items",
			})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"meeting-notes", "get", "n0000000000000000000001", "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)

	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &out))
	assert.Equal(t, "Weekly Sync", out["title"])
	assert.Equal(t, "Discussed the roadmap.", out["summary"])
}

func TestMeetingNotesGet_Human(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /meeting-notes/n0000000000000000000001": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"_id": "n0000000000000000000001", "title": "Weekly Sync", "status": "COMPLETED",
				"startedAt": "2025-05-01T10:00:00Z", "hasSummary": true,
				"summary": "Discussed the roadmap.",
			})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"meeting-notes", "get", "n0000000000000000000001", "--output", "table"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Contains(t, res.Stdout, "Weekly Sync")
	assert.Contains(t, res.Stdout, "Summary:")
	assert.Contains(t, res.Stdout, "Discussed the roadmap.")
}

func TestMeetingNotesGet_Transcript(t *testing.T) {
	var includeTranscript string
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /meeting-notes/n0000000000000000000001": func(w http.ResponseWriter, r *http.Request) {
			includeTranscript = r.URL.Query().Get("includeTranscript")
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"_id": "n0000000000000000000001", "title": "Weekly Sync", "status": "COMPLETED",
				"startedAt": "2025-05-01T10:00:00Z", "hasSummary": false,
				"transcript": []map[string]any{
					{"startMs": 0, "endMs": 1000, "text": "Hello team", "speaker": "Alice"},
					{"startMs": 1000, "endMs": 2000, "text": "Hi there"},
				},
			})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"meeting-notes", "get", "n0000000000000000000001", "--transcript", "--output", "table"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Equal(t, "true", includeTranscript)
	assert.Contains(t, res.Stdout, "Transcript")
	assert.Contains(t, res.Stdout, "[Alice] Hello team")
	assert.Contains(t, res.Stdout, "Hi there")
}
