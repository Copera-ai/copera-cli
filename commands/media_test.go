package commands_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/copera/copera-cli/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const phase5DesignID = "aaaaaaaaaaaaaaaaaaaaaaaa"
const phase5OperationID = "bbbbbbbbbbbbbbbbbbbbbbbb"
const phase5FileID = "cccccccccccccccccccccccc"

func TestMediaImageRetriesAndDownloadsVerifiedResult(t *testing.T) {
	payload := "private image bytes"
	var attempts int
	var signedURL string
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"POST /media/images": func(w http.ResponseWriter, r *http.Request) {
			attempts++
			assert.Equal(t, "gen:image-1", r.Header.Get("Idempotency-Key"))
			raw, err := io.ReadAll(r.Body)
			assert.NoError(t, err)
			assert.JSONEq(t, `{"prompt":"a private prompt","size":{"width":1536,"height":1024},"references":[{"kind":"drive_file","fileId":"cccccccccccccccccccccccc"},{"kind":"design_asset","designId":"aaaaaaaaaaaaaaaaaaaaaaaa","assetId":"bbbbbbbbbbbbbbbbbbbbbbbb"}],"transparent":true,"quality":"high","design":"aaaaaaaaaaaaaaaaaaaaaaaa"}`, string(raw))
			if attempts == 1 {
				conn, _, err := w.(http.Hijacker).Hijack()
				if assert.NoError(t, err) {
					_ = conn.Close()
				}
				return
			}
			testutil.RespondJSON(w, http.StatusAccepted, map[string]any{
				"operationId": phase5OperationID, "kind": "image", "status": "queued", "requestedCount": 1, "retryAfterSeconds": 2,
			})
		},
		"GET /media/operations/" + phase5OperationID: func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"operationId": phase5OperationID, "kind": "image", "status": "succeeded", "requestedCount": 1, "design": phase5DesignID,
				"spend": map[string]int{"assessedCents": 11, "chargedCents": 11, "reservedCents": 0},
				"files": []map[string]any{{
					"driveFileId": phase5FileID, "fileName": "design-media-1.png", "mediaType": "image/png", "byteLength": len(payload),
					"sha256":   fmt.Sprintf("%x", sha256.Sum256([]byte(payload))),
					"download": map[string]string{"url": signedURL, "expiresAt": "2026-11-18T12:00:00Z"},
				}},
			})
		},
		"GET /signed-image": func(w http.ResponseWriter, r *http.Request) {
			assert.Empty(t, r.Header.Get("Authorization"))
			_, _ = io.WriteString(w, payload)
		},
	}.Handler())
	signedURL = srv.URL + "/signed-image?signature=private"
	setupHome(t, srv.URL)
	dest := filepath.Join(t.TempDir(), "image.png")

	res := testutil.RunCommand(t, []string{"media", "image", "a private prompt", "--idempotency-key", "gen:image-1",
		"--size", "1536x1024", "--reference", "drive:" + phase5FileID, "--reference", "asset:" + phase5DesignID + ":" + phase5OperationID,
		"--transparent", "--quality", "high", "--design", phase5DesignID, "-o", dest, "--verbose", "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Equal(t, 2, attempts)
	assert.Contains(t, res.Stdout, `"chargedCents": 11`)
	assert.Contains(t, res.Stderr, phase5OperationID)
	assert.NotContains(t, res.Stderr, "private prompt")
	assert.NotContains(t, res.Stderr, "signature=")
	assert.NotContains(t, res.Stderr, "driveFileId")
	data, err := os.ReadFile(dest)
	require.NoError(t, err)
	assert.Equal(t, payload, string(data))
}

func TestMediaTranscribeLocalWritesVTTAndRefusesExistingDestination(t *testing.T) {
	const vtt = "WEBVTT\n\n00:00:00.000 --> 00:00:01.000\nPrivate caption\n"
	uploads := 0
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"POST /media/transcriptions": func(w http.ResponseWriter, r *http.Request) {
			uploads++
			assert.Equal(t, "captions-1", r.Header.Get("Idempotency-Key"))
			assert.NoError(t, r.ParseMultipartForm(1024))
			assert.Equal(t, phase5DesignID, r.FormValue("design"))
			file, header, err := r.FormFile("file")
			if assert.NoError(t, err) {
				defer file.Close()
				assert.Equal(t, "voice.wav", header.Filename)
				data, err := io.ReadAll(file)
				assert.NoError(t, err)
				assert.Equal(t, "signed audio fixture", string(data))
			}
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"operationId": phase5OperationID, "kind": "transcription", "status": "succeeded",
				"requestedCount": 0, "files": []string{},
				"transcript": map[string]any{"durationMs": 1000, "text": "Private caption", "vtt": vtt, "segments": []string{}},
			})
		},
	}.Handler())
	setupHome(t, srv.URL)
	dir := t.TempDir()
	voice, dest := filepath.Join(dir, "voice.wav"), filepath.Join(dir, "voice.vtt")
	require.NoError(t, os.WriteFile(voice, []byte("signed audio fixture"), 0o600))
	args := []string{"media", "transcribe", voice, "--idempotency-key", "captions-1", "--design", phase5DesignID, "-o", dest, "--output", "plain"}

	res := testutil.RunCommand(t, args, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	data, err := os.ReadFile(dest)
	require.NoError(t, err)
	assert.Equal(t, vtt, string(data))
	assert.NotContains(t, res.Stdout, "Private caption")

	res = testutil.RunCommand(t, args, "")
	assert.Equal(t, 2, res.ExitCode, "stderr: %s", res.Stderr)
	assert.Equal(t, 1, uploads)
}

