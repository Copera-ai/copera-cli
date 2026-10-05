package commands

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/copera/copera-cli/internal/api"
	"github.com/copera/copera-cli/internal/exitcodes"
	"github.com/copera/copera-cli/internal/upload"
	"github.com/spf13/cobra"
)

// handoffMaxInlineBytes is the server's ceiling (and the CLI default) for
// source inlined in the hand-off pack: 1 MiB of UTF-8 text.
const handoffMaxInlineBytes = 1 << 20

// handoffMaxAssetBytes bounds one downloaded design asset.
const handoffMaxAssetBytes = 100 << 20

// Paths of the files the CLI writes next to the artifact's own source.
const (
	handoffNotesFile   = "NOTES.md"
	handoffGuideFile   = "HANDOFF.md"
	handoffPagesFile   = "pages.json"
	handoffDesignFile  = "design/design.json"
	handoffAssetsDir   = "design/assets"
	handoffFallbackDir = ".copera"
)

// Reasons a file of the pack is not written.
const (
	skipSensitive   = "sensitive"
	skipMissing     = "missing_content"
	skipUnsignedURL = "unsigned_url"
)

// handoffSkip is one file the pack did not write, and why.
type handoffSkip struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// handoffSummary is the JSON output of `artifacts handoff`.
type handoffSummary struct {
	Dir          string        `json:"dir"`
	ArtifactID   string        `json:"artifactId"`
	VersionID    string        `json:"versionId"`
	URL          string        `json:"url"`
	FilesWritten []string      `json:"filesWritten"`
	Fetched      []string      `json:"fetched"`
	Skipped      []handoffSkip `json:"skipped"`
	Truncated    bool          `json:"truncated"`
	Warnings     []string      `json:"warnings"`
}

func newArtifactsHandoffCmd(cli *CLI) *cobra.Command {
	var flagVersion, flagOut string
	var flagMaxInline int64
	var flagForce bool

	cmd := &cobra.Command{
		Use:   "handoff <artifactId>",
		Short: "Pull an artifact's hand-off pack into a folder",
		Long: `Pull everything needed to build an artifact as a real app into one folder:

  SPEC.md (at the Spec's own path)  the source of truth for what to build
  NOTES.md                           notes grouped by page, Spec notes first
  design/design.json                 shared design digest, paths and asset metadata
  design/assets/*                    logos, fonts and images (downloaded right away)
  pages.json                         pages and the navigation links between them
  HANDOFF.md                         next steps, the artifact URL, how to build
  <every source file>                at its own relative path

Files too large to inline are fetched through the source endpoint until the
end. Files that look like they hold a key or password are never fetched; they
are reported as skipped. --out defaults to ./<slug-of-title>; a non-empty
folder is refused unless --force (existing files are overwritten, others kept).

  copera artifacts handoff <artifactId> --out ./my-app`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if flagMaxInline < 0 || flagMaxInline > handoffMaxInlineBytes {
				cli.Printer.PrintError("invalid_flag", fmt.Sprintf("max-inline-bytes must be between 0 and %d", handoffMaxInlineBytes), "", false)
				return exitcodes.Newf(exitcodes.Usage, "max-inline-bytes must be between 0 and %d", handoffMaxInlineBytes)
			}
			if flagOut != "" {
				if err := checkHandoffOutDir(cli, flagOut, flagForce); err != nil {
					return err
				}
			}
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}
			ctx := context.Background()

			pack, err := client.ArtifactHandoff(ctx, args[0], api.ArtifactHandoffOpts{
				VersionID:      flagVersion,
				MaxInlineBytes: flagMaxInline,
			})
			if err != nil {
				return artifactAPIError(cli, err)
			}

			plan, err := planHandoff(pack)
			if err != nil {
				cli.Printer.PrintError("unsafe_path", err.Error(),
					"The hand-off pack contains a path that would escape the output folder; nothing was written", false)
				return exitcodes.New(exitcodes.Error, err)
			}

			dir := flagOut
			if dir == "" {
				dir = slugify(pack.Artifact.Title, "artifact-"+pack.Artifact.ID)
				if err := checkHandoffOutDir(cli, dir, flagForce); err != nil {
					return err
				}
			}

			summary, err := writeHandoff(ctx, cli, client, pack, plan, dir)
			if err != nil {
				cli.Printer.PrintError("handoff_failed", err.Error(),
					fmt.Sprintf("Files written so far are in %s; re-run with --force to retry", dir), false)
				return exitcodes.New(exitcodes.Error, err)
			}

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(summary)
			}
			cli.Printer.PrintLine(fmt.Sprintf("Artifact:  %s", pack.Artifact.Title))
			cli.Printer.PrintLine(fmt.Sprintf("Version:   %d (%s)", pack.Version.Number, pack.Version.ID))
			cli.Printer.PrintLine(fmt.Sprintf("Folder:    %s", summary.Dir))
			cli.Printer.PrintLine(fmt.Sprintf("Written:   %d files", len(summary.FilesWritten)))
			if len(summary.Fetched) > 0 {
				cli.Printer.PrintLine(fmt.Sprintf("Fetched:   %d large files through the source endpoint", len(summary.Fetched)))
			}
			for _, s := range summary.Skipped {
				cli.Printer.PrintLine(fmt.Sprintf("Skipped:   %s (%s)", s.Path, s.Reason))
			}
			cli.Printer.PrintLine(fmt.Sprintf("Next:      open %s", filepath.Join(summary.Dir, handoffGuideFile)))
			return nil
		},
	}
	cmd.Flags().StringVar(&flagVersion, "version", "", "Version ID (default: the artifact's current version)")
	cmd.Flags().StringVarP(&flagOut, "out", "o", "", "Output folder (default: ./<slug-of-title>)")
	cmd.Flags().Int64Var(&flagMaxInline, "max-inline-bytes", handoffMaxInlineBytes, "Bytes of source the server inlines (0-1048576); the rest is fetched page by page")
	cmd.Flags().BoolVar(&flagForce, "force", false, "Write into a non-empty output folder")
	return cmd
}

