package commands

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"net/http"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/copera/copera-cli/internal/api"
	"golang.org/x/text/unicode/norm"
)

func adoptDesign(ctx context.Context, cli *CLI, dir, kind, entry, title string, decline bool) error {
	root, err := openDesignDir(dir, false)
	if err != nil {
		return designError(cli, err)
	}
	defer root.Close()

	const alreadyDesign = "This folder is already a Design. Use copera designs push or copera designs status."
	if _, err := root.Lstat(designStateFile); !errors.Is(err, fs.ErrNotExist) {
		if err == nil {
			return systemConflict(cli, alreadyDesign)
		}
		return designError(cli, err)
	}
	files, _, err := scanDesignTreeWithRefusals(root, nil, true)
	if err != nil {
		return designUsageError(cli, fmt.Errorf("%w\nRemove these paths or copy the publishable files into a clean folder.", err))
	}
	pages := make([]string, 0)
	topPages := make([]string, 0)
	for _, name := range slices.Sorted(maps.Keys(files)) {
		if files[name].MediaType == "text/html" {
			pages = append(pages, name)
			if path.Dir(name) == "." && strings.EqualFold(path.Ext(name), ".html") {
				topPages = append(topPages, name)
			}
		}
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
	state := &designState{Kind: guide.Kind, Runtime: guide.Runtime, Entrypoint: entry, Title: strings.TrimSpace(title), NoDesignSystem: decline}
	if topic, err := client.ArtifactGuideTopic(ctx, "existing-app"); err == nil {
		if topic.Markdown != "" {
			guide = topic
		}
	} else if !unsupportedDesignGuideTopic(err) {
		return designError(cli, err)
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return designError(cli, err)
	}
	displayTitle := designCreateTitle(absDir, entry, files, state.Title)
	scopes := designStorageScopes(files)
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
	if _, err := root.Lstat(designStateFile); !errors.Is(err, fs.ErrNotExist) {
		if err == nil {
			return systemConflict(cli, alreadyDesign)
		}
		return designError(cli, err)
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
	if cli.Printer.IsJSON() {
		if links == nil {
			links = []designOutsideReference{}
		}
		return cli.Printer.PrintJSON(struct {
			Entrypoint      string                   `json:"entrypoint"`
			Pages           []string                 `json:"pages"`
			OutsideLinks    []designOutsideReference `json:"outsideLinks"`
			StorageDeclared bool                     `json:"storageDeclared"`
			Title           string                   `json:"title"`
			NextCommand     string                   `json:"nextCommand"`
		}{entry, pages, links, len(scopes) != 0, displayTitle, next})
	}
	cli.Printer.PrintLine("Design adopted in " + dir)
	cli.Printer.PrintLine("Entry: " + entry)
	cli.Printer.PrintLine("Pages: " + strings.Join(pages, ", "))
	if len(links) == 0 {
		cli.Printer.PrintLine("Outside links: none")
	}
	for _, link := range links {
		line := fmt.Sprintf("Outside link: %s (%s, %s)", link.URL, link.Host, link.Path)
		if link.Reason != "" {
			line += " — " + link.Reason
		}
		cli.Printer.PrintLine(line)
	}
	if len(scopes) == 0 {
		cli.Printer.PrintLine("Storage: none")
	} else {
		cli.Printer.PrintLine("Storage: declared (" + strings.Join(scopes, ", ") + ")")
	}
	cli.Printer.PrintLine("Title: " + displayTitle)
	cli.Printer.PrintLine("Run: " + next)
	return nil
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
	designStorageAccess  = regexp.MustCompile(`\bstorage\s*(?:(?:\?\.|\.)\s*(local|session)\b|(?:\?\.)?\[\s*["'](local|session)["']\s*\])`)
	designStorageBinding = regexp.MustCompile(`\{([^{}]*)\}\s*=\s*[\w.$?]+\.\s*storage\b`)
)

func designStorageScopes(files map[string]designFile) []string {
	used := make(map[string]bool, 2)
	for _, file := range files {
		switch file.MediaType {
		case "text/html", "text/javascript", "text/typescript", "text/jsx", "text/tsx":
			if !bytes.Contains(file.Data, []byte("coperaArtifact")) {
				continue
			}
			for _, match := range designStorageAccess.FindAllSubmatch(file.Data, -1) {
				scope := match[1]
				if scope == nil {
					scope = match[2]
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
	}
	return slices.Sorted(maps.Keys(used))
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
