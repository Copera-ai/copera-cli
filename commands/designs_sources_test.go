package commands_test

import (
	"io"
	"net/http"
	"testing"

	"github.com/copera/copera-cli/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDesignSourceCommands(t *testing.T) {
	const route = "/artifacts/" + phase5DesignID + "/sources"
	const target = `{"kind":"meeting","noteId":"cccccccccccccccccccccccc","selection":{"startMs":1000,"endMs":2000}}`
	const source = `{"id":"bbbbbbbbbbbbbbbbbbbbbbbb","target":` + target + `,"label":"Recorded call","href":"/ai/bankai-notes?selected=cccccccccccccccccccccccc","linkedAt":"2026-11-18T12:00:00Z"}`
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"POST " + route: func(w http.ResponseWriter, r *http.Request) {
			body, err := io.ReadAll(r.Body)
			assert.NoError(t, err)
			assert.JSONEq(t, `{"target":`+target+`}`, string(body))
			_, _ = io.WriteString(w, `{"source":`+source+`}`)
		},
		"GET " + route: func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, `{"sources":[`+source+`]}`)
		},
		"DELETE " + route + "/" + phase5OperationID: func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"designs", "link", phase5DesignID, "meeting", phase5FileID, "--from-ms", "1000", "--to-ms", "2000", "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.JSONEq(t, `{"source":`+source+`}`, res.Stdout)
	res = testutil.RunCommand(t, []string{"artifacts", "sources", phase5DesignID, "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.JSONEq(t, `{"sources":[`+source+`]}`, res.Stdout)
	res = testutil.RunCommand(t, []string{"designs", "unlink", phase5DesignID, phase5OperationID, "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.JSONEq(t, `{"removed":true}`, res.Stdout)
	res = testutil.RunCommand(t, []string{"designs", "link", phase5DesignID, "meeting", phase5FileID, "--from-ms", "1000"}, "")
	assert.Equal(t, 2, res.ExitCode)
}
