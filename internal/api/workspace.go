package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// Workspace holds workspace metadata returned by GET /workspace/info.
type Workspace struct {
	ID          string    `json:"_id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	Picture     string    `json:"picture,omitempty"`
	Description string    `json:"description,omitempty"`
	Seats       int       `json:"seats"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// Member holds a workspace member entry.
type Member struct {
	ID        string    `json:"_id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Picture   string    `json:"picture,omitempty"`
	Title     string    `json:"title,omitempty"`
	Type      string    `json:"type"`
	Status    string    `json:"status"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"createdAt"`
}

// MembersPage is the paginated response for workspace members.
type MembersPage struct {
	Members []Member `json:"members"`
	Total   int      `json:"total"`
	Limit   int      `json:"limit"`
	Offset  int      `json:"offset"`
}

// MemberListOpts configures a workspace members list request.
type MemberListOpts struct {
	Query  string // search filter
	Limit  int    // 1–500; 0 = server default (100)
	Offset int    // pagination offset
}

// Team holds a workspace team entry.
type Team struct {
	ID           string    `json:"_id"`
	Name         string    `json:"name"`
	Picture      string    `json:"picture,omitempty"`
	Main         bool      `json:"main"`
	Participants []string  `json:"participants"`
	CreatedBy    string    `json:"createdBy"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// TeamsPage is the paginated response for workspace teams.
type TeamsPage struct {
	Teams  []Team `json:"teams"`
	Total  int    `json:"total"`
	Limit  int    `json:"limit"`
	Offset int    `json:"offset"`
}

// TeamListOpts configures a workspace teams list request.
type TeamListOpts struct {
	Query  string // search filter
	Limit  int    // 1–200; 0 = server default (100)
	Offset int    // pagination offset
}

// ── Workspace methods ───────────────────────────────────────────────────────

// WorkspaceInfo fetches the current workspace metadata.
func (c *Client) WorkspaceInfo(ctx context.Context) (*Workspace, error) {
	var ws Workspace
	if err := c.do(ctx, http.MethodGet, "/workspace/info", nil, &ws); err != nil {
		return nil, err
	}
	return &ws, nil
}

// WorkspaceMembers lists workspace members with optional search and pagination.
func (c *Client) WorkspaceMembers(ctx context.Context, opts MemberListOpts) (*MembersPage, error) {
	params := url.Values{}
	if opts.Query != "" {
		params.Set("q", opts.Query)
	}
	if opts.Limit > 0 {
		params.Set("limit", fmt.Sprintf("%d", opts.Limit))
	}
	if opts.Offset > 0 {
		params.Set("offset", fmt.Sprintf("%d", opts.Offset))
	}

	path := "/workspace/members"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var page MembersPage
	if err := c.do(ctx, http.MethodGet, path, nil, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

// WorkspaceTeams lists workspace teams with optional search and pagination.
func (c *Client) WorkspaceTeams(ctx context.Context, opts TeamListOpts) (*TeamsPage, error) {
	params := url.Values{}
	if opts.Query != "" {
		params.Set("q", opts.Query)
	}
	if opts.Limit > 0 {
		params.Set("limit", fmt.Sprintf("%d", opts.Limit))
	}
	if opts.Offset > 0 {
		params.Set("offset", fmt.Sprintf("%d", opts.Offset))
	}

	path := "/workspace/teams"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var page TeamsPage
	if err := c.do(ctx, http.MethodGet, path, nil, &page); err != nil {
		return nil, err
	}
	return &page, nil
}
