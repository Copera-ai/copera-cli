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

// ChannelCreateInput is the body for POST /chat/channel.
type ChannelCreateInput struct {
	Name               string   `json:"name"`
	Description        string   `json:"description,omitempty"`
	ParticipantUserIDs []string `json:"participantUserIds,omitempty"`
	Type               string   `json:"type,omitempty"`
}

// CreatedChannel is returned after a channel is created.
type CreatedChannel struct {
	ID                 string   `json:"_id"`
	Name               string   `json:"name"`
	ParticipantUserIDs []string `json:"participantUserIds"`
}

// ChannelParticipantsInput is the body for POST /chat/channel/{id}/participants.
type ChannelParticipantsInput struct {
	ParticipantUserIDs []string `json:"participantUserIds"`
}

// ChannelParticipants is returned after participants are added to a channel.
type ChannelParticipants struct {
	ChannelID          string   `json:"channelId"`
	ParticipantUserIDs []string `json:"participantUserIds"`
}

// SuccessResult is the response for participant removal and archive/unarchive.
type SuccessResult struct {
	Success bool `json:"success"`
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

// ChannelCreate creates a new text group channel.
func (c *Client) ChannelCreate(ctx context.Context, input *ChannelCreateInput) (*CreatedChannel, error) {
	var result CreatedChannel
	if err := c.do(ctx, "POST", "/chat/channel", input, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ChannelAddParticipants adds workspace users to a channel.
func (c *Client) ChannelAddParticipants(ctx context.Context, channelID string, input *ChannelParticipantsInput) (*ChannelParticipants, error) {
	path := fmt.Sprintf("/chat/channel/%s/participants", channelID)
	var result ChannelParticipants
	if err := c.do(ctx, "POST", path, input, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ChannelRemoveParticipant removes a single participant from a channel.
func (c *Client) ChannelRemoveParticipant(ctx context.Context, channelID, participantUserID string) (*SuccessResult, error) {
	path := fmt.Sprintf("/chat/channel/%s/participant/%s", channelID, participantUserID)
	var result SuccessResult
	if err := c.do(ctx, "DELETE", path, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ChannelArchive archives a channel.
func (c *Client) ChannelArchive(ctx context.Context, channelID string) (*SuccessResult, error) {
	path := fmt.Sprintf("/chat/channel/%s/archive", channelID)
	var result SuccessResult
	if err := c.do(ctx, "POST", path, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ChannelUnarchive restores an archived channel.
func (c *Client) ChannelUnarchive(ctx context.Context, channelID string) (*SuccessResult, error) {
	path := fmt.Sprintf("/chat/channel/%s/unarchive", channelID)
	var result SuccessResult
	if err := c.do(ctx, "POST", path, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// SendDirectMessage sends a text message to a workspace user by user ID.
func (c *Client) SendDirectMessage(ctx context.Context, input *DirectMessageInput) (*DirectMessageResult, error) {
	var result DirectMessageResult
	if err := c.do(ctx, "POST", "/chat/direct-message/send-message", input, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
