package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// BookingHost is a resolved host on a booking.
type BookingHost struct {
	UserID string `json:"userId"`
	Role   string `json:"role"`
}

// Booker holds the booker's contact details.
type Booker struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Phone    string `json:"phone,omitempty"`
	Timezone string `json:"timezone"`
	Locale   string `json:"locale"`
}

// BookingAnswer is a single intake-question answer.
type BookingAnswer struct {
	QuestionID string `json:"questionId"`
	Label      string `json:"label"`
	Value      any    `json:"value"`
}

// BookingLocation is the resolved meeting location.
type BookingLocation struct {
	Type  string `json:"type"`
	Label string `json:"label,omitempty"`
}

// Booking is the shape returned by both the list and detail booking endpoints.
type Booking struct {
	ID                string          `json:"id"`
	Status            string          `json:"status"`
	BookingTypeID     string          `json:"bookingTypeId"`
	BookingTypeTitle  string          `json:"bookingTypeTitle,omitempty"`
	Start             time.Time       `json:"start"`
	End               time.Time       `json:"end"`
	DurationMinutes   int             `json:"durationMinutes"`
	TimezoneAtBooking string          `json:"timezoneAtBooking"`
	Hosts             []BookingHost   `json:"hosts"`
	Booker            Booker          `json:"booker"`
	Guests            []string        `json:"guests"`
	Answers           []BookingAnswer `json:"answers"`
	Location          BookingLocation `json:"location"`
	RecurringID       string          `json:"recurringId,omitempty"`
	CreatedAt         time.Time       `json:"createdAt,omitempty"`
	UpdatedAt         time.Time       `json:"updatedAt,omitempty"`
}

// BookingsPage is the keyset-paginated response for GET /bookings.
type BookingsPage struct {
	Bookings   []Booking `json:"bookings"`
	NextCursor *string   `json:"nextCursor"`
	HasMore    bool      `json:"hasMore"`
}

// BookingListOpts configures a bookings list request.
type BookingListOpts struct {
	Status        string // PENDING, CONFIRMED, DECLINED, CANCELLED, COMPLETED
	BookingTypeID string // filter to a single booking type
	From          string // ISO-8601 lower bound on start
	To            string // ISO-8601 upper bound on start
	Limit         int    // 1–100; 0 = server default (25)
	Cursor        string // opaque keyset cursor from a prior nextCursor
}

// BookingType is the shape returned by the booking-types list endpoint.
type BookingType struct {
	ID              string    `json:"_id"`
	Title           string    `json:"title"`
	Slug            string    `json:"slug,omitempty"`
	Description     string    `json:"description,omitempty"`
	Kind            string    `json:"kind"`
	DurationMinutes int       `json:"durationMinutes"`
	Color           string    `json:"color,omitempty"`
	Hidden          bool      `json:"hidden"`
	Active          bool      `json:"active"`
	CreatedAt       time.Time `json:"createdAt,omitempty"`
	UpdatedAt       time.Time `json:"updatedAt,omitempty"`
}

// BookingTypesPage is the response for GET /booking-types.
type BookingTypesPage struct {
	BookingTypes []BookingType `json:"bookingTypes"`
}

// ── Booking methods ───────────────────────────────────────────────────────────

// BookingList lists the workspace's bookings, newest first, with optional
// filters and keyset pagination.
func (c *Client) BookingList(ctx context.Context, opts BookingListOpts) (*BookingsPage, error) {
	params := url.Values{}
	if opts.Status != "" {
		params.Set("status", opts.Status)
	}
	if opts.BookingTypeID != "" {
		params.Set("bookingTypeId", opts.BookingTypeID)
	}
	if opts.From != "" {
		params.Set("from", opts.From)
	}
	if opts.To != "" {
		params.Set("to", opts.To)
	}
	if opts.Limit > 0 {
		params.Set("limit", fmt.Sprintf("%d", opts.Limit))
	}
	if opts.Cursor != "" {
		params.Set("cursor", opts.Cursor)
	}

	path := "/bookings"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var page BookingsPage
	if err := c.do(ctx, http.MethodGet, path, nil, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

// BookingGet fetches a single booking by id.
func (c *Client) BookingGet(ctx context.Context, id string) (*Booking, error) {
	var b Booking
	if err := c.do(ctx, http.MethodGet, "/bookings/"+id, nil, &b); err != nil {
		return nil, err
	}
	return &b, nil
}

// BookingTypeList lists the workspace's booking types (event types).
func (c *Client) BookingTypeList(ctx context.Context) (*BookingTypesPage, error) {
	var page BookingTypesPage
	if err := c.do(ctx, http.MethodGet, "/booking-types", nil, &page); err != nil {
		return nil, err
	}
	return &page, nil
}
