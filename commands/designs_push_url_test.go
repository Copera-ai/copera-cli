package commands_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/copera/copera-cli/internal/api"
	"github.com/copera/copera-cli/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDesignPushURL(t *testing.T) {
	const link = "https://app.copera.ai/acme/ai/artifacts/" + artID
	const previewLink = "https://preview.example/exact-version"
	const versionsPath = "/artifacts/" + artID + "/versions"
	saved := designPullFixture(designEntrypoint, "new", 5).CurrentVersion
	saved.ID = ver2ID
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"POST " + versionsPath: func(w http.ResponseWriter, _ *http.Request) {
			testutil.RespondJSON(w, http.StatusCreated, api.ArtifactPushResult{
				ArtifactID: artID, Version: saved, HeadRevision: 5, BuildState: "queued",
			})
		},
		"GET " + versionsPath + "/" + ver2ID: func(w http.ResponseWriter, _ *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, saved)
		},
		"GET " + versionsPath + "/" + ver2ID + "/build": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]json.RawMessage{
				"state": json.RawMessage(`"ready"`), "done": json.RawMessage(`true`), "evidenceStatus": json.RawMessage(`"ready"`),
				"screenshots": json.RawMessage(fmt.Sprintf(`[{"pageId":"home","width":390,"url":%q}]`, "http://"+r.Host+"/image")),
			})
		},
		"GET " + versionsPath + "/" + ver2ID + "/pages/home/preview": func(w http.ResponseWriter, _ *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, api.ArtifactPreview{Status: "ready", URL: previewLink})
		},
		"GET /image": func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("shot")) },
		"GET /artifacts/" + artID + "/handoff": func(w http.ResponseWriter, _ *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, api.ArtifactHandoff{Artifact: api.ArtifactHandoffArtifact{URL: link}})
		},
	}.Handler())
	setupHome(t, srv.URL)
	for _, mode := range []struct {
		name, format, waitFlag string
	}{
		{name: "plain", format: "plain"},
		{name: "json", format: "json"},
		{name: "wait", format: "plain", waitFlag: "--wait"},
		{name: "wait json", format: "json", waitFlag: "--wait"},
		{name: "preview", format: "plain", waitFlag: "--wait-for-preview"},
	} {
		t.Run(mode.name, func(t *testing.T) {
			dir := designPublishFolder(t)
			args := []string{"designs", "push", dir, "-m", "Edit", "--no-check", "--output", mode.format}
			if mode.waitFlag != "" {
				args = append(args, mode.waitFlag)
			}
			res := testutil.RunCommand(t, args, "")
			require.Equal(t, 0, res.ExitCode, res.Stderr)
			if mode.format != "json" {
				assert.Contains(t, res.Stdout, "URL: "+link+"\n")
			} else {
				var result map[string]json.RawMessage
				require.NoError(t, json.Unmarshal([]byte(res.Stdout), &result))
				assert.Equal(t, `"`+link+`"`, string(result["appUrl"]))
				assert.Empty(t, result["url"])
			}
			if mode.name == "wait" {
				assert.Contains(t, res.Stdout, "Screenshots: .copera/shots/")
				assert.Equal(t, "shot", readFile(t, filepath.Join(dir, ".copera/shots/home-390-light.webp")))
			}
			if mode.name == "preview" {
				assert.Contains(t, res.Stdout, previewLink+"\n")
			}
		})
	}
}
