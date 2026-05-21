package api

import (
	"context"
	"fmt"
	"net/http"
)

// ExportFormat represents a supported export format for table views.
type ExportFormat string

const (
	ExportCSV      ExportFormat = "CSV"
	ExportXLSX     ExportFormat = "XLSX"
	ExportJSON     ExportFormat = "JSON"
	ExportMarkdown ExportFormat = "MARKDOWN"
	ExportHTML     ExportFormat = "HTML"
	ExportPDF      ExportFormat = "PDF"
	ExportZIP      ExportFormat = "ZIP"
	ExportICS      ExportFormat = "ICS"
)

// IsValidExportFormat reports whether s is one of the accepted export formats.
func IsValidExportFormat(s string) bool {
	switch ExportFormat(s) {
	case ExportCSV, ExportXLSX, ExportJSON, ExportMarkdown,
		ExportHTML, ExportPDF, ExportZIP, ExportICS:
		return true
	}
	return false
}

// ExportTableInput is the body for POST /board/{boardId}/table/{tableId}/export.
// Required fields: BoardID, ViewID. Format defaults server-side when omitted.
// Optional fields are sent only when set (omitempty).
type ExportTableInput struct {
	BoardID  string       `json:"boardId"`
	ViewID   string       `json:"viewId"`
	Format   ExportFormat `json:"format,omitempty"`
	ColumnIDs []string    `json:"columnIds,omitempty"`
	RowIDs    []string    `json:"rowIds,omitempty"`

	// Format-specific opaque options. The CLI exposes a JSON escape hatch
	// for power users; we pass these through verbatim.
	CSVOptions any `json:"csvOptions,omitempty"`
	PDFOptions any `json:"pdfOptions,omitempty"`
	ZIPOptions any `json:"zipOptions,omitempty"`
	ICSOptions any `json:"icsOptions,omitempty"`

	IncludeHidden        bool   `json:"includeHidden,omitempty"`
	IncludeSystemColumns bool   `json:"includeSystemColumns,omitempty"`
	FileNameTemplate     string `json:"fileNameTemplate,omitempty"`
	ForceAsync           bool   `json:"forceAsync,omitempty"`
	SaveToDrive          bool   `json:"saveToDrive,omitempty"`
	WebhookURL           string `json:"webhookUrl,omitempty"`
}

// ExportColumn is one column descriptor returned in an inline export response.
type ExportColumn struct {
	ID    string `json:"_id"`
	Label string `json:"label"`
	Type  string `json:"type"`
}

// ExportRow is one row returned in an inline export response.
type ExportRow struct {
	ID      string `json:"_id"`
	RowID   int    `json:"rowId"`
	Columns any    `json:"columns"`
}

// AsyncJob describes a queued export job (returned for PDF/ZIP/large renders).
type AsyncJob struct {
	JobID         string  `json:"jobId"`
	Status        string  `json:"status"`
	Format        string  `json:"format"`
	ExpiresAt     string  `json:"expiresAt,omitempty"`
	FileName      *string `json:"fileName,omitempty"`
	MimeType      *string `json:"mimeType,omitempty"`
	FileSizeBytes *int64  `json:"fileSizeBytes,omitempty"`
	RowCount      *int    `json:"rowCount,omitempty"`
	DownloadURL   *string `json:"downloadUrl,omitempty"`
}

// ExportTableResponse covers both inline and async results.
// For text/spreadsheet small renders, Payload + FileName + MimeType are populated.
// For PDF/ZIP/large renders, AsyncJob is populated and the inline fields are empty.
type ExportTableResponse struct {
	FileName string         `json:"fileName,omitempty"`
	MimeType string         `json:"mimeType,omitempty"`
	Payload  string         `json:"payload,omitempty"`
	RowCount int            `json:"rowCount,omitempty"`
	Columns  []ExportColumn `json:"columns,omitempty"`
	Rows     []ExportRow    `json:"rows,omitempty"`
	AsyncJob *AsyncJob      `json:"asyncJob,omitempty"`
}

// IsAsync reports whether the export was queued for async processing.
func (r *ExportTableResponse) IsAsync() bool {
	return r.AsyncJob != nil
}

// TableExport renders a table view to the requested format.
// The server may respond inline (text formats, small renders) or with an
// asyncJob descriptor (PDF/ZIP and large renders).
func (c *Client) TableExport(ctx context.Context, boardID, tableID string, input *ExportTableInput) (*ExportTableResponse, error) {
	if input != nil {
		// Server requires boardId in the body too; mirror the path param.
		input.BoardID = boardID
	}
	var resp ExportTableResponse
	path := fmt.Sprintf("/board/%s/table/%s/export", boardID, tableID)
	if err := c.do(ctx, http.MethodPost, path, input, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
