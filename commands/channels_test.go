package commands_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"path/filepath"
	"testing"

	"github.com/copera/copera-cli/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupHomeWithChannel(t *testing.T, apiURL string) {
	t.Helper()
	home := t.TempDir()
	testutil.WriteTempConfigAt(t, filepath.Join(home, ".copera.toml"), `
[profiles.default]
token = "tok_test_fake"
channel_id = "chan1"

[api]
base_url = "`+apiURL+`"
`)
	testutil.SetEnv(t, "HOME", home)
}

// ── channels list ───────────────────────────────────────────────────────────

func TestChannelsList_JSON(t *testing.T) {
	var capturedQuery url.Values
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /chat/channels": func(w http.ResponseWriter, r *http.Request) {
			capturedQuery = r.URL.Query()
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"channels": []map[string]any{
					{
						"_id": "chan1", "name": "Deploy Alerts", "type": "text", "kind": "group",
						"participantUserIds": []string{"user1"}, "participantTeamIds": []string{},
						"createdBy": "user1", "createdAt": "2026-06-08T12:00:00Z", "updatedAt": "2026-06-08T12:10:00Z",
					},
				},
				"total": 1, "limit": 20, "offset": 5,
			})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{
		"channels", "list", "--json",
		"--query", "deploy",
		"--type", "text",
		"--kind", "group",
		"--participant", "user1",
		"--limit", "20",
		"--offset", "5",
	}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)

	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &out))
	assert.Equal(t, float64(1), out["total"])
	assert.Equal(t, "deploy", capturedQuery.Get("q"))
	assert.Equal(t, "text", capturedQuery.Get("type"))
	assert.Equal(t, "group", capturedQuery.Get("kind"))
	assert.Equal(t, "user1", capturedQuery.Get("participantId"))
	assert.Equal(t, "20", capturedQuery.Get("limit"))
	assert.Equal(t, "5", capturedQuery.Get("offset"))
}

func TestChannelsList_HumanOutput(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /chat/channels": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"channels": []map[string]any{
					{
						"_id": "chan1", "name": "Deploy Alerts", "type": "text", "kind": "group",
						"participantUserIds": []string{"user1"}, "participantTeamIds": []string{},
						"createdBy": "user1", "createdAt": "2026-06-08T12:00:00Z", "updatedAt": "2026-06-08T12:10:00Z",
					},
				},
				"total": 1, "limit": 100, "offset": 0,
			})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"channels", "list", "--output", "table"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Contains(t, res.Stdout, "ID:      chan1")
	assert.Contains(t, res.Stdout, "Name:    Deploy Alerts")
	assert.Contains(t, res.Stdout, "Type:    text")
	assert.Contains(t, res.Stdout, "Kind:    group")
}

// ── send message ─────────────────────────────────────────────────────────────

func TestSendMessage_JSON(t *testing.T) {
	var capturedBody map[string]any
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"POST /chat/channel/chan1/send-message": func(w http.ResponseWriter, r *http.Request) {
			json.NewDecoder(r.Body).Decode(&capturedBody)
			w.WriteHeader(http.StatusNoContent)
		},
	}.Handler())
	setupHomeWithChannel(t, srv.URL)

	res := testutil.RunCommand(t, []string{"channels", "message", "send", "Hello world", "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)

	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &out))
	assert.Equal(t, true, out["sent"])
	assert.Equal(t, "chan1", out["channel"])
	assert.Equal(t, "Hello world", capturedBody["message"])
}

func TestSendMessage_HumanOutput(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"POST /chat/channel/chan1/send-message": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		},
	}.Handler())
	setupHomeWithChannel(t, srv.URL)

	res := testutil.RunCommand(t, []string{"channels", "message", "send", "Hello", "--output", "table"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
}

func TestSendMessage_ChannelFromFlag(t *testing.T) {
	var capturedPath string
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"POST /chat/channel/flagchan/send-message": func(w http.ResponseWriter, r *http.Request) {
			capturedPath = r.URL.Path
			w.WriteHeader(http.StatusNoContent)
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"channels", "message", "send", "Hi", "--channel", "flagchan"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Equal(t, "/chat/channel/flagchan/send-message", capturedPath)
}

func TestSendMessage_Stdin(t *testing.T) {
	var capturedBody map[string]any
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"POST /chat/channel/chan1/send-message": func(w http.ResponseWriter, r *http.Request) {
			json.NewDecoder(r.Body).Decode(&capturedBody)
			w.WriteHeader(http.StatusNoContent)
		},
	}.Handler())
	setupHomeWithChannel(t, srv.URL)

	res := testutil.RunCommand(t, []string{"channels", "message", "send", "--json"}, "piped message content")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Equal(t, "piped message content", capturedBody["message"])
}

