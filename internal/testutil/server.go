package testutil

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// NewMockServer starts an httptest.Server with handler and registers a cleanup
// to close it when the test ends. Pass the returned server's URL to
// config/client as the API base URL.
func NewMockServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

// MockRoutes is a simple path→handler map for building test servers.
// Unregistered paths return 404.
type MockRoutes map[string]http.HandlerFunc

// Handler returns an http.Handler that dispatches by request path.
func (m MockRoutes) Handler() http.Handler {
	mux := http.NewServeMux()
	for path, handler := range m {
		mux.HandleFunc(path, handler)
	}
	return mux
}

// RespondJSON writes v as JSON with the given status code.
func RespondJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// RespondError writes a Copera-style error response.
func RespondError(w http.ResponseWriter, status int, code, message string) {
	RespondJSON(w, status, map[string]string{
		"error": message,
		"code":  code,
	})
}
