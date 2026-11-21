package commands

import (
	"bytes"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/pmezard/go-difflib/difflib"
)

type designEdit struct {
	start, end int
	text       []string
}

func designEdits(base, side []string) []designEdit {
	match := difflib.NewMatcherWithJunk(base, side, false, nil)
	var edits []designEdit
	for _, op := range match.GetOpCodes() {
		if op.Tag != 'e' {
			edits = append(edits, designEdit{op.I1, op.I2, side[op.J1:op.J2]})
		}
	}
	return edits
}

// mergeDesignText applies disjoint edits to a common base. Identical edits
// from both sides apply once. Overlaps stay in the working file for resolution.
func mergeDesignText(base, local, remote []byte) ([]byte, bool) {
	lines := strings.SplitAfter(string(base), "\n")
	ours := designEdits(lines, strings.SplitAfter(string(local), "\n"))
	theirs := designEdits(lines, strings.SplitAfter(string(remote), "\n"))
	edits := append(ours, theirs...)
	slices.SortFunc(edits, func(a, b designEdit) int {
		if a.start == b.start {
			return a.end - b.end
		}
		return a.start - b.start
	})
	var result strings.Builder
	var last designEdit
	for i, edit := range edits {
		if i != 0 {
			if edit.start == last.start && edit.end == last.end {
				if !slices.Equal(edit.text, last.text) {
					return nil, false
				}
				continue
			}
			if edit.start < last.end {
				return nil, false
			}
		}
		for _, line := range lines[last.end:edit.start] {
			result.WriteString(line)
		}
		for _, line := range edit.text {
			result.WriteString(line)
		}
		last = edit
	}
	for _, line := range lines[last.end:] {
		result.WriteString(line)
	}
	return []byte(result.String()), true
}

func designFileNames(a, b map[string]designFile) []string {
	names := make([]string, 0, len(a)+len(b))
	for name := range a {
		names = append(names, name)
	}
	for name := range b {
		if _, exists := a[name]; !exists {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

func sameDesignContent(a, b designFile) bool {
	if a.Upload != nil || b.Upload != nil {
		return a.checksum() == b.checksum()
	}
	return bytes.Equal(a.Data, b.Data)
}

func mergeDesignFiles(base, local, remote map[string]designFile) (map[string]designFile, []string) {
	merged := maps.Clone(local)
	conflicts := []string{}
	for _, name := range designFileNames(base, remote) {
		prior, hadBase := base[name]
		ours, hasLocal := local[name]
		theirs, hasRemote := remote[name]
		switch {
		case hadBase == hasRemote && sameDesignContent(prior, theirs),
			hasLocal == hasRemote && sameDesignContent(ours, theirs):
			continue
		case hadBase == hasLocal && sameDesignContent(prior, ours):
			if hasRemote {
				merged[name] = theirs
			} else {
				delete(merged, name)
			}
		case hasLocal && hasRemote && !prior.Binary && !ours.Binary && !theirs.Binary:
			if content, ok := mergeDesignText(prior.Data, ours.Data, theirs.Data); ok {
				theirs.Data = content
				merged[name] = theirs
			} else {
				conflicts = append(conflicts, name)
			}
		default:
			conflicts = append(conflicts, name)
		}
	}
	return merged, conflicts
}

type designChange struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	Binary bool   `json:"binary,omitempty"`
	Patch  string `json:"patch,omitempty"`
}

func designChanges(state *designState, local map[string]designFile) []designChange {
	changes := []designChange{}
	for _, name := range slices.Sorted(maps.Keys(local)) {
		file := local[name]
		if digest, exists := state.Files[name]; !exists {
			changes = append(changes, designChange{Path: name, Kind: "added", Binary: file.Binary})
		} else if file.checksum() != digest {
			changes = append(changes, designChange{Path: name, Kind: "modified", Binary: file.Binary})
		}
	}
	for name := range state.Files {
		if _, exists := local[name]; !exists {
			changes = append(changes, designChange{Path: name, Kind: "removed"})
		}
	}
	slices.SortFunc(changes, func(a, b designChange) int { return strings.Compare(a.Path, b.Path) })
	return changes
}

func designDiff(change *designChange, base, local map[string]designFile) error {
	oldName, newName := "a/"+change.Path, "b/"+change.Path
	if change.Kind == "added" {
		oldName = "/dev/null"
	}
	if change.Kind == "removed" {
		newName = "/dev/null"
	}
	old, current := base[change.Path], local[change.Path]
	change.Binary = old.Binary || current.Binary
	if change.Binary {
		change.Patch = fmt.Sprintf("Binary files %s and %s differ\n", oldName, newName)
		return nil
	}
	lines := func(data []byte) []string {
		lines := strings.SplitAfter(string(data), "\n")
		last := len(lines) - 1
		if lines[last] == "" {
			return lines[:last]
		}
		lines[last] += "\n\\ No newline at end of file\n"
		return lines
	}
	patch, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A: lines(old.Data), B: lines(current.Data), FromFile: oldName, ToFile: newName, Context: 3,
	})
	if patch == "" {
		patch = fmt.Sprintf("--- %s\n+++ %s\n", oldName, newName)
	}
	change.Patch = patch
	return err
}
