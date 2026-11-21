package commands

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/copera/copera-cli/internal/api"
)

type designVendoredReference struct {
	URL            string   `json:"url"`
	FilesWritten   []string `json:"filesWritten"`
	FilesRewritten []string `json:"filesRewritten"`
}

func designVendorHint(cli *CLI, files map[string]designFile) func(*designBuildReport) {
	shown := false
	return func(report *designBuildReport) {
		if shown {
			return
		}
		isOutside := func(d api.ArtifactDiagnostic) bool { return d.Code == "outside_resource" }
		outside := report.FailureCode == "outside_resource" ||
			slices.ContainsFunc(report.Diagnostics, isOutside) ||
			slices.ContainsFunc(report.Accessibility, isOutside) ||
			slices.ContainsFunc(report.Warnings, func(w api.ArtifactWarning) bool { return w.Code == "outside_resource" })
		for _, resource := range report.FailedResources {
			outside = outside || resource.Failure == "outside_resource"
		}
		if !outside {
			return
		}
		links, err := scanDesignOutsideReferences(files)
		shown = err == nil && slices.ContainsFunc(links, func(link designOutsideReference) bool { return link.Reason == "" })
		if shown {
			cli.Printer.Info("Run: copera designs vendor --all")
		}
	}
}

func runDesignVendorAll(ctx context.Context, cli *CLI, root *os.Root, state *designState, dir string) error {
	files, err := scanDesignFiles(root, state)
	if err != nil {
		return designError(cli, err)
	}
	result := struct {
		Vendored    []designVendoredReference `json:"vendored"`
		Skipped     []designOutsideReference  `json:"skipped"`
		Changed     bool                      `json:"changed"`
		NextCommand string                    `json:"nextCommand"`
	}{Vendored: []designVendoredReference{}, Skipped: []designOutsideReference{}}

	type sourceFile struct {
		data []byte
		refs []designOutsideReference
	}
	toRewrite := make(map[string]sourceFile)
	var sources []designVendorSource
	byURL := make(map[string]int)
	byHost := make(map[string][]int)
	for _, name := range slices.Sorted(maps.Keys(files)) {
		data, err := designReferenceContents(name, files[name])
		if err != nil {
			return designError(cli, err)
		}
		refs := scanDesignReferences(name, data)
		if len(refs) == 0 {
			continue
		}
		toRewrite[name] = sourceFile{data, refs}
		for _, ref := range refs {
			if ref.hint {
				continue
			}
			if ref.Reason != "" {
				result.Skipped = append(result.Skipped, ref)
				continue
			}
			key := ref.source.url.String()
			if _, exists := byURL[key]; exists {
				continue
			}
			i := len(sources)
			byURL[key] = i
			byHost[ref.Host] = append(byHost[ref.Host], i)
			if ref.Host == "fonts.googleapis.com" {
				byHost["fonts.gstatic.com"] = append(byHost["fonts.gstatic.com"], i)
			}
			sources = append(sources, ref.source)
			result.Vendored = append(result.Vendored, designVendoredReference{
				URL: ref.URL, FilesRewritten: []string{},
			})
		}
	}

	for i, source := range sources {
		downloaded, err := downloadDesignVendorSource(ctx, source)
		if err != nil {
			return designError(cli, fmt.Errorf("vendor %s: %w", result.Vendored[i].URL, err))
		}
		written, err := writeDesignVendorFiles(root, downloaded)
		if err != nil {
			return designError(cli, err)
		}
		result.Vendored[i].FilesWritten = written
	}

	for _, name := range slices.Sorted(maps.Keys(toRewrite)) {
		file := toRewrite[name]
		var rewritten bytes.Buffer
		offset := 0
		for _, ref := range file.refs {
			var replacement string
			var owners []int
			switch {
			case ref.hint:
				owners = byHost[ref.Host]
				if len(owners) == 0 {
					continue
				}
			case ref.Reason == "":
				relative, err := filepath.Rel(filepath.Dir(filepath.FromSlash(name)), filepath.FromSlash(ref.source.path))
				if err != nil {
					return designError(cli, err)
				}
				replacement = filepath.ToSlash(relative)
				owners = []int{byURL[ref.source.url.String()]}
			default:
				continue
			}
			rewritten.Write(file.data[offset:ref.start])
			rewritten.WriteString(replacement)
			offset = ref.end
			for _, i := range owners {
				names := result.Vendored[i].FilesRewritten
				if len(names) == 0 || names[len(names)-1] != name {
					result.Vendored[i].FilesRewritten = append(names, name)
				}
			}
		}
		if offset == 0 {
			continue
		}
		rewritten.Write(file.data[offset:])
		current, err := readDesignFile(root, name)
		if err != nil {
			return designError(cli, err)
		}
		if !bytes.Equal(current, file.data) {
			return designError(cli, fmt.Errorf("%q changed during vendoring; run vendor --all again", name))
		}
		if err := writeDesignFile(root, name, rewritten.Bytes()); err != nil {
			return designError(cli, err)
		}
		result.Changed = true
	}

	result.NextCommand = `copera designs push --wait -m "Publish existing folder"`
	if dir != "." {
		result.NextCommand += " '" + strings.ReplaceAll(dir, "'", "'\\''") + "'"
	}
	if cli.Printer.IsJSON() {
		return cli.Printer.PrintJSON(result)
	}
	if !result.Changed {
		cli.Printer.PrintLine("No changes.")
	}
	for _, ref := range result.Vendored {
		cli.Printer.PrintLine("Vendored: " + ref.URL)
		if len(ref.FilesWritten) > 0 {
			cli.Printer.PrintLine("  Wrote: " + strings.Join(ref.FilesWritten, ", "))
		}
		cli.Printer.PrintLine("  Rewrote: " + strings.Join(ref.FilesRewritten, ", "))
	}
	for _, ref := range result.Skipped {
		cli.Printer.PrintLine(fmt.Sprintf("Skipped: %s (%s) — %s", ref.URL, ref.Path, ref.Reason))
	}
	cli.Printer.PrintLine("Run: " + result.NextCommand)
	return nil
}
