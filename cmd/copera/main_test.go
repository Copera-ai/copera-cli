package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/copera/copera-cli/internal/exitcodes"
	"github.com/copera/copera-cli/internal/output"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMainPrintsErrorOnce(t *testing.T) {
	if os.Getenv("COPERA_TEST_MAIN") == "1" {
		os.Args = append(os.Args[:1], os.Args[3:]...)
		main()
		return
	}

	const message = "unknown template, expected review"
	for _, test := range []struct {
		name, message string
		args          []string
		structured    bool
	}{
		{"JSON", message, []string{"designs", "guide", "--template", "bogus", "--json"}, true},
		{"plain", message, []string{"designs", "guide", "--template", "bogus", "--output", "plain"}, true},
		{"unformatted", `unknown format "bogus": must be auto, json, table, or plain`, []string{"version", "--output", "bogus"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			self, err := os.Executable()
			require.NoError(t, err)
			args := append([]string{"-test.run=^TestMainPrintsErrorOnce$", "--"}, test.args...)
			cmd := exec.CommandContext(t.Context(), self, args...)
			cmd.Env = append(os.Environ(), "COPERA_TEST_MAIN=1", "CI=true")
			_, err = cmd.Output()
			var exitErr *exec.ExitError
			require.ErrorAs(t, err, &exitErr)
			assert.Equal(t, exitcodes.Usage, exitErr.ExitCode())
			assert.Equal(t, 1, strings.Count(string(exitErr.Stderr), test.message), string(exitErr.Stderr))
			if test.structured {
				var reported output.StructuredError
				require.NoError(t, json.Unmarshal(exitErr.Stderr, &reported))
				assert.Equal(t, test.message, reported.Message)
			} else {
				assert.Equal(t, test.message+"\n", string(exitErr.Stderr))
			}
		})
	}
}
