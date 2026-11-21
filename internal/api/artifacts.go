package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// ArtifactSharedDesign points at the shared design files of a version.
type ArtifactSharedDesign struct {
	Version        int    `json:"version"`
	Digest         string `json:"digest,omitempty"`
	GuidancePath   string `json:"guidancePath,omitempty"`
	TokensPath     string `json:"tokensPath,omitempty"`
	ComponentsPath string `json:"componentsPath,omitempty"`
	AssetsPath     string `json:"assetsPath,omitempty"`
}

// ArtifactViewport is a page's default width and height in CSS pixels.
type ArtifactViewport struct {
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// ArtifactManifestPage is one page declared by a version's authoring manifest.
type ArtifactManifestPage struct {
	ID              string           `json:"id"`
	Title           string           `json:"title"`
	Entrypoint      string           `json:"entrypoint"`
	Kind            string           `json:"kind"`
	Surface         string           `json:"surface"`
	DefaultViewport ArtifactViewport `json:"defaultViewport"`
	ScaffoldVersion string           `json:"scaffoldVersion"`
}

// ArtifactAsset is an admitted design asset (logo, font, image, reference).
type ArtifactAsset struct {
	ID            string `json:"id"`
	Path          string `json:"path"`
	MediaType     string `json:"mediaType"`
	ContentDigest string `json:"contentDigest"`
	Usage         string `json:"usage"`
	AltText       string `json:"altText,omitempty"`
}

// ArtifactDocument is a non-page document of a version, such as its Spec.
type ArtifactDocument struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Path  string `json:"path"`
	Kind  string `json:"kind"`
}

// ArtifactManifest is the authoring manifest pinned to a version.
type ArtifactManifest struct {
	SchemaVersion int                    `json:"schemaVersion"`
	DefaultPageID string                 `json:"defaultPageId"`
	Pages         []ArtifactManifestPage `json:"pages"`
	SharedDesign  ArtifactSharedDesign   `json:"sharedDesign"`
	Assets        []ArtifactAsset        `json:"assets"`
	Documents     []ArtifactDocument     `json:"documents,omitempty"`
}

// ArtifactFileRef is one source file of a version (inventory only, no content).
type ArtifactFileRef struct {
	Path          string `json:"path"`
	MediaType     string `json:"mediaType"`
	ContentDigest string `json:"contentDigest"`
	UTF8Bytes     int64  `json:"utf8Bytes"`
}

// ArtifactPreview describes the hosted preview of an artifact or version.
type ArtifactPreview struct {
	Status             string     `json:"status"`
	URL                string     `json:"url,omitempty"`
	ExpiresAt          *time.Time `json:"expiresAt,omitempty"`
	ScreenshotURL      string     `json:"screenshotUrl,omitempty"`
	VisualReviewStatus string     `json:"visualReviewStatus,omitempty"`
}

// ArtifactVersion is an immutable artifact version and its file inventory.
type ArtifactVersion struct {
	ID                        string                             `json:"id"`
	ArtifactID                string                             `json:"artifactId"`
	Version                   int                                `json:"version"`
	ParentVersionID           string                             `json:"parentVersionId,omitempty"`
	Runtime                   string                             `json:"runtime"`
	Entrypoint                string                             `json:"entrypoint"`
	ContentDigest             string                             `json:"contentDigest"`
	AuthoringManifest         ArtifactManifest                   `json:"authoringManifest"`
	Files                     []ArtifactFileRef                  `json:"files"`
	CreatedAt                 time.Time                          `json:"createdAt"`
	Origin                    *string                            `json:"origin,omitempty"`
	AuthorKind                *string                            `json:"authorKind,omitempty"`
	Tool                      *string                            `json:"tool,omitempty"`
	ChangeSummary             *string                            `json:"changeSummary,omitempty"`
	Preview                   *ArtifactPreview                   `json:"preview,omitempty"`
	MemberRuntimeDeclarations []ArtifactMemberRuntimeDeclaration `json:"memberRuntimeDeclarations,omitempty"`
	ProjectCapabilities       []ArtifactProjectCapability        `json:"projectCapabilities,omitzero"`
}

// Artifact is an artifact's metadata with its current version.
type Artifact struct {
	ID                    string          `json:"id"`
	Title                 string          `json:"title"`
	Description           string          `json:"description,omitempty"`
	CurrentDraftVersionID string          `json:"currentDraftVersionId"`
	HeadRevision          int             `json:"headRevision"`
	CanEdit               bool            `json:"canEdit"`
	CurrentVersion        ArtifactVersion `json:"currentVersion"`
	Preview               ArtifactPreview `json:"preview"`
}

