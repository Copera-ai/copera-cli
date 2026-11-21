package commands_test

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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
	image, err := os.ReadFile(filepath.Join(dir, ".copera", "shots", "home-390-light.webp"))
	require.NoError(t, err)
	assert.Equal(t, "shot", string(image))
}

func TestDesignsExistingCapabilitySettings(t *testing.T) {
	for _, capabilities := range []string{"", "[]", `[{"kind":"ai","contractVersion":1}]`} {
		t.Run(capabilities, func(t *testing.T) {
			dir := designPublishFolder(t)
			if capabilities != "" {
				path := filepath.Join(dir, ".copera", "design.json")
				state, err := os.ReadFile(path)
				require.NoError(t, err)
				state = []byte(strings.TrimSuffix(string(state), "}") + `,"capabilities":` + capabilities + "}")
				require.NoError(t, os.WriteFile(path, state, 0600))
			}
			var requests atomic.Int32
			checkProject := func(r *http.Request) {
				requests.Add(1)
				var input struct {
					Project json.RawMessage `json:"project"`
				}
				if assert.NoError(t, json.NewDecoder(r.Body).Decode(&input)) {
					if capabilities == "" {
						assert.Empty(t, input.Project)
					} else {
						assert.JSONEq(t, `{"capabilities":`+capabilities+"}", string(input.Project))
					}
				}
			}
			srv := testutil.NewMockServer(t, testutil.MockRoutes{
				"POST /artifacts/check": func(w http.ResponseWriter, r *http.Request) {
					checkProject(r)
					testutil.RespondJSON(w, http.StatusOK, json.RawMessage(`{"ok":true}`))
				},
				"POST /artifacts/" + artID + "/versions": func(w http.ResponseWriter, r *http.Request) {
					checkProject(r)
					testutil.RespondJSON(w, http.StatusOK, api.ArtifactPushResult{
						Outcome: "unchanged", ArtifactID: artID, HeadRevision: 4,
						Version: api.ArtifactVersion{ID: verID, Version: 1}, BuildState: "ready",
					})
				},
			}.Handler())
			setupHome(t, srv.URL)
			for _, args := range [][]string{
				{"designs", "check", dir, "--json"},
				{"designs", "push", dir, "-m", "Update declarations", "--no-check", "--json"},
			} {
				result := testutil.RunCommand(t, args, "")
				assert.Equal(t, 0, result.ExitCode, result.Stderr)
			}
			assert.EqualValues(t, 2, requests.Load())
		})
	}
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
		BuildState string            `json:"buildState"`
	}
	require.NoError(t, json.Unmarshal(state, &updated))
	assert.Equal(t, 5, updated.Revision)
	assert.Equal(t, "ready", updated.BuildState)
	digest := sha256.Sum256([]byte("new"))
	assert.Equal(t, hex.EncodeToString(digest[:]), updated.Files[designEntrypoint])
	assert.Empty(t, updated.AssetPaths)
	asset, err := os.ReadFile(filepath.Join(dir, localPath))
	require.NoError(t, err)
	assert.Equal(t, png, asset)
}

func TestDesignsPushReusesMatchingCheck(t *testing.T) {
	for _, test := range []struct {
		name          string
		edit          func(*testing.T, string)
		rejected      bool
		expectedCalls int32
	}{
		{name: "identical files and base", expectedCalls: 1},
		{name: "changed contents", expectedCalls: 2, edit: func(t *testing.T, dir string) {
			writePublishFixture(t, filepath.Join(dir, designEntrypoint), []byte("changed after check"))
		}},
		{name: "different file set", expectedCalls: 2, edit: func(t *testing.T, dir string) {
			writePublishFixture(t, filepath.Join(dir, "style.css"), []byte("p { color: red; }"))
		}},
		{name: "different base", expectedCalls: 2, edit: func(t *testing.T, dir string) {
			editDesignCheckState(t, dir, func(state map[string]json.RawMessage) {
				state["baseVersionId"] = json.RawMessage(`"` + ver2ID + `"`)
			})
		}},
		{name: "expired", expectedCalls: 2, edit: func(t *testing.T, dir string) {
			editDesignCheckState(t, dir, func(state map[string]json.RawMessage) {
				var receipt map[string]json.RawMessage
				require.NoError(t, json.Unmarshal(state["lastCheck"], &receipt))
				receipt["checkedAt"], _ = json.Marshal(time.Now().Add(-31 * time.Minute).Format(time.RFC3339Nano))
				state["lastCheck"], _ = json.Marshal(receipt)
			})
		}},
		{name: "rejected check invalidates a previous pass", rejected: true, expectedCalls: 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := designPublishFolder(t)
			var checks atomic.Int32
			const prefix = "/artifacts/" + artID + "/versions"
			srv := testutil.NewMockServer(t, testutil.MockRoutes{
				"POST /artifacts/check": func(w http.ResponseWriter, _ *http.Request) {
					n := checks.Add(1)
					testutil.RespondJSON(w, http.StatusOK, map[string]bool{"ok": !test.rejected || n != 2})
				},
				"POST " + prefix: func(w http.ResponseWriter, _ *http.Request) {
					testutil.RespondJSON(w, http.StatusCreated, map[string]any{
						"outcome": "created", "artifactId": artID, "headRevision": 5,
						"version": artifactVersionFixture(ver2ID, 5), "buildState": "queued",
					})
				},
				"GET " + prefix + "/" + ver2ID: func(w http.ResponseWriter, _ *http.Request) {
					testutil.RespondJSON(w, http.StatusOK, artifactVersionFixture(ver2ID, 5))
				},
			}.Handler())
			setupHome(t, srv.URL)

			check := []string{"designs", "check", dir, "--json"}
			passed := testutil.RunCommand(t, check, "")
			require.Equal(t, 0, passed.ExitCode, passed.Stderr)
			state, err := os.ReadFile(filepath.Join(dir, ".copera", "design.json"))
			require.NoError(t, err)
			assert.Contains(t, string(state), `"lastCheck"`)
			if test.rejected {
				failed := testutil.RunCommand(t, check, "")
				require.Equal(t, 1, failed.ExitCode, failed.Stderr)
			}
			if test.edit != nil {
				test.edit(t, dir)
			}

			push := testutil.RunCommand(t, []string{"designs", "push", dir, "-m", "Edit", "--json"}, "")
			require.Equal(t, 0, push.ExitCode, push.Stderr)
			assert.Equal(t, test.expectedCalls, checks.Load())
		})
	}
}

