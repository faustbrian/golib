package middleware_test

import (
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	middleware "github.com/faustbrian/golib/pkg/http-middleware"
	"github.com/faustbrian/golib/pkg/http-middleware/bodylimit"
	compressmw "github.com/faustbrian/golib/pkg/http-middleware/compress"
	"github.com/faustbrian/golib/pkg/http-middleware/content"
	"github.com/faustbrian/golib/pkg/http-middleware/cors"
	"github.com/faustbrian/golib/pkg/http-middleware/observe"
	"github.com/faustbrian/golib/pkg/http-middleware/recovery"
	"github.com/faustbrian/golib/pkg/http-middleware/responsepolicy"
	"github.com/faustbrian/golib/pkg/http-middleware/secureheader"
)

func TestRepresentativeJSONRPCProfile(t *testing.T) {
	t.Parallel()
	recoverer, _ := recovery.New(recovery.Policy{})
	limit, _ := bodylimit.New(bodylimit.Policy{MaxBytes: 1024})
	media, _ := content.New(content.Policy{
		RequestTypes:  []string{"application/json"},
		ResponseTypes: []string{"application/json"},
	})
	compressor, _ := compressmw.New(compressmw.Policy{MinimumBytes: 1})
	chain, _ := middleware.New(recoverer, limit, media, compressor)
	handler, err := chain.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload, err := io.ReadAll(r.Body)
		if err != nil || !strings.Contains(string(payload), `"method":"ping"`) {
			t.Fatalf("payload = %q, error = %v", payload, err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","result":"pong","id":1}`)
	}))
	if err != nil || handler == nil {
		t.Fatalf("Handler() = %v, %v", handler, err)
		return
	}
	request := httptest.NewRequest(http.MethodPost, "/rpc", strings.NewReader(`{"jsonrpc":"2.0","method":"ping","id":1}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Accept-Encoding", "gzip")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	reader, err := gzip.NewReader(recorder.Body)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	_ = reader.Close()
	if recorder.Code != http.StatusOK || !strings.Contains(string(payload), `"result":"pong"`) {
		t.Fatalf("response = %d %q", recorder.Code, payload)
	}
}

func TestRepresentativeWebhookPreservesRawSignedBody(t *testing.T) {
	t.Parallel()
	raw := "timestamp=1&payload=%7B%22id%22%3A42%7D"
	limit, _ := bodylimit.New(bodylimit.Policy{MaxBytes: int64(len(raw))})
	handler := limit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload, err := io.ReadAll(r.Body)
		if err != nil || string(payload) != raw {
			t.Fatalf("payload = %q, error = %v", payload, err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(raw))
	request.Header.Set("X-Signature", "application-owned")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent || recorder.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("response = %d %v", recorder.Code, recorder.Header())
	}
}

func TestRepresentativeRouterHealthAndAdministrativeProfiles(t *testing.T) {
	t.Parallel()
	var events []observe.Event
	observer, _ := observe.New(observe.Policy{
		Route: func(request *http.Request) string { return request.Pattern },
		Observer: func(_ context.Context, event observe.Event) {
			events = append(events, event)
		},
	})
	recoverer, _ := recovery.New(recovery.Policy{})
	headers, _ := secureheader.New(secureheader.APIDefaults())
	crossOrigin, _ := cors.New(cors.Policy{AllowedOrigins: []string{"https://admin.example"}})
	chain, _ := middleware.New(recoverer, observer, crossOrigin, headers, responsepolicy.NoStore())
	router := http.NewServeMux()
	router.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	router.HandleFunc("GET /admin/records/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"visible":true}`)
	})
	handler, err := chain.Handler(router)
	if err != nil || handler == nil {
		t.Fatalf("Handler() = %v, %v", handler, err)
		return
	}

	health := httptest.NewRecorder()
	handler.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/health", nil))
	adminRequest := httptest.NewRequest(http.MethodGet, "/admin/records/secret?token=private", nil)
	adminRequest.Header.Set("Origin", "https://admin.example")
	admin := httptest.NewRecorder()
	handler.ServeHTTP(admin, adminRequest)
	if health.Code != http.StatusNoContent || health.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("health response = %d %v", health.Code, health.Header())
	}
	if admin.Code != http.StatusOK || admin.Header().Get("Access-Control-Allow-Origin") != "https://admin.example" || admin.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatalf("admin response = %d %v", admin.Code, admin.Header())
	}
	if len(events) != 2 || events[0].Route != "GET /health" || events[1].Route != "GET /admin/records/{id}" {
		t.Fatalf("events = %#v", events)
	}
}

func TestRepresentativeStreamingProfileKeepsFlush(t *testing.T) {
	t.Parallel()
	recoverer, _ := recovery.New(recovery.Policy{})
	observer, _ := observe.New(observe.Policy{Observer: func(context.Context, observe.Event) {}})
	headers, _ := secureheader.New(secureheader.APIDefaults())
	chain, _ := middleware.New(recoverer, observer, headers)
	handler, _ := chain.Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "event: ready\n\n")
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Errorf("Flush() error = %v", err)
		}
	}))
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	response, err := server.Client().Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if response == nil {
		t.Fatal("nil response")
		return
	}
	payload, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil || string(payload) != "event: ready\n\n" {
		t.Fatalf("payload = %q, error = %v", payload, err)
	}
}
