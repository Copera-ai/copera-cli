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
	"time"

	"github.com/copera/copera-cli/internal/api"
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
			var metadata map[string]string
			assert.NoError(t, json.Unmarshal([]byte(r.FormValue("metadata")), &metadata))
			assert.Equal(t, localPath, metadata["path"])
			digest := sha256.Sum256(png)
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"id": assetID, "path": localPath, "usage": "image", "mediaType": "image/png",
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
					assert.Equal(t, localPath, req.Manifest.Assets[0].Path)
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
	assert.Empty(t, updated.AssetPaths)
	asset, err := os.ReadFile(filepath.Join(dir, localPath))
	require.NoError(t, err)
	assert.Equal(t, png, asset)
}

func TestDesignsFirstPushWithImages(t *testing.T) {
	testDesignsFirstPush(t, false)
}

func TestDesignsFirstPushRetriesSecondImage(t *testing.T) {
	testDesignsFirstPush(t, true)
}

func testDesignsFirstPush(t *testing.T, busy bool) {
	t.Helper()
	const (
		hero    = "assets/hero.png"
		logo    = "assets/logo.png"
		png     = "\x89PNG\r\n\x1a\n"
		html    = `<img src="assets/hero.png"><img src="assets/logo.png">`
		summary = "Add first screen"
		seed    = "shared/DESIGN.md"
	)
	dir := filepath.Join(t.TempDir(), "draft")
	stateFile := filepath.Join(dir, ".copera", "design.json")
	var mu sync.Mutex
	var steps []string
	var logoBusyAt time.Time
	pushes := 0
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /artifacts/guide": designGuideResponse,
		"GET /workspace/info": func(w http.ResponseWriter, _ *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, api.Workspace{ID: designWorkspaceID})
		},
		"POST /artifacts/check": func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			defer mu.Unlock()
			steps = append(steps, "check")
			if pushes == 0 {
				assert.Contains(t, r.Header.Get("Content-Type"), "multipart/form-data")
			} else {
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
				var req api.ArtifactCheckInput
				if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&req)) ||
					!assert.NotNil(t, req.AuthoringManifest) ||
					!assert.Empty(t, req.AuthoringManifest.SharedDesign.GuidancePath) {
					testutil.RespondJSON(w, http.StatusBadRequest, map[string]string{"message": "manifest shared design file is missing"})
					return
				}
				assert.Len(t, req.AuthoringManifest.Assets, 2)
			}
			testutil.RespondJSON(w, http.StatusOK, map[string]bool{"ok": true})
		},
		"POST /artifacts/": func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			defer mu.Unlock()
			steps = append(steps, "create")
			var req struct {
				ChangeSummary string `json:"changeSummary"`
				Project       struct {
					Files []api.ArtifactContentFile `json:"files"`
				} `json:"project"`
			}
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&req))
			assert.Equal(t, summary, req.ChangeSummary)
			assert.NotEmpty(t, req.Project.Files)
			created := designPullFixture("index.html", html, 1)
			created.CurrentVersion.AuthoringManifest.SharedDesign = api.ArtifactSharedDesign{Version: 1, GuidancePath: seed}
			testutil.RespondJSON(w, http.StatusOK, created)
		},
		"POST /artifacts/" + artID + "/assets": func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			defer mu.Unlock()
			var metadata map[string]string
			assert.NoError(t, json.Unmarshal([]byte(r.FormValue("metadata")), &metadata))
			name := metadata["path"]
			steps = append(steps, name)
			assert.Contains(t, readFile(t, stateFile), artID)
			switch name {
			case hero:
				assertDesignUpload(t, r, "hero.png", png+"hero")
			case logo:
				assertDesignUpload(t, r, "logo.png", png+"logo")
				assert.Contains(t, readFile(t, stateFile), hero)
				if busy && logoBusyAt.IsZero() {
					logoBusyAt = time.Now()
					w.Header().Set("Retry-After", "1")
					testutil.RespondJSON(w, http.StatusServiceUnavailable, map[string]string{"reason": "upload_busy", "message": "upload busy"})
					return
				}
				if busy {
					assert.GreaterOrEqual(t, time.Since(logoBusyAt), time.Second)
				}
			default:
				t.Errorf("unexpected upload path %q", name)
			}
			testutil.RespondJSON(w, http.StatusOK, api.ArtifactAsset{
				ID: digestOf(name)[:24], Path: name, MediaType: "image/png",
				ContentDigest: metadata["expectedContentDigest"], Usage: "image",
			})
		},
		"POST /artifacts/" + artID + "/versions": func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			defer mu.Unlock()
			steps = append(steps, "push")
			var req api.ArtifactPushInput
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&req))
			assert.Equal(t, verID, req.BaseVersionID)
			assert.Equal(t, 1, req.ExpectedHeadRevision)
			assert.Equal(t, summary, req.ChangeSummary)
			assert.Equal(t, []api.ArtifactContentFile{
				{Path: "index.html", MediaType: "text/html", Content: html},
				{Path: "style.css", MediaType: "text/css", Content: "img { width: 100%; }"},
			}, req.Files)
			if assert.NotNil(t, req.AuthoringManifest) && assert.Len(t, req.AuthoringManifest.Assets, 2) {
				assert.Equal(t, seed, req.AuthoringManifest.SharedDesign.GuidancePath)
				assert.Equal(t, hero, req.AuthoringManifest.Assets[0].Path)
				assert.Equal(t, logo, req.AuthoringManifest.Assets[1].Path)
			}
			pushes++
			if busy && pushes == 1 {
				testutil.RespondJSON(w, http.StatusBadRequest, map[string]string{"message": "version refused"})
				return
			}
			testutil.RespondJSON(w, http.StatusCreated, api.ArtifactPushResult{
				Outcome: "created", ArtifactID: artID, HeadRevision: 2, BuildState: "queued",
				Version: designPullFixture("index.html", html, 2).CurrentVersion,
			})
		},
	}.Handler())
	setupHome(t, srv.URL)
	init := testutil.RunCommand(t, []string{"designs", "init", dir, "--kind", "website"}, "")
	require.Equal(t, 0, init.ExitCode, init.Stderr)
	writePublishFixture(t, filepath.Join(dir, "index.html"), []byte(html))
	writePublishFixture(t, filepath.Join(dir, "style.css"), []byte("img { width: 100%; }"))
	writePublishFixture(t, filepath.Join(dir, hero), []byte(png+"hero"))
	writePublishFixture(t, filepath.Join(dir, logo), []byte(png+"logo"))

	args := []string{"designs", "push", dir, "-m", summary, "--json"}
	res := testutil.RunCommand(t, args, "")
	want := []string{"check", "create", hero, logo, "push"}
	if busy && assert.Equal(t, 1, res.ExitCode, res.Stderr) {
		assert.Contains(t, res.Stderr, artID)
		assert.Contains(t, res.Stderr, hero)
		assert.Contains(t, res.Stderr, logo)
		assert.Contains(t, res.Stderr, "version refused")
		res = testutil.RunCommand(t, args, "")
		want = []string{"check", "create", hero, logo, logo, "push", "check", "push"}
	}
	require.Equal(t, 0, res.ExitCode, res.Stderr)
	mu.Lock()
	assert.Equal(t, want, steps)
	mu.Unlock()
	var state struct {
		ID    string            `json:"id"`
		Base  string            `json:"baseVersionId"`
		Files map[string]string `json:"files"`
	}
	require.NoError(t, json.Unmarshal([]byte(readFile(t, stateFile)), &state))
	assert.Equal(t, artID, state.ID)
	assert.Equal(t, ver2ID, state.Base)
	assert.Equal(t, digestOf(png+"hero"), state.Files[hero])
	assert.Equal(t, digestOf(png+"logo"), state.Files[logo])
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