// ArtifactGetResult carries the optional installation on the current draft.
type ArtifactGetResult struct {
	Artifact
	DesignSystemInstallation *ArtifactSystemInstall `json:"designSystemInstallation,omitempty"`
}

// ArtifactEdge is one item of the artifact list connection.
type ArtifactEdge struct {
	Cursor string   `json:"cursor"`
	Node   Artifact `json:"node"`
}

// ArtifactConnection is a page of artifacts.
type ArtifactConnection struct {
	Edges       []ArtifactEdge `json:"edges"`
	EndCursor   string         `json:"endCursor,omitempty"`
	HasNextPage bool           `json:"hasNextPage"`
}

// ArtifactVersionEdge is one item of the version list connection.
type ArtifactVersionEdge struct {
	Cursor string          `json:"cursor"`
	Node   ArtifactVersion `json:"node"`
}

// ArtifactVersionConnection is a page of versions, newest first.
type ArtifactVersionConnection struct {
	Edges       []ArtifactVersionEdge `json:"edges"`
	EndCursor   string                `json:"endCursor,omitempty"`
	HasNextPage bool                  `json:"hasNextPage"`
}

// ArtifactListOpts configures an artifact list request.
type ArtifactListOpts struct {
	Scope  string // mine | shared; "" = server default (mine)
	Search string
	After  string
	Limit  int // 1–50; 0 = server default (20)
}

// ArtifactPageOpts configures a cursor-paginated request.
type ArtifactPageOpts struct {
	After string
	Limit int // 1–50; 0 = server default (20)
}

// ArtifactSourcePage is one bounded UTF-8 slice of a source file.
// Offsets count UTF-8 bytes of the whole file.
type ArtifactSourcePage struct {
	ArtifactID       string  `json:"artifactId"`
	VersionID        string  `json:"versionId"`
	Path             string  `json:"path"`
	MediaType        string  `json:"mediaType"`
	ContentDigest    string  `json:"contentDigest"`
	ProjectionDigest string  `json:"projectionDigest"`
	Text             string  `json:"text"`
	StartOffset      int64   `json:"startOffset"`
	EndOffset        int64   `json:"endOffset"`
	NextCursor       *string `json:"nextCursor"`
	EOF              bool    `json:"eof"`
}

// ArtifactSourceOpts selects where a source read starts. Cursor and
// StartOffset are mutually exclusive; both empty starts at the beginning.
type ArtifactSourceOpts struct {
	Cursor      string
	StartOffset int64
}

// ArtifactContentFile is a file returned with its full text content.
type ArtifactContentFile struct {
	Path      string `json:"path"`
	MediaType string `json:"mediaType"`
	Content   string `json:"content"`
}

// ArtifactDependency is an npm dependency pinned by a design or starter.
type ArtifactDependency struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Integrity string `json:"integrity,omitempty"`
}

// ArtifactDesign is the shared design exported from a pinned version.
type ArtifactDesign struct {
	SchemaVersion    int                   `json:"schemaVersion"`
	SourceArtifactID string                `json:"sourceArtifactId"`
	SourceVersionID  string                `json:"sourceVersionId"`
	DesignDigest     string                `json:"designDigest"`
	Runtime          string                `json:"runtime"`
	Files            []ArtifactContentFile `json:"files"`
	Dependencies     []ArtifactDependency  `json:"dependencies"`
	SharedDesign     ArtifactSharedDesign  `json:"sharedDesign"`
	Assets           []ArtifactAsset       `json:"assets"`
}

// ArtifactStarter is a curated offline design starter.
type ArtifactStarter struct {
	ID           string                `json:"id"`
	Title        string                `json:"title"`
	Runtime      string                `json:"runtime"`
	Files        []ArtifactContentFile `json:"files"`
	Dependencies []ArtifactDependency  `json:"dependencies"`
	SharedDesign ArtifactSharedDesign  `json:"sharedDesign"`
}

// Hand-off omission reasons for files and the Spec.
const (
	ArtifactOmittedOverBudget = "over_budget"
	ArtifactOmittedSensitive  = "sensitive"
)

// ArtifactSpecNotePageID is the pageId of notes pinned to the Spec.
const ArtifactSpecNotePageID = "spec"

// ArtifactHandoffArtifact identifies the artifact a hand-off pack belongs to.
type ArtifactHandoffArtifact struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Kind  string `json:"kind"`
	URL   string `json:"url"`
}

