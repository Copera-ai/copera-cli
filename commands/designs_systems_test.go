package commands_test

import (
	"archive/zip"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/copera/copera-cli/internal/api"
	"github.com/copera/copera-cli/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const systemsPath = "/artifacts/design-systems"

func TestDesignSystemFolderPushChecksRevisionAndRetainsEdits(t *testing.T) {
	var writes atomic.Int32
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /workspace/info": func(w http.ResponseWriter, _ *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, api.Workspace{ID: designWorkspaceID})
		},
		"POST " + systemsPath: func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Name           string          `json:"name"`
				IdempotencyKey string          `json:"idempotencyKey"`
				Format         string          `json:"format"`
				Document       json.RawMessage `json:"document"`
			}
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			assert.Equal(t, "Acme", body.Name)
			assert.NotEmpty(t, body.IdempotencyKey)
			assert.Equal(t, "package", body.Format)
			assert.Contains(t, string(body.Document), `"schemaVersion":1,"kit":null`)
			digest := testSystemDigest(t, []api.ArtifactSystemFile{
				{Path: "DESIGN.md", MediaType: "text/markdown", ContentBase64: base64.StdEncoding.EncodeToString([]byte("# Original\n"))},
				{Path: "tokens.css", MediaType: "text/css", ContentBase64: base64.StdEncoding.EncodeToString([]byte(":root{}"))},
				{Path: "tokens.json", MediaType: "application/json", ContentBase64: base64.StdEncoding.EncodeToString([]byte(`{"schemaVersion":1,"light":{},"dark":{}}`))},
			})
			testutil.RespondJSON(w, http.StatusCreated, map[string]any{"id": verID, "name": "Acme", "revision": 1, "available": true, "packageDigest": digest})
		},
		"PATCH " + systemsPath + "/" + verID: func(w http.ResponseWriter, r *http.Request) {
			writes.Add(1)
			var body struct {
				ExpectedRevision int `json:"expectedRevision"`
				Package          struct {
					Files []struct{ Path, ContentBase64 string }
				} `json:"package"`
			}
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			assert.Equal(t, 1, body.ExpectedRevision)
			if assert.NotEmpty(t, body.Package.Files) {
				assert.Equal(t, "DESIGN.md", body.Package.Files[0].Path)
				assert.Equal(t, base64.StdEncoding.EncodeToString([]byte("# Revised\n")), body.Package.Files[0].ContentBase64)
			}
			testutil.RespondJSON(w, http.StatusConflict, map[string]any{"message": "System changed", "info": map[string]any{"code": "ARTIFACT_DESIGN_SYSTEM_CHANGED", "currentRevision": 2}})
		},
	}.Handler())
	setupHome(t, srv.URL)
	dir := t.TempDir()
	for name, content := range map[string]string{
		"DESIGN.md":   "# Original\n",
		"tokens.json": `{"schemaVersion":1,"light":{},"dark":{}}`,
		"tokens.css":  ":root{}",
	} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
	}

	result := testutil.RunCommand(t, []string{"artifacts", "systems", "push", dir, "--name", "Acme", "--json"}, "")
	require.Equal(t, 0, result.ExitCode, result.Stderr)
	metadataPath := filepath.Join(dir, ".copera", "design-system.json")
	oldMetadata := readFile(t, metadataPath)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "DESIGN.md"), []byte("# Revised\n"), 0o600))
	result = testutil.RunCommand(t, []string{"designs", "systems", "push", dir, "--json"}, "")
	assert.Equal(t, 5, result.ExitCode, result.Stderr)
	assert.Contains(t, result.Stderr, "ARTIFACT_DESIGN_SYSTEM_CHANGED")
	assert.Contains(t, result.Stderr, "2")
	assert.Equal(t, int32(1), writes.Load())
	assert.Equal(t, oldMetadata, readFile(t, metadataPath))
	assert.Equal(t, "# Revised\n", readFile(t, filepath.Join(dir, "DESIGN.md")))
}

func TestDesignSystemImportRefusesArchivePathBeforeRequest(t *testing.T) {
	var requests atomic.Int32
	srv := testutil.NewMockServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.NotFound(w, r)
	}))
	setupHome(t, srv.URL)
	filename := filepath.Join(t.TempDir(), "system.zip")
	file, err := os.Create(filename)
	require.NoError(t, err)
	archive := zip.NewWriter(file)
	member, err := archive.Create("../outside.md")
	require.NoError(t, err)
	_, err = member.Write([]byte("unsafe"))
	require.NoError(t, err)
	require.NoError(t, archive.Close())
	require.NoError(t, file.Close())

	result := testutil.RunCommand(t, []string{"designs", "systems", "import", filename, "--name", "Bad", "--json"}, "")
	assert.Equal(t, 2, result.ExitCode, result.Stderr)
	assert.Contains(t, result.Stderr, "../outside.md")
	assert.Zero(t, requests.Load())
}

