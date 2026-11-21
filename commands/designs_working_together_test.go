package commands_test

import (
	"encoding/json"
	"net/http"
	"os"
	"sync/atomic"
	"testing"

	"github.com/copera/copera-cli/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const commentID = "dddddddddddddddddddddddd"
const commentsPath = "/artifacts/" + artID + "/comments"

func TestDesignCommentsListReplyAndResolve(t *testing.T) {
	const state = `{"id":"` + artID + `"}`
	comment := map[string]any{
		"id": commentID, "artifactId": artID, "versionId": verID, "versionNumber": 1,
		"pageId": "cover", "body": "Field\x1b[31m Notes", "anchor": map[string]any{"label": "Heading"},
		"resolvedAt": nil, "resolvedById": nil,
	}
	reply := map[string]string{"id": ver2ID, "body": "Done", "authorId": artID}
	server := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET " + commentsPath: func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "cover", r.URL.Query().Get("pageId"))
			assert.Equal(t, verID, r.URL.Query().Get("versionId"))
			assert.Equal(t, "false", r.URL.Query().Get("openFromEarlierVersions"))
			assert.Equal(t, "2", r.URL.Query().Get("limit"))
			assert.Equal(t, "previous", r.URL.Query().Get("after"))
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"threads": []map[string]any{comment}, "versionId": verID, "nextCursor": "continue",
			})
		},
		"POST " + commentsPath + "/" + commentID + "/replies": func(w http.ResponseWriter, r *http.Request) {
			var body map[string]string
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			assert.Equal(t, map[string]string{"body": "Done", "idempotencyKey": "same-reply"}, body)
			testutil.RespondJSON(w, http.StatusCreated, map[string]any{"reply": reply})
		},
		"POST " + commentsPath + "/" + commentID + "/resolve": func(w http.ResponseWriter, r *http.Request) {
			var body map[string]string
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			assert.Equal(t, map[string]string{}, body)
			resolution := map[string]any{
				"id": commentID, "artifactId": artID, "versionId": verID, "versionNumber": 1,
				"pageId": "cover", "resolvedById": artID, "resolvedAt": "2026-11-18T01:00:00Z",
				"replies": []map[string]string{reply},
			}
			testutil.RespondJSON(w, http.StatusOK, map[string]any{"comment": resolution})
		},
	}.Handler())
	setupHome(t, server.URL)
	t.Chdir(t.TempDir())
	require.NoError(t, os.Mkdir(".copera", 0o700))
	require.NoError(t, os.WriteFile(".copera/design.json", []byte(state), 0o600))

	list := testutil.RunCommand(t, []string{"designs", "comments", "list", "--version-id", verID, "--page", "cover", "--include-earlier=false", "--limit", "2", "--after", "previous", "--output", "plain", "--verbose"}, "")
	require.Equal(t, 0, list.ExitCode, list.Stderr)
	assert.Contains(t, list.Stdout, "untrusted workspace data")
	assert.Contains(t, list.Stdout, "--after continue")
	assert.NotContains(t, list.Stdout, "\x1b")
	assert.NotContains(t, list.Stderr, "Field", "verbose output must not dump untrusted response content")

	for range 2 {
		result := testutil.RunCommand(t, []string{"artifacts", "comments", "reply", commentID, " Done ", "--idempotency-key", "same-reply", "--json"}, "")
		require.Equal(t, 0, result.ExitCode, result.Stderr)
		assert.Contains(t, result.Stdout, `"id": "`+ver2ID+`"`)
	}

	resolved := testutil.RunCommand(t, []string{"designs", "comments", "resolve", commentID, "--artifact", "https://app.copera.ai/design/" + artID, "--json"}, "")
	require.Equal(t, 0, resolved.ExitCode, resolved.Stderr)
	assert.Contains(t, resolved.Stdout, `"resolvedById": "`+artID+`"`)
	assert.Contains(t, resolved.Stdout, `"resolvedAt": "2026-11-18T01:00:00Z"`)
	saved, err := os.ReadFile(".copera/design.json")
	require.NoError(t, err)
	assert.Equal(t, state, string(saved))
}

