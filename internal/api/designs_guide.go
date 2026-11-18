package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
)

// ArtifactGuide is Copera's authoring guide and its machine-readable facts.
type ArtifactGuide struct {
	Version  int             `json:"version"`
	Kind     string          `json:"kind"`
	Runtime  string          `json:"runtime"`
	Markdown string          `json:"markdown"`
	Facts    json.RawMessage `json:"facts"`
}

func (c *Client) ArtifactGuide(ctx context.Context, kind, runtime string) (*ArtifactGuide, error) {
	params := url.Values{}
	if kind != "" {
		params.Set("kind", kind)
	}
	if runtime != "" {
		params.Set("runtime", runtime)
	}
	var guide ArtifactGuide
	if err := c.do(ctx, http.MethodGet, withQuery("/artifacts/guide", params), nil, &guide); err != nil {
		return nil, err
	}
	return &guide, nil
}
