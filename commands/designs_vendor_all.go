package commands

import (
	"bytes"
	"context"
	"errors"
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

func designVendorHint(cli *CLI, files map[string]designFile, managedFiles map[string]string) func(*designBuildReport) {
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
		shown = err == nil && slices.ContainsFunc(links, func(link designOutsideReference) bool {
			_, managed := managedFiles[link.Path]
			return !managed && link.Reason == ""
		})
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
	type vendoredTarget struct {
		path  string
		index int
	}
	toRewrite := make(map[string]sourceFile)
	var sources []designOutsideReference
	byURL := make(map[string]vendoredTarget)
	byHost := make(map[string][]int)
	failed := make(map[string]string)
	for _, name := range slices.Sorted(maps.Keys(files)) {
		data, err := designReferenceContents(name, files[name])
		if err != nil {
			return designError(cli, err)
		}
		refs := scanDesignReferences(name, data)
		if len(refs) == 0 {
			continue
		}
		if _, managed := state.DesignSystemFiles[name]; managed {
			for _, ref := range refs {
				ref.Reason = "managed by the Design system"
				result.Skipped = append(result.Skipped, ref)
			}
			continue
		}
		toRewrite[name] = sourceFile{data, refs}
		for _, ref := range refs {
			if ref.hint || ref.preload {
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
			byURL[key] = vendoredTarget{}
			sources = append(sources, ref)
		}
	}

	var bytesLeft int64 = 20 << 20
	limitReached := false
	for n, ref := range sources {
		key := ref.source.url.String()
		if n >= 32 {
			limitReached = true
			failed[key] = "reached the limit of 32 distinct URLs"
			continue
		}
		downloaded, err := downloadDesignVendorSource(ctx, ref.source, &bytesLeft)
		if err != nil {
			limitReached = limitReached || errors.Is(err, errDesignVendorDownloadLimit)
			failed[key] = err.Error()
			continue
		}
		written, err := writeDesignVendorFiles(root, downloaded)
		if err != nil {
			return designError(cli, err)
		}
		i := len(result.Vendored)
		byURL[key] = vendoredTarget{path: ref.source.path, index: i}
		for _, file := range downloaded {
			for _, remote := range file.sourceURLs {
				byURL[remote] = vendoredTarget{path: file.Path, index: i}
			}
			if strings.EqualFold(filepath.Ext(file.Path), ".css") && slices.Contains(written, file.Path) {
				result.Skipped = append(result.Skipped, scanDesignCSSReferences(file.Path, file.data)...)
			}
		}
		byHost[ref.Host] = append(byHost[ref.Host], i)
		if ref.Host == "fonts.googleapis.com" {
			byHost["fonts.gstatic.com"] = append(byHost["fonts.gstatic.com"], i)
		}
		result.Vendored = append(result.Vendored, designVendoredReference{
			URL: ref.URL, FilesWritten: written, FilesRewritten: []string{},
		})
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
			case ref.preload || ref.Reason == "":
				var target vendoredTarget
				if ref.source.url != nil {
					key := ref.source.url.String()
					target = byURL[key]
					if reason := failed[key]; reason != "" {
						ref.Reason = reason
					}
				}
				if target.path == "" {
					result.Skipped = append(result.Skipped, ref)
					continue
				}
				relative, err := filepath.Rel(filepath.Dir(filepath.FromSlash(name)), filepath.FromSlash(target.path))
				if err != nil {
					return designError(cli, err)
				}
				replacement = filepath.ToSlash(relative)
				owners = []int{target.index}
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
		info, err := root.Lstat(filepath.FromSlash(name))
		if err != nil {
			return designError(cli, err)
		}
		mode := info.Mode()
		if err := writeDesignContentWithMode(root, name, designFile{Data: rewritten.Bytes()}, &mode); err != nil {
			return designError(cli, err)
		}
		result.Changed = true
	}

	result.NextCommand = `copera designs push --wait -m "<what this version is>"`
	if limitReached {
		result.NextCommand = "copera designs vendor --all"
	}
	if dir != "." {
		result.NextCommand += " '" + strings.ReplaceAll(dir, "'", "'\\''") + "'"
	}
	if cli.Printer.IsJSON() {
		if err := cli.Printer.PrintJSON(result); err != nil {
			return err
		}
	} else {
		if !result.Changed {
			cli.Printer.PrintLine("No changes.")
		}
		for _, ref := range result.Vendored {
			cli.Printer.PrintLine("Vendored: " + designsTerminalLine(ref.URL))
			if len(ref.FilesWritten) > 0 {
				cli.Printer.PrintLine("  Wrote: " + designsTerminalLine(strings.Join(ref.FilesWritten, ", ")))
			}
			cli.Printer.PrintLine("  Rewrote: " + designsTerminalLine(strings.Join(ref.FilesRewritten, ", ")))
		}
		for _, ref := range result.Skipped {
			cli.Printer.PrintLine(fmt.Sprintf("Skipped: %s (%s) — %s",
				designsTerminalLine(ref.URL), designsTerminalLine(ref.Path), designsTerminalLine(ref.Reason)))
		}
		cli.Printer.PrintLine("Run: " + designsTerminalLine(result.NextCommand))
	}
	if len(sources) > 0 && len(result.Vendored) == 0 {
		return designError(cli, fmt.Errorf("no vendorable URLs could be downloaded"))
	}
	return nil
}
