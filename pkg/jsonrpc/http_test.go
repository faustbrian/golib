package jsonrpc

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPHandlerRequestAndNotification(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	_ = registry.Register("ping", func(context.Context, json.RawMessage) (any, error) {
		return "pong", nil
	})
	handler := NewHTTPHandler(NewDispatcher(registry))

	request := httptest.NewRequest(http.MethodPost, "/rpc", strings.NewReader(
		`{"jsonrpc":"2.0","method":"ping","id":1}`,
	))
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}
	assertJSONEqual(t, recorder.Body.Bytes(), []byte(`{"jsonrpc":"2.0","result":"pong","id":1}`))

	notification := httptest.NewRequest(http.MethodPost, "/rpc", strings.NewReader(
		`{"jsonrpc":"2.0","method":"ping"}`,
	))
	notification.Header.Set("Content-Type", "application/json")
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, notification)
	if recorder.Code != http.StatusNoContent || recorder.Body.Len() != 0 {
		t.Errorf("notification response = status %d body %q", recorder.Code, recorder.Body.String())
	}
}

func TestHTTPHandlerTransportErrors(t *testing.T) {
	t.Parallel()

	handler := NewHTTPHandler(NewDispatcher(nil), WithMaxRequestBytes(16))
	tests := []struct {
		name        string
		method      string
		contentType string
		body        string
		status      int
	}{
		{name: "method", method: http.MethodGet, contentType: "application/json", status: http.StatusMethodNotAllowed},
		{name: "missing content type", method: http.MethodPost, status: http.StatusUnsupportedMediaType},
		{name: "unsupported content type", method: http.MethodPost, contentType: "text/plain", status: http.StatusUnsupportedMediaType},
		{name: "oversized", method: http.MethodPost, contentType: "application/json", body: strings.Repeat("x", 17), status: http.StatusRequestEntityTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			request := httptest.NewRequest(tt.method, "/rpc", strings.NewReader(tt.body))
			if tt.contentType != "" {
				request.Header.Set("Content-Type", tt.contentType)
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != tt.status {
				t.Errorf("status = %d, want %d", recorder.Code, tt.status)
			}
			if tt.status == http.StatusMethodNotAllowed && recorder.Header().Get("Allow") != http.MethodPost {
				t.Errorf("Allow = %q", recorder.Header().Get("Allow"))
			}
		})
	}
}

func TestHTTPHandlerReadError(t *testing.T) {
	t.Parallel()

	handler := NewHTTPHandler(NewDispatcher(nil))
	request := httptest.NewRequest(http.MethodPost, "/rpc", nil)
	request.Header.Set("Content-Type", "application/json")
	request.Body = errorReader{}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestHTTPHandlerReadsChunkedBodyAndClosesIt(t *testing.T) {
	t.Parallel()

	body := &oneByteBody{data: []byte(`{"jsonrpc":"2.0","method":"missing","id":1}`)}
	request := httptest.NewRequest(http.MethodPost, "/rpc", nil)
	request.Header.Set("Content-Type", "application/json")
	request.Body = body
	recorder := httptest.NewRecorder()
	NewHTTPHandler(NewDispatcher(nil)).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if !body.closed {
		t.Fatal("request body was not closed")
	}
}

func TestJSONContentTypes(t *testing.T) {
	t.Parallel()

	valid := []string{
		"application/json",
		"application/json; charset=utf-8",
		"application/json-rpc",
		"application/vnd.example+json",
	}
	for _, value := range valid {
		if !IsJSONContentType(value) {
			t.Errorf("IsJSONContentType(%q) = false", value)
		}
	}
	for _, value := range []string{"", "text/plain", "application/xml", "invalid"} {
		if IsJSONContentType(value) {
			t.Errorf("IsJSONContentType(%q) = true", value)
		}
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (errorReader) Close() error             { return nil }

type oneByteBody struct {
	data   []byte
	closed bool
}

func (body *oneByteBody) Read(destination []byte) (int, error) {
	if len(body.data) == 0 {
		return 0, io.EOF
	}
	destination[0] = body.data[0]
	body.data = body.data[1:]
	return 1, nil
}

func (body *oneByteBody) Close() error {
	body.closed = true
	return nil
}
