package commands_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/copera/copera-cli/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// driveTreeResp builds a mock drive tree API response.
func driveTreeResp(nodes []map[string]any) map[string]any {
	return map[string]any{"root": nodes, "totalItems": len(nodes), "truncated": false, "nextParentIds": []string{}}
}

// ── drive tree ───────────────────────────────────────────────────────────────

func TestDriveTree_JSON(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /drive/tree": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, driveTreeResp([]map[string]any{
				{"_id": "f001", "name": "Documents", "type": "folder", "hasChildren": true, "owner": "u1", "createdAt": "2025-01-01T00:00:00Z", "updatedAt": "2025-01-01T00:00:00Z"},
				{"_id": "f002", "name": "report.pdf", "type": "file", "hasChildren": false, "mimeType": "application/pdf", "size": 1048576, "owner": "u1", "createdAt": "2025-01-01T00:00:00Z", "updatedAt": "2025-01-01T00:00:00Z"},
			}))
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"drive", "tree", "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)

	var result map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &result))
	nodes := result["Nodes"].([]any)
	assert.Len(t, nodes, 2)
}

func TestDriveTree_HumanFormat(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /drive/tree": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, driveTreeResp([]map[string]any{
				{"_id": "f001", "name": "Designs", "type": "folder", "hasChildren": true, "owner": "u1", "createdAt": "2025-01-01T00:00:00Z", "updatedAt": "2025-01-01T00:00:00Z", "children": []map[string]any{
					{"_id": "f003", "name": "logo.png", "type": "file", "hasChildren": false, "mimeType": "image/png", "size": 51200, "owner": "u1", "createdAt": "2025-01-01T00:00:00Z", "updatedAt": "2025-01-01T00:00:00Z"},
				}},
				{"_id": "f002", "name": "report.pdf", "type": "file", "hasChildren": false, "mimeType": "application/pdf", "size": 1048576, "owner": "u1", "createdAt": "2025-01-01T00:00:00Z", "updatedAt": "2025-01-01T00:00:00Z"},
			}))
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"drive", "tree", "--output", "table"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Contains(t, res.Stdout, "Designs/")
	assert.Contains(t, res.Stdout, "logo.png")
	assert.Contains(t, res.Stdout, "report.pdf")
	assert.Contains(t, res.Stdout, "f001")
	assert.Contains(t, res.Stdout, "f003")
}

func TestDriveTree_ParentAndDepth(t *testing.T) {
	var capturedParent, capturedDepth string
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /drive/tree": func(w http.ResponseWriter, r *http.Request) {
			capturedParent = r.URL.Query().Get("parentId")
			capturedDepth = r.URL.Query().Get("depth")
			testutil.RespondJSON(w, http.StatusOK, driveTreeResp([]map[string]any{
				{"_id": "child1", "name": "Nested", "type": "folder", "hasChildren": false, "owner": "u1", "createdAt": "2025-01-01T00:00:00Z", "updatedAt": "2025-01-01T00:00:00Z"},
			}))
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"drive", "tree", "--parent", "folder123", "--depth", "5", "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Equal(t, "folder123", capturedParent)
	assert.Equal(t, "5", capturedDepth)
}

func TestDriveTree_Truncated(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /drive/tree": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"root":          []map[string]any{{"_id": "f1", "name": "item", "type": "file", "hasChildren": false, "owner": "u1", "createdAt": "2025-01-01T00:00:00Z", "updatedAt": "2025-01-01T00:00:00Z"}},
				"totalItems":    500,
				"truncated":     true,
				"nextParentIds": []string{"folder999"},
			})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"drive", "tree", "--output", "table"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Contains(t, res.Stderr, "truncated")
}

// ── drive search ─────────────────────────────────────────────────────────────

func TestDriveSearch_PassesParams(t *testing.T) {
	var capturedQuery string
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /drive/search": func(w http.ResponseWriter, r *http.Request) {
			capturedQuery = r.URL.Query().Get("q")
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"hits": []map[string]any{
					{"_id": "f1", "name": "report.pdf", "type": "file", "owner": "u1", "mimeType": "application/pdf", "size": 1024, "createdAt": "2025-01-01T00:00:00Z", "updatedAt": "2025-01-01T00:00:00Z"},
				},
				"totalHits": 1,
				"query":     "report",
			})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"drive", "search", "report", "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Equal(t, "report", capturedQuery)
}

func TestDriveSearch_NoResults(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /drive/search": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"hits":      []map[string]any{},
				"totalHits": 0,
				"query":     "nonexistent",
			})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"drive", "search", "nonexistent", "--output", "table"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Contains(t, res.Stderr, "No results")
}

// ── drive get ────────────────────────────────────────────────────────────────

func TestDriveGet_JSON(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /drive/files/abc123": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"_id": "abc123", "name": "logo.png", "type": "file", "owner": "u1",
				"mimeType": "image/png", "size": 51200,
				"createdAt": "2025-01-01T00:00:00Z", "updatedAt": "2025-01-02T00:00:00Z",
			})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"drive", "get", "abc123", "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)

	var f map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &f))
	assert.Equal(t, "logo.png", f["name"])
	assert.Equal(t, "image/png", f["mimeType"])
}

