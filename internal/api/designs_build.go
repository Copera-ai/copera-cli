package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strconv"
	"time"
)

// ArtifactProjectSettings describes the source configuration a check or push may set.
type ArtifactProjectSettings struct {
	Kind         string                      `json:"artifactKind,omitempty"`
	Runtime      string                      `json:"runtime,omitempty"`
	Entrypoint   string                      `json:"entrypoint,omitempty"`
	Dependencies []ArtifactDependency        `json:"dependencies,omitempty"`
	Presentation *ArtifactPresentation       `json:"presentation,omitempty"`
	Capabilities []ArtifactProjectCapability `json:"capabilities,omitzero"`
}

type ArtifactPresentation struct {
	AspectRatio string `json:"aspectRatio"`
	SlideCount  int    `json:"slideCount,omitempty"`
}

type ArtifactProjectCapability struct {
	Kind          string    `json:"kind"`
	Origins       *[]string `json:"origins,omitempty"`
	Scopes        *[]string `json:"scopes,omitempty"`
	Namespace     *string   `json:"namespace,omitempty"`
	SchemaVersion int       `json:"schemaVersion,omitempty"`
	Migration     *struct {
		Strategy string `json:"strategy"`
	} `json:"migration,omitempty"`
	ContractVersion int                      `json:"contractVersion,omitempty"`
	Collections     []ArtifactDataCollection `json:"collections,omitempty"`
}

func (capability *ArtifactProjectCapability) UnmarshalJSON(data []byte) error {
	type capabilityFields ArtifactProjectCapability
	var decoded capabilityFields
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	switch decoded.Kind {
	case "ai", "speech", "dictation", "images", "files", "copera", "room":
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(data, &fields); err != nil {
			return err
		}
		if len(fields) != 2 || fields["kind"] == nil || fields["contractVersion"] == nil || decoded.ContractVersion != 1 {
			return fmt.Errorf("Design capability %q requires exactly kind and contractVersion 1", decoded.Kind)
		}
	}
	*capability = ArtifactProjectCapability(decoded)
	return nil
}

type ArtifactDataCollection struct {
	Name   string `json:"name"`
	Scope  string `json:"scope"`
	Access string `json:"access,omitempty"`
}

type ArtifactMemberRuntimeDeclaration struct {
	Kind            string                   `json:"kind"`
	ContractVersion int                      `json:"contractVersion"`
	Collections     []ArtifactDataCollection `json:"collections,omitempty"`
}

type ArtifactCheckInput struct {
	ArtifactID         string                   `json:"artifactId,omitempty"`
	BaseVersionID      string                   `json:"baseVersionId,omitempty"`
	IncludeScreenshots bool                     `json:"includeScreenshots"`
	Files              []ArtifactContentFile    `json:"files"`
	Project            *ArtifactProjectSettings `json:"project,omitempty"`
	AuthoringManifest  *ArtifactManifest        `json:"authoringManifest,omitempty"`
}

// ArtifactCheckFile is a binary file sent alongside the project in a check or create request.
type ArtifactCheckFile struct {
	Path      string
	MediaType string
	ArtifactUpload
}

// ArtifactUpload describes exact bytes that can be reopened on a retry.
type ArtifactUpload struct {
	ByteLength    int64
	ContentDigest string
	Open          func() (io.ReadCloser, error)
}

// CopyTo streams the pinned bytes and fails if the input changed after scanning.
func (file ArtifactUpload) CopyTo(dst io.Writer) error {
	src, err := file.Open()
	if err != nil {
		return err
	}
	defer src.Close()
	digest := sha256.New()
	n, err := io.Copy(io.MultiWriter(dst, digest), io.LimitReader(src, file.ByteLength))
	if err != nil {
		return err
	}
	if n != file.ByteLength || hex.EncodeToString(digest.Sum(nil)) != file.ContentDigest {
		return fmt.Errorf("file differs from its scanned size or SHA-256")
	}
	var next [1]byte
	if n, err := io.ReadFull(src, next[:]); n != 0 || err != io.EOF {
		return fmt.Errorf("file changed after scanning")
	}
	return nil
}

