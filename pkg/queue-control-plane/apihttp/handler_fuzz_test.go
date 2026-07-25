package apihttp

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func FuzzCommandHandlerFailsClosed(f *testing.F) {
	f.Add(`{"idempotency_key":"request-1"}`)
	f.Add(`{"actor":"spoofed"}`)
	f.Add("not-json")

	f.Fuzz(func(t *testing.T, body string) {
		if int64(len(body)) > 2*defaultMaxRequestBytes {
			t.Skip()
		}
		handler, err := NewHandler(Config{
			Commands:        &commandExecutorStub{},
			MaxRequestBytes: defaultMaxRequestBytes,
		})
		if err != nil {
			t.Fatalf("NewHandler() error = %v", err)
		}
		request := authenticatedRequest(
			t,
			http.MethodPost,
			"/v1/tenants/tenant-1/commands",
			body,
		)
		response := httptest.NewRecorder()

		handler.ServeHTTP(response, request)
		switch response.Code {
		case http.StatusOK,
			http.StatusBadRequest,
			http.StatusRequestEntityTooLarge:
		default:
			t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
		}
		if response.Body.Len() > 4*1024 {
			t.Fatalf("response body length = %d, want bounded", response.Body.Len())
		}
	})
}

func FuzzRecordHandlerFailsClosed(f *testing.F) {
	f.Add("critical", "queue", "revealed")
	f.Add("", "payload", "raw")
	f.Add("\x00", "occurred_at", "hidden")

	f.Fuzz(func(t *testing.T, search string, sort string, payload string) {
		if int64(len(search)) > 2*defaultMaxRequestBytes ||
			int64(len(sort)) > 2*defaultMaxRequestBytes ||
			int64(len(payload)) > 2*defaultMaxRequestBytes {
			t.Skip()
		}
		handler, err := NewHandler(Config{
			Commands: &commandExecutorStub{}, Records: &recordSourceStub{}, Viewer: &recordViewerStub{},
			SensitiveAudit: &sensitiveAuditStub{},
		})
		if err != nil {
			t.Fatalf("NewHandler() error = %v", err)
		}
		targets := []string{
			"/v1/tenants/tenant-1/failures?search=" + url.QueryEscape(search) +
				"&sort=" + url.QueryEscape(sort),
			"/v1/tenants/tenant-1/failures/failure-1?payload=" + url.QueryEscape(payload),
		}
		for _, target := range targets {
			response := httptest.NewRecorder()
			handler.ServeHTTP(
				response,
				authenticatedRequest(t, http.MethodGet, target, ""),
			)
			switch response.Code {
			case http.StatusOK, http.StatusBadRequest:
			default:
				t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
			}
			if response.Body.Len() > 4*1024 {
				t.Fatalf("response body length = %d, want bounded", response.Body.Len())
			}
		}
	})
}

func FuzzQueueHandlerFailsClosed(f *testing.F) {
	f.Add("current", "25")
	f.Add("", "0")
	f.Add("\x00", "many")

	f.Fuzz(func(t *testing.T, cursor string, limit string) {
		if int64(len(cursor)) > 2*defaultMaxRequestBytes ||
			int64(len(limit)) > 2*defaultMaxRequestBytes {
			t.Skip()
		}
		handler, err := NewHandler(Config{
			Commands: &commandExecutorStub{}, Queues: &queueSourceStub{}, Viewer: &viewerStub{},
		})
		if err != nil {
			t.Fatalf("NewHandler() error = %v", err)
		}
		target := "/v1/tenants/tenant-1/queues?cursor=" + url.QueryEscape(cursor) +
			"&limit=" + url.QueryEscape(limit)
		response := httptest.NewRecorder()
		handler.ServeHTTP(
			response,
			authenticatedRequest(t, http.MethodGet, target, ""),
		)
		switch response.Code {
		case http.StatusOK, http.StatusBadRequest:
		default:
			t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
		}
		if response.Body.Len() > 4*1024 {
			t.Fatalf("response body length = %d, want bounded", response.Body.Len())
		}
	})
}
