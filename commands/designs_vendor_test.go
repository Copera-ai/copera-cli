package commands

import (
	"bytes"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type vendorTestTransport func(*http.Request) (*http.Response, error)

func (f vendorTestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type vendorTestResponse struct {
	status int
	body   string
	header http.Header
}

func vendorTestFolder(t *testing.T, responses map[string]vendorTestResponse) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, ".copera"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".copera", "design.json"),
		[]byte(`{"kind":"website","runtime":"static"}`), 0o600))

	old := http.DefaultTransport
	http.DefaultTransport = vendorTestTransport(func(req *http.Request) (*http.Response, error) {
		assert.Empty(t, req.Header.Get("Authorization"))
		assert.Empty(t, req.Header.Get("Cookie"))
		assert.Contains(t, req.Header.Get("User-Agent"), "Chrome/")
		response, ok := responses[req.URL.String()]
		if !ok {
			t.Errorf("unexpected vendor request %s", req.URL)
			return nil, fmt.Errorf("unregistered test response")
		}
		return &http.Response{StatusCode: response.status, Header: response.header,
			Body: io.NopCloser(strings.NewReader(response.body)), Request: req}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = old })
	return dir
}

func runVendorTestCommand(t *testing.T, dir, remote string, extra ...string) (string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	cmd, _ := newRootCmd(strings.NewReader(""), &stdout, &stderr)
	cmd.SetArgs(append([]string{"designs", "vendor", remote, "--dir", dir, "--json"}, extra...))
	err := cmd.Execute()
	return stdout.String(), err
}

func TestDesignVendorCDNPreservesBytesAndRefusesChanges(t *testing.T) {
	const remote = "https://cdnjs.cloudflare.com/ajax/libs/chart.js/4.4.4/chart.umd.js"
	const metadataURL = "https://api.cdnjs.com/libraries/chart.js/4.4.4?fields=files,sri"
	const local = "vendor/cdnjs/chart.js/4.4.4/chart.umd.js"
	data := []byte("/* " + strings.Repeat("copera", 200000) + " */\n")
	sum512 := sha512.Sum512(data)
	sum256 := sha256.Sum256(data)
	metadata := func(sri string) vendorTestResponse {
		return vendorTestResponse{status: 200, body: fmt.Sprintf(
			`{"files":["chart.umd.js"],"sri":{"chart.umd.js":%q}}`, sri)}
	}
	responses := map[string]vendorTestResponse{
		remote:      {status: 200, body: string(data)},
		metadataURL: metadata("sha256-" + base64.StdEncoding.EncodeToString(make([]byte, 32)) + " sha512-" + base64.StdEncoding.EncodeToString(sum512[:])),
	}
	dir := vendorTestFolder(t, responses)
	got, err := runVendorTestCommand(t, dir, remote)
	require.NoError(t, err)
	assert.JSONEq(t, fmt.Sprintf(`{"files":[{"path":%q,"byteLength":%d,"contentDigest":"%x","verification":"cdn_checksum"}]}`, local, len(data), sum256), got)
	file := filepath.Join(dir, filepath.FromSlash(local))
	written, err := os.ReadFile(file)
	require.NoError(t, err)
	assert.True(t, bytes.Equal(data, written), "vendor changed the CDN bytes")

	got, err = runVendorTestCommand(t, dir, remote)
	require.NoError(t, err)
	assert.Contains(t, got, "cdn_checksum")

	_, err = runVendorTestCommand(t, dir, strings.Replace(remote, "4.4.4", "latest", 1))
	require.ErrorContains(t, err, "exact version")

	responses[metadataURL] = metadata("sha512-" + base64.StdEncoding.EncodeToString(make([]byte, 64)))
	_, err = runVendorTestCommand(t, dir, remote, "--path", "vendor/other.js")
	require.ErrorContains(t, err, "checksum")
	require.NoFileExists(t, filepath.Join(dir, "vendor", "other.js"))

	responses[metadataURL] = vendorTestResponse{status: 503}
	_, err = runVendorTestCommand(t, dir, remote)
	require.ErrorContains(t, err, "503")
	responses[metadataURL] = metadata("sha512-" + base64.StdEncoding.EncodeToString(sum512[:]))

	require.NoError(t, os.WriteFile(file, []byte("existing author file"), 0o600))
	_, err = runVendorTestCommand(t, dir, remote)
	require.ErrorContains(t, err, "different bytes")
	written, err = os.ReadFile(file)
	require.NoError(t, err)
	assert.Equal(t, "existing author file", string(written))

	_, err = runVendorTestCommand(t, dir, remote, "--path", "vendor/CDNJS/chart.js/4.4.4/new.js")
	require.ErrorContains(t, err, "collide")
	require.NoFileExists(t, filepath.Join(dir, "vendor", "CDNJS", "chart.js", "4.4.4", "new.js"))
}

