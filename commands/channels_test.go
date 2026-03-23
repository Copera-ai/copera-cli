package commands_test

import (
	"encoding/json"
	"net/http"
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