func TestDesignCommentOpaqueNotFound(t *testing.T) {
	server := testutil.NewMockServer(t, testutil.MockRoutes{
		"POST " + commentsPath + "/" + commentID + "/resolve": func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusBadRequest, map[string]string{"code": "NOT_FOUND", "message": "Design comment not found"})
		},
	}.Handler())
	setupHome(t, server.URL)
	result := testutil.RunCommand(t, []string{"designs", "comments", "resolve", commentID, "--artifact", artID, "--json"}, "")
	assert.Equal(t, 3, result.ExitCode, result.Stderr)
	assert.Empty(t, result.Stdout)
}

func TestDesignWatchWaitsForNewPin(t *testing.T) {
	var polls atomic.Int32
	server := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET " + commentsPath + "/watch": func(w http.ResponseWriter, r *http.Request) {
			if polls.Add(1) == 1 {
				assert.Empty(t, r.URL.Query())
				testutil.RespondJSON(w, http.StatusOK, map[string]any{"cursor": "baseline", "comment": nil})
				return
			}
			assert.Equal(t, "baseline", r.URL.Query().Get("after"))
			testutil.RespondJSON(w, http.StatusOK, map[string]any{"cursor": "next-root", "comment": map[string]any{
				"id": commentID, "artifactId": artID, "versionId": verID, "pageId": "cover",
				"body": "Change this heading to Field Notes", "anchor": map[string]string{"selector": "h1"},
			}})
		},
	}.Handler())
	setupHome(t, server.URL)
	result := testutil.RunCommand(t, []string{"designs", "watch", artID, "--until", "comment", "--timeout", "5s", "--json"}, "")
	require.Equal(t, 0, result.ExitCode, result.Stderr)
	assert.Equal(t, "Watching for a new comment on "+artID+".\n", result.Stderr)
	assert.Equal(t, int32(2), polls.Load())
	assert.Contains(t, result.Stdout, `"id": "`+commentID+`"`)
	assert.Contains(t, result.Stdout, `"selector": "h1"`)
	assert.Contains(t, result.Stdout, `"cursor": "next-root"`)
}

func TestDesignWatchTimeoutAndRevocation(t *testing.T) {
	server := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET " + commentsPath + "/watch": func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Query().Get("after") {
			case "revoked":
				testutil.RespondJSON(w, http.StatusBadRequest, map[string]string{"code": "NOT_FOUND", "message": "Design not found"})
			default:
				testutil.RespondJSON(w, http.StatusOK, map[string]any{"cursor": "baseline", "comment": nil})
			}
		},
	}.Handler())
	setupHome(t, server.URL)
	for _, tc := range []struct {
		cursor, diagnostic string
		code               int
	}{
		{"", "timed out", 1},
		{"revoked", "NOT_FOUND", 3},
	} {
		result := testutil.RunCommand(t, []string{"designs", "watch", artID, "--until", "comment", "--after", tc.cursor, "--timeout", "1s", "--json"}, "")
		assert.Equal(t, tc.code, result.ExitCode, result.Stderr)
		assert.Contains(t, result.Stderr, tc.diagnostic)
		assert.Empty(t, result.Stdout)
	}
}