func TestSendMessage_WithName(t *testing.T) {
	var capturedBody map[string]any
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"POST /chat/channel/chan1/send-message": func(w http.ResponseWriter, r *http.Request) {
			json.NewDecoder(r.Body).Decode(&capturedBody)
			w.WriteHeader(http.StatusNoContent)
		},
	}.Handler())
	setupHomeWithChannel(t, srv.URL)

	res := testutil.RunCommand(t, []string{"channels", "message", "send", "Hello", "--name", "Deploy Bot", "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Equal(t, "Deploy Bot", capturedBody["name"])
}

func TestSendMessage_DirectMessageByUser_JSON(t *testing.T) {
	var capturedBody map[string]any
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"POST /chat/direct-message/send-message": func(w http.ResponseWriter, r *http.Request) {
			json.NewDecoder(r.Body).Decode(&capturedBody)
			testutil.RespondJSON(w, http.StatusAccepted, map[string]any{
				"channelId": "dm1",
				"queued":    true,
			})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{
		"channels", "message", "send", "Hello user", "--user", "user1", "--json",
	}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)

	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &out))
	assert.Equal(t, true, out["sent"])
	assert.Equal(t, "user1", out["user"])
	assert.Equal(t, "dm1", out["channel"])
	assert.Equal(t, true, out["queued"])
	assert.Equal(t, "user1", capturedBody["userId"])
	assert.Equal(t, "Hello user", capturedBody["message"])
}

func TestSendMessage_DirectMessageStdin(t *testing.T) {
	var capturedBody map[string]any
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"POST /chat/direct-message/send-message": func(w http.ResponseWriter, r *http.Request) {
			json.NewDecoder(r.Body).Decode(&capturedBody)
			testutil.RespondJSON(w, http.StatusAccepted, map[string]any{
				"channelId": "dm1",
				"queued":    true,
			})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{
		"channels", "message", "send", "--user", "user1", "--json",
	}, "piped DM")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Equal(t, "piped DM", capturedBody["message"])
}

func TestSendMessage_DirectMessageRejectsChannelFlag(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{
		"channels", "message", "send", "Hello", "--user", "user1", "--channel", "chan1",
	}, "")
	assert.Equal(t, 2, res.ExitCode)
	assert.Contains(t, res.Stderr, "cannot be used together")
}

func TestSendMessage_DirectMessageRejectsNameFlag(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{
		"channels", "message", "send", "Hello", "--user", "user1", "--name", "Bot",
	}, "")
	assert.Equal(t, 2, res.ExitCode)
	assert.Contains(t, res.Stderr, "only supported with --channel")
}

func TestSendMessage_MissingChannel(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{}.Handler())
	setupHome(t, srv.URL) // no channel_id in config

	res := testutil.RunCommand(t, []string{"channels", "message", "send", "Hello"}, "")
	assert.Equal(t, 2, res.ExitCode)
}

func TestSendMessage_MissingMessage(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{}.Handler())
	setupHomeWithChannel(t, srv.URL)

	res := testutil.RunCommand(t, []string{"channels", "message", "send"}, "") // no args, no stdin
	assert.Equal(t, 2, res.ExitCode)
}

func TestSendMessage_MissingToken(t *testing.T) {
	home := t.TempDir()
	testutil.WriteTempConfigAt(t, filepath.Join(home, ".copera.toml"), `
[profiles.default]
channel_id = "chan1"
`)
	testutil.SetEnv(t, "HOME", home)

	res := testutil.RunCommand(t, []string{"channels", "message", "send", "Hello"}, "")
	assert.Equal(t, 4, res.ExitCode)
}
