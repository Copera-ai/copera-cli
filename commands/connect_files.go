package commands

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const connectSkillDir = "copera-designs"

type connectedFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type connectedTool struct {
	Files       []connectedFile `json:"files"`
	Version     string          `json:"version"`
	ConnectedAt time.Time       `json:"connected_at"`
}

type connectRecord struct {
	Tools map[string]connectedTool `json:"tools"`
}

func (r connectRecord) owns(path, digest string) bool {
	for _, tool := range r.Tools {
		for _, file := range tool.Files {
			if file.Path == path && file.SHA256 == digest {
				return true
			}
		}
	}
	return false
}

func (r connectRecord) shared(path, except string) bool {
	for name, tool := range r.Tools {
		if name == except {
			continue
		}
		for _, file := range tool.Files {
			if file.Path == path {
				return true
			}
		}
	}
	return false
}

func (r connectRecord) updateSharedDigest(file connectedFile) {
	for _, tool := range r.Tools {
		for i, previous := range tool.Files {
			if previous.Path == file.Path {
				tool.Files[i].SHA256 = file.SHA256
			}
		}
	}
}

type connectFilePlan struct {
	Path   string `json:"path"`
	Action string `json:"action"`
	before []byte
	after  []byte
}

func loadConnectRecord(home string) (connectRecord, connectFilePlan, error) {
	path := filepath.Join(home, ".copera", "connect.json")
	data, err := readConnectFile(home, path)
	record := connectRecord{Tools: make(map[string]connectedTool)}
	if err == nil && data != nil {
		var parsed connectRecord
		if json.Unmarshal(data, &parsed) == nil && parsed.Tools != nil {
			record = parsed
		}
	}
	return record, connectFilePlan{Path: path, before: data}, err
}

func planConnectRecord(file connectFilePlan, record connectRecord, changed bool) (connectFilePlan, error) {
	file.Action = "unchanged"
	if !changed {
		return file, nil
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return file, err
	}
	file.Action = "update"
	if file.before == nil {
		file.Action = "new"
	}
	file.after = append(data, '\n')
	return file, nil
}

var errConnectUnsafePath = errors.New("unsafe connection path")

// Explicit configuration directories outside home are rooted at their volume.
func openConnectRoot(home, path string) (*os.Root, string, error) {
	base := home
	rel, err := filepath.Rel(home, path)
	if err != nil || !filepath.IsLocal(rel) {
		base = filepath.VolumeName(path) + string(filepath.Separator)
		rel, err = filepath.Rel(base, path)
		if err != nil || !filepath.IsAbs(path) || !filepath.IsLocal(rel) {
			return nil, "", fmt.Errorf("%w: %s", errConnectUnsafePath, path)
		}
	}
	root, err := os.OpenRoot(base)
	return root, rel, err
}

func inspectConnectPath(root *os.Root, rel string) (fs.FileInfo, error) {
	parts := strings.Split(rel, string(filepath.Separator))
	current := "."
	for i, part := range parts {
		current = filepath.Join(current, part)
		info, err := root.Lstat(current)
		if err != nil {
			return nil, err
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return nil, fmt.Errorf("%w: symlink %s", errConnectUnsafePath, filepath.Join(root.Name(), current))
		}
		if i == len(parts)-1 {
			return info, nil
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("%w: %s is not a directory", errConnectUnsafePath, filepath.Join(root.Name(), current))
		}
	}
	return nil, fmt.Errorf("%w: %s", errConnectUnsafePath, rel)
}

func readConnectFile(home, path string) ([]byte, error) {
	root, rel, err := openConnectRoot(home, path)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return readConnectFileAt(root, rel)
}

func readConnectFileAt(root *os.Root, rel string) ([]byte, error) {
	info, err := inspectConnectPath(root, rel)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: %s is not a regular file", errConnectUnsafePath, filepath.Join(root.Name(), rel))
	}
	return root.ReadFile(rel)
}

