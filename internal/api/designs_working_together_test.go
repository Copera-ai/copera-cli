package api

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDesignCommentWatchReturnsRetryAfterWithoutRetry(t *testing.T) {
	client := New("https://api.example", "tok_test_fake", time.Second)
	polls := 0
	client.httpClient.Transport = designContractTransport(func(r *http.Request) (*http.Response, error) {
		polls++
		assert.Equal(t, "Bearer tok_test_fake", r.Header.Get("Authorization"))
		assert.Equal(t, "baseline", r.URL.Query().Get("after"))
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header:     http.Header{"Retry-After": []string{"17"}},
			Body:       io.NopCloser(strings.NewReader(`{"statusCode":429,"error":"Too Many Requests","retryAfter":3}`)),
		}, nil
	})

	_, err := client.ArtifactCommentWatch(context.Background(), "aaaaaaaaaaaaaaaaaaaaaaaa", "baseline")
	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusTooManyRequests, apiErr.StatusCode)
	assert.Equal(t, 17, apiErr.RetryAfterSeconds)
	assert.Equal(t, 1, polls)
}
