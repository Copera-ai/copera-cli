package api

import (
	"context"
	"errors"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"regexp"
	"time"
)

type MediaInput struct {
	Kind     string `json:"kind"`
	FileID   string `json:"fileId,omitempty"`
	DesignID string `json:"designId,omitempty"`
	AssetID  string `json:"assetId,omitempty"`
}

type MediaSize struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

type MediaImageInput struct {
	Prompt      string       `json:"prompt"`
	AspectRatio string       `json:"aspectRatio,omitempty"`
	Size        *MediaSize   `json:"size,omitempty"`
	Count       int          `json:"count,omitempty"`
	References  []MediaInput `json:"references,omitempty"`
	Transparent bool         `json:"transparent,omitempty"`
	Quality     string       `json:"quality,omitempty"`
	Design      string       `json:"design,omitempty"`
}

type MediaSpeechInput struct {
	Text     string `json:"text"`
	Language string `json:"language"`
	Voice    string `json:"voice,omitempty"`
	Format   string `json:"format,omitempty"`
	Design   string `json:"design,omitempty"`
}

type MediaTranscriptionInput struct {
	Source MediaInput `json:"source"`
	Design string     `json:"design,omitempty"`
}

type MediaAsset struct {
	ID            string `json:"id"`
	Path          string `json:"path"`
	MediaType     string `json:"mediaType"`
	ByteLength    int64  `json:"byteLength"`
	ContentDigest string `json:"contentDigest"`
}

type MediaDownload struct {
	URL       string `json:"url"`
	ExpiresAt string `json:"expiresAt"`
}

type MediaFile struct {
	DriveFileID string         `json:"driveFileId"`
	FileName    string         `json:"fileName"`
	MediaType   string         `json:"mediaType"`
	ByteLength  int64          `json:"byteLength"`
	SHA256      string         `json:"sha256"`
	Download    *MediaDownload `json:"download"`
	Asset       *MediaAsset    `json:"asset"`
}

type MediaTranscriptSegment struct {
	StartMs int     `json:"startMs"`
	EndMs   int     `json:"endMs"`
	Speaker *string `json:"speaker"`
	Text    string  `json:"text"`
}

type MediaTranscript struct {
	DurationMs int                      `json:"durationMs"`
	Text       string                   `json:"text"`
	Segments   []MediaTranscriptSegment `json:"segments"`
	VTT        string                   `json:"vtt"`
}

type MediaSpend struct {
	ReservedCents int `json:"reservedCents"`
	AssessedCents int `json:"assessedCents"`
	ChargedCents  int `json:"chargedCents"`
}

type MediaFailure struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type MediaReceipt struct {
	OperationID       string           `json:"operationId"`
	Kind              string           `json:"kind"`
	Status            string           `json:"status"`
	Design            *string          `json:"design"`
	RequestedCount    int              `json:"requestedCount"`
	RetryAfterSeconds *int             `json:"retryAfterSeconds"`
	CreatedAt         string           `json:"createdAt"`
	ExpiresAt         *string          `json:"expiresAt"`
	Spend             MediaSpend       `json:"spend"`
	Files             []MediaFile      `json:"files"`
	Transcript        *MediaTranscript `json:"transcript"`
	Error             *MediaFailure    `json:"error"`
}

func (r MediaReceipt) Active() bool { return r.Status == "queued" || r.Status == "running" }

type MediaVoice struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Languages  []string `json:"languages"`
	DefaultFor []string `json:"defaultFor"`
	Formats    []string `json:"formats"`
}

type MediaVoices struct {
	Voices []MediaVoice `json:"voices"`
}

type MediaSpendPeriod struct {
	LimitCents    int `json:"limitCents"`
	SpentCents    int `json:"spentCents"`
	ReservedCents int `json:"reservedCents"`
}

type MediaDailySpend struct {
	MediaSpendPeriod
	ResetsAt string `json:"resetsAt"`
}

type MediaSpendMeter struct {
	Daily MediaDailySpend  `json:"daily"`
	Total MediaSpendPeriod `json:"total"`
}

var mediaOperationIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{24}$`)

type mediaTransportError struct{}

func (mediaTransportError) Error() string { return "api: media request connection lost" }

func (c *Client) mediaJSON(ctx context.Context, method, path, key string, body, result any) error {
	for attempt := 0; ; attempt++ {
		err := c.doRequest(ctx, method, path, body, result, 0, &mediaRequest{key: key})
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}

		var apiErr *APIError
		var transportErr mediaTransportError
		backoff := time.Second << attempt
		switch {
		case errors.As(err, &apiErr) &&
			(apiErr.StatusCode == http.StatusTooManyRequests || apiErr.StatusCode == http.StatusServiceUnavailable):
			backoff = max(backoff, time.Duration(apiErr.RetryAfterSeconds)*time.Second)
		case (key != "" || method == http.MethodGet) && errors.As(err, &transportErr):
		default:
			return err
		}
		if attempt >= 2 || backoff > 10*time.Second {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
	}
}

func (c *Client) mediaOperation(ctx context.Context, method, path, key string, body any) (*MediaReceipt, error) {
	var receipt MediaReceipt
	if err := c.mediaJSON(ctx, method, path, key, body, &receipt); err != nil {
		return nil, err
	}
	return &receipt, nil
}

func (c *Client) MediaImage(ctx context.Context, key string, input MediaImageInput) (*MediaReceipt, error) {
	return c.mediaOperation(ctx, http.MethodPost, "/media/images", key, input)
}

func (c *Client) MediaSpeech(ctx context.Context, key string, input MediaSpeechInput) (*MediaReceipt, error) {
	return c.mediaOperation(ctx, http.MethodPost, "/media/speech", key, input)
}

func (c *Client) MediaTranscribe(ctx context.Context, key string, input MediaTranscriptionInput) (*MediaReceipt, error) {
	return c.mediaOperation(ctx, http.MethodPost, "/media/transcriptions", key, input)
}

func (c *Client) MediaTranscribeFile(ctx context.Context, key, design, name, mediaType string, file ArtifactUpload) (*MediaReceipt, error) {
	form := newMultipartRequest(func(writer *multipart.Writer) error {
		if design != "" {
			if err := writer.WriteField("design", design); err != nil {
				return err
			}
		}
		part, err := writer.CreatePart(textproto.MIMEHeader{
			"Content-Disposition": {multipart.FileContentDisposition("file", name)},
			"Content-Type":        {mediaType},
		})
		if err != nil {
			return err
		}
		return file.CopyTo(part)
	})
	return c.mediaOperation(ctx, http.MethodPost, "/media/transcriptions", key, form)
}

func (c *Client) MediaStatus(ctx context.Context, id string) (*MediaReceipt, error) {
	// Status is a single read; submission polling can repeat it until terminal.
	var receipt MediaReceipt
	if err := c.doRequest(ctx, http.MethodGet, "/media/operations/"+id, nil, &receipt, 0, &mediaRequest{}); err != nil {
		return nil, err
	}
	return &receipt, nil
}

func (c *Client) PollMediaStatus(ctx context.Context, id string) (*MediaReceipt, error) {
	return c.mediaOperation(ctx, http.MethodGet, "/media/operations/"+id, "", nil)
}

func (c *Client) MediaVoices(ctx context.Context) (*MediaVoices, error) {
	var result MediaVoices
	if err := c.mediaJSON(ctx, http.MethodGet, "/media/voices", "", nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) MediaUsage(ctx context.Context) (*MediaSpendMeter, error) {
	var result MediaSpendMeter
	if err := c.mediaJSON(ctx, http.MethodGet, "/media/usage", "", nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
