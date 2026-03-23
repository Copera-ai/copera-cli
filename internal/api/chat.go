package api

import (
	"context"
	"fmt"
)

// SendMessageInput is the body for POST /chat/channel/{channelId}/send-message.
type SendMessageInput struct {
	Message string `json:"message"`
	Name    string `json:"name,omitempty"`
}

// SendMessage sends a text message to a channel. Returns nil on success (204 No Content).
func (c *Client) SendMessage(ctx context.Context, channelID string, input *SendMessageInput) error {
	path := fmt.Sprintf("/chat/channel/%s/send-message", channelID)
	return c.do(ctx, "POST", path, input, nil)
}
