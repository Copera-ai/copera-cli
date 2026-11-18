package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeSourcePage(w http.ResponseWriter, text string, start, end int, cursor string, eof bool) {
	next := "null"
	if cursor != "" {
		next = strconv.Quote(cursor)
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = fmt.Fprintf(w, `{"artifactId":"a1","versionId":"v1","path":"big.js","mediaType":"text/javascript","contentDigest":"d","projectionDigest":"p","text":%s,"startOffset":%d,"endOffset":%d,"nextCursor":%s,"eof":%t}`,
		strconv.Quote(text), start, end, next, eof)
}

func TestArtifactSourceFileFollowsCursorUntilEOF(t *testing.T) {
	var cursors []string
	client, cleanup := testClient(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/artifacts/a1/versions/v1/source", r.URL.Path)
		assert.Equal(t, "big.js", r.URL.Query().Get("path"))
		cursor := r.URL.Query().Get("cursor")
		cursors = append(cursors, cursor)
		switch cursor {
		case "":
			writeSourcePage(w, "part one ", 0, 9, "c1", false)
		case "c1":
			writeSourcePage(w, "part two ", 9, 18, "c2", false)
		default:
			writeSourcePage(w, "end", 18, 21, "", true)
		}
	})
	defer cleanup()

	page, text, err := client.ArtifactSourceFile(context.Background(), "a1", "v1", "big.js")
	require.NoError(t, err)
	assert.Equal(t, "part one part two end", text)
	assert.True(t, page.EOF)
	assert.Equal(t, []string{"", "c1", "c2"}, cursors)
}

func TestArtifactSourceFileFallsBackToOffsetWithoutCursor(t *testing.T) {
	var offsets []string
	client, cleanup := testClient(func(w http.ResponseWriter, r *http.Request) {
		offsets = append(offsets, r.URL.Query().Get("startOffset"))
		if r.URL.Query().Get("startOffset") == "" {
			writeSourcePage(w, "abc", 0, 3, "", false)
			return
		}
		writeSourcePage(w, "def", 3, 6, "", true)
	})
	defer cleanup()

	_, text, err := client.ArtifactSourceFile(context.Background(), "a1", "v1", "big.js")
	require.NoError(t, err)
	assert.Equal(t, "abcdef", text)
	assert.Equal(t, []string{"", "3"}, offsets)
}

func TestArtifactSourceFileStopsWhenNoProgress(t *testing.T) {
	client, cleanup := testClient(func(w http.ResponseWriter, r *http.Request) {
		writeSourcePage(w, "", 0, 0, "", false)
	})
	defer cleanup()

	_, _, err := client.ArtifactSourceFile(context.Background(), "a1", "v1", "big.js")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no progress")
}

func TestArtifactHandoffSendsBudgetAndVersion(t *testing.T) {
	client, cleanup := testClient(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/artifacts/a1/handoff", r.URL.Path)
		assert.Equal(t, "0", r.URL.Query().Get("maxInlineBytes"))
		assert.Equal(t, "v9", r.URL.Query().Get("versionId"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"artifact":{"id":"a1","title":"T","kind":"website","url":"u"},"version":{"id":"v9","number":3,"createdAt":"2026-10-01T00:00:00Z"},"pages":[],"links":[],"spec":null,"notes":[],"design":{"digest":null,"files":[],"assets":[]},"files":[],"truncated":false,"nextSteps":"x"}`))
	})
	defer cleanup()

	pack, err := client.ArtifactHandoff(context.Background(), "a1", ArtifactHandoffOpts{VersionID: "v9"})
	require.NoError(t, err)
	assert.Nil(t, pack.Spec)
	assert.Nil(t, pack.Design.Digest)
	assert.Equal(t, 3, pack.Version.Number)
}