// ArtifactHandoffVersion identifies the pinned version of a hand-off pack.
type ArtifactHandoffVersion struct {
	ID        string    `json:"id"`
	Number    int       `json:"number"`
	CreatedAt time.Time `json:"createdAt"`
}

// ArtifactHandoffPage is one page to build, with the files that make it up.
type ArtifactHandoffPage struct {
	ID         string           `json:"id"`
	Title      string           `json:"title"`
	Surface    string           `json:"surface"`
	Viewport   ArtifactViewport `json:"viewport"`
	Entrypoint string           `json:"entrypoint"`
	Files      []string         `json:"files"`
}

// ArtifactHandoffLink is one navigation link between two pages.
type ArtifactHandoffLink struct {
	FromPageID string `json:"fromPageId"`
	ToPageID   string `json:"toPageId"`
}

// ArtifactHandoffSpec is the version's Spec; Markdown is absent when Omitted is set.
type ArtifactHandoffSpec struct {
	Path     string  `json:"path"`
	Markdown *string `json:"markdown,omitempty"`
	Omitted  string  `json:"omitted,omitempty"`
}

// ArtifactHandoffNote is a note pinned to a page or to the Spec ("spec").
type ArtifactHandoffNote struct {
	ID         string    `json:"id"`
	PageID     string    `json:"pageId"`
	Body       string    `json:"body"`
	AuthorKind string    `json:"authorKind"` // ai | user
	CreatedAt  time.Time `json:"createdAt"`
}

// ArtifactHandoffAsset is a design asset with a short-lived download URL
// (nil when the server could not sign one).
type ArtifactHandoffAsset struct {
	ArtifactAsset
	URL *string `json:"url"`
}

// ArtifactHandoffDesign is the shared design to set up first.
type ArtifactHandoffDesign struct {
	Digest         *string                `json:"digest"`
	GuidancePath   string                 `json:"guidancePath,omitempty"`
	TokensPath     string                 `json:"tokensPath,omitempty"`
	ComponentsPath string                 `json:"componentsPath,omitempty"`
	AssetsPath     string                 `json:"assetsPath,omitempty"`
	Files          []string               `json:"files"`
	Assets         []ArtifactHandoffAsset `json:"assets"`
}

// ArtifactHandoffFile is one source file; Content is absent when Omitted is set.
type ArtifactHandoffFile struct {
	Path          string  `json:"path"`
	MediaType     string  `json:"mediaType"`
	ByteLength    int64   `json:"byteLength"`
	ContentDigest string  `json:"contentDigest"`
	Content       *string `json:"content,omitempty"`
	Omitted       string  `json:"omitted,omitempty"`
}

// ArtifactHandoff is the pack a coding agent builds the real app from.
type ArtifactHandoff struct {
	Artifact  ArtifactHandoffArtifact `json:"artifact"`
	Version   ArtifactHandoffVersion  `json:"version"`
	Pages     []ArtifactHandoffPage   `json:"pages"`
	Links     []ArtifactHandoffLink   `json:"links"`
	Spec      *ArtifactHandoffSpec    `json:"spec"`
	Notes     []ArtifactHandoffNote   `json:"notes"`
	Design    ArtifactHandoffDesign   `json:"design"`
	Files     []ArtifactHandoffFile   `json:"files"`
	Truncated bool                    `json:"truncated"`
	NextSteps string                  `json:"nextSteps"`
}

// ArtifactHandoffOpts configures a hand-off request.
type ArtifactHandoffOpts struct {
	VersionID      string // "" = the artifact's current version
	MaxInlineBytes int64  // 0–1048576 bytes of inlined source
}

func pageParams(after string, limit int) url.Values {
	params := url.Values{}
	if after != "" {
		params.Set("after", after)
	}
	if limit > 0 {
		params.Set("limit", strconv.Itoa(limit))
	}
	return params
}

func withQuery(path string, params url.Values) string {
	if len(params) == 0 {
		return path
	}
	return path + "?" + params.Encode()
}

func artifactPath(artifactID string) string {
	return "/artifacts/" + url.PathEscape(artifactID)
}

func artifactVersionPath(artifactID, versionID string) string {
	return artifactPath(artifactID) + "/versions/" + url.PathEscape(versionID)
}

