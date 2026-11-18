package commands

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/copera/copera-cli/internal/api"
)

const (
	designSystemManifest = "design-system.json"
	designSystemLocal    = ".copera/design-system.json"
	designSystemMaxText  = 256
	designSystemMaxAsset = 128
	designSystemMiB      = 1 << 20
)

type designSystemManifestFile struct {
	Path          string `json:"path"`
	MediaType     string `json:"mediaType"`
	ContentDigest string `json:"contentDigest"`
	Usage         string `json:"usage,omitempty"`
}

type designSystemManifestData struct {
	SchemaVersion int                        `json:"schemaVersion"`
	Kit           *api.ArtifactSystemKit     `json:"kit"`
	Files         []designSystemManifestFile `json:"files"`
}

type designSystemContent struct {
	manifest designSystemManifestData
	files    map[string]designFile
}

func validSystemHex(value string, size int) bool {
	if len(value) != size || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

var systemPathAlphabet = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)
var systemComponentName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,79}$`)

func validateSystemPath(name, installedPath string) error {
	if err := validateDesignContentPath(name); err != nil {
		return err
	}
	if len(installedPath) > 240 || strings.Count(installedPath, "/") >= 64 || !systemPathAlphabet.MatchString(name) {
		return fmt.Errorf("invalid system path %q", name)
	}
	for _, part := range strings.Split(name, "/") {
		lower := strings.ToLower(part)
		switch lower {
		case ".claude.json", ".gemini", ".windsurf", ".windsurfrules", ".cursorrules", ".opencode", "opencode.json",
			".agents", ".clinerules", ".mcp", "mcp.json", ".devcontainer", ".pnpmfile.cjs", ".yarnrc", ".yarnrc.yml",
			"bunfig.toml", "agents.override.md", "claude.local.md", "package.json", "tsconfig.json", "jsconfig.json",
			".env", ".hg", ".svn", "node_modules":
			return fmt.Errorf("reserved system path %q", name)
		}
		if strings.HasPrefix(lower, "vite.config.") || strings.HasPrefix(lower, "tailwind.config.") ||
			strings.HasPrefix(lower, "postcss.config.") || strings.HasPrefix(lower, ".postcssrc.") || lower == ".postcssrc" || strings.HasPrefix(lower, ".env.") {
			return fmt.Errorf("reserved system path %q", name)
		}
	}
	return nil
}

func systemInstalledPath(name string) string {
	root, _, _ := strings.Cut(name, "/")
	if root == "components" || root == "lib" || root == "hooks" {
		return "shared/" + name
	}
	return "shared/brand/" + name
}

func systemFileMedia(name string, kit *api.ArtifactSystemKit) (string, error) {
	if err := validateSystemPath(name, systemInstalledPath(name)); err != nil {
		return "", err
	}
	switch name {
	case "DESIGN.md":
		return "text/markdown", nil
	case "tokens.json":
		return "application/json", nil
	case "tokens.css":
		return "text/css", nil
	}
	root, _, _ := strings.Cut(name, "/")
	ext := path.Ext(name)
	if root == "fonts" && ext == ".woff2" {
		return "font/woff2", nil
	}
	if root == "assets" {
		switch ext {
		case ".png":
			return "image/png", nil
		case ".jpg", ".jpeg":
			return "image/jpeg", nil
		case ".webp":
			return "image/webp", nil
		}
	}
	if kit != nil && (root == "components" || root == "lib" || root == "hooks") {
		switch ext {
		case ".ts":
			return "text/typescript", nil
		case ".tsx":
			return "text/tsx", nil
		case ".jsx":
			return "text/jsx", nil
		case ".js", ".mjs":
			return "text/javascript", nil
		case ".css":
			return "text/css", nil
		}
	}
	return "", fmt.Errorf("unsupported system file %q", name)
}

func systemBinary(media string) bool {
	return media == "font/woff2" || strings.HasPrefix(media, "image/")
}

func systemFileLimit(name, media string) int64 {
	if systemBinary(media) {
		return 5 * designSystemMiB
	}
	if name == "DESIGN.md" || name == "tokens.json" {
		return 64 << 10
	}
	return designSystemMiB
}

type systemFileBudget struct {
	text, assets          int
	textBytes, assetBytes int64
}

func (b *systemFileBudget) add(name, media string, size int64) error {
	if size < 0 || size > systemFileLimit(name, media) {
		return fmt.Errorf("system file %q exceeds its size limit", name)
	}
	if systemBinary(media) {
		b.assets++
		b.assetBytes += size
		if b.assets > designSystemMaxAsset || b.assetBytes > 20*designSystemMiB {
			return fmt.Errorf("system package exceeds its binary file limit")
		}
	} else {
		b.text++
		b.textBytes += size
		if b.text > designSystemMaxText || b.textBytes > 5*designSystemMiB {
			return fmt.Errorf("system package exceeds its text file limit")
		}
	}
	return nil
}

func validateSystemKit(kit *api.ArtifactSystemKit) error {
	if kit == nil {
		return nil
	}
	if kit.Kind != "shadcn" || !validSystemHex(kit.RegistryCommit, 40) || kit.SpecJSON == "" ||
		!json.Valid([]byte(kit.SpecJSON)) || len(utf16.Encode([]rune(kit.SpecJSON))) > 49152 ||
		kit.Components == nil || len(kit.Components) > 128 {
		return fmt.Errorf("invalid shadcn kit in %s", designSystemManifest)
	}
	for i, name := range kit.Components {
		if !systemComponentName.MatchString(name) || (i > 0 && kit.Components[i-1] >= name) {
			return fmt.Errorf("shadcn kit contains invalid, repeated or unsorted component %q", name)
		}
	}
	return nil
}

func validateSystemFileUsage(name, media, usage string) error {
	valid := usage == ""
	switch {
	case media == "font/woff2":
		valid = usage == "font"
	case systemBinary(media):
		valid = usage == "logo" || usage == "image"
	}
	if !valid {
		return fmt.Errorf("invalid usage for system file %q", name)
	}
	return nil
}

func validateSystemBytes(name, media string, data []byte) error {
	validDimensions := func(width, height int64) bool {
		return width > 0 && height > 0 && width <= 8192 && height <= 8192 && width*height <= 20_000_000
	}
	valid := false
	switch media {
	case "font/woff2":
		if len(data) >= 48 && bytes.HasPrefix(data, []byte("wOF2")) {
			flavor := binary.BigEndian.Uint32(data[4:])
			tableCount := int(binary.BigEndian.Uint16(data[12:]))
			inflated := int64(binary.BigEndian.Uint32(data[16:]))
			compressed := int64(binary.BigEndian.Uint32(data[20:]))
			valid = int64(binary.BigEndian.Uint32(data[8:])) == int64(len(data)) &&
				(flavor == 0x00010000 || flavor == 0x4f54544f) && tableCount > 0 && tableCount <= 256 &&
				binary.BigEndian.Uint16(data[14:]) == 0 && inflated >= int64(12+16*tableCount) &&
				inflated <= 20*designSystemMiB && compressed > 0 && compressed <= int64(len(data)-48)
		}
	case "image/png":
		valid = len(data) >= 33 && http.DetectContentType(data) == media && string(data[12:16]) == "IHDR" &&
			validDimensions(int64(binary.BigEndian.Uint32(data[16:])), int64(binary.BigEndian.Uint32(data[20:])))
	case "image/jpeg":
		if len(data) >= 12 && http.DetectContentType(data) == media && bytes.HasSuffix(data, []byte{0xff, 0xd9}) {
			config, err := jpeg.DecodeConfig(bytes.NewReader(data))
			valid = err == nil && validDimensions(int64(config.Width), int64(config.Height))
		}
	case "image/webp":
		valid = len(data) >= 12 && http.DetectContentType(data) == media &&
			int64(binary.LittleEndian.Uint32(data[4:]))+8 == int64(len(data))
	default:
		valid = utf8.Valid(data) && bytes.IndexByte(data, 0) < 0 && !systemBinary(http.DetectContentType(data))
	}
	if !valid {
		return fmt.Errorf("invalid %s bytes for system file %q", media, name)
	}
	return nil
}

func systemManifestOrder(a, b designSystemManifestFile) int { return strings.Compare(a.Path, b.Path) }

func canonicalSystemManifest(manifest designSystemManifestData) ([]byte, error) {
	manifest.Files = slices.Clone(manifest.Files)
	slices.SortFunc(manifest.Files, systemManifestOrder)
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(manifest); err != nil {
		return nil, err
	}
	data := bytes.TrimSuffix(encoded.Bytes(), []byte("\n"))
	// json.Encoder still escapes U+2028 and U+2029 with EscapeHTML disabled.
	// JSON.stringify leaves them as literal characters. Skip escaped backslashes
	// so a spec containing the textual sequence \u2028 is unchanged.
	canonical := make([]byte, 0, len(data))
	for i := 0; i < len(data); i++ {
		if data[i] == '\\' && i+1 < len(data) {
			switch {
			case bytes.HasPrefix(data[i:], []byte(`\u2028`)):
				canonical = append(canonical, "\u2028"...)
				i += 5
			case bytes.HasPrefix(data[i:], []byte(`\u2029`)):
				canonical = append(canonical, "\u2029"...)
				i += 5
			default:
				canonical = append(canonical, data[i], data[i+1])
				i++
			}
		} else {
			canonical = append(canonical, data[i])
		}
	}
	return canonical, nil
}

func systemManifestEntries(manifest designSystemManifestData) (map[string]designSystemManifestFile, error) {
	if manifest.SchemaVersion != 1 || manifest.Files == nil || len(manifest.Files) > designSystemMaxText+designSystemMaxAsset {
		return nil, fmt.Errorf("invalid %s", designSystemManifest)
	}
	if err := validateSystemKit(manifest.Kit); err != nil {
		return nil, err
	}
	result := make(map[string]designSystemManifestFile, len(manifest.Files))
	paths := make(designPaths)
	for _, entry := range manifest.Files {
		media, err := systemFileMedia(entry.Path, manifest.Kit)
		if err != nil {
			return nil, err
		}
		if _, ok := result[entry.Path]; ok {
			return nil, fmt.Errorf("duplicate system file %q", entry.Path)
		}
		if err := paths.add(entry.Path, false); err != nil {
			return nil, err
		}
		if media != entry.MediaType || !validSystemHex(entry.ContentDigest, 64) {
			return nil, fmt.Errorf("invalid media type or checksum for system file %q", entry.Path)
		}
		if err := validateSystemFileUsage(entry.Path, media, entry.Usage); err != nil {
			return nil, err
		}
		result[entry.Path] = entry
	}
	return result, nil
}

func decodeSystemJSON(data []byte, value interface{}) error {
	if !utf8.Valid(data) || !json.Valid(data) {
		return fmt.Errorf("invalid design system JSON")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(value)
}

func (content *designSystemContent) finish() error {
	if _, err := systemManifestEntries(content.manifest); err != nil {
		return err
	}
	logos := 0
	for _, entry := range content.manifest.Files {
		if entry.Usage == "logo" {
			logos++
		}
	}
	if logos > 1 {
		return fmt.Errorf("a design system may contain only one logo")
	}
	for _, name := range []string{"DESIGN.md", "tokens.json", "tokens.css"} {
		file, found := content.files[name]
		if !found {
			return fmt.Errorf("missing required system file %q", name)
		}
		if name == "tokens.json" && !json.Valid(file.Data) {
			return fmt.Errorf("invalid tokens.json")
		}
	}
	slices.SortFunc(content.manifest.Files, systemManifestOrder)
	return nil
}

func readRootSystemFile(root *os.Root, name string, maxBytes int64) ([]byte, error) {
	local, err := designRelativePath(name)
	if err != nil {
		return nil, err
	}
	if err := checkDesignSymlinks(root, local); err != nil {
		return nil, err
	}
	info, err := root.Lstat(local)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("system path %q is not a regular file", name)
	}
	if info.Size() > maxBytes {
		return nil, fmt.Errorf("system file %q exceeds its size limit", name)
	}
	f, err := root.Open(local)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return readBoundedSystemFile(f, name, maxBytes)
}

func readBoundedSystemFile(reader io.Reader, name string, maxBytes int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err == nil && int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("system file %q exceeds its size limit", name)
	}
	return data, err
}

func readSystemFolder(root *os.Root) (*designSystemContent, error) {
	manifest := designSystemManifestData{SchemaVersion: 1, Files: []designSystemManifestFile{}}
	meta, err := readRootSystemFile(root, designSystemManifest, designSystemMiB)
	if err == nil {
		if err := decodeSystemJSON(meta, &manifest); err != nil {
			return nil, fmt.Errorf("read %s: %w", designSystemManifest, err)
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	entries, err := systemManifestEntries(manifest)
	if err != nil {
		return nil, err
	}
	content := &designSystemContent{
		manifest: designSystemManifestData{SchemaVersion: 1, Kit: manifest.Kit, Files: []designSystemManifestFile{}},
		files:    make(map[string]designFile),
	}
	paths := make(designPaths)
	budget := systemFileBudget{}
	err = fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || name == "." {
			return walkErr
		}
		if name == ".copera" && entry.IsDir() {
			return fs.SkipDir
		}
		if name == designSystemManifest {
			return nil
		}
		if err := validateSystemPath(name, systemInstalledPath(name)); err != nil {
			return err
		}
		if err := paths.add(name, entry.IsDir()); err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("system path %q is not a regular file", name)
		}
		media, err := systemFileMedia(name, manifest.Kit)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if err := budget.add(name, media, info.Size()); err != nil {
			return err
		}
		data, err := readRootSystemFile(root, name, info.Size())
		if err != nil {
			return err
		}
		usage := entries[name].Usage
		if usage == "" {
			switch {
			case media == "font/woff2":
				usage = "font"
			case systemBinary(media):
				usage = "image"
			}
		}
		if err := validateSystemBytes(name, media, data); err != nil {
			return err
		}
		content.manifest.Files = append(content.manifest.Files, designSystemManifestFile{
			Path: name, MediaType: media, ContentDigest: sha256Hex(data), Usage: usage,
		})
		content.files[name] = designFile{Data: data, MediaType: media, Binary: systemBinary(media)}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return content, content.finish()
}

func decodeSystemPackage(document json.RawMessage) (*designSystemContent, error) {
	var pack api.ArtifactSystemPackage
	if err := decodeSystemJSON(document, &pack); err != nil {
		return nil, err
	}
	if pack.SchemaVersion != 1 || pack.Files == nil || len(pack.Files) > designSystemMaxText+designSystemMaxAsset {
		return nil, fmt.Errorf("invalid design system package")
	}
	if err := validateSystemKit(pack.Kit); err != nil {
		return nil, err
	}
	content := &designSystemContent{
		manifest: designSystemManifestData{SchemaVersion: 1, Kit: pack.Kit, Files: []designSystemManifestFile{}},
		files:    make(map[string]designFile, len(pack.Files)),
	}
	budget := systemFileBudget{}
	for _, file := range pack.Files {
		media, err := systemFileMedia(file.Path, pack.Kit)
		if err != nil {
			return nil, err
		}
		if media != file.MediaType {
			return nil, fmt.Errorf("unexpected media type for system file %q", file.Path)
		}
		data, err := decodeSystemBase64(file, systemFileLimit(file.Path, media))
		if err != nil {
			return nil, err
		}
		if err := budget.add(file.Path, media, int64(len(data))); err != nil {
			return nil, err
		}
		content.manifest.Files = append(content.manifest.Files, designSystemManifestFile{
			Path: file.Path, MediaType: media, ContentDigest: sha256Hex(data), Usage: file.Usage,
		})
		content.files[file.Path] = designFile{Data: data, MediaType: media, Binary: systemBinary(media)}
	}
	return content, content.finish()
}

func decodeSystemBase64(file api.ArtifactSystemFile, limit int64) ([]byte, error) {
	if int64(len(file.ContentBase64)) > int64(base64.StdEncoding.EncodedLen(int(limit))) {
		return nil, fmt.Errorf("system file %q exceeds its size limit", file.Path)
	}
	data, err := base64.StdEncoding.Strict().DecodeString(file.ContentBase64)
	if err != nil || base64.StdEncoding.EncodeToString(data) != file.ContentBase64 {
		return nil, fmt.Errorf("invalid base64 for system file %q", file.Path)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("system file %q exceeds its size limit", file.Path)
	}
	if err := validateSystemBytes(file.Path, file.MediaType, data); err != nil {
		return nil, err
	}
	return data, nil
}

func (content *designSystemContent) packageDocument() *api.ArtifactSystemPackage {
	files := make([]api.ArtifactSystemFile, 0, len(content.manifest.Files))
	for _, entry := range content.manifest.Files {
		files = append(files, api.ArtifactSystemFile{Path: entry.Path, MediaType: entry.MediaType,
			ContentBase64: base64.StdEncoding.EncodeToString(content.files[entry.Path].Data), Usage: entry.Usage})
	}
	return &api.ArtifactSystemPackage{SchemaVersion: 1, Kit: content.manifest.Kit, Files: files}
}

func readSystemZIP(data []byte) (*designSystemContent, error) {
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("read design system ZIP: %w", err)
	}
	if len(archive.File) > (designSystemMaxText+designSystemMaxAsset)*64+1 {
		return nil, fmt.Errorf("design system ZIP has too many entries")
	}
	members := make(map[string]*zip.File)
	directories := make(map[string]bool)
	paths := make(designPaths)
	seen := make(map[string]bool)
	for _, entry := range archive.File {
		dir := entry.FileInfo().IsDir()
		name := entry.Name
		if dir {
			name = strings.TrimSuffix(name, "/")
		}
		if err := validateSystemPath(name, systemInstalledPath(name)); err != nil {
			return nil, fmt.Errorf("unsafe design system ZIP entry %q: %w", entry.Name, err)
		}
		if seen[name] || entry.Mode()&fs.ModeSymlink != 0 || (!dir && !entry.Mode().IsRegular()) || (dir && entry.UncompressedSize64 != 0) {
			return nil, fmt.Errorf("invalid or repeated design system ZIP entry %q", entry.Name)
		}
		seen[name] = true
		if err := paths.add(name, dir); err != nil {
			return nil, err
		}
		if dir {
			directories[name] = true
		} else {
			members[name] = entry
		}
	}
	for name := range members {
		for dir := path.Dir(name); dir != "."; dir = path.Dir(dir) {
			delete(directories, dir)
		}
	}
	for dir := range directories {
		return nil, fmt.Errorf("unlisted directory %q in the Design system ZIP", dir)
	}
	meta := members[designSystemManifest]
	if meta == nil {
		return nil, fmt.Errorf("design system ZIP is missing %s", designSystemManifest)
	}
	raw, err := readSystemZIPFile(meta, designSystemMiB)
	if err != nil {
		return nil, err
	}
	var manifest designSystemManifestData
	if err := decodeSystemJSON(raw, &manifest); err != nil {
		return nil, fmt.Errorf("read %s: %w", designSystemManifest, err)
	}
	entries, err := systemManifestEntries(manifest)
	if err != nil {
		return nil, err
	}
	delete(members, designSystemManifest)
	budget := systemFileBudget{}
	for name, member := range members {
		entry, found := entries[name]
		if !found {
			return nil, fmt.Errorf("system file %q is missing from %s", name, designSystemManifest)
		}
		if member.UncompressedSize64 > uint64(systemFileLimit(name, entry.MediaType)) {
			return nil, fmt.Errorf("system file %q exceeds its size limit", name)
		}
		if err := budget.add(name, entry.MediaType, int64(member.UncompressedSize64)); err != nil {
			return nil, err
		}
	}
	content := &designSystemContent{manifest: manifest, files: make(map[string]designFile, len(entries))}
	for _, entry := range manifest.Files {
		name := entry.Path
		member := members[name]
		if member == nil {
			return nil, fmt.Errorf("system file %q is missing from the ZIP", name)
		}
		raw, err := readSystemZIPFile(member, int64(member.UncompressedSize64))
		if err != nil {
			return nil, err
		}
		if sha256Hex(raw) != entry.ContentDigest {
			return nil, fmt.Errorf("system file %q does not match its checksum", name)
		}
		if err := validateSystemBytes(name, entry.MediaType, raw); err != nil {
			return nil, err
		}
		content.files[name] = designFile{Data: raw, MediaType: entry.MediaType, Binary: systemBinary(entry.MediaType)}
	}
	return content, content.finish()
}

func readSystemZIPFile(member *zip.File, limit int64) ([]byte, error) {
	if member.UncompressedSize64 > uint64(limit) {
		return nil, fmt.Errorf("system file %q exceeds its size limit", member.Name)
	}
	f, err := member.Open()
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return readBoundedSystemFile(f, member.Name, limit)
}

func writeSystemZIP(content *designSystemContent) ([]byte, error) {
	var output bytes.Buffer
	archive := zip.NewWriter(&output)
	manifest, err := canonicalSystemManifest(content.manifest)
	if err != nil {
		return nil, err
	}
	entry, err := archive.Create(designSystemManifest)
	if err != nil {
		return nil, err
	}
	if _, err := entry.Write(manifest); err != nil {
		return nil, err
	}
	for _, file := range content.manifest.Files {
		entry, err := archive.Create(file.Path)
		if err != nil {
			return nil, err
		}
		if _, err := entry.Write(content.files[file.Path].Data); err != nil {
			return nil, err
		}
	}
	if err := archive.Close(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func readSystemExchangeFile(filename string, limit int64) ([]byte, error) {
	root, err := openDesignDir(filepath.Dir(filename), false)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return readRootSystemFile(root, filepath.Base(filename), limit)
}
