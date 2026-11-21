package api

import (
	"context"
	"net/http"
)

type ArtifactSourceSelection struct {
	StartMs    *int64   `json:"startMs,omitempty"`
	EndMs      *int64   `json:"endMs,omitempty"`
	MessageIDs []string `json:"messageIds,omitempty"`
}

type ArtifactSourceTarget struct {
	Kind      string                   `json:"kind"`
	BoardID   string                   `json:"boardId,omitempty"`
	TableID   string                   `json:"tableId,omitempty"`
	RowID     string                   `json:"rowId,omitempty"`
	NoteID    string                   `json:"noteId,omitempty"`
	ChannelID string                   `json:"channelId,omitempty"`
	DocID     string                   `json:"docId,omitempty"`
	FileID    string                   `json:"fileId,omitempty"`
	Selection *ArtifactSourceSelection `json:"selection,omitempty"`
}

type ArtifactSourceLink struct {
	ID       string               `json:"id"`
	Target   ArtifactSourceTarget `json:"target"`
	Label    string               `json:"label"`
	Href     string               `json:"href"`
	LinkedAt string               `json:"linkedAt"`
}

type ArtifactSourceInput struct {
	Target ArtifactSourceTarget `json:"target"`
}

type ArtifactSourceResult struct {
	Source ArtifactSourceLink `json:"source"`
}

type ArtifactSourcesResult struct {
	Sources []ArtifactSourceLink `json:"sources"`
}

func (c *Client) ArtifactLinkSource(ctx context.Context, id string, target ArtifactSourceTarget) (*ArtifactSourceResult, error) {
	var result ArtifactSourceResult
	if err := c.do(ctx, http.MethodPost, "/artifacts/"+id+"/sources", ArtifactSourceInput{Target: target}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) ArtifactListSources(ctx context.Context, id string) (*ArtifactSourcesResult, error) {
	var result ArtifactSourcesResult
	if err := c.do(ctx, http.MethodGet, "/artifacts/"+id+"/sources", nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) ArtifactUnlinkSource(ctx context.Context, id, sourceLinkID string) error {
	return c.do(ctx, http.MethodDelete, "/artifacts/"+id+"/sources/"+sourceLinkID, nil, nil)
}