// checkHandoffOutDir refuses an output folder that already has content unless
// force is set, and refuses a path that exists but is not a folder.
func checkHandoffOutDir(cli *CLI, dir string, force bool) error {
	info, err := os.Stat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		cli.Printer.PrintError("invalid_path", err.Error(), "", false)
		return exitcodes.New(exitcodes.Error, err)
	}
	if !info.IsDir() {
		err := fmt.Errorf("output path %q exists and is not a folder", dir)
		cli.Printer.PrintError("invalid_path", err.Error(), "Choose another --out", false)
		return exitcodes.New(exitcodes.Usage, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		cli.Printer.PrintError("invalid_path", err.Error(), "", false)
		return exitcodes.New(exitcodes.Error, err)
	}
	if len(entries) > 0 && !force {
		err := fmt.Errorf("output folder %q is not empty", dir)
		cli.Printer.PrintError("out_dir_not_empty", err.Error(),
			"Choose an empty --out folder, or pass --force to write into it", false)
		return exitcodes.New(exitcodes.Conflict, err)
	}
	return nil
}

// handoffPlan maps every server-provided path of the pack to a validated
// local relative path, before anything is written.
type handoffPlan struct {
	files    map[string]string // pack file path -> local path
	spec     string            // local Spec path ("" when the pack has none)
	assets   []string          // local asset path, index-matched to design.assets
	notes    string
	guide    string
	pages    string
	design   string
	reserved map[string]bool
}

// planHandoff validates every path in the pack and resolves the CLI's own
// files around the artifact's source: when a source file already uses one of
// their names, the CLI file moves under .copera/ instead of overwriting it.
func planHandoff(pack *api.ArtifactHandoff) (*handoffPlan, error) {
	plan := &handoffPlan{files: map[string]string{}, reserved: map[string]bool{}}
	claim := func(p string) { plan.reserved[strings.ToLower(p)] = true }
	taken := func(p string) bool { return plan.reserved[strings.ToLower(p)] }

	for _, f := range pack.Files {
		local, err := safeRelPath(f.Path)
		if err != nil {
			return nil, err
		}
		plan.files[f.Path] = local
		claim(local)
	}
	if pack.Spec != nil {
		local, err := safeRelPath(pack.Spec.Path)
		if err != nil {
			return nil, err
		}
		plan.spec = local
		claim(local)
	}
	// Page entrypoints and file lists are written to pages.json only, but an
	// unsafe one still means the pack cannot be trusted.
	for _, p := range pack.Pages {
		for _, fp := range append([]string{p.Entrypoint}, p.Files...) {
			if _, err := safeRelPath(fp); err != nil {
				return nil, err
			}
		}
	}
	for _, fp := range pack.Design.Files {
		if _, err := safeRelPath(fp); err != nil {
			return nil, err
		}
	}

	own := func(name string) string {
		local := filepath.FromSlash(name)
		if taken(local) {
			local = filepath.Join(handoffFallbackDir, local)
		}
		claim(local)
		return local
	}
	plan.notes = own(handoffNotesFile)
	plan.guide = own(handoffGuideFile)
	plan.pages = own(handoffPagesFile)
	plan.design = own(handoffDesignFile)

	for _, a := range pack.Design.Assets {
		if _, err := safeRelPath(a.Path); err != nil {
			return nil, err
		}
		name := path.Base(a.Path)
		local := filepath.Join(filepath.FromSlash(handoffAssetsDir), name)
		if taken(local) {
			local = filepath.Join(filepath.FromSlash(handoffAssetsDir), sanitizeFilename(a.ID)+"-"+name)
		}
		if taken(local) {
			return nil, fmt.Errorf("design asset %q collides with another file of the pack", a.Path)
		}
		claim(local)
		plan.assets = append(plan.assets, local)
	}
	return plan, nil
}