func TestDesignVendorScopedNPMAndRedirect(t *testing.T) {
	const npm = "https://cdn.jsdelivr.net/npm/@scope/package@1.2.3/dist/main.js"
	const npmMetadata = "https://data.jsdelivr.com/v1/packages/npm/@scope/package@1.2.3?structure=flat"
	const npmFile = "vendor/npm-scoped/scope/package/1.2.3/dist/main.js"
	const contents = "window.test = true;\n"
	sum := sha256.Sum256([]byte(contents))
	responses := map[string]vendorTestResponse{
		npm:         {status: 200, body: contents},
		npmMetadata: {status: 200, body: fmt.Sprintf(`{"files":[{"name":"/dist/main.js","hash":%q}]}`, base64.StdEncoding.EncodeToString(sum[:]))},
	}
	dir := vendorTestFolder(t, responses)
	got, err := runVendorTestCommand(t, dir, npm)
	require.NoError(t, err)
	assert.Contains(t, got, npmFile)
	assert.Contains(t, got, "cdn_checksum")

	responses[npmMetadata] = vendorTestResponse{status: 200, body: `{"files":[{"name":"/dist/main.js"}]}`}
	got, err = runVendorTestCommand(t, dir, npm)
	require.NoError(t, err)
	assert.Contains(t, got, "local_sha256")

	responses[npm] = vendorTestResponse{status: 302, header: http.Header{
		"Location": {"https://cdn.jsdelivr.net/npm/@scope/package@1.2.4/dist/main.js"},
	}}
	_, err = runVendorTestCommand(t, dir, npm, "--path", "vendor/redirect.js")
	require.ErrorContains(t, err, "302")
	require.NoFileExists(t, filepath.Join(dir, "vendor", "redirect.js"))

	const transformed = "https://cdn.jsdelivr.net/npm/@scope/package@1.2.3/dist/main.min.js"
	_, err = runVendorTestCommand(t, dir, transformed)
	require.ErrorContains(t, err, "exact file")
}

func TestDesignVendorGoogleFontsSnapshotsLocalCSS(t *testing.T) {
	const remote = "https://fonts.googleapis.com/css2?family=Inter&display=swap"
	const canonical = "https://fonts.googleapis.com/css2?display=swap&family=Inter"
	const font = "https://fonts.gstatic.com/s/inter/v20/example.woff2"
	fontBytes := make([]byte, 64)
	copy(fontBytes, "wOF2")
	binary.BigEndian.PutUint32(fontBytes[8:12], uint32(len(fontBytes)))
	binary.BigEndian.PutUint16(fontBytes[12:14], 1)
	binary.BigEndian.PutUint32(fontBytes[20:24], 10)
	fontSum := sha256.Sum256(fontBytes)
	cssSum := sha256.Sum256([]byte(canonical))
	localDir := fmt.Sprintf("vendor/fonts/%x", cssSum[:8])
	css := "@font-face { font-family: 'Inter'; font-style: italic; font-weight: 100 900; font-display: swap; src: url(" +
		font + ") format('woff2'); unicode-range: U+0000-00FF; }"
	responses := map[string]vendorTestResponse{
		canonical: {status: 200, body: "/* latin */\n" + css},
		font:      {status: 200, body: string(fontBytes)},
	}
	dir := vendorTestFolder(t, responses)
	got, err := runVendorTestCommand(t, dir, remote)
	require.NoError(t, err)
	var result struct {
		Files []struct {
			Path         string `json:"path"`
			Verification string `json:"verification"`
		} `json:"files"`
	}
	require.NoError(t, json.Unmarshal([]byte(got), &result))
	require.Len(t, result.Files, 2)
	assert.Equal(t, localDir+"/fonts.css", result.Files[0].Path)
	assert.Equal(t, "local_sha256", result.Files[0].Verification)
	assert.Equal(t, fmt.Sprintf("%s/%x.woff2", localDir, fontSum), result.Files[1].Path)
	localCSS, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(result.Files[0].Path)))
	require.NoError(t, err)
	assert.Contains(t, string(localCSS), fmt.Sprintf("url(%x.woff2)", fontSum))
	assert.Contains(t, string(localCSS), "font-weight: 100 900")
	assert.Contains(t, string(localCSS), "font-style: italic")
	assert.Contains(t, string(localCSS), "unicode-range: U+0000-00FF")
	assert.NotContains(t, string(localCSS), "https://")
	written, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(result.Files[1].Path)))
	require.NoError(t, err)
	assert.Equal(t, fontBytes, written)

	responses[canonical] = vendorTestResponse{status: 200, body: css + "\n@import 'https://example.com/remote.css';"}
	_, err = runVendorTestCommand(t, dir, remote, "--path", "vendor/new/fonts.css")
	require.ErrorContains(t, err, "font-face")
	require.NoDirExists(t, filepath.Join(dir, "vendor", "new"))
}
