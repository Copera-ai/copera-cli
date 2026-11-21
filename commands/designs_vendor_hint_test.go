package commands_test

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/copera/copera-cli/internal/api"
	"github.com/copera/copera-cli/internal/testutil"
	"github.com/stretchr/testify/assert"
)

func TestDesignBuildOutsideVendorHint(t *testing.T) {
	const hint = "Run: copera designs vendor --all\n"
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"POST /artifacts/check": func(w http.ResponseWriter, _ *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, api.ArtifactCheckResult{
				OK: false,
				ArtifactBuildFindings: api.ArtifactBuildFindings{
					Diagnostics: []api.ArtifactDiagnostic{
						{ArtifactWarning: api.ArtifactWarning{Code: "outside_resource"}, Severity: "error"},
						{ArtifactWarning: api.ArtifactWarning{Code: "outside_resource"}, Severity: "error"},
					},
				},
			})
		},
	}.Handler())
	setupHome(t, srv.URL)

	dir := designPublishFolder(t)
	file := filepath.Join(dir, "index.html")
	writePublishFixture(t, file, []byte(`<script src="https://cdn.jsdelivr.net/npm/chart.js@4.4.1/dist/chart.umd.js"></script>`))
	for _, args := range [][]string{
		{"designs", "check", dir, "--json"},
		{"designs", "push", dir, "-m", "Edit", "--json"},
	} {
		res := testutil.RunCommand(t, args, "")
		assert.Equal(t, 1, res.ExitCode, res.Stderr)
		assert.Equal(t, 1, strings.Count(res.Stderr, hint), res.Stderr)
	}
	writePublishFixture(t, file, []byte(`<script src="https://cdn.jsdelivr.net/npm/chart.js/dist/chart.umd.js"></script>`))
	res := testutil.RunCommand(t, []string{"designs", "check", dir, "--json"}, "")
	assert.Equal(t, 1, res.ExitCode, res.Stderr)
	assert.NotContains(t, res.Stderr, hint)

	const brandCSS = `@import "https://fonts.googleapis.com/css2?family=Inter";`
	writePublishFixture(t, filepath.Join(dir, ".copera", "design.json"), []byte(fmt.Sprintf(
		`{"kind":"website","runtime":"static","designSystemFiles":{"design/brand.css":%q}}`, digestOf(brandCSS))))
	writePublishFixture(t, filepath.Join(dir, "design", "brand.css"), []byte(brandCSS))
	writePublishFixture(t, file, []byte(`<link rel="stylesheet" href="design/brand.css">`))
	for _, args := range [][]string{
		{"designs", "check", dir, "--json"},
		{"designs", "push", dir, "-m", "Edit", "--wait", "--json"},
	} {
		res := testutil.RunCommand(t, args, "")
		assert.Equal(t, 1, res.ExitCode, res.Stderr)
		assert.NotContains(t, res.Stderr, hint)
	}
}
