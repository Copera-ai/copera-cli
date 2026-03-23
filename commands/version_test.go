package commands_test

import (
	"encoding/json"
	"testing"

	"github.com/copera/copera-cli/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVersionHumanOutput(t *testing.T) {
	// In tests stdout is not a TTY, so --output table forces the human path.
	res := testutil.RunCommand(t, []string{"version", "--output", "table"}, "")
	require.Equal(t, 0, res.ExitCode)
	assert.Contains(t, res.Stdout, "copera")
}

func TestVersionDefaultsToJSONWhenNotTTY(t *testing.T) {
	// No flags — auto format resolves to JSON because the test buffer is not a TTY.
	res := testutil.RunCommand(t, []string{"version"}, "")
	require.Equal(t, 0, res.ExitCode)

	var out map[string]string
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &out), "expected JSON output when stdout is not a TTY")
	assert.Contains(t, out, "version")
}

func TestVersionJSONFlag(t *testing.T) {
	res := testutil.RunCommand(t, []string{"version", "--json"}, "")
	require.Equal(t, 0, res.ExitCode)

	var out map[string]string
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &out))
	assert.Contains(t, out, "version")
	assert.Contains(t, out, "build_time")
	assert.Equal(t, "1", out["schema_version"])
}

func TestVersionOutputFlag(t *testing.T) {
	res := testutil.RunCommand(t, []string{"version", "--output", "json"}, "")
	require.Equal(t, 0, res.ExitCode)

	var out map[string]string
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &out))
	assert.Contains(t, out, "version")
}

func TestInvalidOutputFlag(t *testing.T) {
	res := testutil.RunCommand(t, []string{"version", "--output", "xml"}, "")
	assert.Equal(t, 2, res.ExitCode)
}
