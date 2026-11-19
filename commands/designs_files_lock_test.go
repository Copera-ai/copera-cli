package commands

import (
	"os"
	"testing"

	"github.com/copera/copera-cli/internal/config"
	"github.com/stretchr/testify/require"
)

func TestDesignCheckDoesNotWriteLockedState(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	require.NoError(t, err)
	defer root.Close()
	state := &designState{ID: "design", BaseVersionID: "base"}
	require.NoError(t, writeDesignState(root, state))

	files := map[string]designFile{"index.html": newLocalDesignFile("index.html", []byte("new"))}
	cfg := &config.Config{}
	digest, err := designCheckDigest(cfg, state, files)
	require.NoError(t, err)
	release, err := lockDesignState(root, true)
	require.NoError(t, err)
	defer release()

	require.ErrorIs(t, recordDesignCheck(root, cfg, files, digest, true), errDesignStateBusy)
	saved, err := loadDesignState(root)
	require.NoError(t, err)
	require.Nil(t, saved.LastCheck)
}
