package commands_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"testing"

	"github.com/copera/copera-cli/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupHomeForNotifications(t *testing.T, apiURL string) {
	t.Helper()
	home := t.TempDir()
	testutil.WriteTempConfigAt(t, filepath.Join(home, ".copera.toml"), `
[profiles.default]
token = "tok_test_fake"

[api]
base_url = "`+apiURL+`"
`)
	testutil.SetEnv(t, "HOME", home)
}

func sampleNotification(id, status string) map[string]any {
	return map[string]any{
		"_id":       id,
		"type":      "row_comment",
		"owner":     "u1",
		"workspace": "w1",
		"data": map[string]any{
			"rowId":   "r1",
			"preview": "Looks great!",
		},
		"status":    status,
		"sender":    "u2",
		"createdAt": "2025-06-01T12:00:00Z",
		"updatedAt": "2025-06-01T12:00:00Z",
	}
}

func TestNotificationsList_JSON(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /notifications/": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"notifications": []map[string]any{
					sampleNotification("n1", "unread"),
					sampleNotification("n2", "read"),
				},
				"unreadCount": 1,
				"count":       2,
			})
		},
	}.Handler())
	setupHomeForNotifications(t, srv.URL)

	res := testutil.RunCommand(t, []string{"notifications", "list", "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)

	var page map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &page))
	items := page["notifications"].([]any)
	assert.Len(t, items, 2)
	assert.Equal(t, float64(1), page["unreadCount"])
	assert.Equal(t, float64(2), page["count"])
}

func TestNotificationsList_QueryParams(t *testing.T) {
	var capturedQuery string
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /notifications/": func(w http.ResponseWriter, r *http.Request) {
			capturedQuery = r.URL.RawQuery
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"notifications": []map[string]any{},
				"unreadCount":   0,
				"count":         0,
			})
		},
	}.Handler())
	setupHomeForNotifications(t, srv.URL)

	res := testutil.RunCommand(t, []string{
		"notifications", "list", "--json",
		"--after", "507f1f77bcf86cd799439011",
		"--before", "507f1f77bcf86cd799439012",
	}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)

	values, err := url.ParseQuery(capturedQuery)
	require.NoError(t, err)
	assert.Equal(t, "507f1f77bcf86cd799439011", values.Get("after"))
	assert.Equal(t, "507f1f77bcf86cd799439012", values.Get("before"))
}

func TestNotificationsList_Empty(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /notifications/": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"notifications": []map[string]any{},
				"unreadCount":   0,
				"count":         0,
			})
		},
	}.Handler())
	setupHomeForNotifications(t, srv.URL)

	res := testutil.RunCommand(t, []string{"notifications", "list", "--output", "table"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Contains(t, res.Stderr, "No notifications.")
}

func TestNotificationsList_HumanOutput(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /notifications/": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"notifications": []map[string]any{
					sampleNotification("n1", "unread"),
				},
				"unreadCount": 1,
				"count":       1,
			})
		},
	}.Handler())
	setupHomeForNotifications(t, srv.URL)

	res := testutil.RunCommand(t, []string{"notifications", "list", "--output", "table"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Contains(t, res.Stdout, "ID:      n1")
	assert.Contains(t, res.Stdout, "Type:    row_comment")
	assert.Contains(t, res.Stdout, "Status:  unread")
	assert.Contains(t, res.Stdout, "preview=Looks great!")
	assert.Contains(t, res.Stderr, "1 unread / 1 total.")
}

func TestNotificationsRead(t *testing.T) {
	var capturedMethod string
	var capturedBody map[string]string
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"PATCH /notifications/n1": func(w http.ResponseWriter, r *http.Request) {
			capturedMethod = r.Method
			_ = json.NewDecoder(r.Body).Decode(&capturedBody)
			testutil.RespondJSON(w, http.StatusOK, sampleNotification("n1", "read"))
		},
	}.Handler())
	setupHomeForNotifications(t, srv.URL)

	res := testutil.RunCommand(t, []string{"notifications", "read", "n1", "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Equal(t, "PATCH", capturedMethod)
	assert.Equal(t, "read", capturedBody["status"])

	var n map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &n))
	assert.Equal(t, "n1", n["_id"])
	assert.Equal(t, "read", n["status"])
}

func TestNotificationsUnread(t *testing.T) {
	var capturedBody map[string]string
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"PATCH /notifications/n2": func(w http.ResponseWriter, r *http.Request) {
			capturedBody, _ = decodeJSONBody(r.Body)
			testutil.RespondJSON(w, http.StatusOK, sampleNotification("n2", "unread"))
		},
	}.Handler())
	setupHomeForNotifications(t, srv.URL)

	res := testutil.RunCommand(t, []string{"notifications", "unread", "n2", "--output", "table"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Equal(t, "unread", capturedBody["status"])
	assert.Contains(t, res.Stderr, "marked as unread")
}

func TestNotificationsDelete_Force(t *testing.T) {
	var deleteCalled bool
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"DELETE /notifications/n1": func(w http.ResponseWriter, r *http.Request) {
			deleteCalled = true
			testutil.RespondJSON(w, http.StatusOK, map[string]any{"_id": "n1"})
		},
	}.Handler())
	setupHomeForNotifications(t, srv.URL)

	res := testutil.RunCommand(t, []string{"notifications", "delete", "n1", "--force", "--output", "table"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.True(t, deleteCalled)
	assert.Contains(t, res.Stderr, "Notification deleted.")
}

func TestNotificationsDelete_JSON(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"DELETE /notifications/n1": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{"_id": "n1"})
		},
	}.Handler())
	setupHomeForNotifications(t, srv.URL)

	res := testutil.RunCommand(t, []string{"notifications", "delete", "n1", "--force", "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)

	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &out))
	assert.Equal(t, true, out["deleted"])
	assert.Equal(t, "n1", out["notificationId"])
}

func decodeJSONBody(body io.Reader) (map[string]string, error) {
	var m map[string]string
	if err := json.NewDecoder(body).Decode(&m); err != nil {
		return nil, err
	}
	return m, nil
}