// safeRelPath turns a server-provided, slash-separated path into a local
// relative path that cannot leave the output folder. Absolute paths, drive
// or UNC prefixes, backslashes, NUL bytes and any ".." segment are rejected.
func safeRelPath(p string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("empty file path in hand-off pack")
	}
	if strings.ContainsAny(p, "\\\x00") {
		return "", fmt.Errorf("unsafe file path %q: backslashes and NUL bytes are not allowed", p)
	}
	if strings.HasPrefix(p, "/") || filepath.IsAbs(p) || filepath.VolumeName(p) != "" ||
		(len(p) >= 2 && p[1] == ':') {
		return "", fmt.Errorf("unsafe file path %q: absolute paths are not allowed", p)
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return "", fmt.Errorf("unsafe file path %q: path traversal is not allowed", p)
		}
	}
	cleaned := path.Clean(p)
	if cleaned == "." {
		return "", fmt.Errorf("unsafe file path %q: not a file", p)
	}
	return filepath.FromSlash(cleaned), nil
}

// slugify turns a title into a folder name: lowercase ASCII letters and
// digits joined by single dashes, at most 60 characters.
func slugify(title, fallback string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(title) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
		if b.Len() >= 60 {
			break
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		return sanitizeFilename(fallback)
	}
	return slug
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// handoffWriter writes inside one output folder through os.Root, so neither
// a crafted path nor a pre-existing symlink can redirect a write outside it.
type handoffWriter struct {
	root    *os.Root
	summary *handoffSummary
}

func (w *handoffWriter) write(local string, data []byte) error {
	if dir := filepath.Dir(local); dir != "." {
		if err := w.root.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create folder for %s: %w", local, err)
		}
	}
	if err := w.root.WriteFile(local, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", local, err)
	}
	w.summary.FilesWritten = append(w.summary.FilesWritten, filepath.ToSlash(local))
	return nil
}

func writeHandoffJSON[T handoffDesignFileContent | handoffPagesFileContent](w *handoffWriter, local string, v T) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", local, err)
	}
	return w.write(local, append(data, '\n'))
}

func (w *handoffWriter) warn(cli *CLI, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	w.summary.Warnings = append(w.summary.Warnings, msg)
	cli.Printer.Info("warning: %s", msg)
}

