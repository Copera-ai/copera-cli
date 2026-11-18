package commands_test

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/copera/copera-cli/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDesignsManage(t *testing.T) {
	const (
		archiveBytes = "PK\x05\x06\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00"
		assetBytes   = "\x89PNG\r\n\x1a\nmock image"
		designPath   = "/artifacts/" + artID
		archivePath  = designPath + "/versions/" + verID + "/archive"
		restorePath  = designPath + "/versions/" + ver2ID + "/restore"
	)
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
			testutil.RespondJSON(w, http.StatusOK, map[string]string{
				"id": "dddddddddddddddddddddddd", "path": "assets/hero.png",
				"mediaType": "image/png", "contentDigest": digestOf(assetBytes), "usage": "image",
			})
		},
		"GET " + archivePath: func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, struct {
				URL           string   `json:"url"`
				ByteLength    int      `json:"byteLength"`
				ContentDigest string   `json:"contentDigest"`
				OmittedPaths  []string `json:"omittedPaths"`
			}{"http://" + r.Host + "/signed/pack.zip", len(archiveBytes), digestOf(archiveBytes), []string{}})
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
		{"assets add", []string{"assets", "add", "hero.png"}, "assets/hero.png"},
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
