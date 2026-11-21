package commands

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/copera/copera-cli/internal/api"
	"golang.org/x/text/unicode/norm"
)

var (
	designAdoptPathSegment = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	designAdoptBuildConfig = regexp.MustCompile(`^(vite|tailwind|postcss)\.config\.(cjs|cts|js|mjs|mts|ts)$`)
)

func validateAdoptDesignPath(name string) error {
	base := path.Base(name)
	if !designAdoptPathSegment.MatchString(base) {
		return fmt.Errorf("unsafe design path %q: use only letters, digits, dots, underscores and hyphens", name)
	}
	lower := strings.ToLower(base)
	refused := func() error { return fmt.Errorf("unsafe design path %q: refused name %q", name, base) }
	if strings.HasPrefix(lower, ".") || strings.HasPrefix(lower, "id_rsa") ||
		strings.HasPrefix(lower, "id_ed25519") || designAdoptBuildConfig.MatchString(lower) {
		return refused()
	}
	switch lower {
	case "package.json", "tsconfig.json", "jsconfig.json", "credentials":
		return refused()
	}
	switch path.Ext(lower) {
	case ".pem", ".key", ".p12", ".pfx", ".keystore", ".sql", ".sqlite", ".db":
		return refused()
	}
	return nil
}

func checkAdoptMetadata(cli *CLI, root *os.Root, locked bool) error {
	info, err := root.Lstat(".copera")
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return designError(cli, err)
	}
	if !info.IsDir() {
		return designUsageError(cli, fmt.Errorf(".copera must be a directory"))
	}
	entries, err := fs.ReadDir(root.FS(), ".copera")
	if err != nil {
		return designError(cli, err)
	}
	if slices.ContainsFunc(entries, func(entry fs.DirEntry) bool { return entry.Name() == "design.json" }) {
		return systemConflict(cli, "This folder is already a Design. Use copera designs push or copera designs status.")
	}
	for _, entry := range entries {
		if !locked || entry.Name() != ".design-state.lock" {
			return systemConflict(cli, ".copera must be empty before adoption. Move its contents or copy the publishable files into a clean folder.")
		}
	}
	return nil
}

