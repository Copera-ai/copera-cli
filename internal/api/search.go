package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// GlobalSearchHit is a single result from the global search endpoint.
// All entity types share a flat struct with omitempty on optional fields;
// the EntityType field discriminates which fields are populated.
//
// Timestamps are int64 epoch milliseconds (Meilisearch stores numeric timestamps),
// unlike workspace endpoints which use ISO strings.
type GlobalSearchHit struct {
	// Common fields (all entity types).
	EntityType string `json:"entityType"`
	ID         string `json:"_id"`
	Workspace  string `json:"workspace,omitempty"`
	CreatedAt  int64  `json:"createdAt"`
	UpdatedAt  int64  `json:"updatedAt"`

	// document
	Title  string `json:"title,omitempty"`
	MdBody string `json:"mdBody,omitempty"`
	Owner  string `json:"owner,omitempty"`

	// channel
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Type        string `json:"type,omitempty"`
	Kind        string `json:"kind,omitempty"`
	CreatedBy   string `json:"createdBy,omitempty"`

	// channelMessage
	Content     string `json:"content,omitempty"`
	Author      string `json:"author,omitempty"`
	Channel     string `json:"channel,omitempty"`
	ContentType string `json:"contentType,omitempty"`

	// todoItem
	Completed bool  `json:"completed,omitempty"`
	List      string `json:"list,omitempty"`
	DueDate   int64  `json:"dueDate,omitempty"`

	// driveContent (Type field is shared with channel)
	MimeType string `json:"mimeType,omitempty"`
	Size     int64  `json:"size,omitempty"`
	User     string `json:"user,omitempty"`

	// voiceTranscription
	Transcription string `json:"transcription,omitempty"`
	ChatSession   string `json:"chatSession,omitempty"`

	// aiChat
	LastMessageAt int64 `json:"lastMessageAt,omitempty"`

	// aiChatMessage
	Query     string `json:"query,omitempty"`
	Reasoning string `json:"reasoning,omitempty"`
	Chat      string `json:"chat,omitempty"`
	Role      string `json:"role,omitempty"`
}

// DisplayTitle returns the best human-readable display string for the hit
// based on its entity type.
func (h GlobalSearchHit) DisplayTitle() string {
	switch h.EntityType {
	case "document", "voiceTranscription", "aiChat":
		if h.Title != "" {
			return h.Title
		}
	case "channel":
		if h.Name != "" {
			return h.Name
		}
	case "todo":
		if h.Name != "" {
			return h.Name
		}
	case "todoItem":
		if h.Title != "" {
			return h.Title
		}
	case "driveContent":
		if h.Name != "" {
			return h.Name
		}
	case "channelMessage", "aiChatMessage":
		if h.Content != "" {
			if len(h.Content) > 80 {
				return h.Content[:80] + "..."
			}
			return h.Content
		}
	}
	return h.ID
}

// UpdatedAtTime converts the epoch millisecond timestamp to time.Time.
func (h GlobalSearchHit) UpdatedAtTime() time.Time {
	if h.UpdatedAt == 0 {
		return time.Time{}
	}
	return time.UnixMilli(h.UpdatedAt)
}

// GlobalSearchOpts configures a global search request.
type GlobalSearchOpts struct {
	Types     []string // entity type filter; sent as CSV
	SortBy    string   // createdAt | updatedAt
	SortOrder string   // asc | desc
	Limit     int      // 1–100; 0 = server default (50)
}

// GlobalSearchResult holds the full response from the global search endpoint.
type GlobalSearchResult struct {
	Query            string            `json:"query"`
	TotalHits        int               `json:"totalHits"`
	ProcessingTimeMs int               `json:"processingTimeMs"`
	Hits             []GlobalSearchHit `json:"hits"`
}

// GlobalSearch performs a cross-entity search across the workspace.
func (c *Client) GlobalSearch(ctx context.Context, q string, opts GlobalSearchOpts) (*GlobalSearchResult, error) {
	params := url.Values{"q": {q}}
	if len(opts.Types) > 0 {
		params.Set("types", strings.Join(opts.Types, ","))
	}
	if opts.SortBy != "" {
		params.Set("sortBy", opts.SortBy)
	}
	if opts.SortOrder != "" {
		params.Set("sortOrder", opts.SortOrder)
	}
	if opts.Limit > 0 {
		params.Set("limit", fmt.Sprintf("%d", opts.Limit))
	}

	var result GlobalSearchResult
	if err := c.do(ctx, http.MethodGet, "/search?"+params.Encode(), nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
