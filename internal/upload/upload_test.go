package upload

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/copera/copera-cli/internal/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNumParts(t *testing.T) {
	tests := []struct {
		fileSize, chunkSize int64
		want                int
	}{
		{0, 10, 1},
		{5, 10, 1},
		{10, 10, 1},
		{11, 10, 2},
		{20, 10, 2},
		{21, 10, 3},
		{100, 33, 4},
	}
	for _, tt := range tests {
		got := NumParts(tt.fileSize, tt.chunkSize)
		assert.Equal(t, tt.want, got, "NumParts(%d, %d)", tt.fileSize, tt.chunkSize)
	}
}

func TestUploadParts_Success(t *testing.T) {
	t.Setenv("GO_TEST", "1")
	content := []byte("hello world data for upload testing")

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			http.Error(w, "expected PUT", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("ETag", `"etag-`+r.URL.Path+`"`)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// Write temp file
	tmpFile := filepath.Join(t.TempDir(), "test.bin")
	require.NoError(t, os.WriteFile(tmpFile, content, 0644))

	parts := []api.PresignedPart{
		{SignedURL: srv.URL + "/part1", PartNumber: 1},
	}

	u := NewUploader(srv.Client(), MinChunkSize, 2, NoopProgress{})
	completed, err := u.UploadParts(t.Context(), tmpFile, parts)
	require.NoError(t, err)
	require.Len(t, completed, 1)
	assert.Equal(t, 1, completed[0].PartNumber)
	assert.Equal(t, "etag-/part1", completed[0].ETag)
}

func TestUploadParts_MultiPart(t *testing.T) {
	t.Setenv("GO_TEST", "1")
	content := make([]byte, 45)
	for i := range content {
		content[i] = byte('a' + (i % 26))
	}

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"etag-`+r.URL.Path+`"`)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	tmpFile := filepath.Join(t.TempDir(), "multi.bin")
	require.NoError(t, os.WriteFile(tmpFile, content, 0644))

	// Directly set chunkSize=15 to get 3 parts (15+15+15)
	u := &Uploader{
		httpClient:  srv.Client(),
		chunkSize:   15,
		concurrency: 2,
		progress:    NoopProgress{},
	}

	parts := []api.PresignedPart{
		{SignedURL: srv.URL + "/p1", PartNumber: 1},
		{SignedURL: srv.URL + "/p2", PartNumber: 2},
		{SignedURL: srv.URL + "/p3", PartNumber: 3},
	}

	completed, err := u.UploadParts(t.Context(), tmpFile, parts)
	require.NoError(t, err)
	require.Len(t, completed, 3)

	for i, c := range completed {
		assert.Equal(t, i+1, c.PartNumber)
		assert.NotEmpty(t, c.ETag)
	}
}

func TestUploadParts_ServerError_Retries(t *testing.T) {
	t.Setenv("GO_TEST", "1")
	attempts := 0
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("ETag", `"success-etag"`)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	tmpFile := filepath.Join(t.TempDir(), "retry.bin")
	require.NoError(t, os.WriteFile(tmpFile, []byte("data"), 0644))

	parts := []api.PresignedPart{
		{SignedURL: srv.URL + "/part1", PartNumber: 1},
	}

	u := NewUploader(srv.Client(), DefaultChunkSize, 1, NoopProgress{})
	completed, err := u.UploadParts(t.Context(), tmpFile, parts)
	require.NoError(t, err)
	require.Len(t, completed, 1)
	assert.Equal(t, "success-etag", completed[0].ETag)
	assert.Equal(t, 3, attempts, "should have retried twice then succeeded")
}

func TestValidatePresignedURL(t *testing.T) {
	// Unset GO_TEST so validation runs in production mode
	orig := os.Getenv("GO_TEST")
	os.Unsetenv("GO_TEST")
	defer os.Setenv("GO_TEST", orig)

	tests := []struct {
		url     string
		wantErr bool
		errMsg  string
	}{
		{"https://s3.amazonaws.com/bucket/key?sig=abc", false, ""},
		{"http://s3.amazonaws.com/bucket/key", true, "HTTPS"},             // not HTTPS
		{"https://localhost/upload", true, "localhost"},                    // localhost
		{"https://127.0.0.1/upload", true, "loopback"},                   // loopback
		{"https://192.168.1.1/upload", true, "private"},                   // private
		{"https://10.0.0.1/upload", true, "private"},                      // private
		{"https://169.254.169.254/metadata", true, "private"},             // link-local
	}
	for _, tt := range tests {
		err := ValidatePresignedURL(tt.url)
		if tt.wantErr {
			assert.Error(t, err, "expected error for %s", tt.url)
			if tt.errMsg != "" {
				assert.Contains(t, err.Error(), tt.errMsg, "wrong error for %s", tt.url)
			}
		} else {
			assert.NoError(t, err, "unexpected error for %s", tt.url)
		}
	}
}

func TestDetectMimeType(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"report.pdf", "application/pdf"},
		{"image.png", "image/png"},
		{"data.json", "application/json"},
		{"noext", "application/octet-stream"},
	}
	for _, tt := range tests {
		got := DetectMimeType(tt.path)
		assert.Equal(t, tt.want, got, "DetectMimeType(%q)", tt.path)
	}
}

func TestWalkDir(t *testing.T) {
	root := t.TempDir()

	// Create structure: root/a/ root/a/file1.txt root/b.txt
	require.NoError(t, os.MkdirAll(filepath.Join(root, "a"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "a", "file1.txt"), []byte("hi"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "b.txt"), []byte("hey"), 0644))

	entries, err := WalkDir(root)
	require.NoError(t, err)
	require.Len(t, entries, 3)

	// Directories should come before files at the same depth
	assert.True(t, entries[0].IsDir, "first entry should be directory 'a'")
	assert.Equal(t, "a", entries[0].RelativePath)
	assert.False(t, entries[1].IsDir)
	assert.Equal(t, "b.txt", entries[1].RelativePath)
	assert.False(t, entries[2].IsDir)
	assert.Equal(t, filepath.Join("a", "file1.txt"), entries[2].RelativePath)
}

func TestWalkDir_RejectsEscapingSymlink(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0644))

	// Create a symlink inside root that points outside
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "escape")))

	_, err := WalkDir(root)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "escapes root")
}
