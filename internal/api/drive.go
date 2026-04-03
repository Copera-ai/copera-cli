package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// DriveNode is an item in a drive tree response.
// Children are fully hydrated recursive DriveNode entries.
type DriveNode struct {
	ID          string      `json:"_id"`
	Name        string      `json:"name"`
	Type        string      `json:"type"` // "file" | "folder"
	Owner       string      `json:"owner"`
	CreatedAt   time.Time   `json:"createdAt"`
	UpdatedAt   time.Time   `json:"updatedAt"`
	HasChildren bool        `json:"hasChildren"`
	Children    []DriveNode `json:"children,omitempty"`
	MimeType    string      `json:"mimeType,omitempty"`
	Size        int64       `json:"size,omitempty"`
	Parent      string      `json:"parent,omitempty"`
}

// DriveFile holds file or folder metadata returned by get/create/finalize endpoints.
type DriveFile struct {
	ID        string    `json:"_id"`
	Name      string    `json:"name"`
	Type      string    `json:"type"` // "file" | "folder"
	Owner     string    `json:"owner"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	MimeType  string    `json:"mimeType,omitempty"`
	Size      int64     `json:"size,omitempty"`
	Parent    string    `json:"parent,omitempty"`
}

// DriveSearchHit is one result from drive search.
type DriveSearchHit struct {
	ID        string    `json:"_id"`
	Name      string    `json:"name"`
	Type      string    `json:"type"`
	Owner     string    `json:"owner"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	MimeType  string    `json:"mimeType,omitempty"`
	Size      int64     `json:"size,omitempty"`
	Parent    string    `json:"parent,omitempty"`
}

// DriveSearchOpts configures a drive search request.
type DriveSearchOpts struct {
	SortBy    string // createdAt | updatedAt
	SortOrder string // asc | desc
	Limit     int    // 1–50; 0 = server default (20)
}

// MultipartStartRequest initiates a multipart upload.
type MultipartStartRequest struct {
	FileName string `json:"fileName"`
	FileSize int64  `json:"fileSize"`
	MimeType string `json:"mimeType"`
	ParentID string `json:"parentId,omitempty"`
}

// MultipartStartResponse contains IDs needed for subsequent upload steps.
type MultipartStartResponse struct {
	UploadID string `json:"uploadId"`
	FileKey  string `json:"fileKey"`
}

// PresignedURLsRequest asks for signed URLs for each upload part.
type PresignedURLsRequest struct {
	UploadID string `json:"uploadId"`
	Parts    int    `json:"parts"`
	FileKey  string `json:"fileKey"`
}

// PresignedPart is a single presigned URL with its part number.
type PresignedPart struct {
	SignedURL  string `json:"signedUrl"`
	PartNumber int    `json:"PartNumber"`
}

// PresignedURLsResponse holds the array of presigned part URLs.
type PresignedURLsResponse struct {
	Parts []PresignedPart `json:"parts"`
}

// CompletedPart is sent in the finalize request with the ETag from S3.
type CompletedPart struct {
	PartNumber int    `json:"partNumber"`
	ETag       string `json:"eTag"`
}

// MultipartFinalizeRequest completes a multipart upload.
type MultipartFinalizeRequest struct {
	UploadID string          `json:"uploadId"`
	FileKey  string          `json:"fileKey"`
	Parts    []CompletedPart `json:"parts"`
}

// CreateFolderRequest creates a new folder in the drive.
type CreateFolderRequest struct {
	Name     string `json:"name"`
	ParentID string `json:"parentId,omitempty"`
}

type driveTreeResponse struct {
	Root         []DriveNode `json:"root"`
	TotalItems   int         `json:"totalItems"`
	Truncated    bool        `json:"truncated"`
	NextParentID []string    `json:"nextParentIds"`
}

// DriveTreeResult holds the tree response including pagination metadata.
type DriveTreeResult struct {
	Nodes        []DriveNode
	TotalItems   int
	Truncated    bool
	NextParentID []string
}

// DriveTree fetches the drive hierarchy.
// parentID="" returns root-level items. depth controls nesting (1-10, default 3).
func (c *Client) DriveTree(ctx context.Context, parentID string, depth int) (*DriveTreeResult, error) {
	params := url.Values{}
	if parentID != "" {
		params.Set("parentId", parentID)
	}
	if depth > 0 {
		params.Set("depth", fmt.Sprintf("%d", depth))
	}
	path := "/drive/tree"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var resp driveTreeResponse
	if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return &DriveTreeResult{
		Nodes:        resp.Root,
		TotalItems:   resp.TotalItems,
		Truncated:    resp.Truncated,
		NextParentID: resp.NextParentID,
	}, nil
}

// DriveSearch searches across accessible files and folders.
func (c *Client) DriveSearch(ctx context.Context, q string, opts DriveSearchOpts) ([]DriveSearchHit, int, error) {
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
		Hits      []DriveSearchHit `json:"hits"`
		TotalHits int              `json:"totalHits"`
		Query     string           `json:"query"`
	}
	if err := c.do(ctx, http.MethodGet, "/drive/search?"+params.Encode(), nil, &resp); err != nil {
		return nil, 0, err
	}
	return resp.Hits, resp.TotalHits, nil
}

// DriveFileGet fetches metadata for a file or folder.
func (c *Client) DriveFileGet(ctx context.Context, fileID string) (*DriveFile, error) {
	var f DriveFile
	if err := c.do(ctx, http.MethodGet, "/drive/files/"+fileID, nil, &f); err != nil {
		return nil, err
	}
	return &f, nil
}

// DriveDownloadURL returns a presigned CloudFront URL for downloading a file.
func (c *Client) DriveDownloadURL(ctx context.Context, fileID string) (string, error) {
	var resp struct {
		URL string `json:"url"`
	}
	if err := c.do(ctx, http.MethodGet, "/drive/files/"+fileID+"/download", nil, &resp); err != nil {
		return "", err
	}
	return resp.URL, nil
}

// DriveMultipartStart initiates a multipart upload and returns the upload ID and file key.
func (c *Client) DriveMultipartStart(ctx context.Context, req *MultipartStartRequest) (*MultipartStartResponse, error) {
	var resp MultipartStartResponse
	if err := c.do(ctx, http.MethodPost, "/drive/files/upload/multipart/start", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// DrivePresignedURLs returns presigned URLs for uploading file parts to S3.
func (c *Client) DrivePresignedURLs(ctx context.Context, req *PresignedURLsRequest) (*PresignedURLsResponse, error) {
	var resp PresignedURLsResponse
	if err := c.do(ctx, http.MethodPost, "/drive/files/upload/multipart/presigned-urls", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// DriveMultipartFinalize completes a multipart upload and returns the created file metadata.
func (c *Client) DriveMultipartFinalize(ctx context.Context, req *MultipartFinalizeRequest) (*DriveFile, error) {
	var f DriveFile
	if err := c.do(ctx, http.MethodPost, "/drive/files/upload/multipart/finalize", req, &f); err != nil {
		return nil, err
	}
	return &f, nil
}

// DriveFolderCreate creates a new folder in the drive.
func (c *Client) DriveFolderCreate(ctx context.Context, req *CreateFolderRequest) (*DriveFile, error) {
	var f DriveFile
	if err := c.do(ctx, http.MethodPost, "/drive/folders", req, &f); err != nil {
		return nil, err
	}
	return &f, nil
}