func editDesignCheckState(t *testing.T, dir string, edit func(map[string]json.RawMessage)) {
	t.Helper()
	name := filepath.Join(dir, ".copera", "design.json")
	data, err := os.ReadFile(name)
	require.NoError(t, err)
	var state map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &state))
	edit(state)
	data, err = json.Marshal(state)
	require.NoError(t, err)
	writePublishFixture(t, name, data)
}

func TestDesignsPushWaitForPreview(t *testing.T) {
	const (
		prefix   = "/artifacts/" + artID + "/versions"
		readyAt  = "2026-11-18T15:00:00Z"
		shellURL = "https://preview.example/exact-version"
		appURL   = "https://app.copera.ai/acme/ai/artifacts/" + artID
	)
	for _, test := range []struct {
		name          string
		build         map[string]any
		expectedError string
	}{
		{name: "preview while evidence is pending", build: map[string]any{
			"state": "building", "done": false, "previewReadyAt": readyAt, "evidenceStatus": "pending",
		}},
		{name: "assembly fails", expectedError: "artifact_compile_failed", build: map[string]any{
			"state": "failed", "done": true, "previewReadyAt": nil, "evidenceStatus": "failed", "failureCode": "artifact_compile_failed",
		}},
		{name: "old API ignores waitFor", expectedError: "--wait", build: map[string]any{
			"state": "ready", "done": true,
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := designPublishFolder(t)
			var previews, versionReads atomic.Int32
			srv := testutil.NewMockServer(t, testutil.MockRoutes{
				"POST " + prefix: func(w http.ResponseWriter, _ *http.Request) {
					compact := artifactVersionFixture(ver2ID, 5)
					delete(compact, "authoringManifest")
					testutil.RespondJSON(w, http.StatusCreated, map[string]any{
						"outcome": "created", "artifactId": artID, "headRevision": 5,
						"version": compact, "buildState": "queued",
					})
				},
				"GET " + prefix + "/" + ver2ID: func(w http.ResponseWriter, _ *http.Request) {
					versionReads.Add(1)
					testutil.RespondJSON(w, http.StatusOK, artifactVersionFixture(ver2ID, 5))
				},
				"GET " + prefix + "/" + ver2ID + "/build": func(w http.ResponseWriter, r *http.Request) {
					assert.Equal(t, "preview", r.URL.Query().Get("waitFor"))
					testutil.RespondJSON(w, http.StatusOK, test.build)
				},
				"GET " + prefix + "/" + ver2ID + "/pages/home/preview": func(w http.ResponseWriter, _ *http.Request) {
					n := previews.Add(1)
					response := map[string]string{"status": "ready"}
					if n > 1 {
						response["url"] = shellURL
					}
					testutil.RespondJSON(w, http.StatusOK, response)
				},
				"GET /artifacts/" + artID + "/handoff": func(w http.ResponseWriter, _ *http.Request) {
					testutil.RespondJSON(w, http.StatusOK, api.ArtifactHandoff{Artifact: api.ArtifactHandoffArtifact{URL: appURL}})
				},
			}.Handler())
			setupHome(t, srv.URL)

			push := testutil.RunCommand(t, []string{"designs", "push", dir, "-m", "Edit", "--no-check", "--wait-for-preview", "--json"}, "")
			if test.expectedError != "" {
				require.Equal(t, 1, push.ExitCode, push.Stderr)
				assert.Contains(t, push.Stderr, test.expectedError)
				assert.Zero(t, previews.Load())
				state, err := os.ReadFile(filepath.Join(dir, ".copera", "design.json"))
				require.NoError(t, err)
				assert.Contains(t, string(state), `"baseVersionId": "`+ver2ID+`"`)
				return
			}
			require.Equal(t, 0, push.ExitCode, push.Stderr)
			assert.JSONEq(t, `{"artifactId":"`+artID+`","versionId":"`+ver2ID+`","state":"building","previewReadyAt":"`+readyAt+`","evidenceStatus":"pending","url":"`+shellURL+`","appUrl":"`+appURL+`"}`, push.Stdout)
			assert.EqualValues(t, 2, previews.Load())
			assert.EqualValues(t, 1, versionReads.Load())
		})
	}
}

