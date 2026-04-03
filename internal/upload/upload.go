package upload

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/copera/copera-cli/internal/api"
)

const (
	// DefaultChunkSize is the default part size for multipart uploads (10 MB).
	DefaultChunkSize int64 = 10 * 1024 * 1024
	// MinChunkSize is the minimum part size allowed by S3 (5 MB).
	MinChunkSize int64 = 5 * 1024 * 1024
	// MaxChunkSize is the maximum part size allowed by S3 (5 GB).
	MaxChunkSize int64 = 5 * 1024 * 1024 * 1024
	// DefaultConcurrency is the default number of concurrent part uploads.
	DefaultConcurrency = 4
	// MaxConcurrency is the upper bound for concurrent part uploads.
	MaxConcurrency = 32
	// maxRetries is the number of retry attempts for transient S3 PUT errors.
	maxRetries = 2
)

// Uploader orchestrates multipart file uploads to S3 via presigned URLs.
type Uploader struct {
	httpClient  *http.Client
	chunkSize   int64
	concurrency int
	progress    Progress
}

// NewUploader creates an Uploader with the given settings.
func NewUploader(httpClient *http.Client, chunkSize int64, concurrency int, progress Progress) *Uploader {
	if chunkSize < MinChunkSize {
		chunkSize = MinChunkSize
	}
	if chunkSize > MaxChunkSize {
		chunkSize = MaxChunkSize
	}
	if concurrency < 1 {
		concurrency = 1
	}
	if concurrency > MaxConcurrency {
		concurrency = MaxConcurrency
	}
	if progress == nil {
		progress = NoopProgress{}
	}
	return &Uploader{
		httpClient:  httpClient,
		chunkSize:   chunkSize,
		concurrency: concurrency,
		progress:    progress,
	}
}

// ChunkSize returns the configured chunk size.
func (u *Uploader) ChunkSize() int64 { return u.chunkSize }

// NumParts calculates the number of parts needed for a file of the given size.
func NumParts(fileSize, chunkSize int64) int {
	if fileSize <= 0 {
		return 1
	}
	n := int(fileSize / chunkSize)
	if fileSize%chunkSize != 0 {
		n++
	}
	return n
}

type partResult struct {
	partNumber int
	eTag       string
	err        error
}

// UploadParts reads the file in chunks and PUTs each chunk to its presigned URL.
// Returns completed parts sorted by part number.
func (u *Uploader) UploadParts(ctx context.Context, filePath string, parts []api.PresignedPart) ([]api.CompletedPart, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat file: %w", err)
	}

	u.progress.Init(fi.Name(), fi.Size())
	defer u.progress.Finish()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	results := make(chan partResult, len(parts))
	sem := make(chan struct{}, u.concurrency)
	var wg sync.WaitGroup

	for _, part := range parts {
		wg.Add(1)
		go func(p api.PresignedPart) {
			defer wg.Done()

			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				results <- partResult{partNumber: p.PartNumber, err: ctx.Err()}
				return
			}

			offset := int64(p.PartNumber-1) * u.chunkSize
			remaining := fi.Size() - offset
			if remaining <= 0 {
				results <- partResult{partNumber: p.PartNumber, err: fmt.Errorf("part %d: offset %d beyond file size %d", p.PartNumber, offset, fi.Size())}
				return
			}
			size := u.chunkSize
			if remaining < size {
				size = remaining
			}

			buf := make([]byte, size)
			n, err := f.ReadAt(buf, offset)
			if err != nil && err != io.EOF {
				results <- partResult{partNumber: p.PartNumber, err: fmt.Errorf("read part %d: %w", p.PartNumber, err)}
				return
			}
			buf = buf[:n]

			eTag, err := u.putPart(ctx, p.SignedURL, buf)
			if err != nil {
				cancel()
				results <- partResult{partNumber: p.PartNumber, err: fmt.Errorf("upload part %d: %w", p.PartNumber, err)}
				return
			}

			u.progress.Add(int64(n))
			results <- partResult{partNumber: p.PartNumber, eTag: eTag}
		}(part)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var completed []api.CompletedPart
	for r := range results {
		if r.err != nil {
			return nil, r.err
		}
		completed = append(completed, api.CompletedPart{
			PartNumber: r.partNumber,
			ETag:       r.eTag,
		})
	}

	sort.Slice(completed, func(i, j int) bool {
		return completed[i].PartNumber < completed[j].PartNumber
	})
	return completed, nil
}

// putPart PUTs a chunk to a presigned S3 URL and returns the ETag.
func (u *Uploader) putPart(ctx context.Context, rawURL string, data []byte) (string, error) {
	if err := ValidatePresignedURL(rawURL); err != nil {
		return "", err
	}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, rawURL, bytes.NewReader(data))
		if err != nil {
			return "", err
		}
		req.ContentLength = int64(len(data))

		resp, err := u.httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			eTag := strings.Trim(resp.Header.Get("ETag"), "\"")
			return eTag, nil
		}
		lastErr = fmt.Errorf("S3 PUT returned %d", resp.StatusCode)
	}
	return "", lastErr
}

// ValidatePresignedURL checks that a presigned URL is safe to use.
// Rejects non-HTTPS URLs and URLs pointing to private/internal hosts.
// In test environments (GO_TEST=1), scheme validation is relaxed for
// httptest servers while still checking for private/internal hosts.
func ValidatePresignedURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid presigned URL: %w", err)
	}
	inTest := os.Getenv("GO_TEST") == "1"
	if u.Scheme != "https" && !inTest {
		return fmt.Errorf("presigned URL must use HTTPS, got %q", u.Scheme)
	}
	host := u.Hostname()
	if ip := net.ParseIP(host); ip != nil {
		// Allow loopback in tests (httptest binds to 127.0.0.1)
		if !inTest && ip.IsLoopback() {
			return fmt.Errorf("presigned URL points to loopback address: %s", host)
		}
		if ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return fmt.Errorf("presigned URL points to private address: %s", host)
		}
	}
	if !inTest && host == "localhost" {
		return fmt.Errorf("presigned URL points to localhost")
	}
	return nil
}
