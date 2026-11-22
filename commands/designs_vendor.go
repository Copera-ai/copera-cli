package commands

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"
	"golang.org/x/text/unicode/norm"
)

const (
	designVendorFileLimit     = 5 << 20
	designVendorCSSLimit      = 1 << 20
	designVendorMetadataLimit = 8 << 20
	designVendorUserAgent     = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
)

var (
	errDesignVendorDownloadLimit = errors.New("reached the 20 MiB download limit")

	designVendorPathChars = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)
	designVendorCDNJSPath = regexp.MustCompile(`^/ajax/libs/([A-Za-z0-9._-]+)/([^/]+)/(.+)$`)
	designVendorNPMPath   = regexp.MustCompile(`^/npm/(?:@([a-z0-9._-]+)/)?([a-z0-9._-]+)@([^/]+)/(.+)$`)
	designVendorCDNJSVer  = regexp.MustCompile(`^v?[0-9]+(?:\.[0-9]+){1,3}(?:-[A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)*)?$`)
	designVendorNPMVer    = regexp.MustCompile(`^(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:-[A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)*)?$`)
)

type designVendorSource struct {
	url         *url.URL
	path        string
	cdnFile     string
	metadataURL string
}

type designVendorFile struct {
	Path          string `json:"path"`
	ByteLength    int    `json:"byteLength"`
	ContentDigest string `json:"contentDigest"`
	Verification  string `json:"verification"`
	data          []byte
	sourceURLs    []string
}

type designVendorTemporaryError struct{ error }

func (e designVendorTemporaryError) Unwrap() error { return e.error }

func isDesignVendorTemporary(err error) bool {
	var temporary designVendorTemporaryError
	return !errors.Is(err, context.Canceled) &&
		(errors.Is(err, context.DeadlineExceeded) || errors.As(err, &temporary))
}

