package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
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
	Offset *int
	Limit  *int
}

// RowsPage is returned only when offset or limit is explicitly provided.
// Omitting both retains the public API's legacy bare-array response.
type RowsPage struct {
	Rows    []Row `json:"rows"`
	Total   int   `json:"total"`
	Offset  int   `json:"offset"`
	Limit   int   `json:"limit"`
	HasMore bool  `json:"hasMore"`
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
		if opts.Offset != nil {
			q.Set("offset", fmt.Sprintf("%d", *opts.Offset))
		}
		if opts.Limit != nil {
			q.Set("limit", fmt.Sprintf("%d", *opts.Limit))
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

// RowListPage opts into the paginated envelope response. At least one of
// Offset or Limit must be non-nil.
func (c *Client) RowListPage(ctx context.Context, boardID, tableID string, opts *RowListOptions) (*RowsPage, error) {
	if opts == nil || (opts.Offset == nil && opts.Limit == nil) {
		return nil, fmt.Errorf("row pagination requires offset or limit")
	}
	path := fmt.Sprintf("/board/%s/table/%s/rows", boardID, tableID)
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
	if opts.Offset != nil {
		q.Set("offset", fmt.Sprintf("%d", *opts.Offset))
	}
	if opts.Limit != nil {
		q.Set("limit", fmt.Sprintf("%d", *opts.Limit))
	}
	path += "?" + q.Encode()
	var page RowsPage
	if err := c.do(ctx, http.MethodGet, path, nil, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

func (c *Client) RowGet(ctx context.Context, boardID, tableID, rowID string) (*Row, error) {
	var r Row
	path := fmt.Sprintf("/board/%s/table/%s/row/%s", boardID, tableID, rowID)
	if err := c.do(ctx, "GET", path, nil, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (c *Client) RowGetByRowID(ctx context.Context, boardID, tableID string, rowID int) (*Row, error) {
	var r Row
	path := fmt.Sprintf("/board/%s/table/%s/row-number/%d", boardID, tableID, rowID)
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

// ── Board write methods ──────────────────────────────────────────────────────

// CreateBoardInput is the body for POST /board.
type CreateBoardInput struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	TableName   string                 `json:"tableName,omitempty"`
	Columns     []BoardBlueprintColumn `json:"columns,omitempty"`
}

// BoardBlueprintColumn describes a custom column created with a board's
// initial table. The API validates which formats/options apply to each type.
type BoardBlueprintColumn struct {
	Label   string              `json:"label"`
	Type    string              `json:"type"`
	Format  string              `json:"format,omitempty"`
	Options []BoardColumnOption `json:"options,omitempty"`
}

type BoardColumnOption struct {
	Label string `json:"label"`
	Color string `json:"color,omitempty"`
}

// CreatedBoard is the response for POST /board.
type CreatedBoard struct {
	ID             string `json:"_id"`
	Name           string `json:"name"`
	Description    string `json:"description,omitempty"`
	DefaultTableID string `json:"defaultTableId,omitempty"`
	Columns        any    `json:"columns,omitempty"`
}

// UpdateBoardInput is the body for PATCH /board/{boardId}. Fields are optional.
type UpdateBoardInput struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	Icon        *string `json:"icon,omitempty"`
}

// UpdatedBoard is the response for PATCH /board/{boardId}.
type UpdatedBoard struct {
	ID          string `json:"_id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// DeletedResponse is the shape returned by board/table/column/file delete endpoints.
type DeletedResponse struct {
	Success bool `json:"success"`
}

// AddParticipantsInput is the body for POST /board/{boardId}/participants.
type AddParticipantsInput struct {
	ParticipantIDs []string `json:"participantIds"`
}

// BoardParticipants is the response for POST /board/{boardId}/participants.
type BoardParticipants struct {
	BoardID            string   `json:"boardId"`
	ParticipantUserIDs []string `json:"participantUserIds"`
}

func (c *Client) BoardCreate(ctx context.Context, input *CreateBoardInput) (*CreatedBoard, error) {
	var b CreatedBoard
	if err := c.do(ctx, http.MethodPost, "/board", input, &b); err != nil {
		return nil, err
	}
	return &b, nil
}

func (c *Client) BoardUpdate(ctx context.Context, boardID string, input *UpdateBoardInput) (*UpdatedBoard, error) {
	var b UpdatedBoard
	if err := c.do(ctx, http.MethodPatch, fmt.Sprintf("/board/%s", boardID), input, &b); err != nil {
		return nil, err
	}
	return &b, nil
}

func (c *Client) BoardDelete(ctx context.Context, boardID string) (*DeletedResponse, error) {
	var d DeletedResponse
	if err := c.do(ctx, http.MethodDelete, fmt.Sprintf("/board/%s", boardID), nil, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

func (c *Client) BoardAddParticipants(ctx context.Context, boardID string, input *AddParticipantsInput) (*BoardParticipants, error) {
	var p BoardParticipants
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/board/%s/participants", boardID), input, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (c *Client) BoardRemoveParticipant(ctx context.Context, boardID, participantID string) (*DeletedResponse, error) {
	var d DeletedResponse
	if err := c.do(ctx, http.MethodDelete, fmt.Sprintf("/board/%s/participant/%s", boardID, participantID), nil, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

// ── Table write methods ──────────────────────────────────────────────────────

// CreateTableInput is the body for POST /board/{boardId}/table.
type CreateTableInput struct {
	Name string `json:"name"`
}

// UpdateTableInput is the body for PATCH /board/{boardId}/table/{tableId}.
type UpdateTableInput struct {
	Name string `json:"name"`
}

// CreatedBoardTable is the response for table create/update.
type CreatedBoardTable struct {
	ID      string `json:"_id"`
	Name    string `json:"name"`
	BoardID string `json:"boardId"`
}

func (c *Client) TableCreate(ctx context.Context, boardID string, input *CreateTableInput) (*CreatedBoardTable, error) {
	var t CreatedBoardTable
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/board/%s/table", boardID), input, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

func (c *Client) TableUpdate(ctx context.Context, boardID, tableID string, input *UpdateTableInput) (*CreatedBoardTable, error) {
	var t CreatedBoardTable
	if err := c.do(ctx, http.MethodPatch, fmt.Sprintf("/board/%s/table/%s", boardID, tableID), input, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

func (c *Client) TableDelete(ctx context.Context, boardID, tableID string) (*DeletedResponse, error) {
	var d DeletedResponse
	if err := c.do(ctx, http.MethodDelete, fmt.Sprintf("/board/%s/table/%s", boardID, tableID), nil, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

// ── Column write methods ─────────────────────────────────────────────────────

// ColumnOptionInput is a selectable option supplied when creating/updating a column.
type ColumnOptionInput struct {
	Label string `json:"label"`
	Color string `json:"color,omitempty"`
}

// AddColumnInput is the body for POST /board/{boardId}/table/{tableId}/column.
// Type must be one of the public creatable column types (UPPERCASE).
type AddColumnInput struct {
	Label   string              `json:"label"`
	Type    string              `json:"type"`
	Options []ColumnOptionInput `json:"options,omitempty"`
}

// UpdateColumnInput is the body for PATCH .../column/{columnId}.
// Options are append-only via AddOptions and only valid for STATUS/DROPDOWN/LABELS.
type UpdateColumnInput struct {
	Label      *string             `json:"label,omitempty"`
	AddOptions []ColumnOptionInput `json:"addOptions,omitempty"`
}

// ColumnWithOptions is the response for column create/update.
type ColumnWithOptions struct {
	ColumnID string         `json:"columnId"`
	Label    string         `json:"label"`
	Type     string         `json:"type"`
	Options  []ColumnOption `json:"options,omitempty"`
}

// PublicCreatableColumnTypes lists the column types the public API accepts on create.
var PublicCreatableColumnTypes = []string{
	"TEXT", "PARAGRAPH", "NUMBER", "MONEY", "DATE", "EMAIL", "PHONE", "WEBSITE",
	"CHECKBOX", "STATUS", "DROPDOWN", "LABELS", "USERS", "LOCATION", "DESCRIPTION", "FILE",
}

// IsValidColumnType reports whether t is a public creatable column type (case-sensitive, UPPERCASE).
func IsValidColumnType(t string) bool {
	for _, v := range PublicCreatableColumnTypes {
		if v == t {
			return true
		}
	}
	return false
}

func (c *Client) ColumnAdd(ctx context.Context, boardID, tableID string, input *AddColumnInput) (*ColumnWithOptions, error) {
	var col ColumnWithOptions
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/board/%s/table/%s/column", boardID, tableID), input, &col); err != nil {
		return nil, err
	}
	return &col, nil
}

func (c *Client) ColumnUpdate(ctx context.Context, boardID, tableID, columnID string, input *UpdateColumnInput) (*ColumnWithOptions, error) {
	var col ColumnWithOptions
	if err := c.do(ctx, http.MethodPatch, fmt.Sprintf("/board/%s/table/%s/column/%s", boardID, tableID, columnID), input, &col); err != nil {
		return nil, err
	}
	return &col, nil
}

// ── Row FILE-column attachment write methods ─────────────────────────────────

// RowAttachmentDelete detaches a file from a FILE-column cell.
func (c *Client) RowAttachmentDelete(ctx context.Context, boardID, tableID, rowID, columnID, fileID string) (*DeletedResponse, error) {
	var d DeletedResponse
	path := fmt.Sprintf("/board/%s/table/%s/row/%s/column/%s/file/%s", boardID, tableID, rowID, columnID, fileID)
	if err := c.do(ctx, http.MethodDelete, path, nil, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

// RowAttachmentUpload uploads a file to a FILE-column cell via multipart/form-data.
// The whole file is buffered so a valid Content-Length is sent (required by the API).
func (c *Client) RowAttachmentUpload(ctx context.Context, boardID, tableID, rowID, columnID, filePath string) (*Attachment, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("api: open file: %w", err)
	}
	defer f.Close()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		return nil, fmt.Errorf("api: build multipart: %w", err)
	}
	if _, err := io.Copy(part, f); err != nil {
		return nil, fmt.Errorf("api: read file: %w", err)
	}
	if err := mw.Close(); err != nil {
		return nil, fmt.Errorf("api: finalize multipart: %w", err)
	}

	path := fmt.Sprintf("/board/%s/table/%s/row/%s/column/%s/file", boardID, tableID, rowID, columnID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, &buf)
	if err != nil {
		return nil, fmt.Errorf("api: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", mw.FormDataContentType())

	if c.verbose != nil {
		fmt.Fprintf(c.verbose, ">> %s %s (multipart, %d bytes)\n", http.MethodPost, req.URL.String(), buf.Len())
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("api: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("api: read response: %w", err)
	}

	if c.verbose != nil {
		fmt.Fprintf(c.verbose, "<< %d %s\n", resp.StatusCode, http.StatusText(resp.StatusCode))
		fmt.Fprintf(c.verbose, "<< %s\n", string(respBody))
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		apiErr := &APIError{StatusCode: resp.StatusCode}
		var errBody struct {
			Code    string `json:"code"`
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		if json.Unmarshal(respBody, &errBody) == nil {
			apiErr.Code = errBody.Code
			apiErr.Message = errBody.Error
			if apiErr.Message == "" {
				apiErr.Message = errBody.Message
			}
		}
		if apiErr.Message == "" {
			apiErr.Message = http.StatusText(resp.StatusCode)
		}
		return nil, apiErr
	}

	var att Attachment
	if len(respBody) > 0 {
		if err := json.Unmarshal(respBody, &att); err != nil {
			return nil, fmt.Errorf("api: decode response: %w", err)
		}
	}
	return &att, nil
}