func TestDesignSystemCodebaseGuideAndDecline(t *testing.T) {
	var defaultReads atomic.Int32
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /artifacts/guide": func(w http.ResponseWriter, r *http.Request) {
			topic := r.URL.Query().Get("topic")
			if topic != "" {
				assert.Equal(t, "design-system-from-codebase", topic)
				assert.Empty(t, r.URL.Query().Get("kind"))
				testutil.RespondJSON(w, http.StatusOK, json.RawMessage(`{"version":1,"kind":null,"runtime":null,"markdown":"# Derive a Design system from a codebase","facts":{}}`))
				return
			}
			designGuideResponse(w, r)
		},
		"GET " + systemsPath + "/defaults": func(w http.ResponseWriter, r *http.Request) {
			locked := defaultReads.Add(1) > 1
			testutil.RespondJSON(w, http.StatusOK, map[string]any{"workspaceSystemId": verID, "locked": locked})
		},
	}.Handler())
	setupHome(t, srv.URL)
	result := testutil.RunCommand(t, []string{"designs", "guide", "--topic", "design-system-from-codebase", "--json"}, "")
	require.Equal(t, 0, result.ExitCode, result.Stderr)
	assert.Contains(t, result.Stdout, `"kind": null`)

	dir := filepath.Join(t.TempDir(), "declined")
	args := []string{"designs", "init", dir, "--kind", "website", "--no-system", "--json"}
	result = testutil.RunCommand(t, args, "")
	require.Equal(t, 0, result.ExitCode, result.Stderr)
	assert.Contains(t, result.Stdout, `"noDesignSystem": true`)
	args[2] = filepath.Join(t.TempDir(), "locked")
	result = testutil.RunCommand(t, args, "")
	assert.Equal(t, 5, result.ExitCode, result.Stderr)
	assert.Contains(t, result.Stderr, "workspace")
	_, err := os.Stat(args[2])
	assert.ErrorIs(t, err, os.ErrNotExist)
}

const testBrandPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+j4ycAAAAASUVORK5CYII="

func systemTestProjection() []api.ArtifactSystemFile {
	return []api.ArtifactSystemFile{
		{Path: "shared/brand/DESIGN.md", MediaType: "text/markdown", ContentBase64: base64.StdEncoding.EncodeToString([]byte("# Brand\n"))},
		{Path: "shared/brand/tokens.css", MediaType: "text/css", ContentBase64: base64.StdEncoding.EncodeToString([]byte(":root{}"))},
		{Path: "shared/brand/tokens.json", MediaType: "application/json", ContentBase64: base64.StdEncoding.EncodeToString([]byte(`{"schemaVersion":1,"light":{},"dark":{}}`))},
		{Path: "shared/brand/assets/logo.png", MediaType: "image/png", ContentBase64: testBrandPNG, Usage: "logo"},
	}
}

