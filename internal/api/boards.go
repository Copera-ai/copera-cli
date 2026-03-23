package api

import (
	"context"
	"fmt"
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
	ID        string      `json:"_id"`
	RowID     int         `json:"rowId"`
	Owner     string      `json:"owner"`
	Table     string      `json:"table"`
	Board     string      `json:"board"`
	CreatedAt time.Time   `json:"createdAt"`
	UpdatedAt time.Time   `json:"updatedAt"`
	Columns   []RowColumn `json:"columns"`
}

// CommentAuthor holds author details for a row comment.
type CommentAuthor struct {
	ID      string `json:"_id"`
	Name    string `json:"name"`
	Picture string `json:"picture"`
	Email   string `json:"email"`
}

// Comment is a row comment.
type Comment struct {
	ID          string        `json:"_id"`
	Content     string        `json:"content"`
	ContentType string        `json:"contentType"`
	Visibility  string        `json:"visibility"`
	Author      CommentAuthor `json:"author"`
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

// CreateCommentInput is the body for POST .../row/{rowId}/comment.
type CreateCommentInput struct {
	Content    string `json:"content"`
	Visibility string `json:"visibility,omitempty"` // internal | external
}

// ── Board methods ────────────────────────────────────────────────────────────

func (c *Client) BoardList(ctx context.Context) ([]Board, error) {
	var boards []Board
	if err := c.do(ctx, "GET", "/board/list-boards", nil, &boards); err != nil {
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

func (c *Client) TableList(ctx context.Context, boardID string) ([]Table, error) {
	var tables []Table
	if err := c.do(ctx, "GET", fmt.Sprintf("/board/%s/tables", boardID), nil, &tables); err != nil {
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

func (c *Client) RowList(ctx context.Context, boardID, tableID string) ([]Row, error) {
	var rows []Row
	if err := c.do(ctx, "GET", fmt.Sprintf("/board/%s/table/%s/rows", boardID, tableID), nil, &rows); err != nil {
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
			path += fmt.Sprintf("%s%s=%s", sep, p.k, p.v)
			sep = "&"
		}
	}
	var page CommentsPage
	if err := c.do(ctx, "GET", path, nil, &page); err != nil {
		return nil, err
	}
	return &page, nil
}