func newDesignVendorCmd(cli *CLI) *cobra.Command {
	var dir, destination string
	var all bool
	cmd := &cobra.Command{
		Use:   "vendor <url> | vendor --all [dir]",
		Short: "Save pinned CDN files and Google fonts to the local Design",
		Long: "Save a pinned CDN file or Google font to the local Design. " +
			"Paste a Google Fonts stylesheet URL directly, including semicolon-separated axis values and multiple families. " +
			"Use --all to vendor and rewrite outside stylesheet and script references.",
		Args: func(cmd *cobra.Command, args []string) error {
			if all {
				return nil
			}
			return cobra.ExactArgs(1)(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if all {
				for _, arg := range args {
					lower := strings.ToLower(arg)
					if strings.HasPrefix(lower, "https:") || strings.HasPrefix(lower, "http:") ||
						strings.HasPrefix(arg, "//") || strings.Contains(arg, "://") {
						return designUsageError(cli, fmt.Errorf("--all cannot be used with a URL"))
					}
				}
				if cmd.Flags().Changed("path") {
					return designUsageError(cli, fmt.Errorf("--all cannot be used with --path"))
				}
				if len(args) > 1 || len(args) == 1 && cmd.Flags().Changed("dir") {
					return designUsageError(cli, fmt.Errorf("--all accepts at most one Design folder"))
				}
				if len(args) == 1 {
					dir = args[0]
				}
			}
			root, err := openDesignDir(dir, false)
			if err != nil {
				return designError(cli, err)
			}
			defer func() { _ = root.Close() }()
			state, err := loadDesignState(root)
			if err != nil {
				return designError(cli, err)
			}
			if all {
				return runDesignVendorAll(cmd.Context(), cli, root, state, dir)
			}
			source, err := parseDesignVendorSource(args[0], destination)
			if err != nil {
				return designError(cli, err)
			}
			files, err := downloadDesignVendorSource(cmd.Context(), source, nil)
			if err != nil {
				return designError(cli, err)
			}
			if _, err := writeDesignVendorFiles(root, files); err != nil {
				return designError(cli, err)
			}
			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(struct {
					Files []designVendorFile `json:"files"`
				}{files})
			}
			for _, file := range files {
				cli.Printer.PrintLine(fmt.Sprintf("%s  %s", file.ContentDigest, file.Path))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "Vendor and rewrite outside resources in the Design")
	cmd.Flags().StringVar(&dir, "dir", ".", "Initialized Design folder")
	cmd.Flags().StringVar(&destination, "path", "", "Output path under vendor/")
	return cmd
}

func downloadDesignVendorSource(ctx context.Context, source designVendorSource, bytesLeft *int64) ([]designVendorFile, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	var files []designVendorFile
	var err error
	if source.url.Host == "fonts.googleapis.com" {
		files, err = vendorGoogleFonts(ctx, client, source, bytesLeft)
	} else {
		files, err = vendorCDNFile(ctx, client, source, bytesLeft)
	}
	if err != nil {
		return nil, err
	}
	return files, ctx.Err()
}

func parseDesignVendorURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil ||
		strings.Contains(raw, "#") || u.EscapedPath() != u.Path ||
		(u.Host != u.Hostname() && u.Host != u.Hostname()+":443") {
		return nil, fmt.Errorf("expected a canonical HTTPS vendor URL without credentials, fragments or custom ports")
	}
	u.Host = strings.ToLower(u.Hostname())
	return u, nil
}

func parseDesignVendorSource(raw, override string) (designVendorSource, error) {
	u, err := parseDesignVendorURL(raw)
	if err != nil {
		return designVendorSource{}, err
	}
	source := designVendorSource{url: u}
	switch u.Host {
	case "cdnjs.cloudflare.com":
		parts := designVendorCDNJSPath.FindStringSubmatch(u.Path)
		if parts == nil || !designVendorCDNJSVer.MatchString(parts[2]) {
			return source, fmt.Errorf("cdnjs needs an exact version and an explicit file path")
		}
		source.cdnFile = parts[3]
		source.path = "vendor/cdnjs/" + parts[1] + "/" + parts[2] + "/" + source.cdnFile
		source.metadataURL = "https://api.cdnjs.com/libraries/" + parts[1] + "/" + parts[2] + "?fields=files,sri"
	case "cdn.jsdelivr.net":
		parts := designVendorNPMPath.FindStringSubmatch(u.Path)
		if parts == nil || !designVendorNPMVer.MatchString(parts[3]) {
			return source, fmt.Errorf("jsDelivr needs an exact npm version and an explicit file path")
		}
		scope, name, version, file := parts[1], parts[2], parts[3], parts[4]
		source.cdnFile = file
		source.path = "vendor/npm/" + name + "/" + version + "/" + file
		pkg := name
		if scope != "" {
			pkg = "@" + scope + "/" + name
			source.path = "vendor/npm-scoped/" + scope + "/" + name + "/" + version + "/" + file
		}
		source.metadataURL = "https://data.jsdelivr.com/v1/packages/npm/" + pkg + "@" + version + "?structure=flat"
	case "fonts.googleapis.com":
		if u.Path != "/css" && u.Path != "/css2" {
			return source, fmt.Errorf("Google Fonts URLs must use /css or /css2")
		}
		rawQuery := u.RawQuery
		if u.Path == "/css2" && strings.Contains(rawQuery, ";") {
			// Go rejects literal semicolons in queries, but CSS2 uses them in axis values.
			params := strings.Split(rawQuery, "&")
			for i, param := range params {
				value, ok := strings.CutPrefix(param, "family=")
				if !ok || !strings.Contains(value, ";") {
					continue
				}
				spec, err := url.QueryUnescape(value)
				beforeSemicolon, _, _ := strings.Cut(spec, ";")
				_, style, _ := strings.Cut(beforeSemicolon, ":")
				if err == nil && strings.Contains(style, "@") && !strings.Contains(spec, "=") {
					params[i] = "family=" + strings.ReplaceAll(value, ";", "%3B")
				}
			}
			rawQuery = strings.Join(params, "&")
		}
		query, err := url.ParseQuery(rawQuery)
		if err != nil || query.Get("family") == "" {
			return source, fmt.Errorf("Google Fonts requires a valid stylesheet family query")
		}
		u.RawQuery = query.Encode()
		sum := sha256.Sum256([]byte(u.String()))
		source.path = fmt.Sprintf("vendor/fonts/%x/fonts.css", sum[:8])
	default:
		return source, fmt.Errorf("vendor supports pinned cdnjs, jsDelivr npm and Google Fonts URLs")
	}
	ext := strings.ToLower(path.Ext(source.path))
	if source.cdnFile != "" {
		if u.RawQuery != "" || u.ForceQuery || !designVendorPathChars.MatchString(source.cdnFile) ||
			(ext != ".js" && ext != ".css" && ext != ".json") {
			return source, fmt.Errorf("a pinned CDN URL must name an exact .js, .css or .json file without a query")
		}
		if _, err := designRelativePath(source.path); err != nil {
			return source, err
		}
	}
	if override != "" {
		source.path = override
	}
	return source, validateDesignVendorPath(source.path, ext)
}

func validateDesignVendorPath(name, ext string) error {
	if !strings.HasPrefix(name, "vendor/") || len(name) > 240 || strings.Count(name, "/") >= 64 ||
		!designVendorPathChars.MatchString(name) || !strings.EqualFold(path.Ext(name), ext) ||
		len(path.Base(name)) <= len(ext) {
		return fmt.Errorf("expected a safe %s output path under vendor/ (at most 240 characters)", ext)
	}
	if err := validateDesignContentPath(name); err != nil {
		return err
	}
	for _, part := range strings.Split(strings.ToLower(name), "/") {
		if part == "node_modules" || part == ".hg" || part == ".svn" || part == ".env" || strings.HasPrefix(part, ".env.") {
			return fmt.Errorf("unsafe vendor path %q", name)
		}
	}
	base := strings.ToLower(path.Base(name))
	if slices.Contains([]string{"package.json", "tsconfig.json", "jsconfig.json", ".postcssrc.js", ".postcssrc.json",
		"postcss.config.js", "tailwind.config.js", "vite.config.js"}, base) {
		return fmt.Errorf("vendor path %q is reserved for build configuration", name)
	}
	return nil
}

func downloadDesignVendor(ctx context.Context, client *http.Client, remote string, limit int64, bytesLeft *int64) ([]byte, string, error) {
	data, mediaType, err := downloadDesignVendorAttempt(ctx, client, remote, limit, bytesLeft)
	if isDesignVendorTemporary(err) && ctx.Err() == nil {
		return downloadDesignVendorAttempt(ctx, client, remote, limit, bytesLeft)
	}
	return data, mediaType, err
}

func downloadDesignVendorAttempt(ctx context.Context, client *http.Client, remote string, limit int64, bytesLeft *int64) ([]byte, string, error) {
	readLimit := limit + 1
	if bytesLeft != nil {
		if *bytesLeft == 0 {
			return nil, "", errDesignVendorDownloadLimit
		}
		readLimit = min(readLimit, *bytesLeft)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, remote, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", designVendorUserAgent)
	res, err := client.Do(req)
	if err != nil {
		return nil, "", designVendorTemporaryError{err}
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		err := fmt.Errorf("vendor download from %s returned HTTP %d", req.URL.Host, res.StatusCode)
		if res.StatusCode == http.StatusTooManyRequests || res.StatusCode >= 500 && res.StatusCode < 600 {
			err = designVendorTemporaryError{err}
		}
		return nil, "", err
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, readLimit))
	if bytesLeft != nil {
		*bytesLeft -= int64(len(data))
		if *bytesLeft == 0 && res.ContentLength != int64(len(data)) {
			return nil, "", errDesignVendorDownloadLimit
		}
	}
	if int64(len(data)) > limit || (len(data) == 0 && err == nil) {
		return nil, "", fmt.Errorf("vendor download from %s must contain 1 to %d bytes", req.URL.Host, limit)
	}
	if err != nil {
		return nil, "", designVendorTemporaryError{err}
	}
	mediaType := res.Header.Get("Content-Type")
	if mediaType != "" && req.URL.Host != "fonts.gstatic.com" {
		mediaType, _, err = mime.ParseMediaType(mediaType)
		if err != nil {
			return nil, "", fmt.Errorf("vendor download from %s returned an invalid content type", req.URL.Host)
		}
	}
	return data, mediaType, nil
}

func vendorCDNFile(ctx context.Context, client *http.Client, source designVendorSource, bytesLeft *int64) ([]designVendorFile, error) {
	metadata, _, err := downloadDesignVendor(ctx, client, source.metadataURL, designVendorMetadataLimit, bytesLeft)
	if err != nil {
		return nil, fmt.Errorf("read CDN metadata: %w", err)
	}
	var checksum string
	var exact bool
	if source.url.Host == "cdnjs.cloudflare.com" {
		var version struct {
			Files []string          `json:"files"`
			SRI   map[string]string `json:"sri"`
		}
		if err := json.Unmarshal(metadata, &version); err != nil {
			return nil, fmt.Errorf("read cdnjs metadata: %w", err)
		}
		exact = slices.Contains(version.Files, source.cdnFile)
		checksum = version.SRI[source.cdnFile]
	} else {
		var version struct {
			Files []struct {
				Name string `json:"name"`
				Hash string `json:"hash"`
			} `json:"files"`
		}
		if err := json.Unmarshal(metadata, &version); err != nil {
			return nil, fmt.Errorf("read jsDelivr metadata: %w", err)
		}
		for _, file := range version.Files {
			if file.Name == "/"+source.cdnFile {
				exact = true
				if file.Hash != "" {
					checksum = "sha256-" + file.Hash
				}
				break
			}
		}
	}
	if !exact {
		return nil, fmt.Errorf("CDN metadata did not confirm the exact file %q", source.cdnFile)
	}
	data, mediaType, err := downloadDesignVendor(ctx, client, source.url.String(), designVendorFileLimit, bytesLeft)
	if err != nil {
		return nil, err
	}
	if !validDesignVendorText(data, mediaType, strings.ToLower(path.Ext(source.path))) {
		return nil, fmt.Errorf("CDN returned a file inconsistent with %q", source.path)
	}
	verification := "local_sha256"
	if checksum != "" {
		if err := verifyDesignVendorChecksum(data, checksum); err != nil {
			return nil, err
		}
		verification = "cdn_checksum"
	}
	return []designVendorFile{newDesignVendorFile(source.path, data, verification)}, nil
}

func validDesignVendorText(data []byte, mediaType, ext string) bool {
	if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 ||
		strings.HasPrefix(http.DetectContentType(bytes.TrimSpace(data)), "text/html") {
		return false
	}
	generic := mediaType == "" || mediaType == "application/octet-stream" || mediaType == "text/plain"
	switch ext {
	case ".js":
		return generic || mediaType == "application/javascript" || mediaType == "text/javascript" || mediaType == "application/x-javascript"
	case ".css":
		return generic || mediaType == "text/css"
	case ".json":
		return (generic || mediaType == "application/json" || mediaType == "text/json") && json.Valid(data)
	}
	return false
}

func verifyDesignVendorChecksum(data []byte, published string) error {
	tokens := strings.Fields(published)
	sum512, sum384, sum256 := sha512.Sum512(data), sha512.Sum384(data), sha256.Sum256(data)
	for _, algo := range []struct {
		prefix string
		digest []byte
	}{{"sha512-", sum512[:]}, {"sha384-", sum384[:]}, {"sha256-", sum256[:]}} {
		present, matched := false, false
		for _, token := range tokens {
			hash, ok := strings.CutPrefix(token, algo.prefix)
			if !ok {
				continue
			}
			present = true
			decoded, err := base64.StdEncoding.DecodeString(hash)
			if err != nil || len(decoded) != len(algo.digest) {
				return fmt.Errorf("CDN published an invalid checksum")
			}
			matched = matched || bytes.Equal(decoded, algo.digest)
		}
		if matched {
			return nil
		}
		if present {
			return fmt.Errorf("CDN checksum does not match the downloaded bytes")
		}
	}
	return fmt.Errorf("CDN did not publish a supported checksum")
}

func newDesignVendorFile(name string, data []byte, verification string) designVendorFile {
	return designVendorFile{
		Path: name, ByteLength: len(data), ContentDigest: sha256Hex(data), Verification: verification, data: data,
	}
}

var (
	designVendorCSSComments = regexp.MustCompile(`(?s)/\*.*?\*/`)
	designVendorFontRule    = regexp.MustCompile(`^@font-face\s*\{([^{}]*)\}`)
	designVendorFontSrc     = regexp.MustCompile(`^url\(\s*(?:"([^"\s]+)"|'([^'\s]+)'|([^'"\s()]+))\s*\)\s+format\(\s*(?:"woff2"|'woff2'|woff2)\s*\)$`)
	designVendorFontProps   = map[string]*regexp.Regexp{
		"font-family":   regexp.MustCompile(`^(?:'[-\pL\pN ._]+'|"[-\pL\pN ._]+"|[-\pL\pN ._]+)$`),
		"font-style":    regexp.MustCompile(`^(?:normal|italic|oblique(?:\s+-?[0-9]+(?:\.[0-9]+)?deg){0,2})$`),
		"font-weight":   regexp.MustCompile(`^(?:normal|bold|(?:[1-9][0-9]{0,2}|1000)(?:\s+(?:[1-9][0-9]{0,2}|1000))?)$`),
		"font-display":  regexp.MustCompile(`^(?:auto|block|swap|fallback|optional)$`),
		"font-stretch":  regexp.MustCompile(`^(?:normal|[0-9]+(?:\.[0-9]+)?%(?:\s+[0-9]+(?:\.[0-9]+)?%)?)$`),
		"unicode-range": regexp.MustCompile(`(?i)^u\+[0-9a-f?]{1,6}(?:-[0-9a-f]{1,6})?(?:\s*,\s*u\+[0-9a-f?]{1,6}(?:-[0-9a-f]{1,6})?)*$`),
	}
)

type designVendorFontDeclaration struct {
	name  string
	value string
}

func parseDesignVendorFontCSS(css []byte) ([][]designVendorFontDeclaration, error) {
	var rules [][]designVendorFontDeclaration
	for rest := strings.TrimSpace(designVendorCSSComments.ReplaceAllString(string(css), " ")); rest != ""; {
		rule := designVendorFontRule.FindStringSubmatch(rest)
		if rule == nil {
			return nil, fmt.Errorf("Google Fonts CSS must contain only supported @font-face rules")
		}
		rest = strings.TrimSpace(rest[len(rule[0]):])
		properties := make(map[string]bool)
		var declarations []designVendorFontDeclaration
		for _, declaration := range strings.Split(rule[1], ";") {
			if strings.TrimSpace(declaration) == "" {
				continue
			}
			name, value, ok := strings.Cut(declaration, ":")
			name, value = strings.TrimSpace(name), strings.TrimSpace(value)
			if !ok || properties[name] {
				return nil, fmt.Errorf("invalid declaration in Google Fonts @font-face rule")
			}
			properties[name] = true
			if name == "src" {
				font, err := parseDesignVendorFontURL(value)
				if err != nil {
					return nil, err
				}
				value = font
			} else if pattern := designVendorFontProps[name]; pattern == nil || !pattern.MatchString(value) {
				return nil, fmt.Errorf("unsupported %q in Google Fonts @font-face rule", name)
			}
			declarations = append(declarations, designVendorFontDeclaration{name, value})
		}
		if !properties["src"] || !properties["font-family"] {
			return nil, fmt.Errorf("Google Fonts @font-face rule requires a family and WOFF2 source")
		}
		rules = append(rules, declarations)
	}
	if len(rules) == 0 {
		return nil, fmt.Errorf("Google Fonts returned no @font-face rules")
	}
	return rules, nil
}

func parseDesignVendorFontURL(value string) (string, error) {
	match := designVendorFontSrc.FindStringSubmatch(value)
	if match == nil {
		return "", fmt.Errorf("Google Fonts must specify exactly one WOFF2 source per @font-face")
	}
	u, err := parseDesignVendorURL(match[1] + match[2] + match[3])
	if err != nil || u.Host != "fonts.gstatic.com" || !designVendorPathChars.MatchString(u.Path) || path.Clean(u.Path) != u.Path {
		return "", fmt.Errorf("Google Fonts WOFF2 sources must come from fonts.gstatic.com")
	}
	switch {
	case path.Ext(u.Path) == ".woff2" && u.RawQuery == "" && !u.ForceQuery:
	case u.Path == "/l/font" && u.Query().Get("kit") != "":
		// Google uses this endpoint for WOFF2 subsets selected with text=.
	default:
		return "", fmt.Errorf("Google Fonts source is not a WOFF2 file")
	}
	return u.String(), nil
}

func vendorGoogleFonts(ctx context.Context, client *http.Client, source designVendorSource, bytesLeft *int64) ([]designVendorFile, error) {
	css, mediaType, err := downloadDesignVendor(ctx, client, source.url.String(), designVendorCSSLimit, bytesLeft)
	if err != nil {
		return nil, err
	}
	if !validDesignVendorText(css, mediaType, ".css") {
		return nil, fmt.Errorf("Google Fonts returned invalid CSS")
	}
	rules, err := parseDesignVendorFontCSS(css)
	if err != nil {
		return nil, err
	}
	urls := make(map[string]string)
	for _, rule := range rules {
		for _, declaration := range rule {
			if declaration.name == "src" {
				urls[declaration.value] = ""
			}
		}
	}
	if len(urls) > 32 {
		return nil, fmt.Errorf("Google Fonts stylesheet exceeds 32 font files")
	}
	files := make([]designVendorFile, 1, len(urls)+1)
	var localCSS strings.Builder
	written := make(map[string]int)
	for _, rule := range rules {
		localCSS.WriteString("@font-face {\n")
		for _, declaration := range rule {
			value := declaration.value
			if declaration.name == "src" {
				remote := value
				name := urls[remote]
				if name == "" {
					font, _, err := downloadDesignVendor(ctx, client, remote, designVendorFileLimit, bytesLeft)
					if err != nil {
						return nil, err
					}
					if !validDesignVendorWOFF2(font) {
						return nil, fmt.Errorf("Google Fonts returned an invalid WOFF2 file")
					}
					file := newDesignVendorFile("", font, "local_sha256")
					name = file.ContentDigest + ".woff2"
					file.Path = path.Join(path.Dir(source.path), name)
					if err := validateDesignVendorPath(file.Path, ".woff2"); err != nil {
						return nil, err
					}
					urls[remote] = name
					if i, exists := written[name]; exists {
						files[i].sourceURLs = append(files[i].sourceURLs, remote)
					} else {
						file.sourceURLs = []string{remote}
						written[name] = len(files)
						files = append(files, file)
					}
				}
				value = "url(" + name + ") format('woff2')"
			}
			fmt.Fprintf(&localCSS, "  %s: %s;\n", declaration.name, value)
		}
		localCSS.WriteString("}\n")
	}
	if localCSS.Len() > designVendorCSSLimit {
		return nil, fmt.Errorf("rewritten Google Fonts stylesheet exceeds 1 MiB")
	}
	files[0] = newDesignVendorFile(source.path, []byte(localCSS.String()), "local_sha256")
	return files, nil
}

func validDesignVendorWOFF2(data []byte) bool {
	const headerSize = 48
	return len(data) > headerSize && string(data[:4]) == "wOF2" &&
		binary.BigEndian.Uint32(data[8:12]) == uint32(len(data)) &&
		binary.BigEndian.Uint16(data[12:14]) > 0 && binary.BigEndian.Uint16(data[14:16]) == 0 &&
		binary.BigEndian.Uint32(data[20:24]) > 0 && binary.BigEndian.Uint32(data[20:24]) < uint32(len(data)-headerSize)
}

func writeDesignVendorFiles(root *os.Root, files []designVendorFile) ([]string, error) {
	var newFiles []designVendorFile
	for _, file := range files {
		exists, err := designVendorDestinationExists(root, file)
		if err != nil {
			return nil, err
		}
		if !exists {
			newFiles = append(newFiles, file)
		}
	}
	type stagedLink struct {
		temp  string
		local string
	}
	var staged []stagedLink
	defer func() {
		for _, link := range staged {
			_ = root.Remove(link.temp)
		}
	}()
	for _, file := range newFiles {
		local := filepath.FromSlash(file.Path)
		temp := filepath.Join(filepath.Dir(local), ".copera-vendor-"+rand.Text())
		if err := writeDesignFile(root, filepath.ToSlash(temp), file.data); err != nil {
			return nil, err
		}
		staged = append(staged, stagedLink{temp, local})
	}
	written := make([]string, 0, len(staged))
	for i, link := range staged {
		if err := root.Link(link.temp, link.local); err != nil {
			for _, previous := range staged[:i] {
				local, localErr := root.Lstat(previous.local)
				temp, tempErr := root.Lstat(previous.temp)
				if localErr == nil && tempErr == nil && os.SameFile(local, temp) {
					_ = root.Remove(previous.local)
				}
			}
			return nil, err
		}
		written = append(written, filepath.ToSlash(link.local))
	}
	return written, nil
}

func designVendorDestinationExists(root *os.Root, file designVendorFile) (bool, error) {
	local := filepath.FromSlash(file.Path)
	if err := checkDesignSymlinks(root, local); err != nil {
		return false, err
	}
	parent := "."
	for _, part := range strings.Split(file.Path, "/") {
		entries, err := fs.ReadDir(root.FS(), parent)
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		for _, entry := range entries {
			if entry.Name() != part && strings.ToLower(norm.NFKC.String(entry.Name())) == strings.ToLower(part) {
				return false, fmt.Errorf("vendor path %q and existing path %q collide", file.Path, path.Join(parent, entry.Name()))
			}
		}
		parent = path.Join(parent, part)
	}
	info, err := root.Lstat(local)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || info.Size() != int64(file.ByteLength) {
		return false, fmt.Errorf("vendor destination %q already exists with different bytes", file.Path)
	}
	content, err := readDesignFile(root, file.Path)
	if err != nil {
		return false, err
	}
	if !bytes.Equal(file.data, content) {
		return false, fmt.Errorf("vendor destination %q already exists with different bytes", file.Path)
	}
	return true, nil
}
