package api

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"mime"
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
	Required   []string
	Format     string
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
	Description string
	Parameters  []struct {
		Name   string
		In     string
		Schema *designContractSchema
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

func designContractUpload(data string) ArtifactUpload {
	return ArtifactUpload{
		ByteLength: int64(len(data)), ContentDigest: fmt.Sprintf("%x", sha256.Sum256([]byte(data))),
		Open: func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(data)), nil },
	}
}

func TestDesignsOpenAPIContract(t *testing.T) {
	snapshotJSON, err := os.ReadFile("../../testdata/artifacts.openapi.json")
	require.NoError(t, err)
	var snapshot struct {
		Paths map[string]map[string]designContractOperation
	}
	require.NoError(t, json.Unmarshal(snapshotJSON, &snapshot))
	ctx := context.Background()
	const (
		id         = "aaaaaaaaaaaaaaaaaaaaaaaa"
		versionID  = "bbbbbbbbbbbbbbbbbbbbbbbb"
		artifact   = "/artifacts/{artifactId}"
		version    = artifact + "/versions/{versionId}"
		systems    = "/artifacts/design-systems"
		system     = systems + "/{systemId}"
		data       = artifact + "/data/{collection}"
		dataRecord = data + "/records/{key}"
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
	type dataRecordBody struct {
		Key       string          `json:"key"`
		Revision  int64           `json:"revision"`
		UpdatedAt string          `json:"updatedAt"`
		Subject   *string         `json:"subject"`
		Value     json.RawMessage `json:"value"`
	}
	type dataOne struct {
		DataNotice string         `json:"dataNotice"`
		Record     dataRecordBody `json:"record"`
	}
	type dataBatch struct {
		DataNotice string           `json:"dataNotice"`
		Records    []dataRecordBody `json:"records"`
	}
	type dataBatchInput struct {
		Records []struct {
			Key     string `json:"key"`
			Subject string `json:"subject,omitempty"`
			ArtifactDataSetInput
		} `json:"records"`
	}
	type dataPage struct {
		dataBatch
		NextCursor  *string `json:"nextCursor"`
		WatchCursor string  `json:"watchCursor"`
	}
	type dataDelete struct {
		DataNotice string `json:"dataNotice"`
		Deleted    bool   `json:"deleted"`
	}
	type dataWatch struct {
		DataNotice string `json:"dataNotice"`
		Changed    bool   `json:"changed"`
		Cursor     string `json:"cursor"`
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
			return designContractRequest(c.ArtifactCreate(ctx, ArtifactCreateInput{},
				ArtifactCheckFile{Path: "assets/hero.png", MediaType: "image/png", ArtifactUpload: designContractUpload("image bytes")}))
		}},
		{"get", artifact, "200", nil, ArtifactGetResult{}, func(c *Client) error {
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
			if _, err := c.ArtifactGuide(ctx, "landing_page", "react", ""); err != nil {
				return err
			}
			return designContractRequest(c.ArtifactGuide(ctx, "", "", "review"))
		}},
		{"get", "/artifacts/guide", "200", nil, ArtifactGuide{}, func(c *Client) error {
			return designContractRequest(c.ArtifactGuideTopic(ctx, "design-system-from-codebase"))
		}},
		{"get", systems, "200", nil, ArtifactDesignSystemList{}, func(c *Client) error {
			return designContractRequest(c.ArtifactDesignSystems(ctx))
		}},
		{"get", system, "200", nil, ArtifactDesignSystem{}, func(c *Client) error {
			return designContractRequest(c.ArtifactDesignSystem(ctx, id))
		}},
		{"get", system + "/package", "200", nil, ArtifactSystemTransfer{}, func(c *Client) error {
			return designContractRequest(c.ArtifactDesignSystemPackage(ctx, id, "package", 5))
		}},
		{"get", system + "/installation", "200", nil, ArtifactSystemProjection{}, func(c *Client) error {
			return designContractRequest(c.ArtifactDesignSystemInstallation(ctx, id, "vite_react", "shadcn", 5))
		}},
		{"post", systems, "201", ArtifactSystemCreate{}, ArtifactDesignSystem{}, func(c *Client) error {
			return designContractRequest(c.ArtifactDesignSystemCreate(ctx, ArtifactSystemCreate{}))
		}},
		{"patch", system, "200", ArtifactSystemReplace{}, ArtifactDesignSystem{}, func(c *Client) error {
			return designContractRequest(c.ArtifactDesignSystemReplace(ctx, id, ArtifactSystemReplace{}))
		}},
		{"post", systems + "/save-from", "201", ArtifactSystemSave{}, ArtifactDesignSystem{}, func(c *Client) error {
			return designContractRequest(c.ArtifactDesignSystemSave(ctx, ArtifactSystemSave{}))
		}},
		{"get", systems + "/defaults", "200", nil, ArtifactSystemDefaults{}, func(c *Client) error {
			return designContractRequest(c.ArtifactDesignSystemDefaults(ctx))
		}},
		{"patch", systems + "/defaults", "200", ArtifactSystemDefaultInput{}, ArtifactSystemDefaults{}, func(c *Client) error {
			return designContractRequest(c.ArtifactDesignSystemSetDefault(ctx, ArtifactSystemDefaultInput{}))
		}},
		{"post", artifact + "/design-system-refresh", "201", ArtifactSystemRefresh{}, ArtifactDesignChange{}, func(c *Client) error {
			return designContractRequest(c.ArtifactDesignSystemRefresh(ctx, id, ArtifactSystemRefresh{}))
		}},
		{"post", artifact + "/shadcn/setup", "201", ArtifactShadcnSetup{}, ArtifactShadcnSetupResult{}, func(c *Client) error {
			return designContractRequest(c.ArtifactShadcnSetup(ctx, id, ArtifactShadcnSetup{}))
		}},
		{"post", "/artifacts/check", "200", ArtifactCheckInput{}, ArtifactCheckResult{}, func(c *Client) error {
			if _, err := c.ArtifactCheck(ctx, ArtifactCheckInput{}); err != nil {
				return err
			}
			return designContractRequest(c.ArtifactCheck(ctx, ArtifactCheckInput{},
				ArtifactCheckFile{Path: "assets/hero.png", MediaType: "image/png", ArtifactUpload: designContractUpload("image bytes")}))
		}},
		{"get", version + "/build", "200", nil, ArtifactBuildResult{}, func(c *Client) error {
			if _, err := c.ArtifactBuild(ctx, id, versionID, 10); err != nil {
				return err
			}
			return designContractRequest(c.ArtifactBuildForPreview(ctx, id, versionID, 10))
		}},
		{"get", version + "/pages/{pageId}/preview", "200", nil, ArtifactPreview{}, func(c *Client) error {
			return designContractRequest(c.ArtifactPagePreview(ctx, id, versionID, "home"))
		}},
		{"post", artifact + "/assets", "200", multipartAsset{}, ArtifactUploadedAsset{}, func(c *Client) error {
			return designContractRequest(c.ArtifactAssetUpload(ctx, id, ArtifactCheckFile{Path: "assets/logo.png", MediaType: "image/png", ArtifactUpload: designContractUpload("image bytes")}, "image", "Logo"))
		}},
		{"get", artifact + "/design-assets", "200", nil, struct {
			Assets []ArtifactUploadedAsset `json:"assets"`
		}{}, func(c *Client) error {
			return designContractRequest(c.ArtifactAssets(ctx, id))
		}},
		{"get", version + "/archive", "200", nil, ArtifactArchive{}, func(c *Client) error {
			return designContractRequest(c.ArtifactArchiveLink(ctx, id, versionID, 2))
		}},
		{"post", "/artifacts/archives", "200", multipartArchive{}, Artifact{}, func(c *Client) error {
			return designContractRequest(c.ArtifactImport(ctx, "design.zip", designContractUpload("archive bytes"), "key", "codex"))
		}},
		{"post", version + "/restore", "201", ArtifactRestoreInput{}, ArtifactPushResult{}, func(c *Client) error {
			return designContractRequest(c.ArtifactRestore(ctx, id, versionID, ArtifactRestoreInput{}))
		}},
		{"post", artifact + "/fork", "200", ArtifactForkInput{}, ArtifactForkResult{}, func(c *Client) error {
			return designContractRequest(c.ArtifactFork(ctx, id, ArtifactForkInput{VersionID: versionID, Title: "Copy", Plain: true, IdempotencyKey: "key", Tool: "codex"}))
		}},
		{"get", dataRecord, "200", nil, dataOne{}, func(c *Client) error {
			return designContractRequest(c.ArtifactDataGet(ctx, id, "answers", "item-1", "av_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"))
		}},
		{"get", data + "/records", "200", nil, dataPage{}, func(c *Client) error {
			return designContractRequest(c.ArtifactDataList(ctx, id, "answers", ArtifactDataListOpts{
				Limit: 10, Cursor: "v1.page.sig", Filter: `{"decision":"approved"}`,
			}))
		}},
		{"post", dataRecord, "200", ArtifactDataSetInput{}, dataOne{}, func(c *Client) error {
			return designContractRequest(c.ArtifactDataSet(ctx, id, "answers", "item-1", "", ArtifactDataSetInput{Value: json.RawMessage(`{}`)}))
		}},
		{"delete", dataRecord, "200", ArtifactDataDeleteInput{}, dataDelete{}, func(c *Client) error {
			return designContractRequest(c.ArtifactDataDelete(ctx, id, "answers", "item-1", "", ArtifactDataDeleteInput{IfRevision: 1}))
		}},
		{"post", data + "/batch", "200", dataBatchInput{}, dataBatch{}, func(c *Client) error {
			return designContractRequest(c.ArtifactDataBatch(ctx, id, "answers", json.RawMessage(`{"records":[{"key":"item-1","ifRevision":1,"value":{}}]}`)))
		}},
		{"get", data + "/watch", "200", nil, dataWatch{}, func(c *Client) error {
			return designContractRequest(c.ArtifactDataWatch(ctx, id, "answers", "v1.mark.sig"))
		}},
	}
	for _, route := range routes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			op, ok := snapshot.Paths[route.path][route.method]
			require.True(t, ok, "operation is missing from the snapshot")
			response := op.Responses[route.success].Content["application/json"].Schema
			if route.path == "/artifacts/guide" {
				require.NotNil(t, response)
				facts := response.field("facts")
				require.NotNil(t, facts)
				toolchain, packages := facts.field("toolchain"), facts.field("packages")
				require.NotNil(t, toolchain)
				require.NotNil(t, packages)
				require.NotNil(t, packages.Items)
				assert.NotNil(t, toolchain.field("requiredPackages"))
				assert.NotNil(t, packages.Items.field("name"))
				assert.NotNil(t, packages.Items.field("version"))
				for _, parameter := range op.Parameters {
					if parameter.Name == "template" {
						require.NotNil(t, parameter.Schema)
						assert.True(t, parameter.Schema.allows("review"), "review template is missing")
					}
					if parameter.Name == "kind" {
						require.NotNil(t, parameter.Schema)
						for _, kind := range []string{"landing_page", "web_app", "deck", "report", "dashboard",
							"website", "presentation", "tool", "visualization", "document"} {
							assert.True(t, parameter.Schema.allows(kind), "guide kind %q is missing", kind)
						}
					}
				}
			}
			assertDesignContractFields(t, response, reflect.TypeOf(route.output), "response")
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
				if r.Body != nil {
					_, err := io.Copy(io.Discard, r.Body)
					require.NoError(t, err)
					require.NoError(t, r.Body.Close())
				}
				assert.Equal(t, strings.ToUpper(route.method), r.Method)
				wantPath := strings.NewReplacer(
					"{artifactId}", id,
					"{versionId}", versionID,
					"{systemId}", id,
					"{pageId}", "home",
					"{collection}", "answers",
					"{key}", "item-1",
				).Replace(route.path)
				assert.Equal(t, wantPath, r.URL.Path)
				if route.method == "post" && (route.path == "/artifacts/" || route.path == "/artifacts/check") {
					mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
					require.NoError(t, err)
					assert.Contains(t, op.RequestBody.Content, mediaType)
					if route.path == "/artifacts/" {
						assert.Equal(t, "multipart/form-data", mediaType)
					}
				}
				query := map[string]bool{}
				for _, param := range op.Parameters {
					if param.In == "query" {
						query[param.Name] = true
					}
				}
				for name := range r.URL.Query() {
					assert.True(t, query[name], "query parameter %q is missing from the snapshot", name)
				}
				body := "{}"
				if r.URL.Query().Get("template") == "review" {
					body = `{"template":{"id":"review"}}`
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(body)),
				}, nil
			})
			require.NoError(t, route.call(client))
			assert.True(t, called)
		})
	}

	build := snapshot.Paths[version+"/build"]["get"]
	t.Run("preview versus evidence wait", func(t *testing.T) {
		var waitFor *designContractSchema
		for _, param := range build.Parameters {
			if param.In == "query" && param.Name == "waitFor" {
				waitFor = param.Schema
			}
		}
		require.NotNil(t, waitFor)
		assert.True(t, waitFor.allows("preview"))
		assert.True(t, waitFor.allows("evidence"))

		response := build.Responses["200"].Content["application/json"].Schema
		evidence := response.field("evidenceStatus")
		require.NotNil(t, evidence)
		for _, status := range []string{"pending", "ready", "failed"} {
			assert.True(t, evidence.allows(status), "evidence status %q is missing", status)
		}
	})

	conflict := snapshot.Paths[artifact+"/versions"]["post"].Responses["409"].Content["application/json"].Schema
	require.NotNil(t, conflict)
	for _, name := range []string{"reason", "changedPaths", "retryAfterSeconds"} {
		assert.NotNil(t, conflict.field(name), "push conflict field %q is missing", name)
	}

	t.Run("design authoring additions", func(t *testing.T) {
		create := snapshot.Paths["/artifacts/"]["post"].RequestBody.Content["application/json"].Schema
		assert.NotNil(t, create.field("changeSummary"))
		for _, field := range []string{"designSystemRevision", "designSystemKit", "noDesignSystem"} {
			assert.NotNil(t, create.field(field))
		}

		asset := snapshot.Paths[artifact+"/assets"]["post"]
		metadata := asset.RequestBody.Content["multipart/form-data"].Schema.field("metadata")
		require.NotNil(t, metadata)
		assert.NotNil(t, metadata.field("path"))
		busy := asset.Responses["503"].Content["application/json"].Schema
		if assert.NotNil(t, busy, "an asset upload can return 503") {
			assert.NotNil(t, busy.field("retryAfterSeconds"))
		}
		assert.Contains(t, asset.Description, "Retry-After")

		list := snapshot.Paths[artifact+"/versions"]["get"].Responses["200"].Content["application/json"].Schema
		listedVersion := list.field("edges").Items.field("node")
		getVersion := snapshot.Paths[version]["get"].Responses["200"].Content["application/json"].Schema
		assert.Nil(t, listedVersion.field("memberRuntimeDeclarations"))
		receipt := snapshot.Paths[artifact+"/versions"]["post"].Responses["201"].Content["application/json"].Schema
		assert.Nil(t, receipt.field("version").field("memberRuntimeDeclarations"))
		for _, name := range []string{"version", "createdAt", "origin", "authorKind", "tool", "changeSummary"} {
			assert.NotNil(t, listedVersion.field(name), "version list field %q", name)
			assert.NotNil(t, getVersion.field(name), "version detail field %q", name)
		}

		for _, path := range []string{"/artifacts/", "/artifacts/check"} {
			form := snapshot.Paths[path]["post"].RequestBody.Content["multipart/form-data"].Schema
			require.NotNil(t, form, "%s accepts multipart", path)
			assert.Contains(t, form.Required, "project")
			assert.NotNil(t, form.field("project"))
			file := form.field("file")
			require.NotNil(t, file)
			require.NotNil(t, file.Items)
			assert.Equal(t, "binary", file.Items.Format)
		}
	})
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
		// Plain-page templates use the project settings without dependencies.
		if child == nil && path == "response.template.project" && name == "dependencies" {
			continue
		}
		// Older previews exposed visualReviewStatus. The compact version DTO
		// used by list and push omits expanded source and declarations.
		if child == nil && (name == "visualReviewStatus" ||
			(model == reflect.TypeFor[ArtifactVersion]() && (name == "authoringManifest" || name == "files" || name == "memberRuntimeDeclarations") &&
				(path == "response.edges[].node" || path == "response.version"))) {
			continue
		}
		assertDesignContractFields(t, child, field.Type, path+"."+name)
	}
}

// A changed file must abort a streaming form before its closing boundary.
func TestDesignsUploadRefusesChangedFile(t *testing.T) {
	client := New("https://api.example", "tok_test_fake", time.Second)
	client.httpClient.Transport = designContractTransport(func(r *http.Request) (*http.Response, error) {
		defer r.Body.Close()
		_, err := io.Copy(io.Discard, r.Body)
		require.ErrorContains(t, err, "file differs from its scanned size or SHA-256")
		return nil, err
	})
	file := designContractUpload("before")
	file.Open = func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("change")), nil }
	_, err := client.ArtifactCreate(context.Background(), ArtifactCreateInput{}, ArtifactCheckFile{
		Path: "vendor/example.js", MediaType: "application/javascript", ArtifactUpload: file,
	})
	require.ErrorContains(t, err, "file differs from its scanned size or SHA-256")
}
