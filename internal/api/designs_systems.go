package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

const artifactDesignSystemsPath = "/artifacts/design-systems"
const designSystemResponseLimit = 40 << 20

// ArtifactDesignSystem is the Public API summary, including the owned package status.
type ArtifactDesignSystem struct {
	ID                string          `json:"id"`
	Name              string          `json:"name"`
	Kind              string          `json:"kind"`
	Available         bool            `json:"available"`
	SourceArtifactID  json.RawMessage `json:"sourceArtifactId,omitempty"`
	SourceVersionID   json.RawMessage `json:"sourceVersionId,omitempty"`
	DesignDigest      json.RawMessage `json:"designDigest,omitempty"`
	Spec              json.RawMessage `json:"spec"`
	Summary           *string         `json:"summary"`
	Library           *string         `json:"library"`
	PresetCode        *string         `json:"presetCode"`
	UpdatedAt         string          `json:"updatedAt"`
	Revision          int             `json:"revision"`
	PackageDigest     *string         `json:"packageDigest"`
	UnavailableReason *string         `json:"unavailableReason"`
}

type ArtifactDesignSystemList struct {
	DesignSystems []ArtifactDesignSystem `json:"designSystems"`
}

type ArtifactSystemKit struct {
	Kind           string   `json:"kind"`
	SpecJSON       string   `json:"specJson"`
	RegistryCommit string   `json:"registryCommit"`
	Components     []string `json:"components"`
}

type ArtifactSystemFile struct {
	Path          string `json:"path"`
	MediaType     string `json:"mediaType"`
	ContentBase64 string `json:"contentBase64"`
	Usage         string `json:"usage,omitempty"`
}

type ArtifactSystemPackage struct {
	SchemaVersion int                  `json:"schemaVersion"`
	Kit           *ArtifactSystemKit   `json:"kit"`
	Files         []ArtifactSystemFile `json:"files"`
}

type ArtifactSystemDocument struct {
	Format   string          `json:"format"`
	Document json.RawMessage `json:"document"`
}

type ArtifactSystemTransfer struct {
	SystemID      string `json:"systemId"`
	Revision      int    `json:"revision"`
	PackageDigest string `json:"packageDigest"`
	ArtifactSystemDocument
}

type ArtifactSystemInstall struct {
	SystemID      string `json:"systemId"`
	Revision      int    `json:"revision"`
	PackageDigest string `json:"packageDigest"`
	Kit           string `json:"kit"`
}

