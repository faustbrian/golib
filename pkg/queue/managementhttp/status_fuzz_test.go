package managementhttp

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func FuzzStatusHandlerFailsClosed(f *testing.F) {
	f.Add("current", "25")
	f.Add("", "0")
	f.Add("\x00", "many")

	f.Fuzz(func(t *testing.T, cursor string, limit string) {
		if len(cursor) > 8_192 || len(limit) > 8_192 {
			t.Skip()
		}
		handler, err := NewHandler(HandlerConfig{
			Token: "transport-secret", Status: valueStatusReader{},
		})
		if err != nil {
			t.Fatalf("NewHandler() error = %v", err)
		}
		target := "/v1/status/queues?cursor=" + url.QueryEscape(cursor) +
			"&limit=" + url.QueryEscape(limit)
		request := httptest.NewRequest(http.MethodGet, target, nil)
		request.Header.Set("Authorization", "Bearer transport-secret")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		switch response.Code {
		case http.StatusOK, http.StatusBadRequest:
		default:
			t.Fatalf("response = %d %s", response.Code, response.Body.String())
		}
		if response.Body.Len() > 4_096 {
			t.Fatalf("response body length = %d", response.Body.Len())
		}
	})
}
