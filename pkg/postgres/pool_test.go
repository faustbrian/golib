package postgres

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestNewFailFastDoesNotLeakCredentials(t *testing.T) {
	t.Parallel()

	const password = "startup-secret-password"
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err := New(ctx, Config{
		DSN:            "postgres://app:" + password + "@127.0.0.1:1/app?sslmode=disable",
		ConnectTimeout: 50 * time.Millisecond,
		PingTimeout:    100 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("New() error = nil")
	}
	if strings.Contains(err.Error(), password) {
		t.Fatalf("New() error leaked password: %v", err)
	}
}

func TestNewFailsBoundedlyAgainstWrongProtocolServer(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer func() { _ = listener.Close() }()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	started := time.Now()
	_, err = New(ctx, Config{
		DSN: fmt.Sprintf(
			"postgres://app:wrong-server-secret@%s/app?sslmode=disable",
			listener.Addr(),
		),
		ConnectTimeout: 100 * time.Millisecond,
		PingTimeout:    250 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("New() error = nil")
	}
	if strings.Contains(err.Error(), "wrong-server-secret") {
		t.Fatalf("New() error leaked password: %v", err)
	}
	if elapsed := time.Since(started); elapsed >= 2*time.Second {
		t.Fatalf("wrong-server startup took %s", elapsed)
	}

	_ = listener.Close()
	<-done
}

func TestNewLazyExposesNativePool(t *testing.T) {
	t.Parallel()

	pool, err := New(context.Background(), Config{
		DSN:           "postgres://localhost/app?sslmode=disable",
		MaxConns:      7,
		StartupPolicy: StartupLazy,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if pool.Raw() == nil {
		t.Fatal("Raw() = nil")
	}
	if got := pool.Raw().Config().MaxConns; got != 7 {
		t.Errorf("Raw().Config().MaxConns = %d, want 7", got)
	}
	if err := pool.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestPoolAcquireUsesConfiguredBound(t *testing.T) {
	t.Parallel()

	backend := &stubPoolBackend{
		stats: Stats{AcquiredConns: 1, MaxConns: 1},
		acquire: func(ctx context.Context) (*pgxpool.Conn, error) {
			<-ctx.Done()

			return nil, ctx.Err()
		},
	}
	pool := newPool(nil, backend, time.Nanosecond, time.Second, time.Second)

	_, err := pool.Acquire(context.Background())
	if !errors.Is(err, ErrAcquireTimeout) {
		t.Fatalf("Acquire() error = %v, want ErrAcquireTimeout", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Acquire() error = %v, want context deadline", err)
	}
	if !errors.Is(err, ErrPoolExhausted) || !IsPoolExhaustion(err) {
		t.Fatalf("Acquire() error = %v, want pool exhaustion", err)
	}
}

func TestPoolAcquirePreservesCallerDeadlineWithoutClaimingSaturation(t *testing.T) {
	t.Parallel()

	backend := &stubPoolBackend{
		stats: Stats{MaxConns: 4},
		acquire: func(ctx context.Context) (*pgxpool.Conn, error) {
			<-ctx.Done()

			return nil, ctx.Err()
		},
	}
	pool := newPool(nil, backend, time.Second, time.Second, time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := pool.Acquire(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Acquire() error = %v, want context cancellation", err)
	}
	if errors.Is(err, ErrAcquireTimeout) || errors.Is(err, ErrPoolExhausted) || IsPoolExhaustion(err) {
		t.Fatalf("Acquire() error = %v, incorrectly reports pool exhaustion", err)
	}
}

func TestPoolReadinessUsesBoundedPingAndReportsStats(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("database unavailable")
	backend := &stubPoolBackend{
		ping: func(context.Context) error { return sentinel },
	}
	pool := newPool(nil, backend, time.Second, time.Second, time.Second)

	health := pool.Readiness(context.Background())
	if health.Ready {
		t.Fatal("Readiness().Ready = true")
	}
	if !errors.Is(health.Err, sentinel) {
		t.Fatalf("Readiness().Err = %v, want sentinel", health.Err)
	}
}

func TestPoolCloseIsBoundedAndRunsUnderlyingCloseOnce(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	var closes atomic.Int32
	backend := &stubPoolBackend{
		close: func() {
			closes.Add(1)
			<-release
		},
	}
	pool := newPool(nil, backend, time.Second, time.Second, time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := pool.Close(ctx)
	if !errors.Is(err, ErrShutdownTimeout) {
		t.Fatalf("Close() error = %v, want ErrShutdownTimeout", err)
	}
	close(release)

	if err := pool.Close(context.Background()); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if got := closes.Load(); got != 1 {
		t.Fatalf("underlying close calls = %d, want 1", got)
	}
}

func TestPoolObservationsAreBoundedAndPanicSafe(t *testing.T) {
	t.Parallel()

	var observations []Observation
	observer := ObserverFunc(func(_ context.Context, observation Observation) {
		observations = append(observations, observation)
	})
	backend := &stubPoolBackend{}
	pool := newPool(nil, backend, time.Second, time.Second, time.Second, observer)

	if _, err := pool.Acquire(context.Background()); err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if len(observations) != 1 || observations[0].Operation != OperationAcquire ||
		observations[0].Outcome != OutcomeSuccess || observations[0].Duration < 0 {
		t.Fatalf("observations = %#v", observations)
	}

	pool = newPool(nil, backend, time.Second, time.Second, time.Second, ObserverFunc(func(context.Context, Observation) {
		panic("telemetry failure")
	}))
	if _, err := pool.Acquire(context.Background()); err != nil {
		t.Fatalf("Acquire() changed by observer panic: %v", err)
	}
}

func TestPoolLivenessDoesNotRequireDatabaseConnectivity(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	backend := &stubPoolBackend{
		ping:  func(context.Context) error { return errors.New("database unavailable") },
		close: func() { <-release },
	}
	pool := newPool(nil, backend, time.Second, time.Second, time.Second)
	if health := pool.Liveness(); !health.Ready || health.Err != nil {
		t.Fatalf("Liveness() before close = %#v", health)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = pool.Close(ctx)
	if health := pool.Liveness(); health.Ready || !errors.Is(health.Err, ErrPoolClosed) {
		t.Fatalf("Liveness() after close = %#v", health)
	}
	close(release)
}

func TestNewPreservesNativeConstructionFailureWithoutCredentials(t *testing.T) {
	t.Parallel()

	_, err := New(context.Background(), Config{
		DSN:           "postgres://app:secret@localhost/app?sslmode=disable",
		StartupPolicy: StartupLazy,
		Configure: func(config *PoolConfig) error {
			config.MaxConns = 0

			return nil
		},
	})
	if err == nil {
		t.Fatal("New() error = nil")
	}
	if strings.Contains(err.Error(), "secret") {
		t.Fatalf("New() leaked credentials: %v", err)
	}
	if errors.Unwrap(err) == nil {
		t.Fatalf("New() error = %v, want native cause", err)
	}
}

func TestNewReturnsConfigurationFailure(t *testing.T) {
	t.Parallel()

	_, err := New(context.Background(), Config{})
	var configErr *ConfigError
	if !errors.As(err, &configErr) {
		t.Fatalf("New() error = %v, want ConfigError", err)
	}
}

func TestBoundedContextSupportsNoAdditionalTimeout(t *testing.T) {
	t.Parallel()

	ctx, cancel := boundedContext(context.Background(), 0)
	cancel()
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("context error = %v", ctx.Err())
	}
}

func TestBoundedContextWithCauseSupportsNoAdditionalTimeout(t *testing.T) {
	t.Parallel()

	ctx, cancel := boundedContextWithCause(context.Background(), 0, ErrAcquireTimeout)
	cancel()
	if !errors.Is(ctx.Err(), context.Canceled) || errors.Is(context.Cause(ctx), ErrAcquireTimeout) {
		t.Fatalf("context error and cause = (%v, %v)", ctx.Err(), context.Cause(ctx))
	}
}

type stubPoolBackend struct {
	acquire func(context.Context) (*pgxpool.Conn, error)
	ping    func(context.Context) error
	close   func()
	stats   Stats
}

func (s *stubPoolBackend) Acquire(ctx context.Context) (*pgxpool.Conn, error) {
	if s.acquire == nil {
		return nil, nil
	}

	return s.acquire(ctx)
}

func (s *stubPoolBackend) Ping(ctx context.Context) error {
	if s.ping == nil {
		return nil
	}

	return s.ping(ctx)
}

func (s *stubPoolBackend) Stats() Stats {
	return s.stats
}

func (s *stubPoolBackend) Close() {
	if s.close != nil {
		s.close()
	}
}