func TestDesignsFirstPushTitle(t *testing.T) {
	t.Run("blank override before opening project", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "missing")
		res := testutil.RunCommand(t, []string{"designs", "push", dir, "-m", "Publish", "--title", " \t\n "}, "")
		assert.Equal(t, 2, res.ExitCode, res.Stderr)
		assert.Contains(t, res.Stderr, "--title")
	})
	for _, test := range []struct {
		name, entrypoint, html, title string
		otherTitles                   []string
		savedTitle, override          string
		rejectOverride                bool
	}{
		{name: "manifest entrypoint", entrypoint: "pages/home.html", html: "<h1>Preview</h1><title>  Design &amp;\n   Review </title>", title: "Design & Review"},
		{name: "one page and title limit", html: "<title>é" + strings.Repeat("🌿", 120) + "</title>", title: "é" + strings.Repeat("🌿", 119)},
		{name: "folder fallback", html: "<template><title>Ignored</title></template><title> \n &nbsp; </title>", title: "Design Review"},
		{name: "shared leading title", html: "<title>Ridgeline — Explore</title>", otherTitles: []string{"Ridgeline — Trail"}, title: "Ridgeline"},
		{name: "shared trailing title", html: "<title>Explore | Ridgeline</title>", otherTitles: []string{"Log | Ridgeline"}, title: "Ridgeline"},
		{name: "no shared title", html: "<title>Ridgeline — Explore</title>", otherTitles: []string{"Other — Trails"}, title: "Ridgeline — Explore"},
		{name: "identical titles", html: "<title>Acme — Dashboard</title>", otherTitles: []string{"Acme — Dashboard"}, title: "Acme — Dashboard"},
		{name: "duplicate entry with related title", html: "<title>Acme — Dashboard</title>", otherTitles: []string{"Acme — Dashboard", "Acme — Settings"}, title: "Acme"},
		{name: "separator alone differs", html: "<title>Acme — Dashboard</title>", otherTitles: []string{"Acme | Dashboard"}, title: "Acme — Dashboard"},
		{name: "punctuation alone", html: "<title>✦ — Dashboard</title>", otherTitles: []string{"✦ — Settings"}, title: "✦ — Dashboard"},
		{name: "ignore unrelated title family", html: "<title>Acme | Home</title>", otherTitles: []string{"Acme | Blog", "Docs: Home", "Docs: Guide", "Docs: API"}, title: "Acme"},
		{name: "leading part wins tie", html: "<title>Acme | Home | 2026</title>", otherTitles: []string{"Acme | Blog | 2026"}, title: "Acme"},
		{name: "trailing part wins with more distinct titles", html: "<title>Acme | Home | 2026</title>", otherTitles: []string{"Acme | Blog | 2026", "Acme | Blog | 2026", "Docs | Guide | 2026"}, title: "2026"},
		{name: "entry part breaks tie", html: "<title>Front — Acme</title>", otherTitles: []string{"Docs — Acme", "Reports | Home", "Reports | Charts"}, title: "Acme"},
		{name: "saved init title", html: "<title>Page title</title>", savedTitle: "Selected title", title: "Selected title"},
		{name: "first push override", html: "<title>Page title</title>", savedTitle: "Saved title", override: " \t Override\nTitle  ", title: "Override Title"},
		{name: "blank push override", html: "<title>Page title</title>", override: " \t\n ", title: "Page title", rejectOverride: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "design-review")
			state, err := json.Marshal(map[string]string{"kind": "website", "runtime": "static_web", "entrypoint": test.entrypoint, "title": test.savedTitle})
			require.NoError(t, err)
			writePublishFixture(t, filepath.Join(dir, ".copera", "design.json"), state)
			entrypoint := test.entrypoint
			if entrypoint == "" {
				entrypoint = "index.html"
			} else {
				writePublishFixture(t, filepath.Join(dir, "index.html"), []byte("<title>Wrong entrypoint</title>"))
			}
			entryFile := filepath.Join(dir, entrypoint)
			writePublishFixture(t, entryFile, []byte(test.html))
			for i, title := range test.otherTitles {
				writePublishFixture(t, filepath.Join(dir, "other-"+strconv.Itoa(i)+".html"), []byte("<title>"+title+"</title>"))
			}

			const edited = "<title>Edited page title</title>"
			var creates, versions atomic.Int32
			srv := testutil.NewMockServer(t, testutil.MockRoutes{
				"GET /workspace/info": func(w http.ResponseWriter, _ *http.Request) {
					testutil.RespondJSON(w, http.StatusOK, api.Workspace{ID: designWorkspaceID})
				},
				"POST /artifacts/": func(w http.ResponseWriter, r *http.Request) {
					creates.Add(1)
					var input api.ArtifactCreateInput
					assert.NoError(t, json.Unmarshal([]byte(r.FormValue("project")), &input))
					assert.Equal(t, test.title, input.Title)
					testutil.RespondJSON(w, http.StatusOK, designPullFixture(entrypoint, test.html, 1))
				},
				"PATCH /artifacts/" + artID: func(w http.ResponseWriter, _ *http.Request) {
					t.Error("unexpected Design rename during push")
					w.WriteHeader(http.StatusBadRequest)
				},
				"POST /artifacts/" + artID + "/versions": func(w http.ResponseWriter, r *http.Request) {
					versions.Add(1)
					var input struct {
						Title json.RawMessage           `json:"title"`
						Files []api.ArtifactContentFile `json:"files"`
					}
					assert.NoError(t, json.NewDecoder(r.Body).Decode(&input))
					assert.Empty(t, input.Title)
					assert.Equal(t, []api.ArtifactContentFile{{Path: entrypoint, MediaType: "text/html", Content: edited}}, input.Files)
					version := designPullFixture(entrypoint, edited, 2).CurrentVersion
					testutil.RespondJSON(w, http.StatusOK, api.ArtifactPushResult{
						ArtifactID: artID, HeadRevision: 2, Version: version, BuildState: "ready",
					})
				},
			}.Handler())
			setupHome(t, srv.URL)
			args := []string{"designs", "push", dir, "-m", "First version", "--no-check", "--json"}
			if test.override != "" {
				args = append(args, "--title", test.override)
			}
			first := testutil.RunCommand(t, args, "")
			if test.rejectOverride {
				require.Equal(t, 2, first.ExitCode, first.Stderr)
				assert.Contains(t, first.Stderr, "--title")
				assert.Zero(t, creates.Load())
				return
			}
			require.Equal(t, 0, first.ExitCode, first.Stderr)
			rejected := testutil.RunCommand(t, []string{"designs", "push", dir, "-m", "Edit", "--title", "New name", "--no-check"}, "")
			assert.Equal(t, 2, rejected.ExitCode, rejected.Stderr)
			assert.Contains(t, rejected.Stderr, "copera designs rename")
			if test.entrypoint != "" {
				writePublishFixture(t, entryFile, []byte(edited))
				later := testutil.RunCommand(t, []string{"designs", "push", dir, "-m", "Edit page title", "--no-check", "--json"}, "")
				require.Equal(t, 0, later.ExitCode, later.Stderr)
				assert.EqualValues(t, 1, versions.Load())
			}
			assert.EqualValues(t, 1, creates.Load())
		})
	}
}

