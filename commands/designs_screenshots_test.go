package commands_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/copera/copera-cli/internal/api"
	"github.com/copera/copera-cli/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDesignsCheckReportsScreenshotPages(t *testing.T) {
	dir := designPublishFolder(t)
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"POST /artifacts/check": func(w http.ResponseWriter, r *http.Request) {
			const response = `{"ok":true,"screenshots":[
				{"pageId":"home","width":390,"data":"aG9tZQ=="},
				{"pageId":"trail","width":390,"scheme":"light","route":"trail.html#larch","data":"bGlnaHQ="},
				{"pageId":"trail","width":1440,"scheme":"dark","route":"trail.html#larch","data":"ZGFyaw=="}
			],"screenshotsOmittedPageIds":["faq"]}`
			testutil.RespondJSON(w, http.StatusOK, json.RawMessage(response))
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"designs", "check", dir, "--json"}, "")
	require.Zero(t, res.ExitCode, res.Stderr)
	var report struct {
		Screenshots               []string `json:"screenshots"`
		ScreenshotsOmittedPageIDs []string `json:"screenshotsOmittedPageIds"`
		ScreenshotPages           []struct {
			PageID string   `json:"pageId"`
			Route  string   `json:"route"`
			Paths  []string `json:"paths"`
		} `json:"screenshotPages"`
	}
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &report))
	wantPaths := []string{
		".copera/shots/home-390-light.webp",
		".copera/shots/trail-390-light.webp",
		".copera/shots/trail-1440-dark.webp",
	}
	assert.Equal(t, wantPaths, report.Screenshots)
	assert.Equal(t, []string{"faq"}, report.ScreenshotsOmittedPageIDs)
	for i, data := range []string{"home", "light", "dark"} {
		assert.Equal(t, data, readFile(t, filepath.Join(dir, wantPaths[i])))
	}
	require.Len(t, report.ScreenshotPages, 2)
	assert.Equal(t, "home", report.ScreenshotPages[0].PageID)
	assert.Empty(t, report.ScreenshotPages[0].Route)
	assert.Equal(t, wantPaths[:1], report.ScreenshotPages[0].Paths)
	assert.Equal(t, "trail", report.ScreenshotPages[1].PageID)
	assert.Equal(t, "trail.html#larch", report.ScreenshotPages[1].Route)
	assert.Equal(t, wantPaths[1:], report.ScreenshotPages[1].Paths)
}

func TestDesignsCheckScreenshotTerminalLine(t *testing.T) {
	dir := designPublishFolder(t)
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"POST /artifacts/check": func(w http.ResponseWriter, _ *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, json.RawMessage(`{"ok":true,"screenshots":[
				{"pageId":"home","width":390,"route":"home\nmarker\u001b[8m\u200b","data":"aG9tZQ=="}
			],"screenshotsOmittedPageIds":["other\rmarker\u202e"]}`))
		},
	}.Handler())
	setupHome(t, srv.URL)
	res := testutil.RunCommand(t, []string{"designs", "check", dir, "--output", "plain"}, "")
	require.Zero(t, res.ExitCode, res.Stderr)
	assert.NotContains(t, res.Stdout, "\nmarker")
	assert.NotContains(t, res.Stdout, "\x1b")
	assert.NotContains(t, res.Stdout, "\r")
	assert.Contains(t, res.Stdout, `Page home (home\nmarker\x1b[8m\u200b):`)
	assert.Contains(t, res.Stdout, `Page other\rmarker\u202e: screenshots omitted`)
}

func TestDesignsPushWaitReportsScreenshotPages(t *testing.T) {
	dir := designPublishFolder(t)
	const versionsPath = "/artifacts/" + artID + "/versions"
	savedVersion := api.ArtifactVersion{
		ID: ver2ID, Version: 5, AuthoringManifest: api.ArtifactManifest{DefaultPageID: "home"},
	}
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"POST /artifacts/check": func(w http.ResponseWriter, r *http.Request) {
			const response = `{"ok":true,"screenshots":[
				{"pageId":"trail","width":390,"scheme":"light","route":"trail.html#larch","data":"bGlnaHQ="},
				{"pageId":"trail","width":1440,"scheme":"dark","route":"trail.html#larch","data":"ZGFyaw=="}
			],"screenshotsOmittedPageIds":["faq"]}`
			testutil.RespondJSON(w, http.StatusOK, json.RawMessage(response))
		},
		"POST " + versionsPath: func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusCreated, api.ArtifactPushResult{
				Outcome: "created", ArtifactID: artID, HeadRevision: 5, Version: savedVersion, BuildState: "queued",
			})
		},
		"GET " + versionsPath + "/" + ver2ID: func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, savedVersion)
		},
		"GET " + versionsPath + "/" + ver2ID + "/build": func(w http.ResponseWriter, r *http.Request) {
			const response = `{"done":true,"state":"ready","screenshots":[
				{"pageId":"home","width":390,"url":%q},
				{"pageId":"about","width":390,"route":"about.html","url":%q}
			]}`
			base := "http://" + r.Host
			testutil.RespondJSON(w, http.StatusOK, json.RawMessage(fmt.Sprintf(response, base+"/home", base+"/about")))
		},
		"GET /home": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("home"))
		},
		"GET /about": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("about"))
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"designs", "push", dir, "-m", "Edit copy", "--wait", "--output", "plain"}, "")
	require.Zero(t, res.ExitCode, res.Stderr)
	assert.Contains(t, res.Stdout, "Page home: .copera/shots/home-390.webp\n")
	assert.Contains(t, res.Stdout, "Page about (about.html): .copera/shots/about-390-light.webp\n")
	assert.Contains(t, res.Stdout, "Page trail (trail.html#larch): .copera/shots/trail-390-light.webp, .copera/shots/trail-1440-dark.webp\n")
	assert.Equal(t, 1, strings.Count(res.Stdout, "Page trail"))
	assert.Contains(t, res.Stdout, "Page faq: screenshots omitted\n")
	assert.Equal(t, "home", readFile(t, filepath.Join(dir, ".copera/shots/home-390.webp")))
	assert.Equal(t, "about", readFile(t, filepath.Join(dir, ".copera/shots/about-390-light.webp")))
}