func TestDriveGet_HumanFormat(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /drive/files/abc123": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"_id": "abc123", "name": "logo.png", "type": "file", "owner": "u1",
				"mimeType": "image/png", "size": 51200,
				"createdAt": "2025-01-01T00:00:00Z", "updatedAt": "2025-01-02T00:00:00Z",
			})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"drive", "get", "abc123", "--output", "table"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Contains(t, res.Stdout, "ID:       abc123")
	assert.Contains(t, res.Stdout, "Name:     logo.png")
	assert.Contains(t, res.Stdout, "Type:     file")
	assert.Contains(t, res.Stdout, "MIME:     image/png")
}

func TestDriveGet_MissingID(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"drive", "get"}, "")
	assert.NotEqual(t, 0, res.ExitCode)
}

// ── drive mkdir ──────────────────────────────────────────────────────────────

func TestDriveMkdir_Success(t *testing.T) {
	var capturedBody map[string]any
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"POST /drive/folders": func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&capturedBody)
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"_id": "newf1", "name": capturedBody["name"], "type": "folder", "owner": "u1",
				"createdAt": "2025-01-01T00:00:00Z", "updatedAt": "2025-01-01T00:00:00Z",
			})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"drive", "mkdir", "My Folder", "--parent", "parentABC", "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Equal(t, "My Folder", capturedBody["name"])
	assert.Equal(t, "parentABC", capturedBody["parentId"])

	var f map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &f))
	assert.Equal(t, "newf1", f["_id"])
}

// ── drive upload (single file) ───────────────────────────────────────────────

func TestDriveUpload_SingleFile(t *testing.T) {
	// Create a temp file to upload
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	content := []byte("hello world test content")
	require.NoError(t, os.WriteFile(testFile, content, 0644))

	var startCalled, presignedCalled, finalizeCalled bool
	var finalizeBody map[string]any

	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"POST /drive/files/upload/multipart/start": func(w http.ResponseWriter, r *http.Request) {
			startCalled = true
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"uploadId": "upload-123",
				"fileKey":  "files/test-key",
			})
		},
		"POST /drive/files/upload/multipart/presigned-urls": func(w http.ResponseWriter, r *http.Request) {
			presignedCalled = true
			// Return a presigned URL pointing back to our test server
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"parts": []map[string]any{
					{"signedUrl": fmt.Sprintf("http://%s/s3-upload/part1", r.Host), "PartNumber": 1},
				},
			})
		},
		"PUT /s3-upload/part1": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("ETag", `"etag-abc-123"`)
			w.WriteHeader(http.StatusOK)
		},
		"POST /drive/files/upload/multipart/finalize": func(w http.ResponseWriter, r *http.Request) {
			finalizeCalled = true
			_ = json.NewDecoder(r.Body).Decode(&finalizeBody)
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"_id": "uploaded1", "name": "test.txt", "type": "file", "owner": "u1",
				"mimeType": "text/plain; charset=utf-8", "size": len(content),
				"createdAt": "2025-01-01T00:00:00Z", "updatedAt": "2025-01-01T00:00:00Z",
			})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"drive", "upload", testFile, "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.True(t, startCalled, "multipart start should be called")
	assert.True(t, presignedCalled, "presigned URLs should be called")
	assert.True(t, finalizeCalled, "finalize should be called")

	// Verify finalize body has correct parts with ETag
	parts := finalizeBody["parts"].([]any)
	assert.Len(t, parts, 1)
	part := parts[0].(map[string]any)
	assert.Equal(t, "etag-abc-123", part["eTag"])

	var result map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &result))
	assert.Equal(t, "uploaded1", result["_id"])
}

// ── drive download ───────────────────────────────────────────────────────────

func TestDriveDownload_Success(t *testing.T) {
	fileContent := "binary content here"

	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /drive/files/dl001": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"_id": "dl001", "name": "readme.txt", "type": "file", "owner": "u1",
				"size": len(fileContent), "mimeType": "text/plain",
				"createdAt": "2025-01-01T00:00:00Z", "updatedAt": "2025-01-01T00:00:00Z",
			})
		},
		"GET /drive/files/dl001/download": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"url": fmt.Sprintf("http://%s/cdn/readme.txt", r.Host),
			})
		},
		"GET /cdn/readme.txt": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(fileContent)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(fileContent))
		},
	}.Handler())
	setupHome(t, srv.URL)

	dest := filepath.Join(t.TempDir(), "downloaded.txt")
	res := testutil.RunCommand(t, []string{"drive", "download", "dl001", "--dest", dest, "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)

	got, err := os.ReadFile(dest)
	require.NoError(t, err)
	assert.Equal(t, fileContent, string(got))
}

func TestDriveDownload_FolderFails(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /drive/files/folder1": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"_id": "folder1", "name": "My Folder", "type": "folder", "owner": "u1",
				"createdAt": "2025-01-01T00:00:00Z", "updatedAt": "2025-01-01T00:00:00Z",
			})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"drive", "download", "folder1"}, "")
	assert.Equal(t, 2, res.ExitCode)
	assert.Contains(t, res.Stderr, "cannot download a folder")
}

// ── auth ─────────────────────────────────────────────────────────────────────

func TestDriveMissingToken(t *testing.T) {
	home := t.TempDir()
	testutil.WriteTempConfigAt(t, filepath.Join(home, ".copera.toml"), "[profiles.default]\n")
	testutil.SetEnv(t, "HOME", home)
	testutil.UnsetEnv(t, "COPERA_CLI_AUTH_TOKEN")

	res := testutil.RunCommand(t, []string{"drive", "tree"}, "")
	assert.Equal(t, 4, res.ExitCode)
}