func TestDesignsInitTitleWhitespace(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /artifacts/guide": designGuideResponse,
		"GET /artifacts/design-systems/defaults": func(w http.ResponseWriter, _ *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, api.ArtifactSystemDefaults{})
		},
	}.Handler())
	setupHome(t, srv.URL)
	dir := t.TempDir()
	args := []string{"designs", "init", "--kind", "landing_page", "--no-system", "--json", "--title"}
	blank := testutil.RunCommand(t, append(args, " \t\n ", dir), "")
	assert.Equal(t, 2, blank.ExitCode, blank.Stderr)
	assert.Contains(t, blank.Stderr, "--title")
	assert.NoFileExists(t, filepath.Join(dir, ".copera", "design.json"))

	valid := testutil.RunCommand(t, append(args, " \t  A title\n with spaces \u00a0 ", t.TempDir()), "")
	require.Equal(t, 0, valid.ExitCode, valid.Stderr)
	assert.Contains(t, valid.Stdout, `"title": "A title with spaces"`)
}

func TestDesignsFirstPushWithImages(t *testing.T) {
	const (
		hero    = "assets/hero.png"
		logo    = "assets/logo.png"
		png     = "\x89PNG\r\n\x1a\n"
		html    = `<img src="assets/hero.png"><img src="assets/logo.png">`
		summary = "Add first screen"
	)
	dir := filepath.Join(t.TempDir(), "draft")
	added := []string{"SPEC.md", "shared/DESIGN.md", "shared/assets.json", "shared/components.json"}
	for i := range 17 {
		added = append(added, fmt.Sprintf("shared/generated-%02d.md", i))
	}
	var creates, followups atomic.Int32
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /artifacts/guide": designGuideResponse,
		"GET /workspace/info": func(w http.ResponseWriter, _ *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, api.Workspace{ID: designWorkspaceID})
		},
		"POST /artifacts/check": func(w http.ResponseWriter, r *http.Request) {
			assert.Contains(t, r.Header.Get("Content-Type"), "multipart/form-data")
			testutil.RespondJSON(w, http.StatusOK, map[string]bool{"ok": true})
		},
		"POST /artifacts/": func(w http.ResponseWriter, r *http.Request) {
			creates.Add(1)
			reader, err := r.MultipartReader()
			if !assert.NoError(t, err) {
				w.WriteHeader(http.StatusUnsupportedMediaType)
				return
			}
			project, err := reader.NextPart()
			if !assert.NoError(t, err) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			assert.Equal(t, "project", project.FormName())
			assert.Equal(t, "application/json", project.Header.Get("Content-Type"))
			var input api.ArtifactCreateInput
			assert.NoError(t, json.NewDecoder(project).Decode(&input))
			assert.Equal(t, summary, input.ChangeSummary)
			assert.NotEmpty(t, input.IdempotencyKey)
			assert.Equal(t, []api.ArtifactContentFile{
				{Path: "index.html", MediaType: "text/html", Content: html},
				{Path: "style.css", MediaType: "text/css", Content: "img { width: 100%; }"},
			}, input.Project.Files)
			created := designPullFixture("index.html", html, 1)
			created.CurrentVersion.Files = append(created.CurrentVersion.Files, api.ArtifactFileRef{Path: "style.css"})
			for i := len(added) - 1; i >= 0; i-- {
				created.CurrentVersion.Files = append(created.CurrentVersion.Files, api.ArtifactFileRef{Path: added[i]})
			}
			for name, content := range map[string]string{hero: png + "hero", logo: png + "logo"} {
				created.CurrentVersion.AuthoringManifest.Assets = append(created.CurrentVersion.AuthoringManifest.Assets,
					api.ArtifactAsset{ID: digestOf(name)[:24], Path: name, MediaType: "image/png", ContentDigest: digestOf(content)})
			}
			received := make(map[string]string)
			for range 2 {
				file, err := reader.NextPart()
				if !assert.NoError(t, err) {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				_, params, err := mime.ParseMediaType(file.Header.Get("Content-Disposition"))
				assert.NoError(t, err)
				assert.Equal(t, "file", file.FormName())
				assert.Equal(t, "image/png", file.Header.Get("Content-Type"))
				data, err := io.ReadAll(file)
				assert.NoError(t, err)
				received[params["filename"]] = string(data)
			}
			assert.Equal(t, map[string]string{hero: png + "hero", logo: png + "logo"}, received)
			_, err = reader.NextPart()
			assert.ErrorIs(t, err, io.EOF)
			testutil.RespondJSON(w, http.StatusOK, created)
		},
		"POST /artifacts/" + artID + "/assets": func(w http.ResponseWriter, _ *http.Request) {
			followups.Add(1)
			w.WriteHeader(http.StatusBadRequest)
		},
		"POST /artifacts/" + artID + "/versions": func(w http.ResponseWriter, _ *http.Request) {
			followups.Add(1)
			w.WriteHeader(http.StatusBadRequest)
		},
	}.Handler())
	setupHome(t, srv.URL)
	init := testutil.RunCommand(t, []string{"designs", "init", dir, "--kind", "website"}, "")
	require.Equal(t, 0, init.ExitCode, init.Stderr)
	writePublishFixture(t, filepath.Join(dir, "index.html"), []byte(html))
	writePublishFixture(t, filepath.Join(dir, "style.css"), []byte("img { width: 100%; }"))
	writePublishFixture(t, filepath.Join(dir, hero), []byte(png+"hero"))
	writePublishFixture(t, filepath.Join(dir, logo), []byte(png+"logo"))

	res := testutil.RunCommand(t, []string{"designs", "push", dir, "-m", summary, "--json"}, "")
	require.Equal(t, 0, res.ExitCode, res.Stderr)
	assert.EqualValues(t, 1, creates.Load())
	assert.Zero(t, followups.Load())
	var result struct {
		api.ArtifactPushResult
		ServerAddedFiles        []string
		ServerAddedFilesOmitted int
	}
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &result))
	assert.Equal(t, 1, result.Version.Version)
	assert.Len(t, result.Version.AuthoringManifest.Assets, 2)
	assert.Equal(t, added[:20], result.ServerAddedFiles)
	assert.Equal(t, 1, result.ServerAddedFilesOmitted)
	var state struct {
		ID    string            `json:"id"`
		Base  string            `json:"baseVersionId"`
		Files map[string]string `json:"files"`
	}
	require.NoError(t, json.Unmarshal([]byte(readFile(t, filepath.Join(dir, ".copera", "design.json"))), &state))
	assert.Equal(t, artID, state.ID)
	assert.Equal(t, verID, state.Base)
	assert.Equal(t, digestOf(png+"hero"), state.Files[hero])
	assert.Equal(t, digestOf(png+"logo"), state.Files[logo])
}