type ArtifactWarning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ArtifactDiagnostic struct {
	ArtifactWarning
	Severity string `json:"severity"`
	FilePath string `json:"filePath,omitempty"`
	Line     int    `json:"line,omitempty"`
	Column   int    `json:"column,omitempty"`
}

type ArtifactRuntimeConsole struct {
	Reported  bool `json:"reported"`
	Truncated bool `json:"truncated"`
	Entries   []struct {
		Level  string `json:"level"`
		Text   string `json:"text"`
		Count  int    `json:"count"`
		Page   string `json:"page,omitempty"`
		Source *struct {
			File   string `json:"file"`
			Line   int    `json:"line,omitempty"`
			Column int    `json:"column,omitempty"`
		} `json:"source,omitempty"`
	} `json:"entries"`
}

type ArtifactBuildFindings struct {
	Diagnostics     []ArtifactDiagnostic   `json:"diagnostics"`
	Accessibility   []ArtifactDiagnostic   `json:"accessibility"`
	RuntimeConsole  ArtifactRuntimeConsole `json:"runtimeConsole"`
	FailedFiles     []string               `json:"failedFiles"`
	FailedResources []struct {
		Failure      string `json:"failure"`
		ResourceType string `json:"resourceType"`
		Target       string `json:"target"`
	} `json:"failedResources"`
}

type ArtifactCheckResult struct {
	OK         bool              `json:"ok"`
	Warnings   []ArtifactWarning `json:"warnings"`
	DarkScheme *bool             `json:"darkScheme"`
	ArtifactBuildFindings
	Screenshots []struct {
		PageID string `json:"pageId"`
		Width  int    `json:"width"`
		Scheme string `json:"scheme"`
		Data   []byte `json:"data"`
	} `json:"screenshots"`
}

type ArtifactBuildResult struct {
	State          string     `json:"state"`
	Done           bool       `json:"done"`
	FailureCode    string     `json:"failureCode"`
	PreviewReadyAt *time.Time `json:"previewReadyAt"`
	EvidenceStatus string     `json:"evidenceStatus"`
	ArtifactBuildFindings
	Screenshots []struct {
		PageID string `json:"pageId"`
		Width  int    `json:"width"`
		URL    string `json:"url"`
	} `json:"screenshots"`
}

type ArtifactPushInput struct {
	BaseVersionID        string                   `json:"baseVersionId"`
	ExpectedHeadRevision int                      `json:"expectedHeadRevision"`
	IdempotencyKey       string                   `json:"idempotencyKey"`
	Tool                 string                   `json:"tool"`
	ChangeSummary        string                   `json:"changeSummary"`
	Files                []ArtifactContentFile    `json:"files,omitempty"`
	RemovedPaths         []string                 `json:"removedPaths,omitempty"`
	Project              *ArtifactProjectSettings `json:"project,omitempty"`
	AuthoringManifest    *ArtifactManifest        `json:"authoringManifest,omitempty"`
}

type ArtifactPushResult struct {
	Outcome      string            `json:"outcome"`
	ArtifactID   string            `json:"artifactId"`
	HeadRevision int               `json:"headRevision"`
	Version      ArtifactVersion   `json:"version"`
	BuildState   string            `json:"buildState"`
	Warnings     []ArtifactWarning `json:"warnings"`
}

type ArtifactCreateInput struct {
	Title                string `json:"title"`
	ChangeSummary        string `json:"changeSummary,omitempty"`
	Tool                 string `json:"tool"`
	IdempotencyKey       string `json:"idempotencyKey"`
	DesignSystemID       string `json:"designSystemId,omitempty"`
	DesignSystemRevision int    `json:"designSystemRevision,omitempty"`
	DesignSystemKit      string `json:"designSystemKit,omitempty"`
	NoDesignSystem       bool   `json:"noDesignSystem,omitempty"`
	Project              struct {
		ArtifactProjectSettings
		SchemaVersion int                   `json:"schemaVersion"`
		Files         []ArtifactContentFile `json:"files"`
	} `json:"project"`
}

