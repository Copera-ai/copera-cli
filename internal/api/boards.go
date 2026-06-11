package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// Board represents a Copera board (base).
type Board struct {
	ID          string    `json:"_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// ColumnOption is a selectable value for STATUS, DROPDOWN, or LABELS columns.
type ColumnOption struct {
	OptionID    string `json:"optionId"`
	Label       string `json:"label"`
	Color       string `json:"color"`
	Order       int    `json:"order"`
	StatusGroup string `json:"statusGroup,omitempty"` // TODO | IN_PROGRESS | DONE — STATUS only
}

// Column describes a table column.
type Column struct {
	ColumnID string         `json:"columnId"`
	Label    string         `json:"label"`
	Type     string         `json:"type"`
	Order    int            `json:"order"`
	Options  []ColumnOption `json:"options,omitempty"`
}

// Table represents a table within a board, including column definitions.
type Table struct {
	ID        string    `json:"_id"`
	Name      string    `json:"name"`
	Board     string    `json:"board"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	Columns   []Column  `json:"columns"`
}

// LookupValue holds a foreign row reference in a lookup column.
type LookupValue struct {
	ForeignRowID    string `json:"foreignRowId"`
	ForeignRowValue any    `json:"foreignRowValue"`
}

// RowColumn holds the value of one column in a row.
type RowColumn struct {
	ColumnID    string        `json:"columnId"`
	Value       any           `json:"value"`
	LinkValue   any           `json:"linkValue,omitempty"`
	LookupValue []LookupValue `json:"lookupValue,omitempty"`
}

// Row represents a single row in a table.
type Row struct {
	ID          string      `json:"_id"`
	RowID       int         `json:"rowId"`
	Owner       string      `json:"owner"`
	Table       string      `json:"table"`
	Board       string      `json:"board"`
	Description string      `json:"description,omitempty"`
	CreatedAt   time.Time   `json:"createdAt"`
	UpdatedAt   time.Time   `json:"updatedAt"`
	Columns     []RowColumn `json:"columns"`
}

// CommentAuthor holds author details for a row comment.
type CommentAuthor struct {
	ID      string `json:"_id"`
	Name    string `json:"name"`
	Picture string `json:"picture"`
	Email   string `json:"email"`
}

// Attachment is metadata for a file attached to a row comment.
type Attachment struct {
	FileID   string `json:"fileId"`
	Name     string `json:"name"`
	MimeType string `json:"mimeType"`
	Size     int64  `json:"size"`
}

// Comment is a row comment.
type Comment struct {
	ID          string        `json:"_id"`
	Content     string        `json:"content"`
	ContentType string        `json:"contentType"`
	Visibility  string        `json:"visibility"`
	Author      CommentAuthor `json:"author"`
	Attachment  *Attachment   `json:"attachment,omitempty"`
	CreatedAt   time.Time     `json:"createdAt"`
	UpdatedAt   time.Time     `json:"updatedAt"`
}

// CommentsPage is the paginated response for row comments.
type CommentsPage struct {
	Items    []Comment `json:"items"`
	PageInfo struct {
		EndCursor       *string `json:"endCursor"`
		StartCursor     *string `json:"startCursor"`
		HasNextPage     bool    `json:"hasNextPage"`
		HasPreviousPage bool    `json:"hasPreviousPage"`
	} `json:"pageInfo"`
}

// CreateRowInput is the body for POST /board/{boardId}/table/{tableId}/row.
type CreateRowInput struct {
	Description string `json:"description,omitempty"`
	Columns     []struct {
		ColumnID string `json:"columnId"`
		Value    any    `json:"value"`
	} `json:"columns"`
}

// UpdateRowInput is the body for PATCH /board/{boardId}/table/{tableId}/row/{rowId}.
type UpdateRowInput struct {
	Columns []struct {
		ColumnID string `json:"columnId"`
		Value    any    `json:"value"`
	} `json:"columns"`
}

// AuthenticateRowInput is the body for POST .../row/authenticate.
type AuthenticateRowInput struct {
	IdentifierColumnID    string `json:"identifierColumnId"`
	IdentifierColumnValue string `json:"identifierColumnValue"`
	PasswordColumnID      string `json:"passwordColumnId"`
	PasswordColumnValue   string `json:"passwordColumnValue"`
}

