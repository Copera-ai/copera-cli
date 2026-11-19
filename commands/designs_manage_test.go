package commands_test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/copera/copera-cli/internal/api"

	"github.com/copera/copera-cli/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDesignsManage(t *testing.T) {
	const (
		assetBytes  = "\x89PNG\r\n\x1a\nmock image"
		designPath  = "/artifacts/" + artID
		archivePath = designPath + "/versions/" + verID + "/archive"
		restorePath = designPath + "/versions/" + ver2ID + "/restore"
	)
	archiveBytes := designArchiveFixture(t, 2)
	server := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /workspace/info": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]string{"slug": "launch"})
		},
		"GET " + designPath: func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, artifactFixture())
		},
		"GET " + designPath + "/versions": func(w http.ResponseWriter, r *http.Request) {
			response := `{"edges":[{"cursor":"v2","node":{"id":"` + ver2ID + `","version":2}}],"hasNextPage":false}`
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, response)
		},
		"GET " + designPath + "/versions/" + ver2ID: func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, artifactVersionFixture(ver2ID, 2))
		},
		"POST " + designPath + "/assets": func(w http.ResponseWriter, r *http.Request) {
			metadata := assertDesignUpload(t, r, "hero.png", assetBytes)
			assert.Equal(t, digestOf(assetBytes), metadata["expectedContentDigest"])
			assert.Equal(t, "assets/hero.png", metadata["path"])
			testutil.RespondJSON(w, http.StatusOK, map[string]string{
				"id": "dddddddddddddddddddddddd", "path": "assets/hero.png",
				"mediaType": "image/png", "contentDigest": digestOf(assetBytes), "usage": "image",
			})
		},
		"GET " + archivePath: func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "2", r.URL.Query().Get("archiveVersion"))
			testutil.RespondJSON(w, http.StatusOK, struct {
				URL           string   `json:"url"`
				ByteLength    int      `json:"byteLength"`
				ContentDigest string   `json:"contentDigest"`
				OmittedPaths  []string `json:"omittedPaths"`
				Version       int      `json:"archiveVersion"`
			}{"http://" + r.Host + "/signed/pack.zip", len(archiveBytes), digestOf(archiveBytes), []string{}, 2})
		},
		"GET /signed/pack.zip": func(w http.ResponseWriter, r *http.Request) {
			assert.Empty(t, r.Header.Get("Authorization"))
			_, _ = io.WriteString(w, archiveBytes)
		},
		"POST /artifacts/archives": func(w http.ResponseWriter, r *http.Request) {
			metadata := assertDesignUpload(t, r, "launch.zip", archiveBytes)
			assert.NotEmpty(t, metadata["idempotencyKey"])
			assert.Equal(t, "claude_code", metadata["tool"])
			testutil.RespondJSON(w, http.StatusOK, artifactFixture())
		},
		"PATCH " + designPath: func(w http.ResponseWriter, r *http.Request) {
			var body map[string]string
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			assert.Equal(t, map[string]string{"title": "New title"}, body)
			testutil.RespondJSON(w, http.StatusOK, map[string]string{"id": artID, "title": "New title"})
		},
		"DELETE " + designPath: func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"`+artID+`","trashed":true}`)
		},
		"POST " + restorePath: func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				BaseVersionID        string `json:"baseVersionId"`
				ExpectedHeadRevision int    `json:"expectedHeadRevision"`
				IdempotencyKey       string `json:"idempotencyKey"`
			}
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			assert.Equal(t, verID, body.BaseVersionID)
			assert.Equal(t, 4, body.ExpectedHeadRevision)
			assert.NotEmpty(t, body.IdempotencyKey)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"outcome":"created","buildState":"queued","version":{"version":5}}`)
		},
		"POST " + designPath + "/fork": func(w http.ResponseWriter, r *http.Request) {
			var body map[string]string
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			assert.NotEmpty(t, body["idempotencyKey"])
			assert.Equal(t, "claude_code", body["tool"])
			testutil.RespondJSON(w, http.StatusCreated, artifactFixture())
		},
	}.Handler())
	setupHome(t, server.URL)
	t.Setenv("GO_TEST", "1")
	t.Setenv("CLAUDECODE", "1")

	project := t.TempDir()
	t.Chdir(project)
	require.NoError(t, os.Mkdir(".copera", 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(".copera", "design.json"), []byte(`{"id":"`+artID+`"}`), 0o600))
	require.NoError(t, os.WriteFile("hero.png", []byte(assetBytes), 0o600))
	require.NoError(t, os.WriteFile("launch.zip", []byte(archiveBytes), 0o600))

	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"assets add", []string{"assets", "add", "hero.png", "--path", "assets/hero.png"}, "assets/hero.png"},
		{"export", []string{"export", artID, "-o", "exported.zip"}, "exported.zip"},
		{"import", []string{"import", "launch.zip", "--workspace", "launch"}, artID},
		{"rename", []string{"rename", artID, "New title"}, "New title"},
		{"trash", []string{"trash", artID}, `"trashed": true`},
		{"restore", []string{"restore", artID, "--version", "2"}, `"buildState": "queued"`},
		{"fork", []string{"fork", artID}, artID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := testutil.RunCommand(t, append([]string{"designs", "--json"}, tc.args...), "")
			require.Equal(t, 0, result.ExitCode, "stderr: %s", result.Stderr)
			assert.Contains(t, result.Stdout, tc.want)
			if tc.name == "export" {
				assert.Equal(t, archiveBytes, readFile(t, "exported.zip"))
			}
		})
	}
}

