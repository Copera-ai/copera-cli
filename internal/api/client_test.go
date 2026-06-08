package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testClient(handler http.HandlerFunc) (*Client, func()) {
	srv := httptest.NewServer(handler)
	return New(srv.URL, "tok_test", 0), srv.Close
}

func TestClientDecodesAcceptedResponseBody(t *testing.T) {
	client, cleanup := testClient(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/chat/direct-message/send-message", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"channelId":"chan_dm","queued":true}`))
	})
	defer cleanup()

	result, err := client.SendDirectMessage(context.Background(), &DirectMessageInput{
		UserID:  "user1",
		Message: "Hello",
	})

	require.NoError(t, err)
	assert.Equal(t, "chan_dm", result.ChannelID)
	assert.True(t, result.Queued)
}

func TestClientParsesMessageErrorBody(t *testing.T) {
	client, cleanup := testClient(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"No channel access","code":"FORBIDDEN"}`))
	})
	defer cleanup()

	_, err := client.ChannelList(context.Background(), nil)

	var apiErr *APIError
	require.True(t, errors.As(err, &apiErr))
	assert.Equal(t, http.StatusForbidden, apiErr.StatusCode)
	assert.Equal(t, "FORBIDDEN", apiErr.Code)
	assert.Equal(t, "No channel access", apiErr.Message)
}

func TestChannelListQueryAndDirectMessageBody(t *testing.T) {
	var listQuery url.Values
	var dmBody string
	client, cleanup := testClient(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/chat/channels":
			listQuery = r.URL.Query()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"channels":[],"total":0,"limit":25,"offset":5}`))
		case "/chat/direct-message/send-message":
			buf, _ := io.ReadAll(r.Body)
			dmBody = string(buf)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"channelId":"dm1","queued":true}`))
		default:
			http.NotFound(w, r)
		}
	})
	defer cleanup()

	page, err := client.ChannelList(context.Background(), &ChannelListOptions{
		Query:         "deploy",
		Type:          "text",
		Kind:          "dm",
		ParticipantID: "user1",
		Limit:         25,
		Offset:        5,
	})
	require.NoError(t, err)
	assert.Equal(t, 25, page.Limit)
	assert.Equal(t, "deploy", listQuery.Get("q"))
	assert.Equal(t, "text", listQuery.Get("type"))
	assert.Equal(t, "dm", listQuery.Get("kind"))
	assert.Equal(t, "user1", listQuery.Get("participantId"))
	assert.Equal(t, "25", listQuery.Get("limit"))
	assert.Equal(t, "5", listQuery.Get("offset"))

	result, err := client.SendDirectMessage(context.Background(), &DirectMessageInput{
		UserID:  "user1",
		Message: "Hello",
	})
	require.NoError(t, err)
	assert.Equal(t, "dm1", result.ChannelID)
	assert.JSONEq(t, `{"userId":"user1","message":"Hello"}`, dmBody)
}

func TestBoardTableAndRowListQueries(t *testing.T) {
	var boardQuery, tableQuery, rowQuery url.Values
	client, cleanup := testClient(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/board/list-boards":
			boardQuery = r.URL.Query()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
		case "/board/board1/tables":
			tableQuery = r.URL.Query()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
		case "/board/board1/table/table1/rows":
			rowQuery = r.URL.Query()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
		default:
			http.NotFound(w, r)
		}
	})
	defer cleanup()

	_, err := client.BoardList(context.Background(), &BoardListOptions{Query: "roadmap"})
	require.NoError(t, err)
	_, err = client.TableList(context.Background(), "board1", &TableListOptions{Query: "tasks"})
	require.NoError(t, err)
	_, err = client.RowList(context.Background(), "board1", "table1", &RowListOptions{
		Query:  "oauth",
		Filter: `{"match":"and","conditions":[]}`,
		Sort:   "col_a:asc",
	})
	require.NoError(t, err)

	assert.Equal(t, "roadmap", boardQuery.Get("q"))
	assert.Equal(t, "tasks", tableQuery.Get("q"))
	assert.Equal(t, "oauth", rowQuery.Get("q"))
	assert.Equal(t, `{"match":"and","conditions":[]}`, rowQuery.Get("filter"))
	assert.Equal(t, "col_a:asc", rowQuery.Get("sort"))
}
