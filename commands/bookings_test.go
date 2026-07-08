package commands_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/copera/copera-cli/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func bookingObj() map[string]any {
	return map[string]any{
		"id":                "b0000000000000000000001",
		"status":            "CONFIRMED",
		"bookingTypeId":     "t0000000000000000000001",
		"bookingTypeTitle":  "Intro Call",
		"start":             "2025-06-01T15:00:00Z",
		"end":               "2025-06-01T15:30:00Z",
		"durationMinutes":   30,
		"timezoneAtBooking": "America/New_York",
		"hosts":             []map[string]any{{"userId": "u0000000000000000000001", "role": "host"}},
		"booker":            map[string]any{"name": "Jane Doe", "email": "jane@example.com", "timezone": "UTC", "locale": "en"},
		"guests":            []string{},
		"answers":           []map[string]any{},
		"location":          map[string]any{"type": "google_meet", "label": "Google Meet"},
	}
}

// ── bookings list ─────────────────────────────────────────────────────────────

func TestBookingsList_JSON(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /bookings": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"bookings":   []map[string]any{bookingObj()},
				"nextCursor": nil,
				"hasMore":    false,
			})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"bookings", "list", "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)

	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &out))
	bookings := out["bookings"].([]any)
	assert.Len(t, bookings, 1)
	assert.Equal(t, false, out["hasMore"])
}

func TestBookingsList_Human(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /bookings": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"bookings":   []map[string]any{bookingObj()},
				"nextCursor": nil,
				"hasMore":    false,
			})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"bookings", "list", "--output", "table"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Contains(t, res.Stdout, "Status:")
	assert.Contains(t, res.Stdout, "CONFIRMED")
	assert.Contains(t, res.Stdout, "Intro Call")
	assert.Contains(t, res.Stdout, "Jane Doe")
	assert.Contains(t, res.Stdout, "jane@example.com")
}

func TestBookingsList_Filters(t *testing.T) {
	var status, bookingType, from, to, limit, cursor string
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /bookings": func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			status = q.Get("status")
			bookingType = q.Get("bookingTypeId")
			from = q.Get("from")
			to = q.Get("to")
			limit = q.Get("limit")
			cursor = q.Get("cursor")
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"bookings":   []map[string]any{},
				"nextCursor": nil,
				"hasMore":    false,
			})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{
		"bookings", "list",
		"--status", "CONFIRMED",
		"--type", "t0000000000000000000001",
		"--from", "2025-01-01T00:00:00Z",
		"--to", "2025-12-31T00:00:00Z",
		"--limit", "10",
		"--cursor", "abc",
		"--json",
	}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Equal(t, "CONFIRMED", status)
	assert.Equal(t, "t0000000000000000000001", bookingType)
	assert.Equal(t, "2025-01-01T00:00:00Z", from)
	assert.Equal(t, "2025-12-31T00:00:00Z", to)
	assert.Equal(t, "10", limit)
	assert.Equal(t, "abc", cursor)
}

func TestBookingsList_Empty(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /bookings": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"bookings":   []map[string]any{},
				"nextCursor": nil,
				"hasMore":    false,
			})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"bookings", "list", "--output", "table"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Contains(t, res.Stderr, "No bookings found")
}

// ── bookings get ──────────────────────────────────────────────────────────────

func TestBookingsGet_JSON(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /bookings/b0000000000000000000001": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, bookingObj())
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"bookings", "get", "b0000000000000000000001", "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)

	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &out))
	assert.Equal(t, "CONFIRMED", out["status"])
	assert.Equal(t, "b0000000000000000000001", out["id"])
}

func TestBookingsGet_Human(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /bookings/b0000000000000000000001": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, bookingObj())
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"bookings", "get", "b0000000000000000000001", "--output", "table"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Contains(t, res.Stdout, "ID:")
	assert.Contains(t, res.Stdout, "b0000000000000000000001")
	assert.Contains(t, res.Stdout, "Intro Call")
	assert.Contains(t, res.Stdout, "Jane Doe")
}

// ── booking-types list ────────────────────────────────────────────────────────

func TestBookingTypesList_JSON(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /booking-types": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"bookingTypes": []map[string]any{
					{
						"_id": "t0000000000000000000001", "title": "Intro Call", "slug": "intro",
						"kind": "SOLO", "durationMinutes": 30, "hidden": false, "active": true,
					},
				},
			})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"booking-types", "list", "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)

	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &out))
	types := out["bookingTypes"].([]any)
	assert.Len(t, types, 1)
}

func TestBookingTypesList_Human(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /booking-types": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"bookingTypes": []map[string]any{
					{
						"_id": "t0000000000000000000001", "title": "Intro Call", "slug": "intro",
						"kind": "SOLO", "durationMinutes": 30, "hidden": false, "active": true,
					},
				},
			})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"booking-types", "list", "--output", "table"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Contains(t, res.Stdout, "Title:")
	assert.Contains(t, res.Stdout, "Intro Call")
	assert.Contains(t, res.Stdout, "Kind:")
	assert.Contains(t, res.Stdout, "SOLO")
}
