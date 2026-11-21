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
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
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

	const css2 = "https://fonts.googleapis.com/css2?"
	for _, test := range []struct {
		name, query, canonical string
	}{
		{"literal weights", "family=Inter:wght@400;600&display=swap", "display=swap&family=Inter%3Awght%40400%3B600"},
		{"encoded weights", "family=Inter:wght@400%3B600&display=swap", "display=swap&family=Inter%3Awght%40400%3B600"},
		{"multiple families and axes", "family=Inter:ital,wght@0,400;1,700&family=Roboto+Flex:opsz,wght@12..96,600&display=swap",
			"display=swap&family=Inter%3Aital%2Cwght%400%2C400%3B1%2C700&family=Roboto+Flex%3Aopsz%2Cwght%4012..96%2C600"},
	} {
		t.Run(test.name, func(t *testing.T) {
			responses[css2+test.canonical] = responses[canonical]
			local := "vendor/" + strings.ReplaceAll(test.name, " ", "-") + "/fonts.css"
			_, err := runVendorTestCommand(t, dir, css2+test.query, "--path", local)
			require.NoError(t, err)
			data, err := os.ReadFile(filepath.Join(dir, local))
			require.NoError(t, err)
			assert.Contains(t, string(data), fmt.Sprintf("url(%x.woff2)", fontSum))
		})
	}
	for _, query := range []string{
		"display=swap", "family=", "family=Inter:wght@400;%ZZ", "family=Inter&display=swap;other=value",
		"family=Inter;display=swap", "family=Inter;Invalid:wght@400",
	} {
		t.Run(query, func(t *testing.T) {
			_, err := runVendorTestCommand(t, dir, css2+query)
			require.ErrorContains(t, err, "valid stylesheet family query")
		})
	}

	responses[canonical] = vendorTestResponse{status: 200, body: css + "\n@import 'https://example.com/remote.css';"}
	_, err = runVendorTestCommand(t, dir, remote, "--path", "vendor/new/fonts.css")
	require.ErrorContains(t, err, "font-face")
	require.NoDirExists(t, filepath.Join(dir, "vendor", "new"))
}