// CreateCommentInput is the body for POST .../row/{rowId}/comment.
type CreateCommentInput struct {
	Content    string `json:"content"`
	Visibility string `json:"visibility,omitempty"` // internal | external
}

// ── Board methods ────────────────────────────────────────────────────────────

// BoardListOptions configures GET /board/list-boards.
type BoardListOptions struct {
	Query string
}

func (c *Client) BoardList(ctx context.Context, opts *BoardListOptions) ([]Board, error) {
	path := "/board/list-boards"
	if opts != nil && opts.Query != "" {
		q := url.Values{}
		q.Set("q", opts.Query)
		path += "?" + q.Encode()
	}

	var boards []Board
	if err := c.do(ctx, "GET", path, nil, &boards); err != nil {
		return nil, err
	}
	return boards, nil
}

func (c *Client) BoardGet(ctx context.Context, boardID string) (*Board, error) {
	var b Board
	if err := c.do(ctx, "GET", fmt.Sprintf("/board/%s", boardID), nil, &b); err != nil {
		return nil, err
	}
	return &b, nil
}

// ── Table methods ────────────────────────────────────────────────────────────

// TableListOptions configures GET /board/{boardId}/tables.
type TableListOptions struct {
	Query string
}

func (c *Client) TableList(ctx context.Context, boardID string, opts *TableListOptions) ([]Table, error) {
	path := fmt.Sprintf("/board/%s/tables", boardID)
	if opts != nil && opts.Query != "" {
		q := url.Values{}
		q.Set("q", opts.Query)
		path += "?" + q.Encode()
	}

	var tables []Table
	if err := c.do(ctx, "GET", path, nil, &tables); err != nil {
		return nil, err
	}
	return tables, nil
}