type ArtifactSystemProjection struct {
	Install      ArtifactSystemInstall `json:"install"`
	Files        []ArtifactSystemFile  `json:"files"`
	Dependencies []struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"dependencies"`
}

type ArtifactSystemDefaults struct {
	PersonalSystemID       *string `json:"personalSystemId"`
	WorkspaceSystemID      *string `json:"workspaceSystemId"`
	Locked                 bool    `json:"locked"`
	CanSetWorkspaceDefault bool    `json:"canSetWorkspaceDefault"`
}

type ArtifactSystemDefaultInput struct {
	Scope    string  `json:"scope"`
	SystemID *string `json:"systemId"`
	Locked   *bool   `json:"locked,omitempty"`
}

type ArtifactSystemCreate struct {
	Name           string `json:"name"`
	IdempotencyKey string `json:"idempotencyKey"`
	ArtifactSystemDocument
}

type ArtifactSystemReplace struct {
	ExpectedRevision int                    `json:"expectedRevision"`
	Name             string                 `json:"name,omitempty"`
	Package          *ArtifactSystemPackage `json:"package,omitempty"`
}

type ArtifactSystemSave struct {
	ArtifactID string `json:"artifactId"`
	VersionID  string `json:"versionId"`
	Name       string `json:"name"`
}

type ArtifactDesignChangeHead struct {
	BaseVersionID        string `json:"baseVersionId"`
	ExpectedHeadRevision int    `json:"expectedHeadRevision"`
	IdempotencyKey       string `json:"idempotencyKey"`
}

type ArtifactSystemRefresh struct {
	ArtifactDesignChangeHead
	ExpectedSystemRevision int `json:"expectedSystemRevision"`
}

type ArtifactShadcnSetup struct {
	ArtifactDesignChangeHead
	Setup string `json:"setup"`
}

type ArtifactDesignChange struct {
	Outcome      string                 `json:"outcome"`
	ArtifactID   string                 `json:"artifactId"`
	VersionID    string                 `json:"versionId"`
	HeadRevision int                    `json:"headRevision"`
	Install      *ArtifactSystemInstall `json:"install"`
}

type ArtifactShadcnSetupResult struct {
	ArtifactDesignChange
	Shadcn struct {
		Summary     string   `json:"summary"`
		Library     string   `json:"library"`
		PresetCode  *string  `json:"presetCode"`
		InitCommand *string  `json:"initCommand"`
		Warnings    []string `json:"warnings"`
	} `json:"shadcn"`
	Warnings []string `json:"warnings"`
}

func artifactDesignSystemPath(id string) string {
	return artifactDesignSystemsPath + "/" + url.PathEscape(id)
}

func (c *Client) ArtifactDesignSystems(ctx context.Context) (*ArtifactDesignSystemList, error) {
	var result ArtifactDesignSystemList
	err := c.do(ctx, http.MethodGet, artifactDesignSystemsPath, nil, &result)
	return &result, err
}

func (c *Client) ArtifactDesignSystem(ctx context.Context, id string) (*ArtifactDesignSystem, error) {
	var result ArtifactDesignSystem
	err := c.do(ctx, http.MethodGet, artifactDesignSystemPath(id), nil, &result)
	return &result, err
}

func (c *Client) ArtifactDesignSystemPackage(ctx context.Context, id, format string, revision int) (*ArtifactSystemTransfer, error) {
	params := url.Values{"format": {format}}
	if revision > 0 {
		params.Set("expectedRevision", strconv.Itoa(revision))
	}
	var result ArtifactSystemTransfer
	err := c.doBounded(ctx, http.MethodGet, withQuery(artifactDesignSystemPath(id)+"/package", params), nil, &result, designSystemResponseLimit)
	return &result, err
}

func (c *Client) ArtifactDesignSystemInstallation(ctx context.Context, id, runtime, kit string, revision int) (*ArtifactSystemProjection, error) {
	params := url.Values{"runtime": {runtime}, "kit": {kit}, "expectedRevision": {strconv.Itoa(revision)}}
	var result ArtifactSystemProjection
	err := c.doBounded(ctx, http.MethodGet, withQuery(artifactDesignSystemPath(id)+"/installation", params), nil, &result, designSystemResponseLimit)
	return &result, err
}

func (c *Client) ArtifactDesignSystemCreate(ctx context.Context, input ArtifactSystemCreate) (*ArtifactDesignSystem, error) {
	var result ArtifactDesignSystem
	err := c.doDesignMutation(ctx, artifactDesignSystemsPath, input, &result)
	return &result, err
}

func (c *Client) ArtifactDesignSystemReplace(ctx context.Context, id string, input ArtifactSystemReplace) (*ArtifactDesignSystem, error) {
	var result ArtifactDesignSystem
	err := c.do(ctx, http.MethodPatch, artifactDesignSystemPath(id), input, &result)
	return &result, err
}

func (c *Client) ArtifactDesignSystemSave(ctx context.Context, input ArtifactSystemSave) (*ArtifactDesignSystem, error) {
	var result ArtifactDesignSystem
	err := c.do(ctx, http.MethodPost, artifactDesignSystemsPath+"/save-from", input, &result)
	return &result, err
}

func (c *Client) ArtifactDesignSystemDefaults(ctx context.Context) (*ArtifactSystemDefaults, error) {
	var result ArtifactSystemDefaults
	err := c.do(ctx, http.MethodGet, artifactDesignSystemsPath+"/defaults", nil, &result)
	return &result, err
}

func (c *Client) ArtifactDesignSystemSetDefault(ctx context.Context, input ArtifactSystemDefaultInput) (*ArtifactSystemDefaults, error) {
	var result ArtifactSystemDefaults
	err := c.do(ctx, http.MethodPatch, artifactDesignSystemsPath+"/defaults", input, &result)
	return &result, err
}

func (c *Client) ArtifactDesignSystemRefresh(ctx context.Context, id string, input ArtifactSystemRefresh) (*ArtifactDesignChange, error) {
	var result ArtifactDesignChange
	err := c.doDesignMutation(ctx, artifactPath(id)+"/design-system-refresh", input, &result)
	return &result, err
}

func (c *Client) ArtifactShadcnSetup(ctx context.Context, id string, input ArtifactShadcnSetup) (*ArtifactShadcnSetupResult, error) {
	var result ArtifactShadcnSetupResult
	err := c.doDesignMutation(ctx, artifactPath(id)+"/shadcn/setup", input, &result)
	return &result, err
}

// Keys belong to the supplied request. A transport retry uses the same request,
// including the original key and version pins.
func (c *Client) doDesignMutation(ctx context.Context, path string, input, result any) error {
	err := c.do(ctx, http.MethodPost, path, input, result)
	var transport *url.Error
	if ctx.Err() == nil && (errors.As(err, &transport) || errors.Is(err, io.ErrUnexpectedEOF)) {
		return c.do(ctx, http.MethodPost, path, input, result)
	}
	return err
}
