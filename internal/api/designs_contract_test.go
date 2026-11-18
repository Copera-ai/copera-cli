package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type designContractSchema struct {
	Properties map[string]*designContractSchema
	Items      *designContractSchema
	AnyOf      []*designContractSchema
	Enum       []json.RawMessage
}

func (s *designContractSchema) field(name string) *designContractSchema {
	if field := s.Properties[name]; field != nil {
		return field
	}
	for _, choice := range s.AnyOf {
		if field := choice.field(name); field != nil {
			return field
		}
	}
	return nil
}

func (s *designContractSchema) allows(value string) bool {
	for _, raw := range s.Enum {
		var name string
		if json.Unmarshal(raw, &name) == nil && name == value {
			return true
		}
	}
	for _, choice := range s.AnyOf {
		if choice.allows(value) {
			return true
		}
	}
	return false
}

type designContractContent map[string]struct {
	Schema *designContractSchema
}

type designContractOperation struct {
	Parameters []struct {
		Name string
		In   string
	}
	RequestBody struct {
		Content designContractContent
	}
	Responses map[string]struct {
		Content designContractContent
	}
}

type designContractTransport func(*http.Request) (*http.Response, error)

func (f designContractTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func designContractRequest(_ any, err error) error { return err }

func TestDesignsOpenAPIContract(t *testing.T) {
	data, err := os.ReadFile("../../testdata/artifacts.openapi.json")
	require.NoError(t, err)
	var snapshot struct {
		Paths map[string]map[string]designContractOperation
	}
	require.NoError(t, json.Unmarshal(data, &snapshot))
	ctx := context.Background()
	const (
		id        = "aaaaaaaaaaaaaaaaaaaaaaaa"
		versionID = "bbbbbbbbbbbbbbbbbbbbbbbb"
		artifact  = "/artifacts/{artifactId}"
		version   = artifact + "/versions/{versionId}"
	)
	type multipartAsset struct {
		File     string                `json:"file"`
		Metadata artifactAssetMetadata `json:"metadata"`
	}
	type multipartArchive struct {
		File     string                 `json:"file"`
		Metadata artifactImportMetadata `json:"metadata"`
	}
	type startersResult struct {
		Starters []ArtifactStarter `json:"starters"`
	}
	type systemsResult struct {
		DesignSystems []ArtifactDesignSystem `json:"designSystems"`
	}

	routes := []struct {
		method, path, success string
		input, output         any
		call                  func(*Client) error
	}{
		{"get", "/artifacts/", "200", nil, ArtifactConnection{}, func(c *Client) error {
			return designContractRequest(c.ArtifactsList(ctx, ArtifactListOpts{Scope: "shared", Search: "launch", After: "cursor", Limit: 10}))
		}},
		{"post", "/artifacts/", "200", ArtifactCreateInput{}, ArtifactCreateResult{}, func(c *Client) error {
			return designContractRequest(c.ArtifactCreate(ctx, ArtifactCreateInput{}))
		}},
		{"get", artifact, "200", nil, Artifact{}, func(c *Client) error {
			return designContractRequest(c.ArtifactGet(ctx, id))
		}},
		{"patch", artifact, "200", artifactRenameInput{}, ArtifactRenameResult{}, func(c *Client) error {
			return designContractRequest(c.ArtifactRename(ctx, id, "Design"))
		}},
		{"delete", artifact, "200", nil, ArtifactTrashResult{}, func(c *Client) error {
			return designContractRequest(c.ArtifactTrash(ctx, id))
		}},
		{"get", artifact + "/versions", "200", nil, ArtifactVersionConnection{}, func(c *Client) error {
			return designContractRequest(c.ArtifactVersions(ctx, id, ArtifactPageOpts{After: "cursor", Limit: 10}))
		}},
		{"post", artifact + "/versions", "201", ArtifactPushInput{}, ArtifactPushResult{}, func(c *Client) error {
			return designContractRequest(c.ArtifactPush(ctx, id, ArtifactPushInput{}))
		}},
		{"get", version, "200", nil, ArtifactVersion{}, func(c *Client) error {
			return designContractRequest(c.ArtifactVersionGet(ctx, id, versionID))
		}},
		{"get", version + "/source", "200", nil, ArtifactSourcePage{}, func(c *Client) error {
			if _, err := c.ArtifactSource(ctx, id, versionID, "index.html", ArtifactSourceOpts{Cursor: "cursor"}); err != nil {
				return err
			}
			return designContractRequest(c.ArtifactSource(ctx, id, versionID, "index.html", ArtifactSourceOpts{StartOffset: 1}))
		}},
		{"get", version + "/design", "200", nil, ArtifactDesign{}, func(c *Client) error {
			return designContractRequest(c.ArtifactDesign(ctx, id, versionID))
		}},
		{"get", artifact + "/handoff", "200", nil, ArtifactHandoff{}, func(c *Client) error {
			return designContractRequest(c.ArtifactHandoff(ctx, id, ArtifactHandoffOpts{VersionID: versionID, MaxInlineBytes: 1000}))
		}},
		{"get", "/artifacts/starters", "200", nil, startersResult{}, func(c *Client) error {
			return designContractRequest(c.ArtifactStarters(ctx))
		}},
		{"get", "/artifacts/guide", "200", nil, ArtifactGuide{}, func(c *Client) error {
			return designContractRequest(c.ArtifactGuide(ctx, "landing_page", "react"))
		}},
		{"get", "/artifacts/design-systems", "200", nil, systemsResult{}, func(c *Client) error {
			return designContractRequest(c.ArtifactDesignSystems(ctx))
		}},
		{"post", "/artifacts/check", "200", ArtifactCheckInput{}, ArtifactCheckResult{}, func(c *Client) error {
			return designContractRequest(c.ArtifactCheck(ctx, ArtifactCheckInput{}))
		}},
		{"get", version + "/build", "200", nil, ArtifactBuildResult{}, func(c *Client) error {
			return designContractRequest(c.ArtifactBuild(ctx, id, versionID, 10))
		}},
		{"post", artifact + "/assets", "200", multipartAsset{}, ArtifactUploadedAsset{}, func(c *Client) error {
			return designContractRequest(c.ArtifactAssetUpload(ctx, id, "file.png", []byte("image bytes"), "image", "Logo"))
		}},
		{"get", version + "/archive", "200", nil, ArtifactArchive{}, func(c *Client) error {
			return designContractRequest(c.ArtifactArchiveLink(ctx, id, versionID))
		}},
		{"post", "/artifacts/archives", "200", multipartArchive{}, Artifact{}, func(c *Client) error {
			return designContractRequest(c.ArtifactImport(ctx, "design.zip", []byte("archive bytes"), "key", "codex"))
		}},
		{"post", version + "/restore", "201", ArtifactRestoreInput{}, ArtifactPushResult{}, func(c *Client) error {
			return designContractRequest(c.ArtifactRestore(ctx, id, versionID, ArtifactRestoreInput{}))
		}},
		{"post", artifact + "/fork", "201", artifactForkInput{}, Artifact{}, func(c *Client) error {
			return designContractRequest(c.ArtifactFork(ctx, id, "key", "codex"))
		}},
	}
	for _, route := range routes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			op, ok := snapshot.Paths[route.path][route.method]
			require.True(t, ok, "operation is missing from the snapshot")
			response := op.Responses[route.success].Content["application/json"].Schema
			assertDesignContractFields(t, response, reflect.TypeOf(route.output), "response")
			if route.path == "/artifacts/guide" {
				facts := response.field("facts")
				require.NotNil(t, facts)
				toolchain, packages := facts.field("toolchain"), facts.field("packages")
				require.NotNil(t, toolchain)
				require.NotNil(t, packages)
				require.NotNil(t, packages.Items)
				assert.NotNil(t, toolchain.field("requiredPackages"))
				assert.NotNil(t, packages.Items.field("name"))
				assert.NotNil(t, packages.Items.field("version"))
			}
			if route.input != nil {
				content := "application/json"
				switch route.input.(type) {
				case multipartAsset, multipartArchive:
					content = "multipart/form-data"
				}
				schema := op.RequestBody.Content[content].Schema
				assertDesignContractFields(t, schema, reflect.TypeOf(route.input), "request")
				require.NotNil(t, schema)
				metadata := schema
				if content == "multipart/form-data" {
					metadata = schema.field("metadata")
				}
				if metadata != nil && metadata.field("tool") != nil {
					for _, name := range []string{"claude_code", "codex", "copera_cli"} {
						assert.True(t, metadata.field("tool").allows(name), "tool %q is missing from the snapshot", name)
					}
				}
			}

			client := New("https://api.example", "tok_test_fake", time.Second)
			called := false
			client.httpClient.Transport = designContractTransport(func(r *http.Request) (*http.Response, error) {
				called = true
				assert.Equal(t, strings.ToUpper(route.method), r.Method)
				assert.Equal(t, strings.NewReplacer("{artifactId}", id, "{versionId}", versionID).Replace(route.path), r.URL.Path)
				query := map[string]bool{}
				for _, param := range op.Parameters {
					if param.In == "query" {
						query[param.Name] = true
					}
				}
				for name := range r.URL.Query() {
					assert.True(t, query[name], "query parameter %q is missing from the snapshot", name)
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader("{}")),
				}, nil
			})
			require.NoError(t, route.call(client))
			assert.True(t, called)
		})
	}

	conflict := snapshot.Paths[artifact+"/versions"]["post"].Responses["409"].Content["application/json"].Schema
	require.NotNil(t, conflict)
	for _, name := range []string{"reason", "changedPaths", "retryAfterSeconds"} {
		assert.NotNil(t, conflict.field(name), "push conflict field %q is missing", name)
	}
}

// Match JSON tags on the real API models, including embedded objects and array elements.
func assertDesignContractFields(t *testing.T, schema *designContractSchema, model reflect.Type, path string) {
	t.Helper()
	if !assert.NotNil(t, schema, "%s is missing from the snapshot", path) {
		return
	}
	if model.Kind() == reflect.Pointer {
		model = model.Elem()
	}
	if model.Kind() == reflect.Slice && schema.Items != nil {
		assertDesignContractFields(t, schema.Items, model.Elem(), path+"[]")
	}
	if model.Kind() != reflect.Struct {
		return
	}
	for field := range model.Fields() {
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if field.Anonymous && name == "" {
			assertDesignContractFields(t, schema, field.Type, path)
			continue
		}
		if name == "" || name == "-" {
			continue
		}
		child := schema.field(name)
		// Older previews exposed visualReviewStatus. The compact version DTO
		// used by the list and push omits the full source manifest and inventory.
		if child == nil && (name == "visualReviewStatus" ||
			(model == reflect.TypeFor[ArtifactVersion]() && (name == "authoringManifest" || name == "files") &&
				(path == "response.edges[].node" || path == "response.version"))) {
			continue
		}
		assertDesignContractFields(t, child, field.Type, path+"."+name)
	}
}