func TestDesignsForcePushStockReviewWithMissingIndexes(t *testing.T) {
	const (
		entrypoint = "index.html"
		html       = "<h1>Review</h1>"
		guidance   = "shared/DESIGN.md"
		tokens     = "shared/tokens.css"
		components = "shared/components.json"
		assets     = "shared/assets.json"
	)
	remote := designPullFixture(entrypoint, html, 1)
	remote.CurrentVersion.AuthoringManifest.SharedDesign = api.ArtifactSharedDesign{
		Version: 1, GuidancePath: guidance, TokensPath: tokens, ComponentsPath: components, AssetsPath: assets,
	}
	remote.CurrentVersion.Files = append(remote.CurrentVersion.Files,
		api.ArtifactFileRef{Path: guidance, MediaType: "text/markdown", ContentDigest: digestOf("# Review")},
		api.ArtifactFileRef{Path: tokens, MediaType: "text/css", ContentDigest: digestOf("body {}")},
		api.ArtifactFileRef{Path: components, MediaType: "application/json", ContentDigest: digestOf(`{"schemaVersion":1,"components":[]}`)},
		api.ArtifactFileRef{Path: assets, MediaType: "application/json", ContentDigest: digestOf(`{"schemaVersion":1,"assets":[]}`)},
	)
	const guide = `{"version":1,"kind":"web_app","runtime":"static_web","markdown":"# Review","facts":{},"template":{
		"id":"review","subject":"Review","project":{"artifactKind":"web_app","runtime":"static_web","entrypoint":"index.html","capabilities":` + reviewDeclarations + `},
		"files":[{"path":"index.html","mediaType":"text/html","content":"<h1>Review</h1>"},
			{"path":"shared/DESIGN.md","mediaType":"text/markdown","content":"# Review"},
			{"path":"shared/tokens.css","mediaType":"text/css","content":"body {}"}]}}`
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /artifacts/guide": func(w http.ResponseWriter, _ *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, json.RawMessage(guide))
		},
		"GET /workspace/info": func(w http.ResponseWriter, _ *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, api.Workspace{ID: designWorkspaceID})
		},
		"POST /artifacts/": func(w http.ResponseWriter, _ *http.Request) {
			testutil.RespondJSON(w, http.StatusCreated, remote)
		},
		"GET /artifacts/" + artID: func(w http.ResponseWriter, _ *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, remote)
		},
		"POST /artifacts/" + artID + "/versions": func(w http.ResponseWriter, r *http.Request) {
			var input api.ArtifactPushInput
			if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&input)) || !assert.NotNil(t, input.AuthoringManifest) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			assert.ElementsMatch(t, []string{components, assets}, input.RemovedPaths)
			if !assert.Equal(t, api.ArtifactSharedDesign{Version: 1, GuidancePath: guidance, TokensPath: tokens}, input.AuthoringManifest.SharedDesign) {
				testutil.RespondJSON(w, http.StatusBadRequest, map[string]string{"message": "manifest references missing shared indexes"})
				return
			}
			version := remote.CurrentVersion
			version.ID, version.Version, version.AuthoringManifest = ver2ID, 2, *input.AuthoringManifest
			testutil.RespondJSON(w, http.StatusCreated, api.ArtifactPushResult{
				Outcome: "created", ArtifactID: artID, HeadRevision: 2, Version: version,
			})
		},
	}.Handler())
	setupHome(t, srv.URL)

	dir := filepath.Join(t.TempDir(), "review")
	init := testutil.RunCommand(t, []string{"designs", "init", dir, "--template", "review", "--json"}, "")
	require.Equal(t, 0, init.ExitCode, init.Stderr)
	create := testutil.RunCommand(t, []string{"designs", "push", dir, "-m", "First version", "--no-check", "--json"}, "")
	require.Equal(t, 0, create.ExitCode, create.Stderr)
	forced := testutil.RunCommand(t, []string{"designs", "push", dir, "-m", "Update review", "--force", "--no-check", "--json"}, "")
	require.Equal(t, 0, forced.ExitCode, forced.Stderr)
}