type ArtifactCreateResult struct {
	ArtifactGetResult
	Warnings []ArtifactWarning `json:"warnings"`
}

func (c *Client) ArtifactCheck(ctx context.Context, input ArtifactCheckInput, files ...ArtifactCheckFile) (*ArtifactCheckResult, error) {
	// A dry run includes a render and can outlast the default HTTP timeout.
	client := *c
	httpClient := *c.httpClient
	httpClient.Timeout = max(httpClient.Timeout, 100*time.Second)
	client.httpClient = &httpClient
	var result ArtifactCheckResult
	const path = "/artifacts/check"
	if len(files) == 0 {
		err := client.do(ctx, http.MethodPost, path, input, &result)
		return &result, err
	}
	project, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("api: marshal Design check: %w", err)
	}
	err = client.do(ctx, http.MethodPost, path, artifactMultipart("project", project, files), &result)
	return &result, err
}

func artifactMultipart(field string, metadata []byte, files []ArtifactCheckFile) multipartRequest {
	return newMultipartRequest(func(writer *multipart.Writer) error {
		part, err := writer.CreatePart(textproto.MIMEHeader{
			"Content-Disposition": {`form-data; name="` + field + `"`},
			"Content-Type":        {"application/json"},
		})
		if err != nil {
			return err
		}
		if _, err := part.Write(metadata); err != nil {
			return err
		}
		for _, file := range files {
			part, err := writer.CreatePart(textproto.MIMEHeader{
				"Content-Disposition": {multipart.FileContentDisposition("file", file.Path)},
				"Content-Type":        {file.MediaType},
			})
			if err != nil {
				return err
			}
			if err := file.CopyTo(part); err != nil {
				return fmt.Errorf("stream %q: %w", file.Path, err)
			}
		}
		return nil
	})
}

func (c *Client) ArtifactBuild(ctx context.Context, artifactID, versionID string, wait int) (*ArtifactBuildResult, error) {
	return c.artifactBuild(ctx, artifactID, versionID, wait, "")
}

func (c *Client) ArtifactBuildForPreview(ctx context.Context, artifactID, versionID string, wait int) (*ArtifactBuildResult, error) {
	return c.artifactBuild(ctx, artifactID, versionID, wait, "preview")
}

func (c *Client) artifactBuild(ctx context.Context, artifactID, versionID string, wait int, waitFor string) (*ArtifactBuildResult, error) {
	params := url.Values{}
	if wait > 0 {
		params.Set("wait", strconv.Itoa(wait))
	}
	if waitFor != "" {
		params.Set("waitFor", waitFor)
	}
	path := withQuery(artifactVersionPath(artifactID, versionID)+"/build", params)
	var result ArtifactBuildResult
	err := c.do(ctx, http.MethodGet, path, nil, &result)
	return &result, err
}

func (c *Client) ArtifactPagePreview(ctx context.Context, artifactID, versionID, pageID string) (*ArtifactPreview, error) {
	path := artifactVersionPath(artifactID, versionID) + "/pages/" + url.PathEscape(pageID) + "/preview"
	var result ArtifactPreview
	err := c.do(ctx, http.MethodGet, path, nil, &result)
	return &result, err
}

func (c *Client) ArtifactPush(ctx context.Context, artifactID string, input ArtifactPushInput) (*ArtifactPushResult, error) {
	var result ArtifactPushResult
	err := c.do(ctx, http.MethodPost, artifactPath(artifactID)+"/versions", input, &result)
	return &result, err
}

func (c *Client) ArtifactCreate(ctx context.Context, input ArtifactCreateInput, files ...ArtifactCheckFile) (*ArtifactCreateResult, error) {
	project, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("api: marshal Design create: %w", err)
	}
	payload := artifactMultipart("project", project, files)
	var result ArtifactCreateResult
	if input.DesignSystemRevision != 0 || input.NoDesignSystem {
		err = c.doDesignMutation(ctx, "/artifacts/", payload, &result)
	} else {
		err = c.do(ctx, http.MethodPost, "/artifacts/", payload, &result)
	}
	return &result, err
}
