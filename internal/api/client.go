// Package api provides a typed HTTP client for the Copera public API.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"strconv"
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
	StatusCode        int
	Code              string `json:"code"`
	Message           string `json:"error"`
	Reason            string
	ChangedPaths      []string
	CurrentRevision   int
	RetryAfterSeconds int
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

// DoJSON executes an authenticated JSON request against a public API path.
// It is intended for newer API resources whose response shapes are deliberately
// open-ended while the server contracts are still evolving.
func (c *Client) DoJSON(ctx context.Context, method, path string, body, out any) error {
	return c.do(ctx, method, path, body, out)
}

// UploadFile executes an authenticated multipart upload using the form field
// name "file". The caller is responsible for enforcing resource-specific size
// limits before calling this method.
func (c *Client) UploadFile(ctx context.Context, path, filePath string, out any) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("api: open upload: %w", err)
	}
	defer file.Close()

	body, err := c.uploadMultipart(ctx, path, filepath.Base(filePath), "application/octet-stream", file, nil)
	if err != nil {
		return err
	}
	if out != nil && len(body) > 0 {
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("api: decode response: %w", err)
		}
	}
	return nil
}

func (c *Client) uploadMultipart(ctx context.Context, path, filename, mediaType string, file io.Reader, metadata []byte) ([]byte, error) {
	var payload bytes.Buffer
	writer := multipart.NewWriter(&payload)
	if metadata != nil {
		part, err := writer.CreatePart(textproto.MIMEHeader{
			"Content-Disposition": {`form-data; name="metadata"`},
			"Content-Type":        {"application/json"},
		})
		if err != nil {
			return nil, fmt.Errorf("api: create multipart metadata: %w", err)
		}
		if _, err := part.Write(metadata); err != nil {
			return nil, fmt.Errorf("api: write multipart metadata: %w", err)
		}
	}
	part, err := writer.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": {multipart.FileContentDisposition("file", filename)},
		"Content-Type":        {mediaType},
	})
	if err != nil {
		return nil, fmt.Errorf("api: create multipart field: %w", err)
	}
	if _, err := io.Copy(part, file); err != nil {
		return nil, fmt.Errorf("api: read upload: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("api: close multipart body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, &payload)
	if err != nil {
		return nil, fmt.Errorf("api: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("api: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("api: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		apiErr := &APIError{StatusCode: resp.StatusCode}
		var errBody struct {
			Code              string `json:"code"`
			Error             string `json:"error"`
			Message           string `json:"message"`
			RetryAfterSeconds int    `json:"retryAfterSeconds"`
		}
		if json.Unmarshal(respBody, &errBody) == nil {
			apiErr.Code = errBody.Code
			apiErr.RetryAfterSeconds = errBody.RetryAfterSeconds
			apiErr.Message = errBody.Error
			if apiErr.Message == "" {
				apiErr.Message = errBody.Message
			}
		}
		if apiErr.Message == "" {
			apiErr.Message = http.StatusText(resp.StatusCode)
		}
		if retryAfter := resp.Header.Get("Retry-After"); retryAfter != "" {
			if seconds, err := strconv.ParseUint(retryAfter, 10, 31); err == nil {
				apiErr.RetryAfterSeconds = int(seconds)
			} else if at, err := http.ParseTime(retryAfter); err == nil {
				apiErr.RetryAfterSeconds = int(min(max(0, at.Unix()-time.Now().Unix()), 1<<31-1))
			}
		}
		return nil, apiErr
	}
	return respBody, nil
}

// Download executes an authenticated GET request for a binary response.
// Unlike do(), successful responses leave the body open for the caller to
// stream. Non-2xx responses are decoded into APIError using the JSON error
// shape used by the public API.
func (c *Client) Download(ctx context.Context, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("api: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "*/*")

	if c.verbose != nil {
		fmt.Fprintf(c.verbose, ">> %s %s\n", http.MethodGet, req.URL.String())
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("api: %w", err)
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if c.verbose != nil {
			fmt.Fprintf(c.verbose, "<< %d %s\n", resp.StatusCode, http.StatusText(resp.StatusCode))
		}
		return resp, nil
	}

	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("api: read response: %w", err)
	}

	if c.verbose != nil {
		fmt.Fprintf(c.verbose, "<< %d %s\n", resp.StatusCode, http.StatusText(resp.StatusCode))
		fmt.Fprintf(c.verbose, "<< %s\n", string(respBody))
	}

	apiErr := &APIError{StatusCode: resp.StatusCode}
	var errBody struct {
		Code    string `json:"code"`
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal(respBody, &errBody) == nil {
		apiErr.Code = errBody.Code
		apiErr.Message = errBody.Error
		if apiErr.Message == "" {
			apiErr.Message = errBody.Message
		}
	}
	if apiErr.Message == "" {
		apiErr.Message = http.StatusText(resp.StatusCode)
	}
	return nil, apiErr
}

// do executes a request, retrying up to 2 extra times on 429 with exponential backoff.
func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	return c.doBounded(ctx, method, path, body, out, 0)
}

func (c *Client) doBounded(ctx context.Context, method, path string, body, out any, limit int64) error {
	backoff := 3 * time.Second
	for attempt := 0; ; attempt++ {
		err := c.doOnce(ctx, method, path, body, out, limit)
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

type multipartRequest struct {
	data        []byte
	contentType string
}

func (c *Client) doOnce(ctx context.Context, method, path string, body, out any, limit int64) error {
	var bodyReader io.Reader
	contentType := "application/json"
	if multipart, ok := body.(multipartRequest); ok {
		bodyReader = bytes.NewReader(multipart.data)
		contentType = multipart.contentType
	} else if body != nil {
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
		req.Header.Set("Content-Type", contentType)
	}

	if c.verbose != nil {
		fmt.Fprintf(c.verbose, ">> %s %s\n", method, req.URL.String())
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("api: %w", err)
	}
	defer resp.Body.Close()

	// 204 No Content is the only success status that never has a body.
	// 202 Accepted may carry a queued-job response.
	if resp.StatusCode == http.StatusNoContent {
		if c.verbose != nil {
			fmt.Fprintf(c.verbose, "<< %d %s\n", resp.StatusCode, http.StatusText(resp.StatusCode))
		}
		return nil
	}

	var reader io.Reader = resp.Body
	if limit > 0 {
		reader = io.LimitReader(reader, limit+1)
	}
	respBody, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("api: read response: %w", err)
	}

	if limit > 0 && int64(len(respBody)) > limit {
		return fmt.Errorf("api: Design system response exceeds the allowed size")
	}
	if c.verbose != nil {
		fmt.Fprintf(c.verbose, "<< %d %s\n", resp.StatusCode, http.StatusText(resp.StatusCode))
		if limit == 0 {
			fmt.Fprintf(c.verbose, "<< %s\n", string(respBody))
		}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		apiErr := &APIError{StatusCode: resp.StatusCode}
		type errorDetails struct {
			Code              string   `json:"code"`
			Reason            string   `json:"reason"`
			ChangedPaths      []string `json:"changedPaths"`
			Paths             []string `json:"paths"`
			CurrentRevision   int      `json:"currentRevision"`
			RetryAfterSeconds int      `json:"retryAfterSeconds"`
		}
		var errBody struct {
			errorDetails
			Error   string        `json:"error"`
			Message string        `json:"message"`
			Info    *errorDetails `json:"info"`
		}
		if json.Unmarshal(respBody, &errBody) == nil {
			details := errBody.errorDetails
			if errBody.Info != nil && errBody.Info.Code != "" {
				details = *errBody.Info
			}
			apiErr.Code = details.Code
			apiErr.Reason = details.Reason
			apiErr.ChangedPaths = details.ChangedPaths
			if len(apiErr.ChangedPaths) == 0 {
				apiErr.ChangedPaths = details.Paths
			}
			apiErr.CurrentRevision = details.CurrentRevision
			apiErr.RetryAfterSeconds = details.RetryAfterSeconds
			apiErr.Message = errBody.Error
			if apiErr.Message == "" {
				apiErr.Message = errBody.Message
			}
		}
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