func TestDesignsFirstPushRejected(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "draft")
	var creates atomic.Int32
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /artifacts/guide": designGuideResponse,
		"GET /workspace/info": func(w http.ResponseWriter, _ *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, api.Workspace{ID: designWorkspaceID})
		},
		"POST /artifacts/": func(w http.ResponseWriter, r *http.Request) {
			creates.Add(1)
			assert.Contains(t, r.Header.Get("Content-Type"), "multipart/form-data")
			testutil.RespondJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid source"})
		},
	}.Handler())
	setupHome(t, srv.URL)
	init := testutil.RunCommand(t, []string{"designs", "init", dir, "--kind", "website"}, "")
	require.Equal(t, 0, init.ExitCode, init.Stderr)
	writePublishFixture(t, filepath.Join(dir, "index.html"), []byte("<p>draft</p>"))
	stateFile := filepath.Join(dir, ".copera", "design.json")
	before := readFile(t, stateFile)

	res := testutil.RunCommand(t, []string{"designs", "push", dir, "-m", "Add first screen", "--no-check", "--json"}, "")
	require.Equal(t, 1, res.ExitCode, res.Stderr)
	assert.EqualValues(t, 1, creates.Load())
	assert.Contains(t, res.Stderr, "No Design was created")
	assert.Contains(t, res.Stderr, "invalid source")
	assert.Equal(t, before, readFile(t, stateFile))
}

func TestDesignsPushWriterBusyWithoutWait(t *testing.T) {
	dir := designPublishFolder(t)
	var attempts atomic.Int32
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"POST /artifacts/" + artID + "/versions": func(w http.ResponseWriter, r *http.Request) {
			attempts.Add(1)
			testutil.RespondJSON(w, http.StatusConflict, struct {
				Message           string `json:"message"`
				Reason            string `json:"reason"`
				RetryAfterSeconds int    `json:"retryAfterSeconds"`
			}{"Copera AI is editing this Design", "writer_busy", 17})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"designs", "push", dir, "-m", "Edit", "--no-check", "--json"}, "")
	assert.Equal(t, 5, res.ExitCode, res.Stderr)
	assert.Contains(t, res.Stderr, "17 seconds")
	assert.Empty(t, res.Stdout)
	assert.EqualValues(t, 1, attempts.Load())
}

func TestDesignsPushReplacesImageInPlace(t *testing.T) {
	testDesignsPushReplacesImageInPlace(t, "assets/logo.png", "assets/brand/logo.png", map[string]string{"assets/logo.png": "assets/brand/logo.png"})
}

func TestDesignsPushReplacesGeneratedAssetInPlace(t *testing.T) {
	const assetPath = "design/assets/dddddddddddddddddddddddd.png"
	testDesignsPushReplacesImageInPlace(t, assetPath, assetPath, nil)
}

func testDesignsPushReplacesImageInPlace(t *testing.T, localPath, assetPath string, aliases map[string]string) {
	t.Helper()
	const png = "\x89PNG\r\n\x1a\n"
	dir := t.TempDir()
	oldAsset := api.ArtifactAsset{
		ID: "dddddddddddddddddddddddd", Path: assetPath, ContentDigest: digestOf(png + "old"),
		MediaType: "image/png", Usage: "logo", AltText: "Company logo",
	}
	replacement := oldAsset
	replacement.ID = "eeeeeeeeeeeeeeeeeeeeeeee"
	replacement.ContentDigest = digestOf(png + "new")
	stateFile := filepath.Join(dir, ".copera", "design.json")
	state, err := json.Marshal(struct {
		ID            string               `json:"id"`
		BaseVersionID string               `json:"baseVersionId"`
		HeadRevision  int                  `json:"headRevision"`
		Files         map[string]string    `json:"files"`
		AssetPaths    map[string]string    `json:"assetPaths"`
		Manifest      api.ArtifactManifest `json:"manifest"`
	}{
		ID: artID, BaseVersionID: verID, HeadRevision: 1,
		Files:      map[string]string{"index.html": digestOf("<p>Home</p>"), localPath: oldAsset.ContentDigest},
		AssetPaths: aliases,
		Manifest:   api.ArtifactManifest{Assets: []api.ArtifactAsset{oldAsset}},
	})
	require.NoError(t, err)
	writePublishFixture(t, stateFile, state)
	writePublishFixture(t, filepath.Join(dir, "index.html"), []byte("<p>Home</p>"))
	writePublishFixture(t, filepath.Join(dir, localPath), []byte(png+"new"))

	var uploads, pushes atomic.Int32
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"POST /artifacts/check": func(w http.ResponseWriter, r *http.Request) {
			reader, err := r.MultipartReader()
			if !assert.NoError(t, err) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			project, err := reader.NextPart()
			if !assert.NoError(t, err) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			var input api.ArtifactCheckInput
			assert.NoError(t, json.NewDecoder(project).Decode(&input))
			if assert.NotNil(t, input.AuthoringManifest) {
				assert.Empty(t, input.AuthoringManifest.Assets)
			}
			file, err := reader.NextPart()
			if !assert.NoError(t, err) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_, params, err := mime.ParseMediaType(file.Header.Get("Content-Disposition"))
			assert.NoError(t, err)
			assert.Equal(t, assetPath, params["filename"])
			testutil.RespondJSON(w, http.StatusOK, map[string]bool{"ok": true})
		},
		"POST /artifacts/" + artID + "/assets": func(w http.ResponseWriter, r *http.Request) {
			uploads.Add(1)
			metadata := assertDesignUpload(t, r, filepath.Base(assetPath), png+"new")
			if !assert.Equal(t, assetPath, metadata["path"]) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			assert.Equal(t, replacement.ContentDigest, metadata["expectedContentDigest"])
			assert.Equal(t, oldAsset.Usage, metadata["usage"])
			assert.Equal(t, oldAsset.AltText, metadata["altText"])
			testutil.RespondJSON(w, http.StatusOK, replacement)
		},
		"POST /artifacts/" + artID + "/versions": func(w http.ResponseWriter, r *http.Request) {
			pushes.Add(1)
			assert.EqualValues(t, 1, uploads.Load())
			var input api.ArtifactPushInput
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&input))
			assert.Equal(t, verID, input.BaseVersionID)
			assert.Empty(t, input.RemovedPaths)
			if assert.NotNil(t, input.AuthoringManifest) {
				assert.Equal(t, []api.ArtifactAsset{replacement}, input.AuthoringManifest.Assets)
			}
			testutil.RespondJSON(w, http.StatusCreated, api.ArtifactPushResult{
				Outcome: "created", ArtifactID: artID, HeadRevision: 2, BuildState: "queued",
				Version: designPullFixture("index.html", "<p>Home</p>", 2).CurrentVersion,
			})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"designs", "push", dir, "-m", "Refresh logo", "--json"}, "")
	require.Equal(t, 0, res.ExitCode, res.Stderr)
	assert.EqualValues(t, 1, uploads.Load())
	assert.EqualValues(t, 1, pushes.Load())
	var saved struct {
		Files      map[string]string    `json:"files"`
		AssetPaths map[string]string    `json:"assetPaths"`
		Manifest   api.ArtifactManifest `json:"manifest"`
	}
	require.NoError(t, json.Unmarshal([]byte(readFile(t, stateFile)), &saved))
	assert.Equal(t, replacement.ContentDigest, saved.Files[localPath])
	assert.Equal(t, aliases, saved.AssetPaths)
	assert.Equal(t, []api.ArtifactAsset{replacement}, saved.Manifest.Assets)
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

