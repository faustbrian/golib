package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	postgres "github.com/faustbrian/golib/pkg/postgres"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := postgres.New(ctx, postgres.Config{
		DSN:             os.Getenv("DATABASE_URL"),
		MaxConns:        20,
		AcquireTimeout:  2 * time.Second,
		PingTimeout:     time.Second,
		ShutdownTimeout: 10 * time.Second,
		Observer:        postgres.NewSlogObserver(slog.Default()),
	})
	if err != nil {
		slog.Error("database startup failed", "error", err)
		os.Exit(1)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/ready", func(writer http.ResponseWriter, request *http.Request) {
		probeCtx, cancel := context.WithTimeout(request.Context(), time.Second)
		defer cancel()
		if health := pool.Readiness(probeCtx); !health.Ready {
			http.Error(writer, "not ready", http.StatusServiceUnavailable)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/live", func(writer http.ResponseWriter, _ *http.Request) {
		if health := pool.Liveness(); !health.Ready {
			http.Error(writer, "not live", http.StatusServiceUnavailable)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	})

	server := &http.Server{Addr: ":8080", Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("HTTP server failed", "error", err)
	}
	if err := pool.Close(context.Background()); err != nil {
		slog.Error("database shutdown failed", "error", err)
	}
}