func TestDesignSystemInitPinsDefaultAndPushOmitsExactInstall(t *testing.T) {
	var creates, checks atomic.Int32
	const template = `<!doctype html><html><head><link href="base.css" rel="stylesheet"></head><body>Sample</body></html>`
	pin := api.ArtifactSystemInstall{SystemID: verID, Revision: 2, PackageDigest: digestOf("brand"), Kit: "none"}
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /artifacts/guide": func(w http.ResponseWriter, _ *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, api.ArtifactGuide{
				Version: 1, Kind: "website", Runtime: "static_web", Facts: json.RawMessage(`{}`),
				Template: &api.ArtifactGuideTemplate{
					Project: api.ArtifactProjectSettings{Kind: "website", Runtime: "static_web", Entrypoint: "index.html"},
					Files: []api.ArtifactContentFile{
						{Path: "index.html", MediaType: "text/html", Content: template},
					},
				},
			})
		},
		"GET " + systemsPath + "/defaults": func(w http.ResponseWriter, _ *http.Request) {
			personal, workspace := verID, ver2ID
			testutil.RespondJSON(w, http.StatusOK, api.ArtifactSystemDefaults{PersonalSystemID: &personal, WorkspaceSystemID: &workspace})
		},
		"GET " + systemsPath + "/" + verID: func(w http.ResponseWriter, _ *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, api.ArtifactDesignSystem{ID: verID, Revision: 2, PackageDigest: &pin.PackageDigest, Available: true})
		},
		"GET " + systemsPath + "/" + verID + "/installation": func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "expectedRevision=2&kit=none&runtime=static_web", r.URL.RawQuery)
			testutil.RespondJSON(w, http.StatusOK, api.ArtifactSystemProjection{Install: pin, Files: systemTestProjection()})
		},
		"GET /workspace/info": func(w http.ResponseWriter, _ *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, api.Workspace{ID: designWorkspaceID})
		},
		"POST /artifacts/check": func(w http.ResponseWriter, r *http.Request) {
			checks.Add(1)
			reader, err := r.MultipartReader()
			if !assert.NoError(t, err) {
				return
			}
			project, err := reader.NextPart()
			if !assert.NoError(t, err) {
				return
			}
			var request api.ArtifactCheckInput
			assert.NoError(t, json.NewDecoder(project).Decode(&request))
			assert.True(t, slices.ContainsFunc(request.Files, func(file api.ArtifactContentFile) bool {
				return file.Path == "shared/brand/DESIGN.md" && file.Content == "# Brand\n"
			}))
			logo, err := reader.NextPart()
			if assert.NoError(t, err) {
				data, err := io.ReadAll(logo)
				assert.NoError(t, err)
				assert.Equal(t, testBrandPNG, base64.StdEncoding.EncodeToString(data))
			}
			testutil.RespondJSON(w, http.StatusOK, api.ArtifactCheckResult{OK: true})
		},
		"POST /artifacts/": func(w http.ResponseWriter, r *http.Request) {
			creates.Add(1)
			reader, err := r.MultipartReader()
			if !assert.NoError(t, err) {
				return
			}
			project, err := reader.NextPart()
			if !assert.NoError(t, err) {
				return
			}
			var request api.ArtifactCreateInput
			assert.NoError(t, json.NewDecoder(project).Decode(&request))
			assert.Equal(t, pin.SystemID, request.DesignSystemID)
			assert.Equal(t, pin.Revision, request.DesignSystemRevision)
			assert.Equal(t, pin.Kit, request.DesignSystemKit)
			assert.NotEmpty(t, request.IdempotencyKey)
			if assert.Len(t, request.Project.Files, 1) {
				html := request.Project.Files[0]
				assert.Equal(t, "index.html", html.Path)
				assert.Less(t, strings.Index(html.Content, "base.css"), strings.Index(html.Content, "shared/brand/tokens.css"))
			}
			_, err = reader.NextPart()
			assert.ErrorIs(t, err, io.EOF)
			testutil.RespondJSON(w, http.StatusOK, api.ArtifactGetResult{
				Artifact: designPullFixture("index.html", template, 1), DesignSystemInstallation: &pin,
			})
		},
	}.Handler())
	setupHome(t, srv.URL)
	dir := filepath.Join(t.TempDir(), "my-design")
	result := testutil.RunCommand(t, []string{"designs", "init", dir, "--kind", "website", "--json"}, "")
	require.Equal(t, 0, result.ExitCode, result.Stderr)
	assert.Contains(t, result.Stdout, `"designSystemRevision": 2`)
	assert.Equal(t, "# Brand\n", readFile(t, filepath.Join(dir, "shared", "brand", "DESIGN.md")))

	changedPath := filepath.Join(dir, "shared", "brand", "tokens.css")
	require.NoError(t, os.WriteFile(changedPath, []byte(":root{color:red}"), 0o600))
	push := []string{"designs", "push", dir, "-m", "First version", "--json"}
	result = testutil.RunCommand(t, push, "")
	assert.Equal(t, 5, result.ExitCode, result.Stderr)
	assert.Contains(t, result.Stderr, "shared/brand/tokens.css")
	assert.Zero(t, creates.Load())
	assert.Zero(t, checks.Load())

	require.NoError(t, os.WriteFile(changedPath, []byte(":root{}"), 0o600))
	result = testutil.RunCommand(t, push, "")
	require.Equal(t, 0, result.ExitCode, result.Stderr)
	assert.Equal(t, int32(1), creates.Load())
	assert.Equal(t, int32(1), checks.Load())
}