func writeHandoff(ctx context.Context, cli *CLI, client *api.Client, pack *api.ArtifactHandoff, plan *handoffPlan, dir string) (*handoffSummary, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create output folder: %w", err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("open output folder: %w", err)
	}
	defer func() { _ = root.Close() }()

	summary := &handoffSummary{
		Dir:          dir,
		ArtifactID:   pack.Artifact.ID,
		VersionID:    pack.Version.ID,
		URL:          pack.Artifact.URL,
		FilesWritten: []string{},
		Fetched:      []string{},
		Skipped:      []handoffSkip{},
		Truncated:    pack.Truncated,
		Warnings:     []string{},
	}
	w := &handoffWriter{root: root, summary: summary}

	// source writes one pack file: inline content, or fetched page by page
	// when it was over budget. Sensitive files are never fetched.
	source := func(remote, local string, content *string, omitted, digest string) error {
		switch {
		case omitted == api.ArtifactOmittedSensitive:
			summary.Skipped = append(summary.Skipped, handoffSkip{Path: remote, Reason: skipSensitive})
			w.warn(cli, "%s looks like it holds a key or password and was left out; ask the artifact's owner", remote)
			return nil
		case omitted == api.ArtifactOmittedOverBudget:
			_, text, err := client.ArtifactSourceFile(ctx, pack.Artifact.ID, pack.Version.ID, remote)
			if err != nil {
				return fmt.Errorf("fetch %s: %w", remote, err)
			}
			content = &text
			summary.Fetched = append(summary.Fetched, remote)
		case content == nil:
			summary.Skipped = append(summary.Skipped, handoffSkip{Path: remote, Reason: skipMissing})
			w.warn(cli, "%s came back without content", remote)
			return nil
		}
		data := []byte(*content)
		if digest != "" && sha256Hex(data) != digest {
			return fmt.Errorf("%s does not match its content digest; the version may have changed, re-run the hand-off", remote)
		}
		return w.write(local, data)
	}

	if pack.Spec != nil {
		if err := source(pack.Spec.Path, plan.spec, pack.Spec.Markdown, pack.Spec.Omitted, ""); err != nil {
			return nil, err
		}
	}
	for _, f := range pack.Files {
		if err := source(f.Path, plan.files[f.Path], f.Content, f.Omitted, f.ContentDigest); err != nil {
			return nil, err
		}
	}

	designAssets := make([]handoffDesignAsset, 0, len(pack.Design.Assets))
	for i, a := range pack.Design.Assets {
		local := plan.assets[i]
		entry := handoffDesignAsset{ArtifactAsset: a.ArtifactAsset, LocalPath: filepath.ToSlash(local)}
		if a.URL == nil || *a.URL == "" {
			summary.Skipped = append(summary.Skipped, handoffSkip{Path: a.Path, Reason: skipUnsignedURL})
			w.warn(cli, "design asset %s has no download link; ask for the hand-off again", a.Path)
			entry.LocalPath = ""
		} else {
			data, err := downloadHandoffAsset(ctx, client, *a.URL)
			if err != nil {
				return nil, fmt.Errorf("download design asset %s: %w", a.Path, err)
			}
			if a.ContentDigest != "" && sha256Hex(data) != a.ContentDigest {
				w.warn(cli, "design asset %s does not match its content digest", a.Path)
			}
			if err := w.write(local, data); err != nil {
				return nil, err
			}
		}
		designAssets = append(designAssets, entry)
	}

	if err := writeHandoffJSON(w, plan.design, handoffDesignFileContent{
		Digest:         pack.Design.Digest,
		GuidancePath:   pack.Design.GuidancePath,
		TokensPath:     pack.Design.TokensPath,
		ComponentsPath: pack.Design.ComponentsPath,
		AssetsPath:     pack.Design.AssetsPath,
		Files:          nonNil(pack.Design.Files),
		Assets:         designAssets,
	}); err != nil {
		return nil, err
	}
	if err := writeHandoffJSON(w, plan.pages, handoffPagesFileContent{
		Pages: nonNil(pack.Pages),
		Links: nonNil(pack.Links),
	}); err != nil {
		return nil, err
	}
	if err := w.write(plan.notes, []byte(renderHandoffNotes(pack))); err != nil {
		return nil, err
	}
	if err := w.write(plan.guide, []byte(renderHandoffGuide(pack, plan, summary))); err != nil {
		return nil, err
	}
	return summary, nil
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// handoffDesignAsset is design asset metadata plus where the CLI saved it.
type handoffDesignAsset struct {
	api.ArtifactAsset
	LocalPath string `json:"localPath,omitempty"`
}

// handoffDesignFileContent is design/design.json. Asset download links are
// left out: they expire within minutes.
type handoffDesignFileContent struct {
	Digest         *string              `json:"digest"`
	GuidancePath   string               `json:"guidancePath,omitempty"`
	TokensPath     string               `json:"tokensPath,omitempty"`
	ComponentsPath string               `json:"componentsPath,omitempty"`
	AssetsPath     string               `json:"assetsPath,omitempty"`
	Files          []string             `json:"files"`
	Assets         []handoffDesignAsset `json:"assets"`
}

// handoffPagesFileContent is pages.json.
type handoffPagesFileContent struct {
	Pages []api.ArtifactHandoffPage `json:"pages"`
	Links []api.ArtifactHandoffLink `json:"links"`
}

// downloadHandoffAsset fetches a short-lived signed asset link. The link is
// pre-authorized, so the API token is never sent with it.
func downloadHandoffAsset(ctx context.Context, client *api.Client, rawURL string) ([]byte, error) {
	if err := upload.ValidatePresignedURL(rawURL); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	resp, err := client.HTTPClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download failed with status %d (links expire after a few minutes; re-run the hand-off)", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, handoffMaxAssetBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > handoffMaxAssetBytes {
		return nil, fmt.Errorf("asset is larger than %s", humanSize(handoffMaxAssetBytes))
	}
	return data, nil
}

