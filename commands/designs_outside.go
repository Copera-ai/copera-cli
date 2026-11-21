package commands

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/net/html"
)

type designOutsideReference struct {
	URL    string `json:"url"`
	Host   string `json:"host"`
	Path   string `json:"path"`
	Reason string `json:"reason,omitempty"`

	start, end int
	hint       bool
	source     designVendorSource
}

func scanDesignOutsideReferences(files map[string]designFile) ([]designOutsideReference, error) {
	var outside []designOutsideReference
	for _, name := range slices.Sorted(maps.Keys(files)) {
		data, err := designReferenceContents(name, files[name])
		if err != nil {
			return nil, err
		}
		for _, ref := range scanDesignReferences(name, data) {
			if !ref.hint {
				outside = append(outside, ref)
			}
		}
	}
	return outside, nil
}

func designReferenceContents(name string, file designFile) ([]byte, error) {
	if !strings.EqualFold(path.Ext(name), ".css") || file.Upload == nil {
		return file.Data, nil
	}
	reader, err := file.Upload.Open()
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(reader, file.Upload.ByteLength+1))
	if err := errors.Join(err, reader.Close()); err != nil {
		return nil, err
	}
	if sha256Hex(data) != file.Upload.ContentDigest {
		return nil, fmt.Errorf("%q changed during the outside reference scan", name)
	}
	return data, nil
}

func scanDesignReferences(name string, data []byte) []designOutsideReference {
	switch strings.ToLower(path.Ext(name)) {
	case ".html", ".htm":
		return scanDesignHTMLReferences(name, data)
	case ".css":
		return scanDesignCSSReferences(name, data)
	}
	return nil
}

func newDesignOutsideReference(name, raw string, start, end int, hint bool, reason string) (designOutsideReference, bool) {
	raw = strings.TrimSpace(raw)
	ref := designOutsideReference{URL: raw, Path: name, Reason: reason, start: start, end: end, hint: hint}
	// An invalid URL can still be an outside resource worth reporting.
	lower := strings.ToLower(raw)
	if !strings.HasPrefix(lower, "https://") && !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(raw, "//") {
		return ref, false
	}
	if remote, err := url.Parse(raw); err == nil {
		ref.Host = strings.ToLower(remote.Hostname())
	}
	if !hint && reason == "" {
		source, err := parseDesignVendorSource(raw, "")
		if err != nil {
			ref.Reason = err.Error()
		} else {
			ref.source = source
		}
	}
	return ref, true
}

// The tokenizer identifies real HTML tags and decodes HTML entities. The
// attribute pattern locates the original bytes, including unquoted values.
var designHTMLAttribute = regexp.MustCompile(`([^\t\n\f\r />=]+)(?:[\t\n\f\r ]*=[\t\n\f\r ]*(?:"([^"]*)"|'([^']*)'|([^\t\n\f\r >]+)))?`)

func scanDesignHTMLReferences(name string, data []byte) []designOutsideReference {
	var outside []designOutsideReference
	tokenizer := html.NewTokenizer(bytes.NewReader(data))
	offset := 0
	for kind := tokenizer.Next(); kind != html.ErrorToken; kind = tokenizer.Next() {
		start := offset
		offset += len(tokenizer.Raw())
		if kind != html.StartTagToken && kind != html.SelfClosingTagToken {
			continue
		}
		token := tokenizer.Token()
		var attributes = make(map[string]string, len(token.Attr))
		for _, attr := range token.Attr {
			if _, found := attributes[attr.Key]; !found {
				attributes[attr.Key] = attr.Val
			}
		}
		var key, reason string
		var hint bool
		switch token.Data {
		case "link":
			key = "href"
			rel := strings.Fields(strings.ToLower(attributes["rel"]))
			switch {
			case slices.Contains(rel, "stylesheet"):
			case slices.Contains(rel, "preconnect"), slices.Contains(rel, "dns-prefetch"):
				hint = true
			default:
				reason = "only stylesheet links can be vendored"
			}
		case "script":
			key = "src"
		case "img":
			key, reason = "src", "image URLs cannot be vendored"
		case "audio", "video", "source", "track", "iframe", "embed", "input":
			key, reason = "src", fmt.Sprintf("<%s> URLs cannot be vendored", token.Data)
		default:
			continue
		}
		if attributes[key] == "" {
			continue
		}
		if hint {
			if ref, ok := newDesignOutsideReference(name, attributes[key], start, offset, true, ""); ok {
				outside = append(outside, ref)
			}
			continue
		}
		attrStart := start + 1 + len(token.Data)
		rawAttributes := data[attrStart:offset]
		for _, match := range designHTMLAttribute.FindAllSubmatchIndex(rawAttributes, -1) {
			if !strings.EqualFold(string(rawAttributes[match[2]:match[3]]), key) {
				continue
			}
			for i := 4; i < len(match); i += 2 {
				if match[i] >= 0 {
					if ref, ok := newDesignOutsideReference(name, attributes[key], attrStart+match[i], attrStart+match[i+1], false, reason); ok {
						outside = append(outside, ref)
					}
					break
				}
			}
			break
		}
	}
	return outside
}

// Match CSS comments and strings along with URL tokens so references inside
// those literals do not get rewritten.
var designCSSReferences = regexp.MustCompile(`(?is)/\*.*?(?:\*/|$)|"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|(@import)\b|url\([\t\n\f\r ]*(?:"((?:\\.|[^"\\])*)"|'((?:\\.|[^'\\])*)'|((?:\\.|[^\s"'()\\])*))[\t\n\f\r ]*\)`)

func scanDesignCSSReferences(name string, data []byte) []designOutsideReference {
	var outside []designOutsideReference
	importEnd := -1
	for _, match := range designCSSReferences.FindAllSubmatchIndex(data, -1) {
		afterImport := importEnd >= 0 && len(bytes.TrimSpace(data[importEnd:match[0]])) == 0
		switch {
		case match[2] >= 0:
			importEnd = match[1]
			continue
		case afterImport && bytes.HasPrefix(data[match[0]:], []byte("/*")):
			importEnd = match[1]
			continue
		}
		importEnd = -1
		if match[0] > 0 && cssNameByte(data[match[0]-1]) {
			continue
		}
		for i := 4; i < len(match); i += 2 {
			start, end := match[i], match[i+1]
			if start < 0 {
				continue
			}
			reason := ""
			if !afterImport {
				reason = "only CSS @import URLs can be vendored"
			} else if strings.HasPrefix(name, "vendor/cdnjs/") || strings.HasPrefix(name, "vendor/npm/") ||
				strings.HasPrefix(name, "vendor/npm-scoped/") || strings.HasPrefix(name, "vendor/fonts/") {
				// Downloaded stylesheets must keep the bytes verified by vendor.
				reason = "vendored stylesheets keep their downloaded bytes"
			}
			if ref, ok := newDesignOutsideReference(name, string(data[start:end]), start, end, false, reason); ok {
				outside = append(outside, ref)
			}
			break
		}
	}
	return outside
}

func cssNameByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_' || b == '-' || b >= 0x80
}