func adoptDesign(ctx context.Context, cli *CLI, dir, kind, entry, title string) error {
	root, err := openDesignDir(dir, false)
	if err != nil {
		return designError(cli, err)
	}
	defer root.Close()

	if err := checkAdoptMetadata(cli, root, false); err != nil {
		return err
	}
	files, _, err := scanDesignTreeWithRefusals(root, nil, true)
	if err != nil {
		return designUsageError(cli, fmt.Errorf("%w\nRemove these paths or copy the publishable files into a clean folder.", err))
	}
	groups := []struct{ key, label string }{
		{"pages", "Pages"}, {"scripts", "Scripts"}, {"styles", "Styles"},
		{"otherText", "Other text"}, {"assets", "Assets"},
	}
	published := make(map[string][]string, len(groups))
	for _, group := range groups {
		published[group.key] = []string{}
	}
	topPages := make([]string, 0)
	for _, name := range slices.Sorted(maps.Keys(files)) {
		file := files[name]
		group := "otherText"
		switch {
		case file.Binary:
			group = "assets"
		case file.MediaType == "text/html":
			group = "pages"
			if path.Dir(name) == "." && strings.EqualFold(path.Ext(name), ".html") {
				topPages = append(topPages, name)
			}
		case file.MediaType == "text/css":
			group = "styles"
		case isDesignScript(file.MediaType):
			group = "scripts"
		}
		published[group] = append(published[group], name)
	}
	counts := make(map[string]int, len(groups))
	for _, group := range groups {
		counts[group.key] = len(published[group.key])
	}
	if entry == "" {
		if files["index.html"].MediaType == "text/html" {
			entry = "index.html"
		} else if len(topPages) == 1 {
			entry = topPages[0]
		} else {
			return designUsageError(cli, fmt.Errorf("specify the entry HTML file with --entry"))
		}
	} else {
		entry = filepath.ToSlash(filepath.Clean(entry))
	}
	if files[entry].MediaType != "text/html" {
		return designUsageError(cli, fmt.Errorf("--entry %q must name an existing HTML file in the folder", entry))
	}
	links, err := scanDesignOutsideReferences(files)
	if err != nil {
		return designError(cli, err)
	}

	client, _, err := requireAPIClient(cli)
	if err != nil {
		return err
	}
	guide, err := client.ArtifactGuide(ctx, kind, "static", "")
	if err != nil {
		return designError(cli, err)
	}
	state := &designState{Kind: guide.Kind, Runtime: guide.Runtime, Entrypoint: entry, Title: title, NoDesignSystem: true}
	if topic, err := client.ArtifactGuideTopic(ctx, "existing-app"); err == nil && topic.Markdown != "" {
		guide = topic
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return designError(cli, err)
	}
	displayTitle := designCreateTitle(absDir, entry, files, state.Title)
	scopes, storageDefaulted := designStorageScopes(files)
	if len(scopes) != 0 {
		namespace := designStorageNamespace(displayTitle)
		state.Capabilities = []api.ArtifactProjectCapability{{
			Kind: "storage", Scopes: &scopes, Namespace: &namespace, SchemaVersion: 1,
			Migration: &struct {
				Strategy string `json:"strategy"`
			}{Strategy: "reset"},
		}}
	}
	release, err := lockDesignState(root, true)
	if err != nil {
		return designError(cli, err)
	}
	defer release()
	if err := checkAdoptMetadata(cli, root, true); err != nil {
		return err
	}
	if err := writeDesignGuideFiles(root, guide); err != nil {
		return designError(cli, err)
	}
	if err := writeDesignStateLocked(root, state); err != nil {
		return designError(cli, err)
	}
	next := "copera designs check"
	if slices.ContainsFunc(links, func(link designOutsideReference) bool { return link.Reason == "" }) {
		next = "copera designs vendor --all"
	}
	if dir != "." {
		next += " '" + strings.ReplaceAll(dir, "'", "'\\''") + "'"
	}
	outside := groupAdoptOutsideLinks(links)
	if cli.Printer.IsJSON() {
		return cli.Printer.PrintJSON(struct {
			Entrypoint       string                   `json:"entrypoint"`
			Pages            []string                 `json:"pages"`
			Scripts          []string                 `json:"scripts"`
			Styles           []string                 `json:"styles"`
			OtherText        []string                 `json:"otherText"`
			Assets           []string                 `json:"assets"`
			FileCounts       map[string]int           `json:"fileCounts"`
			OutsideLinks     []designAdoptOutsideLink `json:"outsideLinks"`
			StorageDeclared  bool                     `json:"storageDeclared"`
			StorageDefaulted bool                     `json:"storageDefaulted,omitempty"`
			Title            string                   `json:"title"`
			NextCommand      string                   `json:"nextCommand"`
		}{
			Entrypoint: entry, Pages: published["pages"], Scripts: published["scripts"],
			Styles: published["styles"], OtherText: published["otherText"], Assets: published["assets"],
			FileCounts: counts, OutsideLinks: outside, StorageDeclared: len(scopes) != 0,
			StorageDefaulted: storageDefaulted, Title: displayTitle, NextCommand: next,
		})
	}
	cli.Printer.PrintLine("Design adopted in " + designsTerminalLine(dir))
	cli.Printer.PrintLine("Entry: " + designsTerminalLine(entry))
	remaining, omitted := 40, 0
	for _, group := range groups {
		names := published[group.key]
		shown := min(remaining, len(names))
		line := fmt.Sprintf("%s (%d)", group.label, counts[group.key])
		if shown > 0 {
			line += ": " + designsTerminalLine(strings.Join(names[:shown], ", "))
		}
		cli.Printer.PrintLine(line)
		remaining -= shown
		omitted += len(names) - shown
	}
	if omitted > 0 {
		cli.Printer.PrintLine(fmt.Sprintf("and %d more", omitted))
	}
	if len(outside) == 0 {
		cli.Printer.PrintLine("Outside links: none")
	}
	for _, link := range outside {
		line := fmt.Sprintf("Outside link: %s (%s, %s)", link.URL, link.Host, strings.Join(link.Files, ", "))
		if link.reason != "" {
			line += " — " + link.reason
		}
		cli.Printer.PrintLine(designsTerminalLine(line))
	}
	if len(scopes) == 0 {
		cli.Printer.PrintLine("Storage: none")
	} else {
		line := "Storage: declared (" + strings.Join(scopes, ", ") + ")"
		if storageDefaulted {
			line += " — no scope detected, defaulted to local"
		}
		cli.Printer.PrintLine(line)
	}
	cli.Printer.PrintLine("Title: " + designsTerminalLine(displayTitle))
	cli.Printer.PrintLine("Run: " + designsTerminalLine(next))
	return nil
}