func TestMediaStatusPreservesErrorAndRejectsUnverifiedOutput(t *testing.T) {
	var signedURL string
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"GET /media/operations/" + phase5OperationID: func(w http.ResponseWriter, r *http.Request) {
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"operationId": phase5OperationID, "kind": "speech", "status": "failed", "requestedCount": 1,
				"error": map[string]string{"code": "design_attachment_failed", "message": "Could not attach output."},
				"files": []map[string]any{{
					"fileName": "speech.wav", "byteLength": 7, "sha256": fmt.Sprintf("%x", sha256.Sum256([]byte("correct"))),
					"download": map[string]string{"url": signedURL},
				}},
			})
		},
		"GET /signed-audio": func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "corrupt") },
	}.Handler())
	signedURL = srv.URL + "/signed-audio"
	setupHome(t, srv.URL)
	dest := filepath.Join(t.TempDir(), "audio.wav")

	res := testutil.RunCommand(t, []string{"media", "status", phase5OperationID, "--out", dest, "--json"}, "")
	assert.Equal(t, 1, res.ExitCode)
	assert.Contains(t, res.Stdout, `"code": "design_attachment_failed"`)
	assert.Contains(t, res.Stderr, "SHA-256")
	_, err := os.Lstat(dest)
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestMediaSpeechPreservesHTTPErrorCodeAndExit(t *testing.T) {
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"POST /media/speech": func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "narrate", r.Header.Get("Idempotency-Key"))
			body, err := io.ReadAll(r.Body)
			assert.NoError(t, err)
			assert.JSONEq(t, `{"text":"Explain","language":"en","voice":"inworld-english-narrator","format":"opus"}`, string(body))
			testutil.RespondJSON(w, http.StatusPaymentRequired, map[string]string{
				"code": "media_daily_limit_reached", "message": "Daily limit reached.",
			})
		},
	}.Handler())
	setupHome(t, srv.URL)

	res := testutil.RunCommand(t, []string{"media", "speech", "Explain", "--idempotency-key", "narrate", "--language", "en", "--voice", "inworld-english-narrator", "--format", "opus"}, "")
	assert.Equal(t, 1, res.ExitCode)
	var errorBody struct {
		Error string `json:"error"`
	}
	require.NoError(t, json.Unmarshal([]byte(res.Stderr), &errorBody))
	assert.Equal(t, "media_daily_limit_reached", errorBody.Error)
}