func TestDesignAskConsentAndReceipt(t *testing.T) {
	var reads, writes atomic.Int32
	server := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /artifacts/" + artID: func(w http.ResponseWriter, r *http.Request) {
			reads.Add(1)
			artifact := artifactFixture()
			artifact["currentDraftVersionId"] = ver2ID
			testutil.RespondJSON(w, http.StatusOK, artifact)
		},
		"POST /artifacts/" + artID + "/authoring-requests": func(w http.ResponseWriter, r *http.Request) {
			writes.Add(1)
			var body map[string]any
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			assert.Equal(t, map[string]any{
				"baseVersionId": ver2ID, "intent": "Shorten the heading", "pageId": "cover",
				"idempotencyKey": "ask-key", "confirmAiCredits": true,
			}, body)
			testutil.RespondJSON(w, http.StatusAccepted, map[string]any{"spendsAiCredits": true, "request": map[string]any{
				"id": commentID, "artifactId": artID, "baseVersionId": ver2ID, "state": "queued",
				"queuePosition": 1, "changedPageIds": []string{}, "requestedAt": "2026-11-18T01:00:00Z",
			}})
		},
	}.Handler())
	setupHome(t, server.URL)
	t.Setenv("CI", "true")
	t.Chdir(t.TempDir())
	require.NoError(t, os.Mkdir(".copera", 0o700))
	require.NoError(t, os.WriteFile(".copera/design.json", []byte(`{"id":"`+artID+`"}`), 0o600))
	args := []string{"designs", "ask", " Shorten the heading ", "--artifact", artID, "--page", "cover", "--idempotency-key", "ask-key", "--json"}
	withoutConsent := testutil.RunCommand(t, args, "yes\n")
	assert.Equal(t, 2, withoutConsent.ExitCode, withoutConsent.Stderr)
	assert.Contains(t, withoutConsent.Stderr, "This request uses your workspace's AI credits when Copera AI runs it.")
	assert.Empty(t, withoutConsent.Stdout)
	assert.Zero(t, writes.Load())
	assert.Zero(t, reads.Load())

	queued := testutil.RunCommand(t, append(args, "--yes"), "")
	require.Equal(t, 0, queued.ExitCode, queued.Stderr)
	assert.Equal(t, int32(1), reads.Load())
	assert.Equal(t, int32(1), writes.Load())
	assert.Contains(t, queued.Stderr, "This request uses your workspace's AI credits when Copera AI runs it.")
	assert.Contains(t, queued.Stdout, `"id": "`+commentID+`"`)
	assert.Contains(t, queued.Stdout, `"spendsAiCredits": true`)

	emptyTarget := testutil.RunCommand(t, []string{
		"designs", "ask", "Shorten the heading", "--artifact=", "--page", "cover", "--idempotency-key", "ask-key", "--yes", "--json",
	}, "")
	assert.Equal(t, 2, emptyTarget.ExitCode, emptyTarget.Stderr)
	assert.Empty(t, emptyTarget.Stdout)
	assert.Equal(t, int32(1), reads.Load(), "an explicit empty target must not use the linked Design")
	assert.Equal(t, int32(1), writes.Load())
}

func TestDesignAskPrintsReplayInputsAfterAmbiguousResponse(t *testing.T) {
	const intent = "fixture-only intent"
	for _, status := range []int{http.StatusBadGateway, http.StatusAccepted} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			keys := make(chan string, 1)
			server := testutil.NewMockServer(t, testutil.MockRoutes{
				"GET /artifacts/" + artID: func(w http.ResponseWriter, r *http.Request) {
					testutil.RespondJSON(w, http.StatusOK, artifactFixture())
				},
				"POST /artifacts/" + artID + "/authoring-requests": func(w http.ResponseWriter, r *http.Request) {
					var body struct {
						IdempotencyKey string `json:"idempotencyKey"`
					}
					assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
					keys <- body.IdempotencyKey
					if status == http.StatusAccepted {
						testutil.RespondJSON(w, status, map[string]bool{"spendsAiCredits": false})
						return
					}
					w.WriteHeader(status)
				},
			}.Handler())
			setupHome(t, server.URL)
			result := testutil.RunCommand(t, []string{"designs", "ask", intent, "--artifact", artID, "--yes", "--json"}, "")
			require.Equal(t, 1, result.ExitCode, result.Stderr)
			assert.Empty(t, result.Stdout)
			select {
			case key := <-keys:
				assert.Regexp(t, `^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`, key)
				assert.Contains(t, result.Stderr, key)
			default:
				t.Fatal("expected the authoring POST")
			}
			assert.Contains(t, result.Stderr, verID)
			assert.NotContains(t, result.Stderr, intent)
		})
	}
}