func TestDesignVendorAllRewritesReferences(t *testing.T) {
	const fonts = "https://fonts.googleapis.com/css2?family=Inter&display=swap"
	const canonicalFonts = "https://fonts.googleapis.com/css2?display=swap&family=Inter"
	const font = "https://fonts.gstatic.com/inter.woff2"
	const script = "https://cdnjs.cloudflare.com/ajax/libs/lib/1.2.3/main.js"
	const scriptPath = "vendor/cdnjs/lib/1.2.3/main.js"
	const stylesheet = "https://cdn.jsdelivr.net/npm/lib@1.2.3/main.css"
	const stylesheetPath = "vendor/npm/lib/1.2.3/main.css"
	const unpinned = "https://cdn.jsdelivr.net/npm/lib@latest/main.css"
	const image = "https://images.example.net/logo.png"
	const otherHost = "https://assets.example.net/styles.css"
	const scriptBytes = "window.lib = true;\n"
	const stylesheetBytes = "@import url(" + fonts + ");\nbody { color: blue }\n"

	fontBytes := make([]byte, 64)
	copy(fontBytes, "wOF2")
	binary.BigEndian.PutUint32(fontBytes[8:12], uint32(len(fontBytes)))
	binary.BigEndian.PutUint16(fontBytes[12:14], 1)
	binary.BigEndian.PutUint32(fontBytes[20:24], 10)
	fontCSS := "@font-face { font-family: 'Inter'; src: url(" + font + ") format('woff2'); }"
	scriptDigest := sha256.Sum256([]byte(scriptBytes))
	styleDigest := sha256.Sum256([]byte(stylesheetBytes))
	fontURLDigest := sha256.Sum256([]byte(canonicalFonts))
	fontsPath := fmt.Sprintf("vendor/fonts/%x/fonts.css", fontURLDigest[:8])
	responses := map[string]vendorTestResponse{
		canonicalFonts: {status: 200, body: fontCSS},
		font:           {status: 200, body: string(fontBytes)},
		script:         {status: 200, body: scriptBytes},
		stylesheet:     {status: 200, body: stylesheetBytes},
		"https://api.cdnjs.com/libraries/lib/1.2.3?fields=files,sri": {status: 200,
			body: fmt.Sprintf(`{"files":["main.js"],"sri":{"main.js":"sha256-%s"}}`, base64.StdEncoding.EncodeToString(scriptDigest[:]))},
		"https://data.jsdelivr.com/v1/packages/npm/lib@1.2.3?structure=flat": {status: 200,
			body: fmt.Sprintf(`{"files":[{"name":"/main.css","hash":%q}]}`, base64.StdEncoding.EncodeToString(styleDigest[:]))},
	}
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, ".copera"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".copera", "design.json"),
		[]byte(`{"kind":"website","runtime":"static"}`), 0o600))
	requests := make(map[string]*atomic.Int32, len(responses))
	for remote := range responses {
		requests[remote] = &atomic.Int32{}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		remote := "https://" + r.Host + r.URL.RequestURI()
		response, ok := responses[remote]
		if !ok {
			t.Errorf("unexpected vendor request %s", remote)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		requests[remote].Add(1)
		w.WriteHeader(response.status)
		_, _ = io.WriteString(w, response.body)
	}))
	t.Cleanup(server.Close)
	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	oldTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = oldTransport })
	http.DefaultTransport = vendorTestTransport(func(req *http.Request) (*http.Response, error) {
		local := req.Clone(req.Context())
		local.Host = req.URL.Host
		local.URL.Scheme, local.URL.Host = serverURL.Scheme, serverURL.Host
		return server.Client().Transport.RoundTrip(local)
	})
	stdout := func(args ...string) (string, error) {
		var out, stderr bytes.Buffer
		cmd, _ := newRootCmd(strings.NewReader(""), &out, &stderr)
		cmd.SetArgs(append([]string{"designs", "vendor"}, args...))
		err := cmd.Execute()
		return out.String(), err
	}
	for _, remote := range []string{script, "https://cdnjs.cloudflare.com/%ZZ", strings.TrimPrefix(script, "https:")} {
		_, err := stdout("--all", remote)
		require.ErrorContains(t, err, "--all cannot be used with a URL")
	}

	fontsHTML := strings.ReplaceAll(fonts, "&", "&amp;")
	html := "<!doctype html>\r\n" +
		"  <link href = '" + fontsHTML + "' REL=\"StyleSheet\" data-before='>'>\r\n" +
		"  <link rel=preconnect href=https://fonts.googleapis.com>\r\n" +
		"  <link href='//fonts.gstatic.com' rel='dns-prefetch'>\r\n" +
		"  <link href='//retained.example.net' rel='preconnect'>\r\n" +
		"  <script async src=" + script + "></script>\r\n" +
		"  <link rel=stylesheet href='" + unpinned + "'>\r\n" +
		"  <link rel=stylesheet href='" + otherHost + "'>\r\n" +
		"  <img src='" + image + "'>\r\n" +
		"  <!-- <script src='" + script + "'></script> -->\r\n"
	css := "/* @import url(" + stylesheet + "); */\n" +
		"@import URL( \"" + stylesheet + "\" ) screen;\n" +
		"body::before { content: \"url(" + stylesheet + ")\"; }\n" +
		"/* @import url(" + stylesheet + ");"
	pages := []string{"index.html", "trail.html", "pages/plan.htm", "pages/log.html"}
	require.NoError(t, os.Mkdir(filepath.Join(dir, "pages"), 0o755))
	for _, page := range pages {
		require.NoError(t, os.WriteFile(filepath.Join(dir, page), []byte(html), 0o644))
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pages", "styles.css"), []byte(css), 0o644))

	got, err := stdout("--all", dir, "--json")
	require.NoError(t, err)
	assert.Contains(t, got, strings.ReplaceAll(fonts, "&", "\\u0026"))
	assert.Contains(t, got, stylesheet)
	assert.Contains(t, got, script)
	assert.Contains(t, got, scriptPath)
	assert.Contains(t, got, stylesheetPath)
	assert.Contains(t, got, fontsPath)
	assert.Contains(t, got, "pages/styles.css")
	assert.Contains(t, got, "pages/log.html")
	assert.Contains(t, got, unpinned)
	assert.Contains(t, got, "exact npm version")
	assert.Contains(t, got, otherHost)
	assert.Contains(t, got, image)
	assert.Contains(t, got, "image")
	for remote, count := range requests {
		assert.EqualValues(t, 1, count.Load(), remote)
	}

	for _, page := range pages {
		prefix := ""
		if strings.HasPrefix(page, "pages/") {
			prefix = "../"
		}
		want := strings.Replace(html, fontsHTML, prefix+fontsPath, 1)
		want = strings.Replace(want, script, prefix+scriptPath, 1)
		want = strings.Replace(want, "<link rel=preconnect href=https://fonts.googleapis.com>", "", 1)
		want = strings.Replace(want, "<link href='//fonts.gstatic.com' rel='dns-prefetch'>", "", 1)
		got, err := os.ReadFile(filepath.Join(dir, page))
		require.NoError(t, err)
		assert.Equal(t, want, string(got), page)
	}
	actualCSS, err := os.ReadFile(filepath.Join(dir, "pages", "styles.css"))
	require.NoError(t, err)
	assert.Equal(t, strings.Replace(css, "\""+stylesheet, "\"../"+stylesheetPath, 1), string(actualCSS))
	for _, name := range []string{fontsPath, scriptPath, stylesheetPath} {
		require.FileExists(t, filepath.Join(dir, name))
	}

	got, err = stdout("--all", dir, "--output", "plain")
	require.NoError(t, err)
	assert.Contains(t, got, "No changes")
	assert.Contains(t, got, image)
	for remote, count := range requests {
		assert.EqualValues(t, 1, count.Load(), remote)
	}
	downloaded, err := os.ReadFile(filepath.Join(dir, stylesheetPath))
	require.NoError(t, err)
	assert.Equal(t, stylesheetBytes, string(downloaded))
	afterCSS, err := os.ReadFile(filepath.Join(dir, "pages", "styles.css"))
	require.NoError(t, err)
	assert.Equal(t, actualCSS, afterCSS)
}

