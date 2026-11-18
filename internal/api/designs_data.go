package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const ArtifactDataBodyLimit = 24 * 1024

type ArtifactDataSetInput struct {
	IfRevision *int64          `json:"ifRevision"`
	Value      json.RawMessage `json:"value"`
}

type ArtifactDataDeleteInput struct {
	IfRevision int64 `json:"ifRevision"`
}

type ArtifactDataListOpts struct {
	Limit  int
	Cursor string
	Filter string
}

func artifactDataPath(id, collection string) string {
	return artifactPath(id) + "/data/" + url.PathEscape(collection)
}

func artifactDataRecordPath(id, collection, key, subject string) string {
	params := url.Values{}
	if subject != "" {
		params.Set("subject", subject)
	}
	return withQuery(artifactDataPath(id, collection)+"/records/"+url.PathEscape(key), params)
}

func (c *Client) ArtifactDataGet(ctx context.Context, id, collection, key, subject string) (json.RawMessage, error) {
	return c.doArtifactData(ctx, http.MethodGet, artifactDataRecordPath(id, collection, key, subject), nil)
}

func (c *Client) ArtifactDataList(ctx context.Context, id, collection string, opts ArtifactDataListOpts) (json.RawMessage, error) {
	params := url.Values{}
	if opts.Limit > 0 {
		params.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.Cursor != "" {
		params.Set("cursor", opts.Cursor)
	}
	if opts.Filter != "" {
		params.Set("filter", opts.Filter)
	}
	return c.doArtifactData(ctx, http.MethodGet, withQuery(artifactDataPath(id, collection)+"/records", params), nil)
}

func (c *Client) ArtifactDataSet(ctx context.Context, id, collection, key, subject string, input ArtifactDataSetInput) (json.RawMessage, error) {
	var body bytes.Buffer
	encoder := json.NewEncoder(&body)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(input); err != nil {
		return nil, fmt.Errorf("invalid Design record JSON")
	}
	return c.doArtifactData(ctx, http.MethodPost, artifactDataRecordPath(id, collection, key, subject), bytes.TrimSuffix(body.Bytes(), []byte("\n")))
}

func (c *Client) ArtifactDataBatch(ctx context.Context, id, collection string, input json.RawMessage) (json.RawMessage, error) {
	return c.doArtifactData(ctx, http.MethodPost, artifactDataPath(id, collection)+"/batch", input)
}

func (c *Client) ArtifactDataDelete(ctx context.Context, id, collection, key, subject string, input ArtifactDataDeleteInput) (json.RawMessage, error) {
	body, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	return c.doArtifactData(ctx, http.MethodDelete, artifactDataRecordPath(id, collection, key, subject), body)
}

func (c *Client) ArtifactDataWatch(ctx context.Context, id, collection, cursor string) (json.RawMessage, error) {
	params := url.Values{}
	if cursor != "" {
		params.Set("cursor", cursor)
	}
	return c.doArtifactData(ctx, http.MethodGet, withQuery(artifactDataPath(id, collection)+"/watch", params), nil)
}

// Data requests contain untrusted values, including the filter in the URL.
// Their bodies and URLs must stay out of verbose logs and transport errors.
func (c *Client) doArtifactData(ctx context.Context, method, path string, body []byte) (json.RawMessage, error) {
	if len(body) > ArtifactDataBodyLimit {
		return nil, &APIError{StatusCode: http.StatusRequestEntityTooLarge, Code: "payload_too_large", Message: "Design data request exceeds 24 KiB"}
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, fmt.Errorf("could not build Design data request")
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	// A redirect could forward a record body or filter outside the API.
	client := *c.httpClient
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("Design data request failed")
	}
	defer resp.Body.Close()
	var raw json.RawMessage
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
			return nil, fmt.Errorf("invalid Design data response")
		}
		return raw, nil
	}

	apiErr := &APIError{StatusCode: resp.StatusCode, Message: http.StatusText(resp.StatusCode)}
	var envelope struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if json.NewDecoder(resp.Body).Decode(&envelope) == nil {
		apiErr.Code = envelope.Code
		if envelope.Message != "" {
			apiErr.Message = envelope.Message
		}
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