func TestDesignsVendoredFontSources(t *testing.T) {
	const (
		fontDir = "vendor/fonts/49753cca61a7e1a8/"
		cssPath = fontDir + "fonts.css"
		css     = `@font-face{font-family:Inter;src:url(regular.woff2)}@font-face{font-family:Inter;src:url(bold.woff2);font-weight:700}`
		jsPath  = "vendor/npm/chart/chart.js"
		js      = "const chart = true;"
		html    = `<link rel="stylesheet" href="` + cssPath + `">`
	)
	fontData := map[string]string{fontDir + "regular.woff2": "wOF2\x00regular", fontDir + "bold.woff2": "wOF2\x00bold"}
	vendorSources := []api.ArtifactContentFile{
		{Path: cssPath, MediaType: "text/css", Content: css},
		{Path: jsPath, MediaType: "text/javascript", Content: js},
	}
	allSources := append([]api.ArtifactContentFile{{Path: "index.html", MediaType: "text/html", Content: html}}, vendorSources...)
	created := designPullFixture("index.html", html, 1)
	for name, content := range fontData {
		created.CurrentVersion.AuthoringManifest.Assets = append(created.CurrentVersion.AuthoringManifest.Assets,
			api.ArtifactAsset{ID: digestOf(name)[:24], Path: name, MediaType: "font/woff2", ContentDigest: digestOf(content)})
	}
	readMultipart := func(r *http.Request) json.RawMessage {
		reader, err := r.MultipartReader()
		if !assert.NoError(t, err) {
			return nil
		}
		project, err := reader.NextPart()
		if !assert.NoError(t, err) {
			return nil
		}
		assert.Equal(t, "project", project.FormName())
		var payload json.RawMessage
		assert.NoError(t, json.NewDecoder(project).Decode(&payload))
		received := make(map[string]string)
		for range fontData {
			part, err := reader.NextPart()
			if !assert.NoError(t, err) {
				return nil
			}
			_, params, err := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
			assert.NoError(t, err)
			assert.Equal(t, "file", part.FormName())
			assert.Equal(t, "font/woff2", part.Header.Get("Content-Type"))
			data, err := io.ReadAll(part)
			assert.NoError(t, err)
			received[params["filename"]] = string(data)
		}
		assert.Equal(t, fontData, received)
		_, err = reader.NextPart()
		assert.ErrorIs(t, err, io.EOF)
		return payload
	}
	later := designPullFixture("index.html", html, 2)
	later.CurrentVersion.AuthoringManifest = created.CurrentVersion.AuthoringManifest
	const versionsPath = "/artifacts/" + artID + "/versions"
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"POST /artifacts/check": func(w http.ResponseWriter, r *http.Request) {
			var input api.ArtifactCheckInput
			assert.NoError(t, json.Unmarshal(readMultipart(r), &input))
			assert.Equal(t, allSources, input.Files)
			testutil.RespondJSON(w, http.StatusOK, map[string]bool{"ok": true})
		},
		"GET /workspace/info": func(w http.ResponseWriter, _ *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, api.Workspace{ID: designWorkspaceID})
		},
		"POST /artifacts/": func(w http.ResponseWriter, r *http.Request) {
			var input api.ArtifactCreateInput
			assert.NoError(t, json.Unmarshal(readMultipart(r), &input))
			assert.Equal(t, allSources, input.Project.Files)
			testutil.RespondJSON(w, http.StatusOK, created)
		},
		"POST /artifacts/" + artID + "/assets": func(w http.ResponseWriter, _ *http.Request) {
			assert.Fail(t, "push should use the existing font pins without uploading sources")
			w.WriteHeader(http.StatusBadRequest)
		},
		"POST " + versionsPath: func(w http.ResponseWriter, r *http.Request) {
			var input api.ArtifactPushInput
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&input))
			assert.Equal(t, vendorSources, input.Files)
			assert.Nil(t, input.AuthoringManifest, "the existing font pins should be retained")
			testutil.RespondJSON(w, http.StatusCreated, api.ArtifactPushResult{
				Outcome: "created", ArtifactID: artID, HeadRevision: 2, Version: later.CurrentVersion,
			})
		},
		"GET " + versionsPath + "/" + ver2ID: func(w http.ResponseWriter, _ *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, later.CurrentVersion)
		},
	}.Handler())
	setupHome(t, srv.URL)
	dir := t.TempDir()
	statePath := filepath.Join(dir, ".copera", "design.json")
	writePublishFixture(t, statePath, []byte(`{"kind":"web_app","runtime":"static_web","entrypoint":"index.html"}`))
	for _, file := range allSources {
		writePublishFixture(t, filepath.Join(dir, file.Path), []byte(file.Content))
	}
	for name, content := range fontData {
		writePublishFixture(t, filepath.Join(dir, name), []byte(content))
	}
	check := testutil.RunCommand(t, []string{"designs", "check", dir, "--json"}, "")
	require.Zero(t, check.ExitCode, check.Stderr)
	first := testutil.RunCommand(t, []string{"designs", "push", dir, "-m", "Create", "--no-check", "--json"}, "")
	require.Zero(t, first.ExitCode, first.Stderr)

	// Model an existing Design with font pins and newly added vendor sources.
	baseFiles := map[string]string{"index.html": digestOf(html)}
	for _, asset := range created.CurrentVersion.AuthoringManifest.Assets {
		baseFiles[asset.Path] = asset.ContentDigest
	}
	state, err := json.Marshal(struct {
		ID            string               `json:"id"`
		BaseVersionID string               `json:"baseVersionId"`
		HeadRevision  int                  `json:"headRevision"`
		Files         map[string]string    `json:"files"`
		Manifest      api.ArtifactManifest `json:"manifest"`
	}{ID: artID, BaseVersionID: verID, HeadRevision: 1, Files: baseFiles, Manifest: created.CurrentVersion.AuthoringManifest})
	require.NoError(t, err)
	writePublishFixture(t, statePath, state)
	push := testutil.RunCommand(t, []string{"designs", "push", dir, "-m", "Add sources", "--no-check", "--json"}, "")
	require.Zero(t, push.ExitCode, push.Stderr)
}