func TestDesignSystemExportImportPreservesPackageAcrossProfiles(t *testing.T) {
	files := systemTestProjection()
	for i := range files {
		files[i].Path = strings.TrimPrefix(files[i].Path, "shared/brand/")
	}
	slices.SortFunc(files, func(a, b api.ArtifactSystemFile) int { return strings.Compare(a.Path, b.Path) })
	digest := testSystemDigest(t, files)
	document, err := json.Marshal(api.ArtifactSystemPackage{SchemaVersion: 1, Files: files})
	require.NoError(t, err)
	var imports atomic.Int32
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET " + systemsPath + "/" + verID + "/package": func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "Bearer tok_test_fake", r.Header.Get("Authorization"))
			assert.Equal(t, "2", r.URL.Query().Get("expectedRevision"))
			testutil.RespondJSON(w, http.StatusOK, api.ArtifactSystemTransfer{
				SystemID: verID, Revision: 2, PackageDigest: digest,
				ArtifactSystemDocument: api.ArtifactSystemDocument{Format: "package", Document: document},
			})
		},
		"GET /workspace/info": func(w http.ResponseWriter, r *http.Request) {
			workspace := designWorkspaceID
			if r.Header.Get("Authorization") == "Bearer tok_other_fake" {
				workspace = "bbbbbbbbbbbbbbbbbbbbbbbb"
			}
			testutil.RespondJSON(w, http.StatusOK, api.Workspace{ID: workspace})
		},
		"POST " + systemsPath: func(w http.ResponseWriter, r *http.Request) {
			imports.Add(1)
			assert.Equal(t, "Bearer tok_other_fake", r.Header.Get("Authorization"))
			var input api.ArtifactSystemCreate
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&input))
			assert.Equal(t, "Imported brand", input.Name)
			assert.Equal(t, "package", input.Format)
			assert.JSONEq(t, string(document), string(input.Document))
			testutil.RespondJSON(w, http.StatusCreated, api.ArtifactDesignSystem{ID: ver2ID, Revision: 1, PackageDigest: &digest, Available: true})
		},
	}.Handler())
	home := setupHome(t, srv.URL)
	f, err := os.OpenFile(filepath.Join(home, ".copera.toml"), os.O_WRONLY|os.O_APPEND, 0o600)
	require.NoError(t, err)
	_, err = f.WriteString("\n[profiles.other]\ntoken = \"tok_other_fake\"\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	dir := filepath.Join(t.TempDir(), "brand")
	result := testutil.RunCommand(t, []string{"designs", "systems", "pull", verID, dir, "--expected-revision", "2", "--json"}, "")
	require.Equal(t, 0, result.ExitCode, result.Stderr)
	assert.Contains(t, result.Stdout, digest)
	assert.Contains(t, readFile(t, filepath.Join(dir, ".copera", "design-system.json")), designWorkspaceID)
	result = testutil.RunCommand(t, []string{"designs", "systems", "push", dir, "--profile", "other", "--json"}, "")
	assert.Equal(t, 5, result.ExitCode, result.Stderr)

	filename := filepath.Join(t.TempDir(), "brand.zip")
	result = testutil.RunCommand(t, []string{"designs", "systems", "export", verID, "-o", filename, "--expected-revision", "2", "--json"}, "")
	require.Equal(t, 0, result.ExitCode, result.Stderr)
	assert.Contains(t, result.Stdout, digest)
	archive, err := zip.OpenReader(filename)
	require.NoError(t, err)
	for _, member := range archive.File {
		assert.False(t, strings.HasPrefix(member.Name, ".copera/"))
	}
	require.NoError(t, archive.Close())
	result = testutil.RunCommand(t, []string{"designs", "systems", "import", filename, "--name", "Imported brand", "--profile", "other", "--json"}, "")
	require.Equal(t, 0, result.ExitCode, result.Stderr)
	assert.Contains(t, result.Stdout, digest)
	assert.Contains(t, result.Stdout, ver2ID)
	assert.Equal(t, int32(1), imports.Load())
}

func testSystemDigest(t *testing.T, files []api.ArtifactSystemFile) string {
	t.Helper()
	canonical := `{"schemaVersion":1,"kit":null,"files":[`
	for i, file := range files {
		binary, err := base64.StdEncoding.DecodeString(file.ContentBase64)
		assert.NoError(t, err)
		if i > 0 {
			canonical += ","
		}
		canonical += `{"path":"` + file.Path + `","mediaType":"` + file.MediaType + `","contentDigest":"` + digestOf(string(binary)) + `"`
		if file.Usage != "" {
			canonical += `,"usage":"` + file.Usage + `"`
		}
		canonical += "}"
	}
	return digestOf(canonical + "]}")
}
