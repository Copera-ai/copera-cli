package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/copera/copera-cli/internal/api"
	"github.com/copera/copera-cli/internal/output"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type designWatchTransport func(*http.Request) (*http.Response, error)

func (f designWatchTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestDesignsDataWatchRetryAndInterrupt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		var stdout, stderr bytes.Buffer
		cli := &CLI{Printer: output.New(output.FormatJSON, &stdout, &stderr, false)}
		client := api.New("https://api.example", "owner-token", time.Minute)
		checks := []struct {
			at         time.Duration
			cursor     string
			status     int
			retryAfter string
			body       string
		}{
			{0, "", 200, "", `{"dataNotice":"<notice> & data","changed":false,"cursor":"start"}`},
			{3 * time.Second, "start", 429, "8", `{"code":"rate_limit_exceeded","message":"Wait"}`},
			{11 * time.Second, "start", 200, "", `{"dataNotice":"notice","changed":false,"cursor":"start"}`},
			{14 * time.Second, "start", 200, "", `{"dataNotice":"notice","changed":true,"cursor":"new"}`},
		}
		i, start := 0, time.Now()
		client.HTTPClient().Transport = designWatchTransport(func(request *http.Request) (*http.Response, error) {
			assert.Equal(t, http.MethodGet, request.Method)
			if i == len(checks) {
				assert.Equal(t, "new", request.URL.Query().Get("cursor"))
				cancel()
				return nil, request.Context().Err()
			}
			check := checks[i]
			i++
			assert.Equal(t, check.at, time.Since(start))
			assert.Equal(t, check.cursor, request.URL.Query().Get("cursor"))
			header := http.Header{}
			header.Set("Retry-After", check.retryAfter)
			return &http.Response{StatusCode: check.status, Header: header, Body: io.NopCloser(strings.NewReader(check.body))}, nil
		})

		require.NoError(t, watchDesignData(ctx, cli, client, "id", "answers", "", false))
		require.Equal(t, len(checks), i)
		lines := strings.Split(strings.TrimSuffix(stdout.String(), "\n"), "\n")
		require.Len(t, lines, 2)
		for _, line := range lines {
			assert.True(t, json.Valid([]byte(line)))
		}
		assert.Equal(t, checks[0].body, lines[0])
		assert.JSONEq(t, checks[3].body, lines[1])
		assert.Empty(t, stderr.String())
	})
}