func TestDesignsCheckClassifiesVendorAndMedia(t *testing.T) {
	dir := t.TempDir()
	const tracked = "vendor/authored.js"
	state := struct {
		Kind    string            `json:"kind"`
		Runtime string            `json:"runtime"`
		Files   map[string]string `json:"files"`
	}{"website", "static_web", map[string]string{tracked: digestOf("old")}}
	data, err := json.Marshal(state)
	require.NoError(t, err)
	writePublishFixture(t, filepath.Join(dir, ".copera", "design.json"), data)
	writePublishFixture(t, filepath.Join(dir, tracked), []byte("const authored = true;"))

	vendor := "/*" + strings.Repeat("x", 1<<20) + "*/"
	media := "\x00\x00\x00\x18ftypmp42" + strings.Repeat("\x00", 5<<20)
	const binaryCSSPath, binaryCSS = "vendor/binary.css", "\x00\xff"
	const vendorPath, mediaPath = "vendor/npm/chart/1.2.3/chart.JS", "movie.MP4"
	writePublishFixture(t, filepath.Join(dir, binaryCSSPath), []byte(binaryCSS))
	writePublishFixture(t, filepath.Join(dir, vendorPath), []byte(vendor))
	writePublishFixture(t, filepath.Join(dir, mediaPath), []byte(media))

	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"POST /artifacts/check": func(w http.ResponseWriter, r *http.Request) {
			reader, err := r.MultipartReader()
			if !assert.NoError(t, err) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			project, err := reader.NextPart()
			if !assert.NoError(t, err) {
				return
			}
			var input api.ArtifactCheckInput
			assert.NoError(t, json.NewDecoder(project).Decode(&input))
			assert.Equal(t, []api.ArtifactContentFile{{Path: tracked, MediaType: "text/javascript", Content: "const authored = true;"}}, input.Files)
			for _, file := range []struct{ path, mime, digest string }{
				{mediaPath, "video/mp4", digestOf(media)},
				{binaryCSSPath, "text/css", digestOf(binaryCSS)},
				{vendorPath, "application/javascript", digestOf(vendor)},
			} {
				part, err := reader.NextPart()
				if !assert.NoError(t, err) {
					return
				}
				_, params, err := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
				assert.NoError(t, err)
				assert.Equal(t, file.path, params["filename"])
				assert.Equal(t, file.mime, part.Header.Get("Content-Type"))
				hash := sha256.New()
				_, err = io.Copy(hash, part)
				assert.NoError(t, err)
				assert.Equal(t, file.digest, hex.EncodeToString(hash.Sum(nil)))
			}
			_, err = reader.NextPart()
			assert.ErrorIs(t, err, io.EOF)
			testutil.RespondJSON(w, http.StatusOK, map[string]bool{"ok": true})
		},
	}.Handler())
	setupHome(t, srv.URL)
	result := testutil.RunCommand(t, []string{"designs", "check", dir, "--json"}, "")
	require.Equal(t, 0, result.ExitCode, result.Stderr)
}
