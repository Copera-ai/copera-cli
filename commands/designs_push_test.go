package commands_test

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/copera/copera-cli/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const designEntrypoint = "src/main.tsx"

func writePublishFixture(t *testing.T, name string, data []byte) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(name), 0o755))
	require.NoError(t, os.WriteFile(name, data, 0o644))
}

func designPublishFolder(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	digest := sha256.Sum256([]byte("old"))
	version := artifactVersionFixture(verID, 4)
	state, err := json.Marshal(map[string]any{
		"id": artID, "baseVersionId": verID, "headRevision": 4,
		"kind": "web_app", "runtime": "vite_react", "entrypoint": designEntrypoint,
		"manifest": version["authoringManifest"],
		"files":    map[string]string{designEntrypoint: hex.EncodeToString(digest[:]), "obsolete.txt": digestOf("old text")},
	})
	require.NoError(t, err)
	writePublishFixture(t, filepath.Join(dir, ".copera", "design.json"), state)
	writePublishFixture(t, filepath.Join(dir, designEntrypoint), []byte("new"))
	return dir
}

func TestDesignsCheck(t *testing.T) {
	dir := designPublishFolder(t)
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"POST /artifacts/check": func(w http.ResponseWriter, r *http.Request) {
			var req struct {
				ID                 string `json:"artifactId"`
				Base               string `json:"baseVersionId"`
				IncludeScreenshots bool   `json:"includeScreenshots"`
				Files              []struct {
					Path    string `json:"path"`
					Content string `json:"content"`
				} `json:"files"`
			}
			if assert.NoError(t, json.NewDecoder(r.Body).Decode(&req)) {
				assert.Equal(t, artID, req.ID)
				assert.Equal(t, verID, req.Base)
				assert.True(t, req.IncludeScreenshots)
				if assert.Len(t, req.Files, 1) {
					assert.Equal(t, designEntrypoint, req.Files[0].Path)
					assert.Equal(t, "new", req.Files[0].Content)
				}
			}
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"ok": false,
				"diagnostics": []map[string]any{{
					"filePath": designEntrypoint, "line": 7, "column": 2, "message": "invalid syntax", "severity": "error",
				}},
				"runtimeConsole": map[string]any{
					"reported": true, "entries": []map[string]any{{"level": "error", "text": "page crashed", "count": 1}},
				},
				"screenshots": []map[string]any{{
					"pageId": "home", "width": 390, "height": 600, "mediaType": "image/webp",
					"byteLength": 4, "data": base64.StdEncoding.EncodeToString([]byte("shot")),
				}},
			})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"designs", "check", dir, "--output", "plain"}, "")
	require.Equal(t, 1, res.ExitCode, res.Stderr)
	assert.Contains(t, res.Stdout, designEntrypoint+":7:2")
	assert.Contains(t, res.Stdout, "invalid syntax")
	assert.Contains(t, res.Stdout, "page crashed")
	image, err := os.ReadFile(filepath.Join(dir, ".copera", "shots", "home-390.webp"))
	require.NoError(t, err)
	assert.Equal(t, "shot", string(image))
}

