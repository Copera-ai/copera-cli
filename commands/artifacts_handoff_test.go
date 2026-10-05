package commands_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/copera/copera-cli/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func digestOf(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

const (
	inlineMain   = "import './app.css';\nrender();\n"
	bigPartOne   = "// big file, part one\n"
	bigPartTwo   = "export const data = [1, 2, 3];\n"
	specMarkdown = "# Spec\n\nBuild a launch site.\n"
	logoBytes    = "<svg xmlns='http://www.w3.org/2000/svg'/>"
)

type handoffFile struct {
	Path    string
	Content *string
	Omitted string
	Digest  string
}

func inline(path, content string) handoffFile {
	return handoffFile{Path: path, Content: &content, Digest: digestOf(content)}
}

func omitted(path, reason, fullContent string) handoffFile {
	return handoffFile{Path: path, Omitted: reason, Digest: digestOf(fullContent)}
}

// handoffPack builds a hand-off response. assetURL "" means the asset link
// could not be signed (null).
func handoffPack(files []handoffFile, spec map[string]any, assetURL string) map[string]any {
	fs := make([]map[string]any, 0, len(files))
	for _, f := range files {
		entry := map[string]any{"path": f.Path, "mediaType": "text/javascript", "byteLength": 10, "contentDigest": f.Digest}
		if f.Content != nil {
			entry["content"] = *f.Content
		}
		if f.Omitted != "" {
			entry["omitted"] = f.Omitted
		}
		fs = append(fs, entry)
	}
	var url any
	if assetURL != "" {
		url = assetURL
	}
	var specValue any
	if spec != nil {
		specValue = spec
	}
	return map[string]any{
		"artifact": map[string]any{"id": artID, "title": "Launch Site: v2!", "kind": "website", "url": "https://app.copera.ai/acme/artifacts/" + artID},
		"version":  map[string]any{"id": verID, "number": 4, "createdAt": "2026-10-01T10:00:00Z"},
		"pages": []map[string]any{
			{"id": "home", "title": "Home", "surface": "web", "viewport": map[string]any{"width": 1440, "height": 900}, "entrypoint": "src/main.js", "files": []string{"src/main.js"}},
			{"id": "pricing", "title": "Pricing", "surface": "web", "viewport": map[string]any{"width": 1440, "height": 900}, "entrypoint": "src/big.js", "files": []string{"src/big.js"}},
		},
		"links": []map[string]any{{"fromPageId": "home", "toPageId": "pricing"}},
		"spec":  specValue,
		"notes": []map[string]any{
			{"id": "n1", "pageId": "pricing", "body": "Annual plan shows 2 months free.", "authorKind": "user", "createdAt": "2026-10-01T11:00:00Z"},
			{"id": "n2", "pageId": "spec", "body": "Spec rule: English only.", "authorKind": "ai", "createdAt": "2026-10-01T12:00:00Z"},
			{"id": "n3", "pageId": "home", "body": "Hero video autoplays muted.", "authorKind": "ai", "createdAt": "2026-10-01T13:00:00Z"},
		},
		"design": map[string]any{
			"digest": strings.Repeat("d", 64), "tokensPath": "design/tokens.css", "files": []string{"design/tokens.css"},
			"assets": []map[string]any{{"id": "logo1", "path": "assets/logo.svg", "mediaType": "image/svg+xml", "contentDigest": digestOf(logoBytes), "usage": "logo", "url": url}},
		},
		"files":     fs,
		"truncated": true,
		"nextSteps": "Build this as a real app.",
	}
}

type handoffServer struct {
	srv          *httptest.Server
	handoffCalls atomic.Int32
	sourceCalls  atomic.Int32
	query        atomic.Value
}

// newHandoffServer serves the hand-off pack, a two-page source read for
// src/big.js, a one-page read for the Spec, and the logo asset.
func newHandoffServer(t *testing.T, pack func(base string) map[string]any) *handoffServer {
	t.Helper()
	hs := &handoffServer{}
	var base string
	hs.srv = testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /artifacts/" + artID + "/handoff": func(w http.ResponseWriter, r *http.Request) {
			hs.handoffCalls.Add(1)
			hs.query.Store(r.URL.RawQuery)
			testutil.RespondJSON(w, http.StatusOK, pack(base))
		},
		"GET /artifacts/" + artID + "/versions/" + verID + "/source": func(w http.ResponseWriter, r *http.Request) {
			hs.sourceCalls.Add(1)
			switch p := r.URL.Query().Get("path"); p {
			case "src/big.js":
				if r.URL.Query().Get("cursor") == "" {
					testutil.RespondJSON(w, http.StatusOK, sourcePageResp(p, bigPartOne, 0, len(bigPartOne), "page2", false))
					return
				}
				testutil.RespondJSON(w, http.StatusOK, sourcePageResp(p, bigPartTwo, len(bigPartOne), len(bigPartOne+bigPartTwo), "", true))
			case "docs/SPEC.md":
				testutil.RespondJSON(w, http.StatusOK, sourcePageResp(p, specMarkdown, 0, len(specMarkdown), "", true))
			default:
				t.Errorf("unexpected source read of %q", p)
				testutil.RespondError(w, http.StatusBadRequest, "NOT_FOUND", "File not found in the pinned version")
			}
		},
		"GET /assets/logo.svg": func(w http.ResponseWriter, r *http.Request) {
			assert.Empty(t, r.Header.Get("Authorization"), "the API token must not be sent to the asset link")
			_, _ = w.Write([]byte(logoBytes))
		},
	}.Handler())
	base = hs.srv.URL
	setupHome(t, hs.srv.URL)
	return hs
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

