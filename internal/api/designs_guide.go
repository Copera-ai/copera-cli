package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// ArtifactGuide is Copera's authoring guide and its machine-readable facts.
type ArtifactGuide struct {
	Version  int                    `json:"version"`
	Kind     string                 `json:"kind"`
	Runtime  string                 `json:"runtime"`
	Markdown string                 `json:"markdown"`
	Facts    json.RawMessage        `json:"facts"`
	Template *ArtifactGuideTemplate `json:"template,omitempty"`
}

// ArtifactGuideTemplate is the optional starting project for the resolved Design kind.
type ArtifactGuideTemplate struct {
	ID      string                  `json:"id"`
	Subject string                  `json:"subject"`
	Project ArtifactProjectSettings `json:"project"`
	Files   []ArtifactContentFile   `json:"files"`
}

func (c *Client) ArtifactGuide(ctx context.Context, kind, runtime, template string) (*ArtifactGuide, error) {
	return c.artifactGuide(ctx, kind, runtime, nil, template)
}

func (c *Client) ArtifactGuideTopic(ctx context.Context, topic string) (*ArtifactGuide, error) {
	return c.artifactGuide(ctx, "", "", &topic, "")
}

func (c *Client) artifactGuide(ctx context.Context, kind, runtime string, topic *string, template string) (*ArtifactGuide, error) {
	params := url.Values{}
	if topic != nil {
		params.Set("topic", *topic)
	}
	if kind != "" {
		params.Set("kind", kind)
	}
	if runtime != "" {
		params.Set("runtime", runtime)
	}
	if template != "" {
		params.Set("template", template)
	}
	var guide ArtifactGuide
	if err := c.do(ctx, http.MethodGet, withQuery("/artifacts/guide", params), nil, &guide); err != nil {
		return nil, err
	}
	if template != "" && (guide.Template == nil || guide.Template.ID != template) {
		return nil, fmt.Errorf("the API did not return the requested Design template")
	}
	return &guide, nil
}
