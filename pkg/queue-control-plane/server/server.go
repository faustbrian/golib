// Package server owns the bounded administrative HTTP server lifecycle.
package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"reflect"
	"time"
)

var ErrInvalidConfiguration = errors.New("server: invalid configuration")

const (
	defaultReadHeaderTimeout = 5 * time.Second
	defaultReadTimeout       = 15 * time.Second
	defaultWriteTimeout      = 30 * time.Second
	defaultIdleTimeout       = 60 * time.Second
	defaultShutdownTimeout   = 10 * time.Second
	defaultMaxHeaderBytes    = 1 << 20
)

// Config bounds HTTP admission and graceful shutdown.
type Config struct {
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
	MaxHeaderBytes    int
}

// Server serves one preconfigured listener without supervising other
// processes.
type Server struct {
	listener        net.Listener
	http            *http.Server
	shutdownTimeout time.Duration
}

// New creates a bounded administrative HTTP server.
func New(listener net.Listener, handler http.Handler, config Config) (*Server, error) {
	if nilInterface(listener) || nilInterface(handler) || invalidConfig(config) {
		return nil, ErrInvalidConfiguration
	}
	applyDefaults(&config)

	return &Server{
		listener: listener,
		http: &http.Server{
			Handler:           handler,
			ReadHeaderTimeout: config.ReadHeaderTimeout,
			ReadTimeout:       config.ReadTimeout,
			WriteTimeout:      config.WriteTimeout,
			IdleTimeout:       config.IdleTimeout,
			MaxHeaderBytes:    config.MaxHeaderBytes,
		},
		shutdownTimeout: config.ShutdownTimeout,
	}, nil
}

// Run serves until the listener fails or ctx requests graceful shutdown.
func (server *Server) Run(ctx context.Context) error {
	served := make(chan error, 1)
	go func() {
		err := server.http.Serve(server.listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		served <- err
	}()

	select {
	case err := <-served:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), server.shutdownTimeout)
		defer cancel()
		shutdownErr := server.http.Shutdown(shutdownCtx)
		if shutdownErr != nil {
			_ = server.http.Close()
		}

		return errors.Join(shutdownErr, <-served)
	}
}

func invalidConfig(config Config) bool {
	return config.ReadHeaderTimeout < 0 || config.ReadTimeout < 0 ||
		config.WriteTimeout < 0 || config.IdleTimeout < 0 ||
		config.ShutdownTimeout < 0 || config.MaxHeaderBytes < 0
}

func applyDefaults(config *Config) {
	if config.ReadHeaderTimeout == 0 {
		config.ReadHeaderTimeout = defaultReadHeaderTimeout
	}
	if config.ReadTimeout == 0 {
		config.ReadTimeout = defaultReadTimeout
	}
	if config.WriteTimeout == 0 {
		config.WriteTimeout = defaultWriteTimeout
	}
	if config.IdleTimeout == 0 {
		config.IdleTimeout = defaultIdleTimeout
	}
	if config.ShutdownTimeout == 0 {
		config.ShutdownTimeout = defaultShutdownTimeout
	}
	if config.MaxHeaderBytes == 0 {
		config.MaxHeaderBytes = defaultMaxHeaderBytes
	}
}

func nilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)

	return reflected.Kind() == reflect.Pointer && reflected.IsNil()
}