func writeConnectFile(root *os.Root, rel string, data []byte) error {
	parent := filepath.Dir(rel)
	if err := root.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	dir, err := root.OpenRoot(parent)
	if err != nil {
		return err
	}
	defer dir.Close()
	temp := ".copera-connect-" + rand.Text()
	f, err := dir.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	defer dir.Remove(temp)
	_, writeErr := f.Write(data)
	if err := errors.Join(writeErr, f.Close()); err != nil {
		return err
	}
	return dir.Rename(temp, filepath.Base(rel))
}

func sameConnectFile(a, b []byte) bool {
	return (a == nil) == (b == nil) && bytes.Equal(a, b)
}

func replaceConnectFile(home, path string, before, after []byte) error {
	root, rel, err := openConnectRoot(home, path)
	if err != nil {
		return err
	}
	defer root.Close()
	current, err := readConnectFileAt(root, rel)
	if err != nil {
		return err
	}
	if !sameConnectFile(current, before) {
		return fmt.Errorf("%w: %s changed since the plan was printed", errConnectUnsafePath, path)
	}
	if sameConnectFile(current, after) {
		return nil
	}
	if after == nil {
		return root.Remove(rel)
	}
	return writeConnectFile(root, rel, after)
}

// Apply the ownership record last. If an operation fails, put back the files
// already changed in this invocation, provided their contents still match.
func applyConnectPlan(home string, files []connectFilePlan) error {
	root, err := os.OpenRoot(home)
	if err != nil {
		return err
	}
	defer root.Close()
	record := files[len(files)-1]
	rel := filepath.Join(".copera", "connect.json")
	if _, err := inspectConnectPath(root, rel); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := root.MkdirAll(".copera", 0o755); err != nil {
		return err
	}
	lock := filepath.Join(".copera", ".connect-lock")
	if err := lockConnectRecord(root, lock); err != nil {
		return err
	}
	defer root.Remove(lock)

	current, err := readConnectFileAt(root, rel)
	if err != nil {
		return err
	}
	if !sameConnectFile(current, record.before) {
		return fmt.Errorf("%w: %s changed since the plan was printed", errConnectUnsafePath, record.Path)
	}
	for _, file := range files[:len(files)-1] {
		if file.Action == "unchanged" {
			if err := replaceConnectFile(home, file.Path, file.before, file.before); err != nil {
				return err
			}
		}
	}
	for i, file := range files {
		if !connectFileChanges(file) {
			continue
		}
		if err := replaceConnectFile(home, file.Path, file.before, file.after); err != nil {
			for j := i - 1; j >= 0; j-- {
				previous := files[j]
				if connectFileChanges(previous) {
					err = errors.Join(err, replaceConnectFile(home, previous.Path, previous.after, previous.before))
				}
			}
			return err
		}
	}
	for _, file := range files {
		if dir := filepath.Dir(file.Path); file.Action == "removed" && filepath.Base(dir) == connectSkillDir {
			if err := removeEmptyConnectSkillDir(home, dir); err != nil {
				return err
			}
		}
	}
	return nil
}

// A connect run holds the lock for well under a second. A lock this old was
// left by a run that was killed, so it must not block every later run.
const connectLockStaleAfter = 2 * time.Minute

func lockConnectRecord(root *os.Root, lock string) error {
	err := root.Mkdir(lock, 0o755)
	if !errors.Is(err, fs.ErrExist) {
		return err
	}
	info, statErr := root.Lstat(lock)
	if statErr != nil || !info.IsDir() || time.Since(info.ModTime()) < connectLockStaleAfter {
		return fmt.Errorf("%w: connection changes are already in progress", errConnectUnsafePath)
	}
	if err := root.Remove(lock); err != nil {
		return err
	}
	return root.Mkdir(lock, 0o755)
}

func connectFileChanges(file connectFilePlan) bool {
	switch file.Action {
	case "new", "update", "removed":
		return true
	}
	return false
}

func removeEmptyConnectSkillDir(home, dir string) error {
	root, rel, err := openConnectRoot(home, dir)
	if err != nil {
		return err
	}
	defer root.Close()
	info, err := inspectConnectPath(root, rel)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%w: %s is not a directory", errConnectUnsafePath, dir)
	}
	if err := root.Remove(rel); err != nil && !errors.Is(err, fs.ErrExist) && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
