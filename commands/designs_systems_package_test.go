package commands

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/copera/copera-cli/internal/api"
	"github.com/stretchr/testify/require"
)

func TestDesignSystemCanonicalEncoding(t *testing.T) {
	data, err := os.ReadFile("testdata/design-system-encoding.json")
	require.NoError(t, err)
	var fixture struct {
		Kit       *api.ArtifactSystemKit
		Files     []designSystemManifestFile
		Canonical string
		Digest    string
	}
	require.NoError(t, json.Unmarshal(data, &fixture))
	canonical, err := canonicalSystemManifest(designSystemManifestData{SchemaVersion: 1, Kit: fixture.Kit, Files: fixture.Files})
	require.NoError(t, err)
	require.Equal(t, fixture.Canonical, string(canonical))
	require.Equal(t, fixture.Digest, sha256Hex(canonical))
}