// ArtifactsList lists the artifacts the token's user can read.
func (c *Client) ArtifactsList(ctx context.Context, opts ArtifactListOpts) (*ArtifactConnection, error) {
	params := pageParams(opts.After, opts.Limit)
	if opts.Scope != "" {
		params.Set("scope", opts.Scope)
	}
	if opts.Search != "" {
		params.Set("search", opts.Search)
	}
	var resp ArtifactConnection
	if err := c.do(ctx, http.MethodGet, withQuery("/artifacts/", params), nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ArtifactGet fetches an artifact's metadata and current version inventory.
func (c *Client) ArtifactGet(ctx context.Context, artifactID string) (*ArtifactGetResult, error) {
	var resp ArtifactGetResult
	if err := c.do(ctx, http.MethodGet, artifactPath(artifactID), nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ArtifactVersions lists an artifact's immutable versions.
func (c *Client) ArtifactVersions(ctx context.Context, artifactID string, opts ArtifactPageOpts) (*ArtifactVersionConnection, error) {
	var resp ArtifactVersionConnection
	path := withQuery(artifactPath(artifactID)+"/versions", pageParams(opts.After, opts.Limit))
	if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ArtifactVersionGet fetches one immutable version's manifest and file digests.
func (c *Client) ArtifactVersionGet(ctx context.Context, artifactID, versionID string) (*ArtifactVersion, error) {
	var resp ArtifactVersion
	if err := c.do(ctx, http.MethodGet, artifactVersionPath(artifactID, versionID), nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ArtifactSource reads one bounded slice of a source file of a pinned version.
func (c *Client) ArtifactSource(ctx context.Context, artifactID, versionID, filePath string, opts ArtifactSourceOpts) (*ArtifactSourcePage, error) {
	params := url.Values{"path": {filePath}}
	if opts.Cursor != "" {
		params.Set("cursor", opts.Cursor)
	} else if opts.StartOffset > 0 {
		params.Set("startOffset", strconv.FormatInt(opts.StartOffset, 10))
	}
	var resp ArtifactSourcePage
	path := withQuery(artifactVersionPath(artifactID, versionID)+"/source", params)
	if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// maxSourcePages bounds a whole-file read so a server that never reports eof
// cannot loop the CLI forever.
const maxSourcePages = 10000

// ArtifactSourceFile reads a whole source file, following nextCursor until eof.
// It returns the last page (for its metadata) and the concatenated text.
func (c *Client) ArtifactSourceFile(ctx context.Context, artifactID, versionID, filePath string) (*ArtifactSourcePage, string, error) {
	var text []byte
	opts := ArtifactSourceOpts{}
	for range maxSourcePages {
		page, err := c.ArtifactSource(ctx, artifactID, versionID, filePath, opts)
		if err != nil {
			return nil, "", err
		}
		text = append(text, page.Text...)
		if page.EOF {
			return page, string(text), nil
		}
		next := ArtifactSourceOpts{StartOffset: page.EndOffset}
		if page.NextCursor != nil && *page.NextCursor != "" {
			next = ArtifactSourceOpts{Cursor: *page.NextCursor}
		} else if page.EndOffset <= page.StartOffset {
			return nil, "", fmt.Errorf("api: source read of %q made no progress at offset %d", filePath, page.StartOffset)
		}
		opts = next
	}
	return nil, "", fmt.Errorf("api: source read of %q did not reach eof after %d pages", filePath, maxSourcePages)
}

// ArtifactDesign exports the shared design pinned to a version.
func (c *Client) ArtifactDesign(ctx context.Context, artifactID, versionID string) (*ArtifactDesign, error) {
	var resp ArtifactDesign
	if err := c.do(ctx, http.MethodGet, artifactVersionPath(artifactID, versionID)+"/design", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ArtifactStarters lists the curated offline design starters.
func (c *Client) ArtifactStarters(ctx context.Context) ([]ArtifactStarter, error) {
	var resp struct {
		Starters []ArtifactStarter `json:"starters"`
	}
	if err := c.do(ctx, http.MethodGet, "/artifacts/starters", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Starters, nil
}

// ArtifactHandoff fetches the hand-off pack used to build the real app.
func (c *Client) ArtifactHandoff(ctx context.Context, artifactID string, opts ArtifactHandoffOpts) (*ArtifactHandoff, error) {
	params := url.Values{"maxInlineBytes": {strconv.FormatInt(opts.MaxInlineBytes, 10)}}
	if opts.VersionID != "" {
		params.Set("versionId", opts.VersionID)
	}
	var resp ArtifactHandoff
	if err := c.do(ctx, http.MethodGet, withQuery(artifactPath(artifactID)+"/handoff", params), nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
