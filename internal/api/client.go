// Package api provides a typed HTTP client for the Copera public API.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/copera/copera-cli/internal/exitcodes"
)

// Client is an HTTP wrapper around the Copera public API.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
	verbose    io.Writer // if non-nil, log request/response details here
}

// New creates a Client. timeout=0 defaults to 30s.
func New(baseURL, token string, timeout time.Duration) *Client {
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		httpClient: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return fmt.Errorf("too many redirects")
				}
				// Do not forward auth headers on cross-host redirects
				if req.URL.Host != via[0].URL.Host {
					req.Header.Del("Authorization")
				}
				return nil
			},
		},
	}
}

// APIError is returned when the server responds with a non-2xx status.
type APIError struct {
	StatusCode int
	Code       string `json:"code"`
	Message    string `json:"error"`
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("%s: %s", e.Code, e.Message)
	}
	return e.Message
}

// ExitCode maps the HTTP status to a CLI exit code.
func (e *APIError) ExitCode() int {
	switch e.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return exitcodes.AuthFailure
	case http.StatusNotFound:
		return exitcodes.NotFound
	case http.StatusConflict:
		return exitcodes.Conflict
	case http.StatusTooManyRequests:
		return exitcodes.RateLimit
	default:
		return exitcodes.Error
	}
}

// SetVerbose enables request/response logging to the given writer (typically stderr).
func (c *Client) SetVerbose(w io.Writer) { c.verbose = w }

// HTTPClient returns the underlying http.Client for direct HTTP operations
// (e.g., uploading file parts to S3 presigned URLs).
func (c *Client) HTTPClient() *http.Client {
	return c.httpClient
}

// do executes a request, retrying up to 2 extra times on 429 with exponential backoff.
func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	backoff := 3 * time.Second
	for attempt := 0; ; attempt++ {
		err := c.doOnce(ctx, method, path, body, out)
		if err == nil {
			return nil
		}
		apiErr, ok := err.(*APIError)
		if !ok || apiErr.StatusCode != http.StatusTooManyRequests || attempt >= 2 {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
			backoff *= 2
		}
	}
}

func (c *Client) doOnce(ctx context.Context, method, path string, body, out any) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("api: marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("api: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	if c.verbose != nil {
		fmt.Fprintf(c.verbose, ">> %s %s\n", method, req.URL.String())
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("api: %w", err)
	}
	defer resp.Body.Close()

	// 204 No Content and 202 Accepted are success with no body
	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusAccepted {
		if c.verbose != nil {
			fmt.Fprintf(c.verbose, "<< %d %s\n", resp.StatusCode, http.StatusText(resp.StatusCode))
		}
		return nil
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("api: read response: %w", err)
	}

	if c.verbose != nil {
		fmt.Fprintf(c.verbose, "<< %d %s\n", resp.StatusCode, http.StatusText(resp.StatusCode))
		fmt.Fprintf(c.verbose, "<< %s\n", string(respBody))
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		apiErr := &APIError{StatusCode: resp.StatusCode}
		_ = json.Unmarshal(respBody, apiErr)
		if apiErr.Message == "" {
			apiErr.Message = http.StatusText(resp.StatusCode)
		}
		return apiErr
	}

	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("api: decode response: %w", err)
		}
	}
	return nil
}