func TestDesignVendorAllContinuesAfterDownloadFailure(t *testing.T) {
	const base = "https://cdnjs.cloudflare.com/ajax/libs/lib/1.2.3/"
	responses := map[string]vendorTestResponse{
		"https://api.cdnjs.com/libraries/lib/1.2.3?fields=files,sri": {status: 200, body: `{"files":["first.js","second.js","missing.js"]}`},
		base + "first.js":   {status: 200, body: "/* first */"},
		base + "second.js":  {status: 200, body: "/* second */"},
		base + "missing.js": {status: 404},
	}
	dir := vendorTestFolder(t, responses)
	source := filepath.Join(dir, "index.html")
	original := `<script src="` + base + `first.js"></script>` +
		`<script src="` + base + `second.js"></script>` +
		`<script src="` + base + `missing.js"></script>` +
		`<link rel="preconnect" href="https://cdnjs.cloudflare.com">`
	require.NoError(t, os.WriteFile(source, []byte(original), 0o644))

	got, err := runVendorTestCommand(t, dir, "--all")
	assert.NoError(t, err)
	assert.Contains(t, got, `"url": "`+base+`missing.js"`)
	assert.Contains(t, got, "returned HTTP 404")
	rewritten, err := os.ReadFile(source)
	require.NoError(t, err)
	assert.Equal(t, `<script src="vendor/cdnjs/lib/1.2.3/first.js"></script>`+
		`<script src="vendor/cdnjs/lib/1.2.3/second.js"></script>`+
		`<script src="`+base+`missing.js"></script>`, string(rewritten))

	got, err = runVendorTestCommand(t, dir, "--all")
	assert.Error(t, err)
	assert.Contains(t, got, `"url": "`+base+`missing.js"`)
	assert.Contains(t, got, "returned HTTP 404")
	assert.Contains(t, got, `"changed": false`)
	after, err := os.ReadFile(source)
	require.NoError(t, err)
	assert.Equal(t, rewritten, after)
}