type handoffSummaryOut struct {
	Dir          string   `json:"dir"`
	ArtifactID   string   `json:"artifactId"`
	VersionID    string   `json:"versionId"`
	URL          string   `json:"url"`
	FilesWritten []string `json:"filesWritten"`
	Fetched      []string `json:"fetched"`
	Skipped      []struct {
		Path   string `json:"path"`
		Reason string `json:"reason"`
	} `json:"skipped"`
	Truncated bool     `json:"truncated"`
	Warnings  []string `json:"warnings"`
}

func fullPack(base string) map[string]any {
	return handoffPack([]handoffFile{
		inline("src/main.js", inlineMain),
		inline("design/tokens.css", ":root{}"),
		omitted("src/big.js", "over_budget", bigPartOne+bigPartTwo),
		omitted(".env.js", "sensitive", "SECRET=1"),
	}, map[string]any{"path": "docs/SPEC.md", "markdown": specMarkdown}, base+"/assets/logo.svg")
}

func TestArtifactsHandoff_WritesThePack(t *testing.T) {
	hs := newHandoffServer(t, fullPack)
	out := filepath.Join(t.TempDir(), "pack")

	res := testutil.RunCommand(t, []string{"artifacts", "handoff", artID, "--out", out, "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Equal(t, "maxInlineBytes=1048576", hs.query.Load())

	var summary handoffSummaryOut
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &summary))
	assert.Equal(t, out, summary.Dir)
	assert.Equal(t, verID, summary.VersionID)
	assert.True(t, summary.Truncated)
	assert.Equal(t, []string{"src/big.js"}, summary.Fetched)
	require.Len(t, summary.Skipped, 1)
	assert.Equal(t, ".env.js", summary.Skipped[0].Path)
	assert.Equal(t, "sensitive", summary.Skipped[0].Reason)
	assert.ElementsMatch(t, []string{
		"docs/SPEC.md", "src/main.js", "design/tokens.css", "src/big.js",
		"design/assets/logo.svg", "design/design.json", "pages.json", "NOTES.md", "HANDOFF.md",
	}, summary.FilesWritten)

	// Source at its own path; the over-budget file fetched page by page.
	assert.Equal(t, inlineMain, readFile(t, filepath.Join(out, "src", "main.js")))
	assert.Equal(t, bigPartOne+bigPartTwo, readFile(t, filepath.Join(out, "src", "big.js")))
	assert.Equal(t, specMarkdown, readFile(t, filepath.Join(out, "docs", "SPEC.md")))
	assert.Equal(t, logoBytes, readFile(t, filepath.Join(out, "design", "assets", "logo.svg")))
	assert.Equal(t, int32(2), hs.sourceCalls.Load(), "only the over-budget file is fetched, in two pages")

	// The sensitive file is never fetched nor written.
	assert.NoFileExists(t, filepath.Join(out, ".env.js"))
	assert.Contains(t, res.Stderr, ".env.js looks like it holds a key or password")

	var design struct {
		Digest     string   `json:"digest"`
		TokensPath string   `json:"tokensPath"`
		Files      []string `json:"files"`
		Assets     []struct {
			ID        string `json:"id"`
			LocalPath string `json:"localPath"`
			URL       string `json:"url"`
		} `json:"assets"`
	}
	require.NoError(t, json.Unmarshal([]byte(readFile(t, filepath.Join(out, "design", "design.json"))), &design))
	assert.Equal(t, strings.Repeat("d", 64), design.Digest)
	assert.Equal(t, "design/tokens.css", design.TokensPath)
	require.Len(t, design.Assets, 1)
	assert.Equal(t, "design/assets/logo.svg", design.Assets[0].LocalPath)
	assert.Empty(t, design.Assets[0].URL, "short-lived links are not persisted")

	var pages struct {
		Pages []struct {
			ID string `json:"id"`
		} `json:"pages"`
		Links []struct {
			FromPageID string `json:"fromPageId"`
			ToPageID   string `json:"toPageId"`
		} `json:"links"`
	}
	require.NoError(t, json.Unmarshal([]byte(readFile(t, filepath.Join(out, "pages.json"))), &pages))
	assert.Len(t, pages.Pages, 2)
	require.Len(t, pages.Links, 1)
	assert.Equal(t, "pricing", pages.Links[0].ToPageID)

	notes := readFile(t, filepath.Join(out, "NOTES.md"))
	specAt := strings.Index(notes, "## Spec")
	homeAt := strings.Index(notes, "## Home")
	pricingAt := strings.Index(notes, "## Pricing")
	require.True(t, specAt >= 0 && homeAt > specAt && pricingAt > homeAt, "Spec notes first, then pages in order:\n%s", notes)
	assert.Contains(t, notes, "### AI — 2026-10-01 12:00 UTC\n\nSpec rule: English only.")
	assert.Contains(t, notes, "### You — 2026-10-01 11:00 UTC\n\nAnnual plan shows 2 months free.")

	guide := readFile(t, filepath.Join(out, "HANDOFF.md"))
	assert.Contains(t, guide, "https://app.copera.ai/acme/artifacts/"+artID)
	assert.Contains(t, guide, "Build this as a real app.")
	assert.Contains(t, guide, "`docs/SPEC.md`")
	assert.Contains(t, guide, "`.env.js` (sensitive)")
}

