package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Doc holds document metadata returned by GET /docs/{id}.
type Doc struct {
	ID        string    `json:"_id"`
	Title     string    `json:"title"`
	Icon      any       `json:"icon,omitempty"`  // {"type":"icon","value":":gear:"} or nil
	Cover     any       `json:"cover,omitempty"` // {"type":"color","value":"..."} or nil
	ParentID  string    `json:"parent,omitempty"`
	OwnerID   string    `json:"owner,omitempty"`
	Starred   bool      `json:"starred"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// DocNode is a Doc entry in a tree response.
// Children are fully hydrated recursive DocNode entries.
type DocNode struct {
	ID          string    `json:"_id"`
	Title       string    `json:"title"`
	ParentID    string    `json:"parent,omitempty"`
	OwnerID     string    `json:"owner,omitempty"`
	HasChildren bool      `json:"hasChildren"`
	Children    []DocNode `json:"children,omitempty"`
	Starred     bool      `json:"starred"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type treeResponse struct {
	Root      []DocNode `json:"root"`
	TotalDocs int       `json:"totalDocs"`
	Truncated bool      `json:"truncated"`
}

// SearchHighlight holds the matched snippet from a search hit.
type SearchHighlight struct {
	Title  string `json:"title"`
	MdBody string `json:"mdBody"`
}

// SearchParent is an ancestor entry in a search hit.
type SearchParent struct {
	ID    string `json:"_id"`
	Title string `json:"title"`
}

// SearchHit is one result from docs search.
// CreatedAt/UpdatedAt come as millisecond-epoch strings from the search API.
type SearchHit struct {
	ID        string          `json:"_id"`
	Title     string          `json:"title"`
	Parents   []SearchParent  `json:"parents"`
	Highlight SearchHighlight `json:"highlight"`
	CreatedAt string          `json:"createdAt"` // millisecond epoch string
	UpdatedAt string          `json:"updatedAt"` // millisecond epoch string
}

// UpdatedAtTime parses the millisecond epoch string into a time.Time.
func (h SearchHit) UpdatedAtTime() time.Time {
	ms, err := strconv.ParseInt(h.UpdatedAt, 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.UnixMilli(ms)
}

// SearchOpts configures a docs search request.
type SearchOpts struct {
	SortBy    string // createdAt | updatedAt
	SortOrder string // asc | desc
	Limit     int    // 1–50; 0 = server default
}

// DocTree fetches documents at the given level.
// parentID="" returns workspace root docs.
// parentID=<id> returns that specific node's view with child stubs (use len(Children) for count).
// The API returns one level at a time — use --parent to drill into subtrees.
func (c *Client) DocTree(ctx context.Context, parentID string) ([]DocNode, error) {
	params := url.Values{}
	if parentID != "" {
		params.Set("parentId", parentID)
	}
	path := "/docs/tree"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var resp treeResponse
	if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Root, nil
}

// DocSearch searches documents by query.
func (c *Client) DocSearch(ctx context.Context, q string, opts SearchOpts) ([]SearchHit, error) {
	params := url.Values{"q": {q}}
	if opts.SortBy != "" {
		params.Set("sortBy", opts.SortBy)
	}
	if opts.SortOrder != "" {
		params.Set("sortOrder", opts.SortOrder)
	}
	if opts.Limit > 0 {
		params.Set("limit", fmt.Sprintf("%d", opts.Limit))
	}

	var resp struct {
		Hits      []SearchHit `json:"hits"`
		TotalHits int         `json:"totalHits"`
	}
	if err := c.do(ctx, http.MethodGet, "/docs/search?"+params.Encode(), nil, &resp); err != nil {
		return nil, err
	}
	return resp.Hits, nil
}

// DocGet fetches document metadata.
func (c *Client) DocGet(ctx context.Context, id string) (*Doc, error) {
	var doc Doc
	if err := c.do(ctx, http.MethodGet, "/docs/"+id, nil, &doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

// DocContent fetches the markdown content of a document.
func (c *Client) DocContent(ctx context.Context, id string) (string, error) {
	var resp struct {
		Content string `json:"content"`
	}
	if err := c.do(ctx, http.MethodGet, "/docs/"+id+"/md", nil, &resp); err != nil {
		return "", err
	}
	return resp.Content, nil
}

// DocUpdateContent sends a content update request (server processes async, returns 202).
func (c *Client) DocUpdateContent(ctx context.Context, id, operation, content string) error {
	return c.do(ctx, http.MethodPost, "/docs/"+id+"/md", map[string]string{
		"operation": operation,
		"content":   content,
	}, nil)
}

// DocCreate creates a new document.
func (c *Client) DocCreate(ctx context.Context, title, parentID, content string) (*Doc, error) {
	body := map[string]string{"title": title}
	if parentID != "" {
		body["parent"] = parentID
	}
	if content != "" {
		body["content"] = content
	}
	var doc Doc
	if err := c.do(ctx, http.MethodPost, "/docs/", body, &doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

// DocUpdateMeta updates document metadata fields (title, icon, cover).
func (c *Client) DocUpdateMeta(ctx context.Context, id string, updates map[string]string) (*Doc, error) {
	var doc Doc
	if err := c.do(ctx, http.MethodPatch, "/docs/"+id, updates, &doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

// DocDelete soft-deletes a document. Only the owner can delete.
func (c *Client) DocDelete(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/docs/"+id, nil, nil)
}
