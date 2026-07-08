package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// MeetingNote is the shape returned by the meeting-notes list and detail
// endpoints. Summary, userNotes, and transcript are only populated by the
// detail (get) endpoint.
type MeetingNote struct {
	ID         string              `json:"_id"`
	Title      string              `json:"title"`
	Status     string              `json:"status"`
	StartedAt  string              `json:"startedAt"`
	EndedAt    string              `json:"endedAt,omitempty"`
	HasSummary bool                `json:"hasSummary"`
	Summary    string              `json:"summary,omitempty"`
	UserNotes  string              `json:"userNotes,omitempty"`
	Transcript []TranscriptSegment `json:"transcript,omitempty"`
}

// TranscriptSegment is one finalized transcript segment.
type TranscriptSegment struct {
	StartMs int64  `json:"startMs"`
	EndMs   int64  `json:"endMs"`
	Text    string `json:"text"`
	Speaker string `json:"speaker,omitempty"`
}

// MeetingNotesPage is the response for GET /meeting-notes.
type MeetingNotesPage struct {
	Notes []MeetingNote `json:"notes"`
}

// MeetingNoteListOpts configures a meeting-notes list request.
type MeetingNoteListOpts struct {
	Query  string // case-insensitive search over titles and notes
	Limit  int    // 1–100; 0 = server default (30)
	Before string // ISO date — page back in time
}

// ── Meeting-note methods ──────────────────────────────────────────────────────

// MeetingNoteList lists meeting notes the token user owns or is shared into,
// newest first.
func (c *Client) MeetingNoteList(ctx context.Context, opts MeetingNoteListOpts) (*MeetingNotesPage, error) {
	params := url.Values{}
	if opts.Query != "" {
		params.Set("q", opts.Query)
	}
	if opts.Limit > 0 {
		params.Set("limit", fmt.Sprintf("%d", opts.Limit))
	}
	if opts.Before != "" {
		params.Set("before", opts.Before)
	}

	path := "/meeting-notes"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var page MeetingNotesPage
	if err := c.do(ctx, http.MethodGet, path, nil, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

// MeetingNoteGet fetches a single meeting note's summary and, when
// includeTranscript is true, its finalized transcript segments.
func (c *Client) MeetingNoteGet(ctx context.Context, noteID string, includeTranscript bool) (*MeetingNote, error) {
	path := "/meeting-notes/" + noteID
	if includeTranscript {
		path += "?includeTranscript=true"
	}

	var note MeetingNote
	if err := c.do(ctx, http.MethodGet, path, nil, &note); err != nil {
		return nil, err
	}
	return &note, nil
}
