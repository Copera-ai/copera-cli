package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path"
	"path/filepath"
	"time"
)

// ArtifactArchive is a signed link to a portable archive of a version.
type ArtifactArchive struct {
	URL           string   `json:"url"`
	ByteLength    int64    `json:"byteLength"`
	ContentDigest string   `json:"contentDigest"`
	OmittedPaths  []string `json:"omittedPaths"`
}

// ArtifactUploadedAsset describes an asset admitted to a Design.
type ArtifactUploadedAsset struct {
	ArtifactAsset
	ByteLength int64 `json:"byteLength"`
}

type artifactAssetMetadata struct {
	Path                  string `json:"path"`
	ExpectedContentDigest string `json:"expectedContentDigest"`
	Usage                 string `json:"usage,omitempty"`
	AltText               string `json:"altText,omitempty"`
}

type artifactImportMetadata struct {
	IdempotencyKey string `json:"idempotencyKey"`
	Tool           string `json:"tool"`
}

type artifactRenameInput struct {
	Title string `json:"title"`
}

// ArtifactRenameResult contains metadata updated by a rename.
type ArtifactRenameResult struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	HeadRevision int    `json:"headRevision"`
}

// ArtifactTrashResult confirms that a Design moved to the trash.
type ArtifactTrashResult struct {
	ID      string `json:"id"`
	Trashed bool   `json:"trashed"`
}

// ArtifactRestoreInput pins the head on top of which a version is restored.
type ArtifactRestoreInput struct {
	BaseVersionID        string `json:"baseVersionId"`
	ExpectedHeadRevision int    `json:"expectedHeadRevision"`
	IdempotencyKey       string `json:"idempotencyKey"`
	Tool                 string `json:"tool"`
}

type artifactForkInput struct {
	IdempotencyKey string `json:"idempotencyKey"`
	Tool           string `json:"tool"`
}

// ArtifactArchiveLink requests a link for a pinned version's ZIP.
func (c *Client) ArtifactArchiveLink(ctx context.Context, artifactID, versionID string) (*ArtifactArchive, error) {
	var result ArtifactArchive
	if err := c.do(ctx, http.MethodGet, artifactVersionPath(artifactID, versionID)+"/archive", nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ArtifactImport creates a Design from a portable ZIP.
func (c *Client) ArtifactImport(ctx context.Context, filename string, data []byte, key, tool string) (*Artifact, error) {
	metadata, err := json.Marshal(artifactImportMetadata{IdempotencyKey: key, Tool: tool})
	if err != nil {
		return nil, err
	}
	body, err := c.uploadMultipart(ctx, "/artifacts/archives", filepath.Base(filename), "application/zip", bytes.NewReader(data), metadata)
	if err != nil {
		return nil, err
	}
	var result Artifact
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("api: decode response: %w", err)
	}
	return &result, nil
}

// ArtifactAssetUpload admits a binary asset so a version can refer to it.
func (c *Client) ArtifactAssetUpload(ctx context.Context, artifactID, assetPath string, data []byte, usage, altText string) (*ArtifactUploadedAsset, error) {
	digest := sha256.Sum256(data)
	metadata, err := json.Marshal(artifactAssetMetadata{
		Path:                  assetPath,
		ExpectedContentDigest: hex.EncodeToString(digest[:]),
		Usage:                 usage,
		AltText:               altText,
	})
	if err != nil {
		return nil, err
	}
	mediaType := http.DetectContentType(data)
	for attempt := 1; ; attempt++ {
		body, err := c.uploadMultipart(ctx, artifactPath(artifactID)+"/assets", path.Base(assetPath), mediaType, bytes.NewReader(data), metadata)
		if err == nil {
			var result ArtifactUploadedAsset
			if err := json.Unmarshal(body, &result); err != nil {
				return nil, fmt.Errorf("api: decode response: %w", err)
			}
			return &result, nil
		}
		var apiErr *APIError
		if attempt == 5 || !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusServiceUnavailable {
			return nil, err
		}
		delay := time.Duration(min(max(apiErr.RetryAfterSeconds, 0), 1<<31-1)) * time.Second
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}
}

// ArtifactRename updates a Design's title.
func (c *Client) ArtifactRename(ctx context.Context, artifactID, title string) (*ArtifactRenameResult, error) {
	var result ArtifactRenameResult
	if err := c.do(ctx, http.MethodPatch, artifactPath(artifactID), artifactRenameInput{Title: title}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ArtifactTrash moves a Design to Drive trash.
func (c *Client) ArtifactTrash(ctx context.Context, artifactID string) (*ArtifactTrashResult, error) {
	var result ArtifactTrashResult
	if err := c.do(ctx, http.MethodDelete, artifactPath(artifactID), nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ArtifactRestore restores a pinned version by creating a new version.
func (c *Client) ArtifactRestore(ctx context.Context, artifactID, versionID string, input ArtifactRestoreInput) (*ArtifactPushResult, error) {
	var result ArtifactPushResult
	if err := c.do(ctx, http.MethodPost, artifactVersionPath(artifactID, versionID)+"/restore", input, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ArtifactFork makes a new private Design from another Design's head.
func (c *Client) ArtifactFork(ctx context.Context, artifactID, key, tool string) (*Artifact, error) {
	var result Artifact
	input := artifactForkInput{IdempotencyKey: key, Tool: tool}
	if err := c.do(ctx, http.MethodPost, artifactPath(artifactID)+"/fork", input, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
