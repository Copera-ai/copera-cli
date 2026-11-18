package api

import (
	"context"
	"encoding/json"
	"net/http"
)

// ArtifactDesignSystem is a saved workspace design system and its pinned source.
type ArtifactDesignSystem struct {
	ID               string          `json:"id"`
	Name             string          `json:"name"`
	Kind             string          `json:"kind"`
	Available        bool            `json:"available"`
	SourceArtifactID string          `json:"sourceArtifactId,omitempty"`
	SourceVersionID  string          `json:"sourceVersionId,omitempty"`
	Spec             json.RawMessage `json:"spec,omitempty"`
}

// ArtifactDesignSystems lists the workspace's saved design systems.
func (c *Client) ArtifactDesignSystems(ctx context.Context) ([]ArtifactDesignSystem, error) {
	var result struct {
		DesignSystems []ArtifactDesignSystem `json:"designSystems"`
	}
	if err := c.do(ctx, http.MethodGet, "/artifacts/design-systems", nil, &result); err != nil {
		return nil, err
	}
	return result.DesignSystems, nil
}
