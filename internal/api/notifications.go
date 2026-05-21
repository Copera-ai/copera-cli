package api

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// Notification represents a single workspace notification owned by the PAT user.
type Notification struct {
	ID             string         `json:"_id"`
	Type           string         `json:"type"`
	Owner          string         `json:"owner"`
	Workspace      string         `json:"workspace"`
	Data           map[string]any `json:"data"`
	Status         string         `json:"status"` // read | unread
	Sender         string         `json:"sender,omitempty"`
	ReadAt         time.Time      `json:"readAt,omitempty"`
	CreatedAt      time.Time      `json:"createdAt,omitempty"`
	UpdatedAt      time.Time      `json:"updatedAt,omitempty"`
	GroupCount     int            `json:"groupCount,omitempty"`
	GroupStartedAt time.Time      `json:"groupStartedAt,omitempty"`
	GroupSenderIDs []string       `json:"groupSenderIds,omitempty"`
}

// NotificationsPage is the response shape for GET /notifications.
type NotificationsPage struct {
	Notifications []Notification `json:"notifications"`
	UnreadCount   int            `json:"unreadCount"`
	Count         int            `json:"count"`
}

// UpdateNotificationStatusInput is the body for PATCH /notifications/{id}.
type UpdateNotificationStatusInput struct {
	Status string `json:"status"` // read | unread
}

// ── Notification methods ────────────────────────────────────────────────────

// NotificationList fetches notifications owned by the PAT user.
// after / before are notification IDs used as cursors (24-char ObjectId).
func (c *Client) NotificationList(ctx context.Context, after, before string) (*NotificationsPage, error) {
	params := url.Values{}
	if after != "" {
		params.Set("after", after)
	}
	if before != "" {
		params.Set("before", before)
	}

	path := "/notifications/"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var page NotificationsPage
	if err := c.do(ctx, http.MethodGet, path, nil, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

// NotificationUpdateStatus marks a notification as read or unread.
func (c *Client) NotificationUpdateStatus(ctx context.Context, id, status string) (*Notification, error) {
	var n Notification
	path := "/notifications/" + id
	if err := c.do(ctx, http.MethodPatch, path, &UpdateNotificationStatusInput{Status: status}, &n); err != nil {
		return nil, err
	}
	return &n, nil
}

// NotificationDelete deletes a notification owned by the PAT user.
func (c *Client) NotificationDelete(ctx context.Context, id string) error {
	path := "/notifications/" + id
	return c.do(ctx, http.MethodDelete, path, nil, nil)
}