func TestDesignsPush(t *testing.T) {
	dir := designPublishFolder(t)
	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	writePublishFixture(t, filepath.Join(dir, "assets", "logo.png"), png)
	writePublishFixture(t, filepath.Join(dir, "AGENTS.md"), []byte("local rules"))
	t.Setenv("CLAUDECODE", "1")

	const (
		prefix    = "/artifacts/" + artID + "/versions"
		localPath = "assets/logo.png"
		assetID   = "dddddddddddddddddddddddd"
		assetPath = "design/assets/" + assetID + ".png"
	)
	var orderMu sync.Mutex
	var order []string
	record := func(step string) {
		orderMu.Lock()
		defer orderMu.Unlock()
		order = append(order, step)
	}
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"POST /artifacts/check": func(w http.ResponseWriter, r *http.Request) {
			record("check")
			testutil.RespondJSON(w, http.StatusOK, map[string]bool{"ok": true})
		},
		"POST /artifacts/" + artID + "/assets": func(w http.ResponseWriter, r *http.Request) {
			record("upload")
			file, header, err := r.FormFile("file")
			if assert.NoError(t, err) {
				defer file.Close()
				assert.Equal(t, "logo.png", header.Filename)
				assert.Equal(t, "image/png", header.Header.Get("Content-Type"))
				data, err := io.ReadAll(file)
				assert.NoError(t, err)
				assert.Equal(t, png, data)
			}
			digest := sha256.Sum256(png)
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"id": assetID, "path": assetPath, "usage": "image", "mediaType": "image/png",
				"contentDigest": hex.EncodeToString(digest[:]), "byteLength": len(png),
			})
		},
		"POST " + prefix: func(w http.ResponseWriter, r *http.Request) {
			record("push")
			var req struct {
				BaseVersionID        string   `json:"baseVersionId"`
				ExpectedHeadRevision int      `json:"expectedHeadRevision"`
				ChangeSummary        string   `json:"changeSummary"`
				IdempotencyKey       string   `json:"idempotencyKey"`
				Tool                 string   `json:"tool"`
				RemovedPaths         []string `json:"removedPaths"`
				Files                []struct {
					Path    string `json:"path"`
					Content string `json:"content"`
				} `json:"files"`
				Manifest struct {
					Assets []struct {
						Path string `json:"path"`
					} `json:"assets"`
				} `json:"authoringManifest"`
			}
			if assert.NoError(t, json.NewDecoder(r.Body).Decode(&req)) {
				assert.Equal(t, verID, req.BaseVersionID)
				assert.Equal(t, 4, req.ExpectedHeadRevision)
				assert.Equal(t, "Refresh logo", req.ChangeSummary)
				assert.Equal(t, "claude_code", req.Tool)
				assert.NotEmpty(t, req.IdempotencyKey)
				assert.Equal(t, []string{"obsolete.txt"}, req.RemovedPaths)
				if assert.Len(t, req.Files, 1) {
					assert.Equal(t, designEntrypoint, req.Files[0].Path)
					assert.Equal(t, "new", req.Files[0].Content)
				}
				if assert.Len(t, req.Manifest.Assets, 1) {
					assert.Equal(t, assetPath, req.Manifest.Assets[0].Path)
				}
			}
			testutil.RespondJSON(w, http.StatusCreated, map[string]any{
				"outcome": "created", "artifactId": artID, "headRevision": 5,
				"version": artifactVersionFixture(ver2ID, 5), "buildState": "queued",
				"warnings": []string{},
			})
		},
		"GET " + prefix + "/" + ver2ID + "/build": func(w http.ResponseWriter, r *http.Request) {
			record("build")
			assert.NotEmpty(t, r.URL.Query().Get("wait"))
			testutil.RespondJSON(w, http.StatusOK, map[string]any{"done": true, "state": "ready"})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"designs", "push", dir, "-m", "Refresh logo", "--wait", "--output", "plain"}, "")
	require.Equal(t, 0, res.ExitCode, res.Stderr)
	orderMu.Lock()
	assert.Equal(t, []string{"check", "upload", "push", "build"}, order)
	orderMu.Unlock()
	assert.Contains(t, res.Stdout, "ready")
	state, err := os.ReadFile(filepath.Join(dir, ".copera", "design.json"))
	require.NoError(t, err)
	assert.Contains(t, string(state), ver2ID)
	var updated struct {
		Revision   int               `json:"headRevision"`
		Files      map[string]string `json:"files"`
		AssetPaths map[string]string `json:"assetPaths"`
	}
	require.NoError(t, json.Unmarshal(state, &updated))
	assert.Equal(t, 5, updated.Revision)
	digest := sha256.Sum256([]byte("new"))
	assert.Equal(t, hex.EncodeToString(digest[:]), updated.Files[designEntrypoint])
	assert.Equal(t, assetPath, updated.AssetPaths[localPath])
	asset, err := os.ReadFile(filepath.Join(dir, localPath))
	require.NoError(t, err)
	assert.Equal(t, png, asset)
}

func TestDesignsPushConflict(t *testing.T) {
	dir := designPublishFolder(t)
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"POST /artifacts/" + artID + "/versions": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusConflict, map[string]any{
				"message": "head moved", "reason": "stale_head", "changedPaths": []string{"src/other.tsx"},
			})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"designs", "push", dir, "-m", "Edit copy", "--no-check"}, "")
	require.Equal(t, 5, res.ExitCode, res.Stderr)
	assert.Contains(t, res.Stderr, "src/other.tsx")
	state, err := os.ReadFile(filepath.Join(dir, ".copera", "design.json"))
	require.NoError(t, err)
	assert.Contains(t, string(state), verID)
}

func TestDesignsScreenshot(t *testing.T) {
	t.Setenv("GO_TEST", "1")
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /artifacts/" + artID: func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, artifactFixture())
		},
		"GET /artifacts/" + artID + "/versions/" + verID + "/build": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"done": true, "state": "ready",
				"screenshots": []map[string]any{
					{"pageId": "home", "width": 1440, "height": 900, "url": "http://" + r.Host + "/desktop"},
					{"pageId": "home", "width": 390, "height": 600, "url": "http://" + r.Host + "/small"},
				},
			})
		},
		"GET /small": func(w http.ResponseWriter, r *http.Request) {
			assert.Empty(t, r.Header.Get("Authorization"))
			_, _ = io.WriteString(w, "mobile")
		},
	}.Handler())
	setupHome(t, srv.URL)
	file := filepath.Join(t.TempDir(), "shot.webp")

	res := testutil.RunCommand(t, []string{"designs", "screenshot", artID, "--page", "home", "--viewport", "390", "-o", file}, "")
	require.Equal(t, 0, res.ExitCode, res.Stderr)
	image, err := os.ReadFile(file)
	require.NoError(t, err)
	assert.Equal(t, "mobile", string(image))
}
