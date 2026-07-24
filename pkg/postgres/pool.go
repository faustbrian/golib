package postgres

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	// ErrAcquireTimeout identifies the configured acquisition deadline.
	ErrAcquireTimeout = errors.New("postgres: acquire timeout")
	// ErrPoolExhausted identifies an acquisition timeout observed while all
	// configured connection slots were acquired or constructing.
	ErrPoolExhausted = errors.New("postgres: pool exhausted")
	// ErrHealthTimeout identifies a configured or caller health-check deadline.
	ErrHealthTimeout = errors.New("postgres: health check timeout")
	// ErrPoolClosed identifies a pool whose shutdown has begun.
	ErrPoolClosed = errors.New("postgres: pool closed")
	// ErrShutdownTimeout identifies a shutdown that outlasted its context.
	ErrShutdownTimeout = errors.New("postgres: shutdown timeout")
)

type poolBackend interface {
	Acquire(context.Context) (*pgxpool.Conn, error)
	Ping(context.Context) error
	Stats() Stats
	Close()
}

type nativePoolBackend struct {
	pool *pgxpool.Pool
}

func (b *nativePoolBackend) Acquire(ctx context.Context) (*pgxpool.Conn, error) {
	return b.pool.Acquire(ctx)
}

func (b *nativePoolBackend) Ping(ctx context.Context) error {
	return b.pool.Ping(ctx)
}

func (b *nativePoolBackend) Stats() Stats {
	return snapshotStats(b.pool.Stat())
}

func (b *nativePoolBackend) Close() {
	b.pool.Close()
}

// Pool adds bounded lifecycle operations while retaining the native pgxpool.
type Pool struct {
	raw             *pgxpool.Pool
	backend         poolBackend
	acquireTimeout  time.Duration
	pingTimeout     time.Duration
	shutdownTimeout time.Duration
	observer        Observer
	closeOnce       sync.Once
	closeDone       chan struct{}
	closed          atomic.Bool
}

// Stats is an immutable snapshot of native pgxpool statistics.
type Stats struct {
	AcquireCount         int64
	AcquiredConns        int32
	CanceledAcquireCount int64
	ConstructingConns    int32
	EmptyAcquireCount    int64
	EmptyAcquireWaitTime time.Duration
	IdleConns            int32
	MaxConns             int32
	NewConnsCount        int64
	TotalConns           int32
}

// Health is the result of a readiness probe and its contemporaneous stats.
type Health struct {
	Ready bool
	Err   error
	Stats Stats
}

// New constructs a native pgxpool and, by default, proves connectivity with a
// bounded ping. StartupLazy skips that initial network operation.
func New(ctx context.Context, input Config) (*Pool, error) {
	config, err := ParseConfig(input)
	if err != nil {
		return nil, err
	}

	raw, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, &startupError{cause: err}
	}

	pool := newPool(
		raw,
		&nativePoolBackend{pool: raw},
		valueOrDefault(input.AcquireTimeout, DefaultAcquireTimeout),
		valueOrDefault(input.PingTimeout, DefaultPingTimeout),
		valueOrDefault(input.ShutdownTimeout, DefaultShutdownTimeout),
		input.Observer,
	)
	if input.StartupPolicy == StartupLazy {
		return pool, nil
	}

	if err := pool.Ping(ctx); err != nil {
		raw.Close()

		return nil, &startupError{cause: err}
	}

	return pool, nil
}

func newPool(
	raw *pgxpool.Pool,
	backend poolBackend,
	acquireTimeout time.Duration,
	pingTimeout time.Duration,
	shutdownTimeout time.Duration,
	observers ...Observer,
) *Pool {
	var observer Observer
	if len(observers) > 0 {
		observer = observers[0]
	}

	return &Pool{
		raw:             raw,
		backend:         backend,
		acquireTimeout:  acquireTimeout,
		pingTimeout:     pingTimeout,
		shutdownTimeout: shutdownTimeout,
		observer:        observer,
		closeDone:       make(chan struct{}),
	}
}

// Raw returns the underlying pgxpool without wrapping or copying it.
func (p *Pool) Raw() *pgxpool.Pool {
	return p.raw
}

