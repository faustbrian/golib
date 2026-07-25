package ui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEmbeddedConsoleServesImmutableSecureAssets(t *testing.T) {
	t.Parallel()

	handler := NewHandler()
	tests := map[string]struct {
		contentType string
		contains    string
	}{
		"/":           {contentType: "text/html", contains: "Queue control plane"},
		"/app.js":     {contentType: "text/javascript", contains: "X-Queue-Control-Key-ID"},
		"/styles.css": {contentType: "text/css", contains: "--surface"},
	}
	for path, expected := range tests {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		result := response.Result()
		body, err := io.ReadAll(result.Body)
		_ = result.Body.Close()
		if err != nil || result.StatusCode != http.StatusOK ||
			!strings.HasPrefix(result.Header.Get("Content-Type"), expected.contentType) ||
			!strings.Contains(string(body), expected.contains) {
			t.Fatalf("GET %s = status %d type %q body %q error %v", path, result.StatusCode, result.Header.Get("Content-Type"), body, err)
		}
		if result.Header.Get("Cache-Control") != "no-store" ||
			!strings.Contains(result.Header.Get("Content-Security-Policy"), "connect-src 'self'") ||
			result.Header.Get("X-Frame-Options") != "DENY" {
			t.Fatalf("GET %s security headers = %v", path, result.Header)
		}
	}
}
