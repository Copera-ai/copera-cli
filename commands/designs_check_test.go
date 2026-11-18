package commands_test

import (
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/copera/copera-cli/internal/api"
	"github.com/copera/copera-cli/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDesignsCheckMultipartWithNewBinary(t *testing.T) {
	dir := t.TempDir()
	const newPath = "assets/hero.png"
	const pinnedPath = "assets/logo.png"
	newPNG := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	pinnedPNG := append([]byte{1}, newPNG...)
	pinnedDigest := digestOf(string(pinnedPNG))
	state, err := json.Marshal(struct {
		ID            string               `json:"id"`
		BaseVersionID string               `json:"baseVersionId"`
		Files         map[string]string    `json:"files"`
		Manifest      api.ArtifactManifest `json:"manifest"`
	}{
		ID: artID, BaseVersionID: verID,
		Files: map[string]string{pinnedPath: pinnedDigest},
		Manifest: api.ArtifactManifest{Assets: []api.ArtifactAsset{{
			ID: artID, Path: pinnedPath, MediaType: "image/png", ContentDigest: pinnedDigest, Usage: "image",
		}}},
	})
	require.NoError(t, err)
	writePublishFixture(t, filepath.Join(dir, ".copera", "design.json"), state)
	writePublishFixture(t, filepath.Join(dir, designEntrypoint), []byte("new"))
	writePublishFixture(t, filepath.Join(dir, pinnedPath), pinnedPNG)
	writePublishFixture(t, filepath.Join(dir, newPath), newPNG)

	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"POST /artifacts/check": func(w http.ResponseWriter, r *http.Request) {
			var project io.Reader = r.Body
			mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if !assert.NoError(t, err) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_, statErr := os.Stat(filepath.Join(dir, newPath))
			if statErr == nil {
				if !assert.Equal(t, "multipart/form-data", mediaType) {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				reader, err := r.MultipartReader()
				if !assert.NoError(t, err) {
					return
				}
				part, err := reader.NextPart()
				if !assert.NoError(t, err) {
					return
				}
				assert.Equal(t, "project", part.FormName())
				project = part
				defer func() {
					part, err := reader.NextPart()
					if !assert.NoError(t, err) {
						return
					}
					_, params, err := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
					assert.NoError(t, err)
					assert.Equal(t, "file", part.FormName())
					assert.Equal(t, newPath, params["filename"])
					assert.Equal(t, "image/png", part.Header.Get("Content-Type"))
					data, err := io.ReadAll(part)
					assert.NoError(t, err)
					assert.Equal(t, newPNG, data)
					_, err = reader.NextPart()
					assert.ErrorIs(t, err, io.EOF)
				}()
			} else {
				assert.ErrorIs(t, statErr, os.ErrNotExist)
				assert.Equal(t, "application/json", mediaType)
			}
			var input api.ArtifactCheckInput
			assert.NoError(t, json.NewDecoder(project).Decode(&input))
			assert.Equal(t, artID, input.ArtifactID)
			assert.Equal(t, verID, input.BaseVersionID)
			assert.Equal(t, []api.ArtifactContentFile{{Path: designEntrypoint, MediaType: "text/tsx", Content: "new"}}, input.Files)
			testutil.RespondJSON(w, http.StatusOK, map[string]bool{"ok": true})
		},
	}.Handler())
	setupHome(t, srv.URL)
	args := []string{"designs", "check", dir, "--json"}
	result := testutil.RunCommand(t, args, "")
	assert.Zero(t, result.ExitCode, result.Stderr)

	require.NoError(t, os.Remove(filepath.Join(dir, newPath)))
	result = testutil.RunCommand(t, args, "")
	assert.Zero(t, result.ExitCode, result.Stderr)
}

func TestDesignsCheckScreenshotSchemes(t *testing.T) {
	const lightShot = `{"pageId":"home","width":390,"data":"bGlnaHQ="}`
	const darkShot = `{"pageId":"home","width":390,"scheme":"dark","data":"ZGFyaw=="}`
	for _, tc := range []struct {
		name     string
		response string
		schemes  []string
	}{
		{"light and dark", `{"ok":true,"darkScheme":true,"screenshots":[` + lightShot + `,` + darkShot + `]}`, []string{"light", "dark"}},
		{"light only", `{"ok":true,"darkScheme":false,"screenshots":[` + lightShot + `]}`, []string{"light"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := designPublishFolder(t)
			srv := testutil.NewMockServer(t, testutil.MockRoutes{
				"POST /artifacts/check": func(w http.ResponseWriter, r *http.Request) {
					testutil.RespondJSON(w, http.StatusOK, json.RawMessage(tc.response))
				},
			}.Handler())
			setupHome(t, srv.URL)

			result := testutil.RunCommand(t, []string{"designs", "check", dir, "--output", "plain"}, "")
			require.Zero(t, result.ExitCode, result.Stderr)
			for _, scheme := range tc.schemes {
				path := ".copera/shots/home-390-" + scheme + ".webp"
				assert.Contains(t, result.Stdout, path)
				assert.Equal(t, scheme, readFile(t, filepath.Join(dir, path)))
			}
			if len(tc.schemes) == 1 {
				assert.Contains(t, result.Stdout, "darkScheme: false\n")
			}
		})
	}
}

func TestDesignsScreenshotFailuresWarn(t *testing.T) {
	t.Setenv("GO_TEST", "1")
	const versionsPath = "/artifacts/" + artID + "/versions"
	savedVersion := api.ArtifactVersion{ID: ver2ID, Version: 5}
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"POST /artifacts/check": func(w http.ResponseWriter, r *http.Request) {
			const response = `{"ok":true,"screenshots":[{"pageId":"home","width":390,"data":"c2hvdA=="}]}`
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
			const response = `{"done":true,"state":"ready","screenshots":[{"pageId":"home","width":390,"url":%q}]}`
			testutil.RespondJSON(w, http.StatusOK, json.RawMessage(fmt.Sprintf(response, "http://"+r.Host+"/shot")))
		},
		"GET /shot": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		},
	}.Handler())
	setupHome(t, srv.URL)

	t.Run("push", func(t *testing.T) {
		dir := designPublishFolder(t)
		result := testutil.RunCommand(t, []string{"designs", "push", dir, "-m", "Edit copy", "--no-check", "--wait", "--json"}, "")
		assert.Zero(t, result.ExitCode, result.Stderr)
		assert.Contains(t, result.Stderr, "Warning: download Design screenshot")
		assert.Contains(t, result.Stderr, "403")
		var report struct {
			State   string              `json:"state"`
			Version api.ArtifactVersion `json:"version"`
		}
		if assert.NoError(t, json.Unmarshal([]byte(result.Stdout), &report)) {
			assert.Equal(t, "ready", report.State)
			assert.Equal(t, savedVersion, report.Version)
		}
	})

	t.Run("check", func(t *testing.T) {
		dir := designPublishFolder(t)
		writePublishFixture(t, filepath.Join(dir, ".copera", "shots"), []byte("blocked"))
		result := testutil.RunCommand(t, []string{"designs", "check", dir, "--json"}, "")
		assert.Zero(t, result.ExitCode, result.Stderr)
		assert.Contains(t, result.Stderr, "Warning: save Design screenshot")
		assert.Contains(t, result.Stdout, `"state": "ready"`)
	})
}