func TestArtifactsHandoff_HumanOutputAndOptions(t *testing.T) {
	hs := newHandoffServer(t, fullPack)
	out := filepath.Join(t.TempDir(), "pack")

	res := testutil.RunCommand(t, []string{"artifacts", "handoff", artID, "--out", out, "--version", verID, "--max-inline-bytes", "0", "--output", "table"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Equal(t, "maxInlineBytes=0&versionId="+verID, hs.query.Load())
	assert.Contains(t, res.Stdout, "Version:   4 ("+verID+")")
	assert.Contains(t, res.Stdout, "Skipped:   .env.js (sensitive)")
	assert.Contains(t, res.Stdout, "HANDOFF.md")
}

func TestArtifactsHandoff_SpecOverBudgetIsFetched(t *testing.T) {
	newHandoffServer(t, func(base string) map[string]any {
		return handoffPack([]handoffFile{inline("src/main.js", inlineMain)},
			map[string]any{"path": "docs/SPEC.md", "omitted": "over_budget"}, base+"/assets/logo.svg")
	})
	out := filepath.Join(t.TempDir(), "pack")

	res := testutil.RunCommand(t, []string{"artifacts", "handoff", artID, "--out", out, "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Equal(t, specMarkdown, readFile(t, filepath.Join(out, "docs", "SPEC.md")))
}

func TestArtifactsHandoff_SensitiveSpecAndUnsignedAssetAreSkipped(t *testing.T) {
	newHandoffServer(t, func(string) map[string]any {
		return handoffPack([]handoffFile{inline("src/main.js", inlineMain)},
			map[string]any{"path": "docs/SPEC.md", "omitted": "sensitive"}, "")
	})
	out := filepath.Join(t.TempDir(), "pack")

	res := testutil.RunCommand(t, []string{"artifacts", "handoff", artID, "--out", out, "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	var summary handoffSummaryOut
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &summary))
	reasons := map[string]string{}
	for _, s := range summary.Skipped {
		reasons[s.Path] = s.Reason
	}
	assert.Equal(t, map[string]string{"docs/SPEC.md": "sensitive", "assets/logo.svg": "unsigned_url"}, reasons)
	assert.NoFileExists(t, filepath.Join(out, "docs", "SPEC.md"))
	assert.NoFileExists(t, filepath.Join(out, "design", "assets", "logo.svg"))
	assert.Contains(t, readFile(t, filepath.Join(out, "HANDOFF.md")), "looks like it holds a secret")
}

func TestArtifactsHandoff_RejectsUnsafePaths(t *testing.T) {
	tests := []struct {
		name  string
		files []handoffFile
		spec  map[string]any
	}{
		{"parent traversal", []handoffFile{inline("../evil.js", "x")}, nil},
		{"nested traversal", []handoffFile{inline("src/../../evil.js", "x")}, nil},
		{"absolute", []handoffFile{inline("/etc/evil", "x")}, nil},
		{"drive letter", []handoffFile{inline("C:/evil.js", "x")}, nil},
		{"backslash", []handoffFile{inline("src\\..\\..\\evil.js", "x")}, nil},
		{"spec traversal", []handoffFile{inline("src/main.js", "x")}, map[string]any{"path": "../SPEC.md", "markdown": "x"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			newHandoffServer(t, func(base string) map[string]any {
				return handoffPack(tc.files, tc.spec, base+"/assets/logo.svg")
			})
			parent := t.TempDir()
			out := filepath.Join(parent, "pack")

			res := testutil.RunCommand(t, []string{"artifacts", "handoff", artID, "--out", out, "--json"}, "")
			assert.Equal(t, 1, res.ExitCode)
			assert.Contains(t, res.Stderr, "unsafe_path")
			assert.NoDirExists(t, out, "nothing is written when the pack holds an unsafe path")
			assert.NoFileExists(t, filepath.Join(parent, "evil.js"))
		})
	}
}

func TestArtifactsHandoff_RejectsUnsafeAssetPath(t *testing.T) {
	newHandoffServer(t, func(base string) map[string]any {
		pack := handoffPack([]handoffFile{inline("src/main.js", inlineMain)}, nil, base+"/assets/logo.svg")
		design := pack["design"].(map[string]any)
		design["assets"].([]map[string]any)[0]["path"] = "../../logo.svg"
		return pack
	})
	out := filepath.Join(t.TempDir(), "pack")

	res := testutil.RunCommand(t, []string{"artifacts", "handoff", artID, "--out", out, "--json"}, "")
	assert.Equal(t, 1, res.ExitCode)
	assert.NoDirExists(t, out)
}

func TestArtifactsHandoff_RefusesNonEmptyOutUnlessForce(t *testing.T) {
	hs := newHandoffServer(t, fullPack)
	out := t.TempDir()
	keep := filepath.Join(out, "keep.txt")
	require.NoError(t, os.WriteFile(keep, []byte("mine"), 0o644))

	res := testutil.RunCommand(t, []string{"artifacts", "handoff", artID, "--out", out, "--json"}, "")
	assert.Equal(t, 5, res.ExitCode)
	assert.Contains(t, res.Stderr, "out_dir_not_empty")
	assert.Equal(t, int32(0), hs.handoffCalls.Load(), "an explicit --out is checked before calling the API")

	res = testutil.RunCommand(t, []string{"artifacts", "handoff", artID, "--out", out, "--force", "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Equal(t, "mine", readFile(t, keep))
	assert.FileExists(t, filepath.Join(out, "HANDOFF.md"))
}

func TestArtifactsHandoff_OutIsAFile(t *testing.T) {
	newHandoffServer(t, fullPack)
	out := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(out, []byte("x"), 0o644))

	res := testutil.RunCommand(t, []string{"artifacts", "handoff", artID, "--out", out, "--force", "--json"}, "")
	assert.Equal(t, 2, res.ExitCode)
}

func TestArtifactsHandoff_DefaultOutIsTitleSlug(t *testing.T) {
	newHandoffServer(t, fullPack)
	cwd := t.TempDir()
	t.Chdir(cwd)

	res := testutil.RunCommand(t, []string{"artifacts", "handoff", artID, "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.FileExists(t, filepath.Join(cwd, "launch-site-v2", "HANDOFF.md"))

	// The derived folder is now non-empty: a second run is refused.
	res = testutil.RunCommand(t, []string{"artifacts", "handoff", artID, "--json"}, "")
	assert.Equal(t, 5, res.ExitCode)
}

func TestArtifactsHandoff_SourceFileNamedLikeAGeneratedFile(t *testing.T) {
	newHandoffServer(t, func(base string) map[string]any {
		return handoffPack([]handoffFile{inline("HANDOFF.md", "the artifact's own file"), inline("design/assets/logo.svg", "<svg/>")}, nil, base+"/assets/logo.svg")
	})
	out := filepath.Join(t.TempDir(), "pack")

	res := testutil.RunCommand(t, []string{"artifacts", "handoff", artID, "--out", out, "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Equal(t, "the artifact's own file", readFile(t, filepath.Join(out, "HANDOFF.md")))
	assert.Contains(t, readFile(t, filepath.Join(out, ".copera", "HANDOFF.md")), "hand-off")
	assert.Equal(t, "<svg/>", readFile(t, filepath.Join(out, "design", "assets", "logo.svg")))
	assert.Equal(t, logoBytes, readFile(t, filepath.Join(out, "design", "assets", "logo1-logo.svg")))
}

func TestArtifactsHandoff_DigestMismatchFails(t *testing.T) {
	newHandoffServer(t, func(base string) map[string]any {
		f := inline("src/main.js", inlineMain)
		f.Digest = strings.Repeat("0", 64)
		return handoffPack([]handoffFile{f}, nil, base+"/assets/logo.svg")
	})
	out := filepath.Join(t.TempDir(), "pack")

	res := testutil.RunCommand(t, []string{"artifacts", "handoff", artID, "--out", out, "--json"}, "")
	assert.Equal(t, 1, res.ExitCode)
	assert.Contains(t, res.Stderr, "content digest")
}

func TestArtifactsHandoff_APIErrors(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		code     string
		wantExit int
	}{
		{"flag off", http.StatusForbidden, "FORBIDDEN", 4},
		{"not found", http.StatusNotFound, "NOT_FOUND", 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := testutil.NewMockServer(t, testutil.MockRoutes{
				"GET /artifacts/" + artID + "/handoff": func(w http.ResponseWriter, r *http.Request) {
					testutil.RespondError(w, tc.status, tc.code, "nope")
				},
			}.Handler())
			setupHome(t, srv.URL)
			out := filepath.Join(t.TempDir(), "pack")

			res := testutil.RunCommand(t, []string{"artifacts", "handoff", artID, "--out", out, "--json"}, "")
			assert.Equal(t, tc.wantExit, res.ExitCode)
			assert.Contains(t, res.Stderr, tc.code)
			assert.NoDirExists(t, out)
		})
	}
}

func TestArtifactsHandoff_InvalidMaxInlineBytes(t *testing.T) {
	setupHome(t, "http://127.0.0.1:1")
	for _, v := range []string{"-1", "1048577"} {
		res := testutil.RunCommand(t, []string{"artifacts", "handoff", artID, "--max-inline-bytes", v, "--out", t.TempDir()}, "")
		assert.Equal(t, 2, res.ExitCode, "value %s", v)
	}
}