func TestMediaSpeechNarrationLength(t *testing.T) {
	narration := strings.Repeat("Word. ", 8333) + "Go"
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"POST /media/speech": func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Text string `json:"text"`
			}
			if assert.NoError(t, json.NewDecoder(r.Body).Decode(&body)) {
				assert.True(t, body.Text == narration, "the complete narration must reach the API")
			}
			testutil.RespondJSON(w, http.StatusPaymentRequired, map[string]string{
				"code": "media_daily_limit_reached", "message": "Daily limit reached.",
			})
		},
	}.Handler())
	setupHome(t, srv.URL)

	accepted := testutil.RunCommand(t, []string{"media", "speech", narration, "--language", "en", "--idempotency-key", "long-speech", "--json"}, "")
	assert.Contains(t, accepted.Stderr, "media_daily_limit_reached")
	rejected := testutil.RunCommand(t, []string{"media", "speech", narration + "!", "--language", "en", "--idempotency-key", "oversize-speech", "--json"}, "")
	assert.Contains(t, rejected.Stderr, "50000 UTF-16 code units")
}

func TestMediaSpeechRecordsExactProjectAssetPin(t *testing.T) {
	const payload = "verified Ogg bytes"
	const assetPath = "assets/narration.ogg"
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(payload)))
	var signedURL string
	srv := testutil.NewMockServer(t, testutil.MockRoutes{
		"POST /media/speech": func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "speech:pin-1", r.Header.Get("Idempotency-Key"))
			testutil.RespondJSON(w, http.StatusOK, map[string]any{
				"operationId": phase5OperationID, "kind": "speech", "status": "succeeded", "design": phase5DesignID,
				"requestedCount": 1, "files": []map[string]any{{
					"driveFileId": phase5FileID, "fileName": "speech.opus", "mediaType": "audio/ogg", "byteLength": len(payload),
					"sha256": digest, "download": map[string]string{"url": signedURL}, "asset": map[string]any{
						"id": phase5FileID, "path": assetPath, "mediaType": "audio/ogg", "byteLength": len(payload), "contentDigest": digest,
					},
				}},
			})
		},
		"GET /signed-speech": func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, payload) },
	}.Handler())
	signedURL = srv.URL + "/signed-speech"
	setupHome(t, srv.URL)
	project := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(project, ".copera"), 0o700))
	stateFile := filepath.Join(project, ".copera", "design.json")
	require.NoError(t, os.WriteFile(stateFile, []byte(`{"id":"`+phase5DesignID+`"}`), 0o600))
	dest := filepath.Join(project, filepath.FromSlash(assetPath))

	res := testutil.RunCommand(t, []string{"media", "speech", "Explain", "--language", "en", "--format", "opus",
		"--design", phase5DesignID, "--idempotency-key", "speech:pin-1", "--out", dest, "--json"}, "")
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	content, err := os.ReadFile(dest)
	require.NoError(t, err)
	assert.Equal(t, payload, string(content))
	state, err := os.ReadFile(stateFile)
	require.NoError(t, err)
	var saved struct {
		PendingAssets map[string]struct {
			ID     string `json:"id"`
			Digest string `json:"contentDigest"`
			Usage  string `json:"usage"`
		} `json:"pendingAssets"`
	}
	require.NoError(t, json.Unmarshal(state, &saved))
	pin := saved.PendingAssets[assetPath]
	assert.Equal(t, phase5FileID, pin.ID)
	assert.Equal(t, digest, pin.Digest)
	assert.Equal(t, "reference", pin.Usage, "Phase 2 checks audio pins as reference assets")
}

func TestPublicCommandsRejectWrongArityBeforeStartup(t *testing.T) {
	for _, args := range [][]string{
		{"media", "image"},
		{"designs", "link", phase5DesignID},
		{"channels", "message", "list", phase5DesignID, phase5FileID},
	} {
		res := testutil.RunCommand(t, append(args, "--json"), "")
		assert.Equal(t, 2, res.ExitCode)
		assert.Contains(t, res.Stderr, `"error":"invalid_request"`)
		assert.Empty(t, res.Stdout)
	}
}
