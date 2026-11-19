package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"strconv"
	"time"
)

// ArtifactArchive is a signed link to a portable archive of a version.
type ArtifactArchive struct {
	URL            string   `json:"url"`
	ByteLength     int64    `json:"byteLength"`
	ContentDigest  string   `json:"contentDigest"`
	OmittedPaths   []string `json:"omittedPaths"`
	ArchiveVersion int      `json:"archiveVersion,omitempty"`
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
func (c *Client) ArtifactArchiveLink(ctx context.Context, artifactID, versionID string, version int) (*ArtifactArchive, error) {
	var result ArtifactArchive
	endpoint := artifactVersionPath(artifactID, versionID) + "/archive"
	if version != 0 {
		endpoint = withQuery(endpoint, url.Values{"archiveVersion": {strconv.Itoa(version)}})
	}
	if err := c.do(ctx, http.MethodGet, endpoint, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ArtifactImport creates a Design from a portable ZIP.
func (c *Client) ArtifactImport(ctx context.Context, filename string, file ArtifactUpload, key, tool string) (*Artifact, error) {
	metadata, err := json.Marshal(artifactImportMetadata{IdempotencyKey: key, Tool: tool})
	if err != nil {
		return nil, err
	}
	form := artifactMultipart("metadata", metadata, []ArtifactCheckFile{
		{Path: filepath.Base(filename), MediaType: "application/zip", ArtifactUpload: file},
	})
	var result Artifact
	if err := c.doOnce(ctx, http.MethodPost, "/artifacts/archives", form, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ArtifactAssetUpload admits an asset so a version can refer to it.
func (c *Client) ArtifactAssetUpload(ctx context.Context, artifactID string, file ArtifactCheckFile, usage, altText string) (*ArtifactUploadedAsset, error) {
	metadata, err := json.Marshal(artifactAssetMetadata{
		Path:                  file.Path,
		ExpectedContentDigest: file.ContentDigest,
		Usage:                 usage,
		AltText:               altText,
	})
	if err != nil {
		return nil, err
	}
	file.Path = path.Base(file.Path)
	form := artifactMultipart("metadata", metadata, []ArtifactCheckFile{file})
	for attempt := 1; ; attempt++ {
		var result ArtifactUploadedAsset
		err := c.doOnce(ctx, http.MethodPost, artifactPath(artifactID)+"/assets", form, &result)
		if err == nil {
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

// ArtifactAssets lists the Design's admitted assets, including their byte lengths.
func (c *Client) ArtifactAssets(ctx context.Context, artifactID string) ([]ArtifactUploadedAsset, error) {
	var result struct {
		Assets []ArtifactUploadedAsset `json:"assets"`
	}
	if err := c.do(ctx, http.MethodGet, artifactPath(artifactID)+"/design-assets", nil, &result); err != nil {
		return nil, err
	}
	return result.Assets, nil
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
