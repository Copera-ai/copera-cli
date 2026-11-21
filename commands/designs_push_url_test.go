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
				"state": json.RawMessage(`"ready"`), "done": json.RawMessage(`true`),
				"screenshots": json.RawMessage(fmt.Sprintf(`[{"pageId":"home","width":390,"url":%q}]`, "http://"+r.Host+"/image")),
			})
		},
		"GET /image": func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("shot")) },
		"GET /artifacts/" + artID + "/handoff": func(w http.ResponseWriter, _ *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, api.ArtifactHandoff{Artifact: api.ArtifactHandoffArtifact{URL: link}})
		},
	}.Handler())
	setupHome(t, srv.URL)
	for _, output := range []string{"plain", "json", "wait"} {
		t.Run(output, func(t *testing.T) {
			dir := designPublishFolder(t)
			args := []string{"designs", "push", dir, "-m", "Edit", "--no-check"}
			if output == "wait" {
				args = append(args, "--wait", "--output", "plain")
			} else {
				args = append(args, "--output", output)
			}
			res := testutil.RunCommand(t, args, "")
			require.Equal(t, 0, res.ExitCode, res.Stderr)
			if output != "json" {
				assert.Contains(t, res.Stdout, "URL: "+link+"\n")
			} else {
				assert.Contains(t, res.Stdout, `"url": "`+link+`"`)
			}
			if output == "wait" {
				assert.Contains(t, res.Stdout, "Screenshots: .copera/shots/")
				assert.Equal(t, "shot", readFile(t, filepath.Join(dir, ".copera/shots/home-390.webp")))
			}
		})
	}
}
