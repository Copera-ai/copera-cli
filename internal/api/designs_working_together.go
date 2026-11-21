package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type ArtifactCommentReply struct {
	ID         string    `json:"id"`
	AuthorID   string    `json:"authorId"`
	AuthorName *string   `json:"authorName"`
	Body       string    `json:"body"`
	CreatedAt  time.Time `json:"createdAt"`
}

type ArtifactCommentAnchor struct {
	Selector string  `json:"selector"`
	Label    string  `json:"label"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	Width    float64 `json:"width"`
	Height   float64 `json:"height"`
}

type ArtifactCommentThread struct {
	ID            string                 `json:"id"`
	ArtifactID    string                 `json:"artifactId"`
	VersionID     string                 `json:"versionId"`
	VersionNumber int                    `json:"versionNumber"`
	PageID        string                 `json:"pageId"`
	AuthorID      string                 `json:"authorId"`
	AuthorName    *string                `json:"authorName"`
	Anchor        ArtifactCommentAnchor  `json:"anchor"`
	Body          string                 `json:"body"`
	Replies       []ArtifactCommentReply `json:"replies"`
	ResolvedAt    *time.Time             `json:"resolvedAt"`
	ResolvedByID  *string                `json:"resolvedById"`
	CreatedAt     time.Time              `json:"createdAt"`
	UpdatedAt     time.Time              `json:"updatedAt"`
}

type ArtifactCommentListOpts struct {
	VersionID               string
	PageID                  string
	After                   string
	Limit                   int
	OpenFromEarlierVersions *bool
}

type ArtifactCommentList struct {
	VersionID  string                  `json:"versionId"`
	Threads    []ArtifactCommentThread `json:"threads"`
	NextCursor *string                 `json:"nextCursor"`
}

type ArtifactCommentReplyInput struct {
	Body           string `json:"body"`
	IdempotencyKey string `json:"idempotencyKey"`
}

type ArtifactCommentReplyResult struct {
	Reply ArtifactCommentReply `json:"reply"`
}

type ArtifactCommentResolveResult struct {
	Comment ArtifactCommentThread `json:"comment"`
}

type ArtifactCommentPoll struct {
	Cursor  string                 `json:"cursor"`
	Comment *ArtifactCommentThread `json:"comment"`
}

type ArtifactAskInput struct {
	BaseVersionID    string `json:"baseVersionId"`
	Intent           string `json:"intent"`
	IdempotencyKey   string `json:"idempotencyKey"`
	ConfirmAICredits bool   `json:"confirmAiCredits"`
	PageID           string `json:"pageId,omitempty"`
}

type ArtifactAuthoringRequest struct {
	ID                string     `json:"id"`
	ArtifactID        string     `json:"artifactId"`
	PageID            string     `json:"pageId,omitempty"`
	State             string     `json:"state"`
	QueuePosition     int        `json:"queuePosition"`
	BaseVersionID     string     `json:"baseVersionId"`
	ResultVersionID   string     `json:"resultVersionId,omitempty"`
	ChangedPageIDs    []string   `json:"changedPageIds"`
	ErrorCode         string     `json:"errorCode,omitempty"`
	RequestedAt       time.Time  `json:"requestedAt"`
	FinishedAt        *time.Time `json:"finishedAt,omitempty"`
	ViaAgentInstallID string     `json:"viaAgentInstallId,omitempty"`
	ViaAgentName      string     `json:"viaAgentName,omitempty"`
}

type ArtifactAskResult struct {
	Request         ArtifactAuthoringRequest `json:"request"`
	SpendsAICredits bool                     `json:"spendsAiCredits"`
}

func (c *Client) ArtifactComments(ctx context.Context, artifactID string, opts ArtifactCommentListOpts) (*ArtifactCommentList, error) {
	params := pageParams(opts.After, opts.Limit)
	if opts.VersionID != "" {
		params.Set("versionId", opts.VersionID)
	}
	if opts.PageID != "" {
		params.Set("pageId", opts.PageID)
	}
	if opts.OpenFromEarlierVersions != nil {
		params.Set("openFromEarlierVersions", strconv.FormatBool(*opts.OpenFromEarlierVersions))
	}
	var result ArtifactCommentList
	if err := c.commentClient().do(ctx, http.MethodGet, withQuery(artifactPath(artifactID)+"/comments", params), nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) ArtifactCommentReply(ctx context.Context, artifactID, commentID string, input ArtifactCommentReplyInput) (*ArtifactCommentReplyResult, error) {
	var result ArtifactCommentReplyResult
	path := artifactPath(artifactID) + "/comments/" + url.PathEscape(commentID) + "/replies"
	if err := c.commentClient().do(ctx, http.MethodPost, path, input, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) ArtifactCommentResolve(ctx context.Context, artifactID, commentID string) (*ArtifactCommentResolveResult, error) {
	var result ArtifactCommentResolveResult
	path := artifactPath(artifactID) + "/comments/" + url.PathEscape(commentID) + "/resolve"
	if err := c.commentClient().do(ctx, http.MethodPost, path, struct{}{}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) commentClient() *Client {
	// The normal verbose trace prints raw response bodies. Comment bodies are
	// untrusted workspace data and belong in the command's formatted output.
	client := *c
	client.verbose = nil
	return &client
}

// ArtifactCommentWatch polls once. The watch command owns the poll deadline and
// retry delay, so it needs the server response's Retry-After header directly.
func (c *Client) ArtifactCommentWatch(ctx context.Context, artifactID, after string) (*ArtifactCommentPoll, error) {
	path := withQuery(artifactPath(artifactID)+"/comments/watch", pageParams(after, 0))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("api: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if c.verbose != nil {
		fmt.Fprintf(c.verbose, ">> %s %s\n", req.Method, req.URL)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("api: %w", err)
	}
	defer resp.Body.Close()
	if c.verbose != nil {
		fmt.Fprintf(c.verbose, "<< %d %s\n", resp.StatusCode, http.StatusText(resp.StatusCode))
	}

	if resp.StatusCode != http.StatusOK {
		var body struct {
			Code       string `json:"code"`
			Message    string `json:"message"`
			RetryAfter int    `json:"retryAfter"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		if body.Message == "" {
			body.Message = http.StatusText(resp.StatusCode)
		}
		header := resp.Header.Get("Retry-After")
		if seconds, err := strconv.ParseUint(header, 10, 31); err == nil {
			body.RetryAfter = int(seconds)
		} else if at, err := http.ParseTime(header); err == nil {
			body.RetryAfter = int(min(max(0, at.Unix()-time.Now().Unix()), 1<<31-1))
		}
		return nil, &APIError{
			StatusCode: resp.StatusCode, Code: body.Code, Message: body.Message, RetryAfterSeconds: min(max(body.RetryAfter, 0), 1<<31-1),
		}
	}

	var result ArtifactCommentPoll
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("api: decode response: %w", err)
	}
	return &result, nil
}

func (c *Client) ArtifactAsk(ctx context.Context, artifactID string, input ArtifactAskInput) (*ArtifactAskResult, error) {
	var result ArtifactAskResult
	if err := c.do(ctx, http.MethodPost, artifactPath(artifactID)+"/authoring-requests", input, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
