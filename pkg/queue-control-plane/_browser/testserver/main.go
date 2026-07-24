package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/faustbrian/golib/pkg/queue-control-plane/apihttp"
	controlui "github.com/faustbrian/golib/pkg/queue-control-plane/ui"
)

const (
	apiAddress      = "127.0.0.1:18081"
	approvedAddress = "127.0.0.1:18080"
	hostileAddress  = "127.0.0.1:18082"
)

func main() {
	requests := &requestObservations{}
	security, err := apihttp.NewSecurityMiddleware(apihttp.SecurityConfig{
		AllowedOrigins:   []string{"http://" + approvedAddress},
		AllowCredentials: true,
	})
	if err != nil {
		log.Fatal(err)
	}

	servers := []*http.Server{
		newServer(approvedAddress, approvedHandler()),
		newServer(hostileAddress, pageHandler()),
		newServer(apiAddress, requests.wrap(security(apiHandler(requests)))),
	}
	listeners := make([]net.Listener, 0, len(servers))
	for _, server := range servers {
		listener, listenErr := net.Listen("tcp", server.Addr)
		if listenErr != nil {
			closeListeners(listeners)
			log.Fatal(listenErr)
		}
		listeners = append(listeners, listener)
	}

	serveErrors := make(chan error, len(servers))
	for index, server := range servers {
		go func() {
			serveErrors <- server.Serve(listeners[index])
		}()
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	select {
	case <-ctx.Done():
	case serveErr := <-serveErrors:
		if !errors.Is(serveErr, http.ErrServerClosed) {
			log.Printf("browser test server failed: %v", serveErr)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, server := range servers {
		if shutdownErr := server.Shutdown(shutdownCtx); shutdownErr != nil {
			log.Printf("browser test server shutdown failed: %v", shutdownErr)
		}
	}
}

func approvedHandler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /security-probe", pageHandler())
	mux.HandleFunc("GET /ready", func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /v1/tenants/{tenant}/workers", authenticatedUIResponse(func(writer http.ResponseWriter, request *http.Request) {
		writeBrowserJSON(writer, map[string]any{"workers": []map[string]any{{
			"tenant_id": request.PathValue("tenant"), "worker_id": "worker-1",
			"version": "v1.0.0", "state": "running", "queues": []string{"critical"},
		}}})
	}))
	mux.HandleFunc("GET /v1/tenants/{tenant}/queues", authenticatedUIResponse(func(writer http.ResponseWriter, _ *http.Request) {
		writeBrowserJSON(writer, map[string]any{"queues": []map[string]any{{
			"backend": "valkey-streams", "queue": "critical",
			"depth": map[string]any{"value": 7, "supported": true},
		}}})
	}))
	mux.HandleFunc("GET /v1/tenants/{tenant}/failures", authenticatedUIResponse(func(writer http.ResponseWriter, request *http.Request) {
		if _, present := request.URL.Query()["payload"]; present {
			http.Error(writer, "payload is not a list parameter", http.StatusBadRequest)
			return
		}
		writeBrowserJSON(writer, map[string]any{"records": []map[string]any{{
			"id": "failure-1", "queue": "critical", "payload_visibility": "hidden",
		}}})
	}))
	mux.HandleFunc("POST /v1/tenants/{tenant}/commands", authenticatedUIResponse(func(writer http.ResponseWriter, request *http.Request) {
		var command map[string]any
		if err := json.NewDecoder(request.Body).Decode(&command); err != nil {
			http.Error(writer, "invalid command", http.StatusBadRequest)
			return
		}
		action, actionOK := command["action"].(string)
		if command["reason"] != "maintenance window" || !actionOK || !browserCommandAction(action) {
			http.Error(writer, "invalid command", http.StatusBadRequest)
			return
		}
		writeBrowserJSON(writer, map[string]any{
			"tenant_id":       request.PathValue("tenant"),
			"idempotency_key": command["idempotency_key"], "status": "accepted",
		})
	}))
	mux.Handle("/", controlui.NewHandler())

	return mux
}

func browserCommandAction(action string) bool {
	switch action {
	case "pause", "resume", "drain", "terminate", "retry", "bulk_retry",
		"delete", "purge", "replay", "scale":
		return true
	default:
		return false
	}
}

func authenticatedUIResponse(next http.HandlerFunc) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("X-Queue-Control-Key-ID") != "browser-key" ||
			request.Header.Get("X-Queue-Control-Key") != "browser-secret" {
			writeBrowserJSONStatus(writer, http.StatusUnauthorized, map[string]string{"code": "unauthenticated"})
			return
		}
		next(writer, request)
	}
}

func writeBrowserJSON(writer http.ResponseWriter, body any) {
	writeBrowserJSONStatus(writer, http.StatusOK, body)
}

func writeBrowserJSONStatus(writer http.ResponseWriter, status int, body any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(body)
}

func newServer(address string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              address,
		Handler:           handler,
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       10 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
}

func pageHandler() http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || (request.URL.Path != "/" && request.URL.Path != "/ready") {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(writer, "<!doctype html><title>browser security probe</title>")
	})
}

func apiHandler(requests *requestObservations) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/ready" && request.Method == http.MethodGet {
			writer.WriteHeader(http.StatusNoContent)
			return
		}
		if request.URL.Path == "/observations" && request.Method == http.MethodGet {
			writer.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(writer).Encode(requests.snapshot())
			return
		}
		if !strings.HasPrefix(request.URL.Path, "/probe/") ||
			(request.Method != http.MethodGet && request.Method != http.MethodPost) {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]string{"status": "ok"})
	})
}

type observedRequest struct {
	Method string `json:"method"`
	Origin string `json:"origin"`
	Path   string `json:"path"`
	Status int    `json:"status"`
}

type requestObservations struct {
	mu       sync.Mutex
	requests []observedRequest
}

func (o *requestObservations) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		observedWriter := &statusWriter{ResponseWriter: writer}
		next.ServeHTTP(observedWriter, request)
		status := observedWriter.status
		if status == 0 {
			status = http.StatusOK
		}

		o.mu.Lock()
		o.requests = append(o.requests, observedRequest{
			Method: request.Method,
			Origin: request.Header.Get("Origin"),
			Path:   request.URL.Path,
			Status: status,
		})
		o.mu.Unlock()
	})
}

func (o *requestObservations) snapshot() []observedRequest {
	o.mu.Lock()
	defer o.mu.Unlock()

	return append([]observedRequest(nil), o.requests...)
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}

	return w.ResponseWriter.Write(body)
}

func closeListeners(listeners []net.Listener) {
	for _, listener := range listeners {
		_ = listener.Close()
	}
}
