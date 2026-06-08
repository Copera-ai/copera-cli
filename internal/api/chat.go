package api

import (
	"context"
	"fmt"
	"net/url"
	"time"
)

// Channel represents a Copera chat channel visible to the token user.
type Channel struct {
	ID                 string     `json:"_id"`
	Name               string     `json:"name"`
	Description        string     `json:"description,omitempty"`
	Picture            string     `json:"picture,omitempty"`
	Type               string     `json:"type"`
	Kind               string     `json:"kind"`
	ParticipantUserIDs []string   `json:"participantUserIds"`
	ParticipantTeamIDs []string   `json:"participantTeamIds"`
	CreatedBy          string     `json:"createdBy"`
	CreatedAt          time.Time  `json:"createdAt"`
	UpdatedAt          time.Time  `json:"updatedAt"`
	LastMessageAt      *time.Time `json:"lastMessageAt,omitempty"`
}

// ChannelListOptions configures GET /chat/channels.
type ChannelListOptions struct {
	Query         string
	Type          string
	Kind          string
	ParticipantID string
	Limit         int
	Offset        int
}

// ChannelListPage is the paginated response for channel discovery.
type ChannelListPage struct {
	Channels []Channel `json:"channels"`
	Total    int       `json:"total"`
	Limit    int       `json:"limit"`
	Offset   int       `json:"offset"`
}

// SendMessageInput is the body for POST /chat/channel/{channelId}/send-message.
type SendMessageInput struct {
	Message string `json:"message"`
	Name    string `json:"name,omitempty"`
}

// DirectMessageInput is the body for POST /chat/direct-message/send-message.
type DirectMessageInput struct {
	UserID  string `json:"userId"`
	Message string `json:"message"`
}

// DirectMessageResult is returned after a direct message has been queued.
type DirectMessageResult struct {
	ChannelID string `json:"channelId"`
	Queued    bool   `json:"queued"`
}

// ChannelList lists channels and DMs visible to the token user.
func (c *Client) ChannelList(ctx context.Context, opts *ChannelListOptions) (*ChannelListPage, error) {
	path := "/chat/channels"
	if opts != nil {
		q := url.Values{}
		if opts.Query != "" {
			q.Set("q", opts.Query)
		}
		if opts.Type != "" {
			q.Set("type", opts.Type)
		}
		if opts.Kind != "" {
			q.Set("kind", opts.Kind)
		}
		if opts.ParticipantID != "" {
			q.Set("participantId", opts.ParticipantID)
		}
		if opts.Limit > 0 {
			q.Set("limit", fmt.Sprintf("%d", opts.Limit))
		}
		if opts.Offset > 0 {
			q.Set("offset", fmt.Sprintf("%d", opts.Offset))
		}
		if encoded := q.Encode(); encoded != "" {
			path += "?" + encoded
		}
	}

	var page ChannelListPage
	if err := c.do(ctx, "GET", path, nil, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

// SendMessage sends a text message to a channel. Returns nil on success (204 No Content).
func (c *Client) SendMessage(ctx context.Context, channelID string, input *SendMessageInput) error {
	path := fmt.Sprintf("/chat/channel/%s/send-message", channelID)
	return c.do(ctx, "POST", path, input, nil)
}

// SendDirectMessage sends a text message to a workspace user by user ID.
func (c *Client) SendDirectMessage(ctx context.Context, input *DirectMessageInput) (*DirectMessageResult, error) {
	var result DirectMessageResult
	if err := c.do(ctx, "POST", "/chat/direct-message/send-message", input, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