func authorLabel(kind string) string {
	if kind == "ai" {
		return "AI"
	}
	return "You"
}

// renderHandoffNotes groups notes by page in page order, Spec notes first.
// Notes on a page the pack does not list (should not happen) come last.
func renderHandoffNotes(pack *api.ArtifactHandoff) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Notes — %s\n\n", pack.Artifact.Title)
	if len(pack.Notes) == 0 {
		b.WriteString("There are no notes.\n")
		return b.String()
	}
	b.WriteString("Each note holds a rule the design alone can't show. Notes are grouped by the page they are pinned to.\n")

	byPage := map[string][]api.ArtifactHandoffNote{}
	for _, n := range pack.Notes {
		byPage[n.PageID] = append(byPage[n.PageID], n)
	}
	section := func(heading string, notes []api.ArtifactHandoffNote) {
		if len(notes) == 0 {
			return
		}
		fmt.Fprintf(&b, "\n## %s\n", heading)
		for _, n := range notes {
			fmt.Fprintf(&b, "\n### %s — %s\n\n%s\n", authorLabel(n.AuthorKind), n.CreatedAt.UTC().Format("2006-01-02 15:04 UTC"), strings.TrimSpace(n.Body))
		}
	}
	section("Spec", byPage[api.ArtifactSpecNotePageID])
	delete(byPage, api.ArtifactSpecNotePageID)
	for _, p := range pack.Pages {
		section(fmt.Sprintf("%s (`%s`)", p.Title, p.ID), byPage[p.ID])
		delete(byPage, p.ID)
	}
	for _, n := range pack.Notes {
		if notes, ok := byPage[n.PageID]; ok {
			section(fmt.Sprintf("Page `%s`", n.PageID), notes)
			delete(byPage, n.PageID)
		}
	}
	return b.String()
}

// renderHandoffGuide writes HANDOFF.md: where the pack came from, the
// server's next steps, and how the folder is laid out.
func renderHandoffGuide(pack *api.ArtifactHandoff, plan *handoffPlan, summary *handoffSummary) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s — hand-off\n\n", pack.Artifact.Title)
	fmt.Fprintf(&b, "- Artifact: %s\n", pack.Artifact.URL)
	fmt.Fprintf(&b, "- Artifact ID: `%s`\n", pack.Artifact.ID)
	fmt.Fprintf(&b, "- Version: %d (`%s`), %s\n", pack.Version.Number, pack.Version.ID, pack.Version.CreatedAt.UTC().Format("2006-01-02 15:04 UTC"))
	if pack.Artifact.Kind != "" {
		fmt.Fprintf(&b, "- Kind: %s\n", pack.Artifact.Kind)
	}

	b.WriteString("\n## Next steps\n\n")
	b.WriteString(strings.TrimSpace(pack.NextSteps))
	b.WriteString("\n\n## How to build from this folder\n\n")
	spec := "There is no Spec: infer the requirements from the pages and notes."
	if pack.Spec != nil {
		if pack.Spec.Omitted == api.ArtifactOmittedSensitive {
			spec = fmt.Sprintf("The Spec (`%s`) was left out because it looks like it holds a secret; ask the artifact's owner.", pack.Spec.Path)
		} else {
			spec = fmt.Sprintf("Start with the Spec: `%s`. It is the source of truth for what to build.", filepath.ToSlash(plan.spec))
		}
	}
	fmt.Fprintf(&b, "1. %s\n", spec)
	fmt.Fprintf(&b, "2. Read `%s`: rules pinned to each page or to the Spec.\n", filepath.ToSlash(plan.notes))
	fmt.Fprintf(&b, "3. Set up the shared design first: `%s` lists its files and assets; assets are saved under `%s/`.\n", filepath.ToSlash(plan.design), handoffAssetsDir)
	fmt.Fprintf(&b, "4. Build each page from its files, at its surface and viewport; `%s` lists pages and the navigation links between them.\n", filepath.ToSlash(plan.pages))
	b.WriteString("5. Every source file of the prototype is in this folder at its own path. Keep its look, copy and flows; use the stack you are asked for.\n")
	if len(summary.Fetched) > 0 {
		fmt.Fprintf(&b, "\nThe Copera CLI already fetched the %d file(s) that were too large to inline, so nothing is missing for size.\n", len(summary.Fetched))
	}
	if len(summary.Skipped) > 0 {
		b.WriteString("\nNot in this folder:\n\n")
		for _, s := range summary.Skipped {
			fmt.Fprintf(&b, "- `%s` (%s)\n", s.Path, s.Reason)
		}
	}
	return b.String()
}