// Acquire obtains a connection while honoring the earlier of the caller's
// deadline and the configured acquisition timeout.
func (p *Pool) Acquire(ctx context.Context) (*pgxpool.Conn, error) {
	started := time.Now()
	ctx, cancel := boundedContextWithCause(ctx, p.acquireTimeout, ErrAcquireTimeout)
	defer cancel()

	conn, err := p.backend.Acquire(ctx)
	stats := p.Stats()
	if err != nil && errors.Is(context.Cause(ctx), ErrAcquireTimeout) {
		err = errors.Join(ErrAcquireTimeout, err)
		if poolIsSaturated(stats) {
			err = errors.Join(ErrPoolExhausted, err)
		}
	}
	observation := observationFor(OperationAcquire, started, err)
	observation.Pool = stats
	observation.HasPoolStats = true
	safeObserve(ctx, p.observer, observation)

	return conn, err
}

// Ping checks PostgreSQL connectivity with a strict configured deadline.
func (p *Pool) Ping(ctx context.Context) error {
	started := time.Now()
	ctx, cancel := boundedContext(ctx, p.pingTimeout)
	defer cancel()

	err := p.backend.Ping(ctx)
	if err != nil && errors.Is(err, context.DeadlineExceeded) {
		err = errors.Join(ErrHealthTimeout, err)
	}
	observation := observationFor(OperationPing, started, err)
	observation.Pool = p.Stats()
	observation.HasPoolStats = true
	safeObserve(ctx, p.observer, observation)

	return err
}

// Readiness performs a bounded ping and returns a pool-statistics snapshot.
func (p *Pool) Readiness(ctx context.Context) Health {
	err := p.Ping(ctx)

	return Health{
		Ready: err == nil,
		Err:   err,
		Stats: p.Stats(),
	}
}

// Liveness reports whether pool shutdown has started without contacting the
// database. Database availability belongs to Readiness rather than liveness.
func (p *Pool) Liveness() Health {
	err := error(nil)
	if p.closed.Load() {
		err = ErrPoolClosed
	}

	return Health{
		Ready: err == nil,
		Err:   err,
		Stats: p.Stats(),
	}
}

// Stats returns an immutable copy of the native pgxpool counters and gauges.
func (p *Pool) Stats() Stats {
	return p.backend.Stats()
}

func snapshotStats(stats *pgxpool.Stat) Stats {
	return Stats{
		AcquireCount:         stats.AcquireCount(),
		AcquiredConns:        stats.AcquiredConns(),
		CanceledAcquireCount: stats.CanceledAcquireCount(),
		ConstructingConns:    stats.ConstructingConns(),
		EmptyAcquireCount:    stats.EmptyAcquireCount(),
		EmptyAcquireWaitTime: stats.EmptyAcquireWaitTime(),
		IdleConns:            stats.IdleConns(),
		MaxConns:             stats.MaxConns(),
		NewConnsCount:        stats.NewConnsCount(),
		TotalConns:           stats.TotalConns(),
	}
}

// Close begins native pool shutdown exactly once and waits only until the
// earlier of the caller deadline or configured shutdown timeout. A timed-out
// close continues in the background until borrowed connections are returned.
func (p *Pool) Close(ctx context.Context) error {
	started := time.Now()
	p.closed.Store(true)
	p.closeOnce.Do(func() {
		go func() {
			p.backend.Close()
			close(p.closeDone)
		}()
	})

	ctx, cancel := boundedContext(ctx, p.shutdownTimeout)
	defer cancel()

	select {
	case <-p.closeDone:
		observation := observationFor(OperationClose, started, nil)
		observation.Pool = p.Stats()
		observation.HasPoolStats = true
		safeObserve(ctx, p.observer, observation)
		return nil
	case <-ctx.Done():
		err := errors.Join(ErrShutdownTimeout, ctx.Err())
		observation := observationFor(OperationClose, started, err)
		observation.Pool = p.Stats()
		observation.HasPoolStats = true
		safeObserve(ctx, p.observer, observation)

		return err
	}
}

func poolIsSaturated(stats Stats) bool {
	return stats.MaxConns > 0 &&
		stats.AcquiredConns+stats.ConstructingConns >= stats.MaxConns
}

func boundedContextWithCause(
	parent context.Context,
	timeout time.Duration,
	cause error,
) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.WithCancel(parent)
	}

	return context.WithTimeoutCause(parent, timeout, cause)
}

type startupError struct {
	cause error
}

func (e *startupError) Error() string {
	return "postgres: startup connectivity check failed"
}

func (e *startupError) Unwrap() error {
	return e.cause
}

func boundedContext(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.WithCancel(parent)
	}

	return context.WithTimeout(parent, timeout)
}