func (c *Client) TableGet(ctx context.Context, boardID, tableID string) (*Table, error) {
	var t Table
	if err := c.do(ctx, "GET", fmt.Sprintf("/board/%s/table/%s", boardID, tableID), nil, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

// ── Row methods ──────────────────────────────────────────────────────────────

// RowListOptions configures filtering and sorting for RowList.
//
// Filter is a JSON-shaped filter (the CLI passes through whatever the user
// provided; the API validates it). Sort is the raw "col:asc,col:desc" spec.
// Both are optional.
type RowListOptions struct {
	Query  string
	Filter string
	Sort   string
}

func (c *Client) RowList(ctx context.Context, boardID, tableID string, opts *RowListOptions) ([]Row, error) {
	path := fmt.Sprintf("/board/%s/table/%s/rows", boardID, tableID)
	if opts != nil {
		q := url.Values{}
		if opts.Query != "" {
			q.Set("q", opts.Query)
		}
		if opts.Filter != "" {
			q.Set("filter", opts.Filter)
		}
		if opts.Sort != "" {
			q.Set("sort", opts.Sort)
		}
		if encoded := q.Encode(); encoded != "" {
			path = path + "?" + encoded
		}
	}

	var rows []Row
	if err := c.do(ctx, "GET", path, nil, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

func (c *Client) RowGet(ctx context.Context, boardID, tableID, rowID string) (*Row, error) {
	var r Row
	path := fmt.Sprintf("/board/%s/table/%s/row/%s", boardID, tableID, rowID)
	if err := c.do(ctx, "GET", path, nil, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (c *Client) RowCreate(ctx context.Context, boardID, tableID string, input *CreateRowInput) (*Row, error) {
	var r Row
	path := fmt.Sprintf("/board/%s/table/%s/row", boardID, tableID)
	if err := c.do(ctx, "POST", path, input, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (c *Client) RowUpdate(ctx context.Context, boardID, tableID, rowID string, input *UpdateRowInput) (*Row, error) {
	var r Row
	path := fmt.Sprintf("/board/%s/table/%s/row/%s", boardID, tableID, rowID)
	if err := c.do(ctx, "PATCH", path, input, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (c *Client) RowDelete(ctx context.Context, boardID, tableID, rowID string) error {
	path := fmt.Sprintf("/board/%s/table/%s/row/%s", boardID, tableID, rowID)
	return c.do(ctx, "DELETE", path, nil, nil)
}

func (c *Client) RowAuthenticate(ctx context.Context, boardID, tableID string, input *AuthenticateRowInput) (*Row, error) {
	var r Row
	path := fmt.Sprintf("/board/%s/table/%s/row/authenticate", boardID, tableID)
	if err := c.do(ctx, "POST", path, input, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// RowUpdateDescription sends a description update request (server processes async, returns 202).
func (c *Client) RowUpdateDescription(ctx context.Context, boardID, tableID, rowID, operation, content string) error {
	path := fmt.Sprintf("/board/%s/table/%s/row/%s/md", boardID, tableID, rowID)
	return c.do(ctx, http.MethodPost, path, map[string]string{
		"operation": operation,
		"content":   content,
	}, nil)
}

// RowDescription fetches the markdown source of a row's description.
func (c *Client) RowDescription(ctx context.Context, boardID, tableID, rowID string) (string, error) {
	var resp struct {
		Content string `json:"content"`
	}
	path := fmt.Sprintf("/board/%s/table/%s/row/%s/md", boardID, tableID, rowID)
	if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return "", err
	}
	return resp.Content, nil
}

// RowColumnContent fetches the markdown content of a RICH TEXT / DESCRIPTION column cell on a row.
// Returns an empty string when the cell has no content.
func (c *Client) RowColumnContent(ctx context.Context, boardID, tableID, rowID, columnID string) (string, error) {
	var resp struct {
		Content string `json:"content"`
	}
	path := fmt.Sprintf("/board/%s/table/%s/row/%s/column/%s/md", boardID, tableID, rowID, columnID)
	if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return "", err
	}
	return resp.Content, nil
}

// RowUpdateColumnContent updates a RICH TEXT / DESCRIPTION column cell on a row (server
// processes async, returns 202). operation is one of replace|append|prepend.
func (c *Client) RowUpdateColumnContent(ctx context.Context, boardID, tableID, rowID, columnID, operation, content string) error {
	path := fmt.Sprintf("/board/%s/table/%s/row/%s/column/%s/md", boardID, tableID, rowID, columnID)
	return c.do(ctx, http.MethodPost, path, map[string]string{
		"operation": operation,
		"content":   content,
	}, nil)
}

func (c *Client) RowAttachmentDownload(ctx context.Context, boardID, tableID, rowID, columnID, fileID string) (*http.Response, error) {
	path := fmt.Sprintf("/board/%s/table/%s/row/%s/column/%s/file/%s/download", boardID, tableID, rowID, columnID, fileID)
	return c.Download(ctx, path)
}

// ── Comment methods ──────────────────────────────────────────────────────────

func (c *Client) CommentCreate(ctx context.Context, boardID, tableID, rowID string, input *CreateCommentInput) (*Comment, error) {
	var cmt Comment
	path := fmt.Sprintf("/board/%s/table/%s/row/%s/comment", boardID, tableID, rowID)
	if err := c.do(ctx, "POST", path, input, &cmt); err != nil {
		return nil, err
	}
	return &cmt, nil
}

func (c *Client) CommentList(ctx context.Context, boardID, tableID, rowID string, after, before, visibility string) (*CommentsPage, error) {
	path := fmt.Sprintf("/board/%s/table/%s/row/%s/comments", boardID, tableID, rowID)
	sep := "?"
	for _, p := range []struct{ k, v string }{
		{"after", after}, {"before", before}, {"visibility", visibility},
	} {
		if p.v != "" {
			path += fmt.Sprintf("%s%s=%s", sep, p.k, url.QueryEscape(p.v))
			sep = "&"
		}
	}
	var page CommentsPage
	if err := c.do(ctx, "GET", path, nil, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

func (c *Client) CommentAttachmentDownload(ctx context.Context, boardID, tableID, rowID, commentID, fileID string) (*http.Response, error) {
	path := fmt.Sprintf("/board/%s/table/%s/row/%s/comment/%s/file/%s/download", boardID, tableID, rowID, commentID, fileID)
	return c.Download(ctx, path)
}
