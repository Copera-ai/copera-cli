package commands_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/copera/copera-cli/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── search JSON ─────────────────────────────────────────────────────────────

func TestSearch_JSON(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /search": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"query":            "mike",
				"totalHits":        2,
				"processingTimeMs": 15,
				"hits": []map[string]any{
					{
						"entityType": "document",
						"_id":        "doc001",
						"title":      "Q4 Planning",
						"workspace":  "ws1",
						"owner":      "u1",
						"createdAt":  1731234567890,
						"updatedAt":  1731345678901,
					},
					{
						"entityType": "channelMessage",
						"_id":        "msg001",
						"content":    "Hey mike, can you review?",
						"workspace":  "ws1",
						"author":     "u2",
						"channel":    "ch1",
						"createdAt":  1731234567890,
						"updatedAt":  1731234567890,
					},
				},
			})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"search", "mike", "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)

	var result map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &result))
	assert.Equal(t, "mike", result["query"])
	assert.Equal(t, float64(2), result["totalHits"])
	hits := result["hits"].([]any)
	assert.Len(t, hits, 2)
}

// ── search human ────────────────────────────────────────────────────────────

func TestSearch_Human(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /search": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"query":            "mike",
				"totalHits":        2,
				"processingTimeMs": 15,
				"hits": []map[string]any{
					{
						"entityType": "document",
						"_id":        "doc001",
						"title":      "Q4 Planning",
						"workspace":  "ws1",
						"owner":      "u1",
						"createdAt":  1731234567890,
						"updatedAt":  1731345678901,
					},
					{
						"entityType": "channelMessage",
						"_id":        "msg001",
						"content":    "Hey mike, can you review?",
						"workspace":  "ws1",
						"author":     "u2",
						"channel":    "ch1",
						"createdAt":  1731234567890,
						"updatedAt":  1731234567890,
					},
				},
			})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"search", "mike", "--output", "table"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Contains(t, res.Stdout, "Type:")
	assert.Contains(t, res.Stdout, "Title:")
	assert.Contains(t, res.Stdout, "Updated:")
	assert.Contains(t, res.Stdout, "document")
	assert.Contains(t, res.Stdout, "channelMessage")
}

// ── search type filter ──────────────────────────────────────────────────────

func TestSearch_TypeFilter(t *testing.T) {
	var capturedTypes string
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /search": func(w http.ResponseWriter, r *http.Request) {
			capturedTypes = r.URL.Query().Get("types")
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"query":            "mike",
				"totalHits":        0,
				"processingTimeMs": 5,
				"hits":             []map[string]any{},
			})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"search", "mike", "--type", "document", "--type", "channel", "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Equal(t, "document,channel", capturedTypes)
}

// ── search sort and limit ───────────────────────────────────────────────────

func TestSearch_SortAndLimit(t *testing.T) {
	var capturedSortBy, capturedSortOrder, capturedLimit string
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /search": func(w http.ResponseWriter, r *http.Request) {
			capturedSortBy = r.URL.Query().Get("sortBy")
			capturedSortOrder = r.URL.Query().Get("sortOrder")
			capturedLimit = r.URL.Query().Get("limit")
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"query":            "test",
				"totalHits":        0,
				"processingTimeMs": 3,
				"hits":             []map[string]any{},
			})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"search", "test", "--sort", "updatedAt", "--order", "desc", "--limit", "10", "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Equal(t, "updatedAt", capturedSortBy)
	assert.Equal(t, "desc", capturedSortOrder)
	assert.Equal(t, "10", capturedLimit)
}

// ── search empty ────────────────────────────────────────────────────────────

func TestSearch_Empty(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /search": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"query":            "nonexistent",
				"totalHits":        0,
				"processingTimeMs": 2,
				"hits":             []map[string]any{},
			})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"search", "nonexistent", "--output", "table"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Contains(t, res.Stderr, "No results for")
}

// ── search showing X of Y ───────────────────────────────────────────────────

func TestSearch_ShowingXofY(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /search": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"query":            "mike",
				"totalHits":        50,
				"processingTimeMs": 10,
				"hits": []map[string]any{
					{
						"entityType": "document",
						"_id":        "doc001",
						"title":      "Q4 Planning",
						"workspace":  "ws1",
						"owner":      "u1",
						"createdAt":  1731234567890,
						"updatedAt":  1731345678901,
					},
					{
						"entityType": "channelMessage",
						"_id":        "msg001",
						"content":    "Hey mike, can you review?",
						"workspace":  "ws1",
						"author":     "u2",
						"channel":    "ch1",
						"createdAt":  1731234567890,
						"updatedAt":  1731234567890,
					},
				},
			})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"search", "mike", "--output", "table"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Contains(t, res.Stderr, "Showing 2 of 50")
}
