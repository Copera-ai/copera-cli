package api

import (
	"context"
	"net/http"
	"strconv"
	"time"
)

// ArtifactProjectSettings describes the source configuration a check or push may set.
type ArtifactProjectSettings struct {
	Kind         string               `json:"artifactKind,omitempty"`
	Runtime      string               `json:"runtime,omitempty"`
	Entrypoint   string               `json:"entrypoint,omitempty"`
	Dependencies []ArtifactDependency `json:"dependencies,omitempty"`
}

type ArtifactCheckInput struct {
	ArtifactID         string                   `json:"artifactId,omitempty"`
	BaseVersionID      string                   `json:"baseVersionId,omitempty"`
	IncludeScreenshots bool                     `json:"includeScreenshots"`
	Files              []ArtifactContentFile    `json:"files"`
	Project            *ArtifactProjectSettings `json:"project,omitempty"`
	AuthoringManifest  *ArtifactManifest        `json:"authoringManifest,omitempty"`
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
	OK       bool              `json:"ok"`
	Warnings []ArtifactWarning `json:"warnings"`
	ArtifactBuildFindings
	Screenshots []struct {
		PageID string `json:"pageId"`
		Width  int    `json:"width"`
		Data   []byte `json:"data"`
	} `json:"screenshots"`
}

type ArtifactBuildResult struct {
	State       string `json:"state"`
	Done        bool   `json:"done"`
	FailureCode string `json:"failureCode"`
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
	Title          string `json:"title"`
	Tool           string `json:"tool"`
	IdempotencyKey string `json:"idempotencyKey"`
	DesignSystemID string `json:"designSystemId,omitempty"`
	Project        struct {
		ArtifactProjectSettings
		SchemaVersion int                   `json:"schemaVersion"`
		Files         []ArtifactContentFile `json:"files"`
	} `json:"project"`
}

type ArtifactCreateResult struct {
	Artifact
	Warnings []ArtifactWarning `json:"warnings"`
}

func (c *Client) ArtifactCheck(ctx context.Context, input ArtifactCheckInput) (*ArtifactCheckResult, error) {
	// A dry run includes a render and can outlast the default HTTP timeout.
	client := *c
	httpClient := *c.httpClient
	httpClient.Timeout = max(httpClient.Timeout, 100*time.Second)
	client.httpClient = &httpClient
	var result ArtifactCheckResult
	err := client.do(ctx, http.MethodPost, "/artifacts/check", input, &result)
	return &result, err
}

func (c *Client) ArtifactBuild(ctx context.Context, artifactID, versionID string, wait int) (*ArtifactBuildResult, error) {
	path := artifactVersionPath(artifactID, versionID) + "/build"
	if wait > 0 {
		path += "?wait=" + strconv.Itoa(wait)
	}
	var result ArtifactBuildResult
	err := c.do(ctx, http.MethodGet, path, nil, &result)
	return &result, err
}

func (c *Client) ArtifactPush(ctx context.Context, artifactID string, input ArtifactPushInput) (*ArtifactPushResult, error) {
	var result ArtifactPushResult
	err := c.do(ctx, http.MethodPost, artifactPath(artifactID)+"/versions", input, &result)
	return &result, err
}

func (c *Client) ArtifactCreate(ctx context.Context, input ArtifactCreateInput) (*ArtifactCreateResult, error) {
	var result ArtifactCreateResult
	err := c.do(ctx, http.MethodPost, "/artifacts/", input, &result)
	return &result, err
}