func assertDesignUpload(t *testing.T, r *http.Request, name, content string) map[string]string {
	t.Helper()
	if !assert.NoError(t, r.ParseMultipartForm(1<<20)) {
		return nil
	}
	file, header, err := r.FormFile("file")
	if !assert.NoError(t, err) {
		return nil
	}
	defer file.Close()
	assert.Equal(t, name, header.Filename)
	data, err := io.ReadAll(file)
	assert.NoError(t, err)
	assert.Equal(t, content, string(data))
	var metadata map[string]string
	assert.NoError(t, json.Unmarshal([]byte(r.FormValue("metadata")), &metadata))
	return metadata
}

const designArchiveSource = "<h1>Portable Design</h1>"

func designArchiveFixture(t *testing.T, version int) string {
	t.Helper()
	var data bytes.Buffer
	writer := zip.NewWriter(&data)
	part, err := writer.Create("index.html")
	require.NoError(t, err)
	_, err = io.WriteString(part, designArchiveSource)
	require.NoError(t, err)
	part, err = writer.Create("copera-artifact.json")
	require.NoError(t, err)
	manifest := struct {
		Version   int                          `json:"archiveVersion"`
		Title     string                       `json:"title"`
		Project   json.RawMessage              `json:"project"`
		Files     []designArchiveSourceFixture `json:"files"`
		Binary    []struct{}                   `json:"binaryAssets"`
		Authoring api.ArtifactManifest         `json:"authoringManifest"`
	}{
		Version: version,
		Title:   "Portable Design",
		Project: json.RawMessage(`{"schemaVersion":1,"runtime":"static_web","entrypoint":"index.html","artifactKind":"website"}`),
		Files:   []designArchiveSourceFixture{{"index.html", "text/html", len(designArchiveSource), digestOf(designArchiveSource)}},
		Binary:  []struct{}{},
		Authoring: api.ArtifactManifest{
			SchemaVersion: 1, DefaultPageID: "home", SharedDesign: api.ArtifactSharedDesign{Version: 1},
			Assets: []api.ArtifactAsset{},
			Pages: []api.ArtifactManifestPage{{
				ID: "home", Title: "Home", Entrypoint: "index.html", Kind: "website", Surface: "web",
				DefaultViewport: api.ArtifactViewport{Width: 1440, Height: 900}, ScaffoldVersion: "1",
			}},
		},
	}
	require.NoError(t, json.NewEncoder(part).Encode(manifest))
	require.NoError(t, writer.Close())
	return data.String()
}

type designArchiveSourceFixture struct {
	Path          string `json:"path"`
	MediaType     string `json:"mediaType"`
	ByteLength    int    `json:"byteLength"`
	ContentDigest string `json:"contentDigest"`
}

func TestDesignsExportRefusesV1WithoutSelectedInventory(t *testing.T) {
	const designPath = "/artifacts/" + artID
	archiveBytes := designArchiveFixture(t, 1)
	artifact := api.Artifact{
		ID: artID, CurrentDraftVersionID: verID,
		CurrentVersion: api.ArtifactVersion{
			ID: verID,
			Files: []api.ArtifactFileRef{
				{Path: "index.html", MediaType: "text/html", ContentDigest: digestOf(designArchiveSource), UTF8Bytes: int64(len(designArchiveSource))},
			},
			AuthoringManifest: api.ArtifactManifest{
				Assets: []api.ArtifactAsset{{Path: "vendor/chart.js", MediaType: "application/javascript", ContentDigest: digestOf("chart bytes")}},
			},
		},
	}
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET " + designPath: func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, artifact)
		},
		"GET " + designPath + "/versions/" + verID + "/archive": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, api.ArtifactArchive{
				URL:        "http://" + r.Host + "/signed/old.zip",
				ByteLength: int64(len(archiveBytes)), ContentDigest: digestOf(archiveBytes),
			})
		},
		"GET /signed/old.zip": func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, archiveBytes)
		},
	}.Handler())
	setupHome(t, srv.URL)
	t.Setenv("GO_TEST", "1")
	filename := filepath.Join(t.TempDir(), "kept.zip")
	require.NoError(t, os.WriteFile(filename, []byte("existing"), 0o600))
	result := testutil.RunCommand(t, []string{"designs", "export", artID, "-o", filename, "--json"}, "")
	assert.NotEqual(t, 0, result.ExitCode)
	assert.Contains(t, result.Stderr, "requires an API that supports portable ZIP v2")
	assert.True(t, readFile(t, filename) == "existing", "existing destination changed")
}