type designAdoptOutsideLink struct {
	URL    string   `json:"url"`
	Host   string   `json:"host"`
	Files  []string `json:"files"`
	reason string
}

func groupAdoptOutsideLinks(refs []designOutsideReference) []designAdoptOutsideLink {
	grouped := make([]designAdoptOutsideLink, 0)
	index := make(map[string]int)
	for _, ref := range refs {
		i, found := index[ref.URL]
		if !found {
			i = len(grouped)
			index[ref.URL] = i
			grouped = append(grouped, designAdoptOutsideLink{URL: ref.URL, Host: ref.Host, reason: ref.Reason})
		}
		link := &grouped[i]
		if !slices.Contains(link.Files, ref.Path) {
			link.Files = append(link.Files, ref.Path)
		}
		if ref.Reason == "" {
			link.reason = ""
		}
	}
	return grouped
}

func isDesignScript(mediaType string) bool {
	switch mediaType {
	case "text/javascript", "text/typescript", "text/jsx", "text/tsx":
		return true
	default:
		return false
	}
}

func unsupportedDesignGuideTopic(err error) bool {
	var refused *api.APIError
	if !errors.As(err, &refused) {
		return false
	}
	switch refused.StatusCode {
	case http.StatusBadRequest, http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusUnprocessableEntity:
		return true
	default:
		return false
	}
}

var (
	designStorageReference = regexp.MustCompile(`\.\s*storage\b`)
	designStorageAccess    = regexp.MustCompile(`([\w$]+)\s*(?:(?:\?\.|\.)\s*(local|session)\b|(?:\?\.)?\[\s*["'](local|session)["']\s*\])`)
	designStorageAlias     = regexp.MustCompile(`(?:^|[^\w$.])([A-Za-z_$][\w$]*)\s*=\s*[\w$.?]+\s*\.\s*storage\b`)
	designStorageBinding   = regexp.MustCompile(`\{([^{}]*)\}\s*=\s*[\w.$?]+\.\s*storage\b`)
)

func designStorageScopes(files map[string]designFile) ([]string, bool) {
	var sdk, storage bool
	used := make(map[string]bool, 2)
	for _, file := range files {
		if file.MediaType != "text/html" && !isDesignScript(file.MediaType) {
			continue
		}
		sdk = sdk || bytes.Contains(file.Data, []byte("coperaArtifact"))
		storage = storage || designStorageReference.Match(file.Data)
		aliases := map[string]bool{"storage": true}
		for _, match := range designStorageAlias.FindAllSubmatch(file.Data, -1) {
			aliases[string(match[1])] = true
		}
		for _, match := range designStorageAccess.FindAllSubmatch(file.Data, -1) {
			if !aliases[string(match[1])] {
				continue
			}
			scope := match[2]
			if scope == nil {
				scope = match[3]
			}
			used[string(scope)] = true
		}
		for _, match := range designStorageBinding.FindAllSubmatch(file.Data, -1) {
			for name := range strings.SplitSeq(string(match[1]), ",") {
				scope, _, _ := strings.Cut(name, ":")
				scope, _, _ = strings.Cut(scope, "=")
				if scope = strings.TrimSpace(scope); scope == "local" || scope == "session" {
					used[scope] = true
				}
			}
		}
	}
	if !sdk || !storage {
		return nil, false
	}
	if len(used) == 0 {
		return []string{"local"}, true
	}
	return slices.Sorted(maps.Keys(used)), false
}

func designStorageNamespace(title string) string {
	words := strings.FieldsFunc(strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Mn, r) {
			return -1
		}
		return r
	}, norm.NFD.String(strings.ToLower(title))), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	})
	slug := strings.Join(words, "-")
	if slug == "" {
		return "design"
	}
	return strings.TrimRight(slug[:min(64, len(slug))], "-")
}