func TestDesignVendorAllPreservesDesignSystemFiles(t *testing.T) {
	const script = "https://cdnjs.cloudflare.com/ajax/libs/lib/1.2.3/main.js"
	original := `<script src="` + script + `"></script>`
	dir := vendorTestFolder(t, map[string]vendorTestResponse{
		"https://api.cdnjs.com/libraries/lib/1.2.3?fields=files,sri": {status: 200, body: `{"files":["main.js"]}`},
		script: {status: 200, body: "/* script */"},
	})
	state := fmt.Sprintf(`{"kind":"website","runtime":"static","designSystemFiles":{"managed.html":"%x"}}`, sha256.Sum256([]byte(original)))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".copera", "design.json"), []byte(state), 0o600))
	managed := filepath.Join(dir, "managed.html")
	unmanaged := filepath.Join(dir, "index.html")
	for _, file := range []string{managed, unmanaged} {
		require.NoError(t, os.WriteFile(file, []byte(original), 0o644))
	}

	got, err := runVendorTestCommand(t, dir, "--all")
	require.NoError(t, err)
	assert.Contains(t, got, "managed by the Design system")
	managedBytes, err := os.ReadFile(managed)
	require.NoError(t, err)
	assert.Equal(t, original, string(managedBytes))
	unmanagedBytes, err := os.ReadFile(unmanaged)
	require.NoError(t, err)
	assert.Equal(t, `<script src="vendor/cdnjs/lib/1.2.3/main.js"></script>`, string(unmanagedBytes))
}

func TestDesignVendorAllDownloadLimits(t *testing.T) {
	const base = "https://cdnjs.cloudflare.com/ajax/libs/lib/1.2.3/"
	for _, tt := range []struct {
		name    string
		count   int
		body    string
		handled int
		reason  string
	}{
		{"distinct URLs", 33, "/* script */", 32, "32 distinct URLs"},
		{"total bytes", 5, strings.Repeat(" ", 5<<20), 3, "20 MiB download limit"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var files []string
			var original, expected strings.Builder
			responses := map[string]vendorTestResponse{}
			for i := 1; i <= tt.count; i++ {
				name := fmt.Sprintf("resource%02d.js", i)
				files = append(files, name)
				remote := base + name
				responses[remote] = vendorTestResponse{status: 200, body: tt.body}
				fmt.Fprintf(&original, `<script src="%s"></script>`, remote)
				if i <= tt.handled {
					remote = "vendor/cdnjs/lib/1.2.3/" + name
				}
				fmt.Fprintf(&expected, `<script src="%s"></script>`, remote)
			}
			metadata, err := json.Marshal(map[string][]string{"files": files})
			require.NoError(t, err)
			responses["https://api.cdnjs.com/libraries/lib/1.2.3?fields=files,sri"] = vendorTestResponse{status: 200, body: string(metadata)}
			dir := vendorTestFolder(t, responses)
			file := filepath.Join(dir, "index.html")
			require.NoError(t, os.WriteFile(file, []byte(original.String()), 0o644))

			got, err := runVendorTestCommand(t, dir, "--all")
			require.NoError(t, err)
			assert.Contains(t, got, tt.reason)
			rewritten, err := os.ReadFile(file)
			require.NoError(t, err)
			assert.Equal(t, expected.String(), string(rewritten))
		})
	}
}

func TestDesignVendorAllSuggestsPushWithMessagePlaceholder(t *testing.T) {
	dir := vendorTestFolder(t, nil)
	got, err := runVendorTestCommand(t, dir, "--all")
	require.NoError(t, err)
	var result struct {
		NextCommand string `json:"nextCommand"`
	}
	require.NoError(t, json.Unmarshal([]byte(got), &result))
	assert.Equal(t, `copera designs push --wait -m "<what this version is>" '`+dir+`'`, result.NextCommand)
}
