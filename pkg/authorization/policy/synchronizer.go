package policy

import (
	"context"
	"errors"
	"sync"
	"time"

	authorization "github.com/faustbrian/golib/pkg/authorization"
)

const DefaultSyncInterval = 30 * time.Second
const DefaultMaxStaleness = 2 * time.Minute

var (
	ErrNilRepository       = errors.New("policy synchronizer repository is nil")
	ErrNilCompiler         = errors.New("policy synchronizer compiler is nil")
	ErrNilEngine           = errors.New("policy synchronizer engine is nil")
	ErrInvalidSyncInterval = errors.New("policy synchronizer interval is invalid")
	ErrInvalidMaxStaleness = errors.New("policy synchronizer maximum staleness is invalid")
	ErrStaleManifest       = errors.New("policy repository manifest is stale")
	ErrPolicyStale         = errors.New("authorization policy verification is stale")
)

type SynchronizerOption func(*Synchronizer)

func WithSyncInterval(interval time.Duration) SynchronizerOption {
	return func(synchronizer *Synchronizer) {
		synchronizer.interval = interval
	}
}

func WithMaxStaleness(maxStaleness time.Duration) SynchronizerOption {
	return func(synchronizer *Synchronizer) {
		synchronizer.maxStaleness = maxStaleness
	}
}

func WithSynchronizerClock(clock func() time.Time) SynchronizerOption {
	return func(synchronizer *Synchronizer) {
		if clock != nil {
			synchronizer.clock = clock
		}
	}
}

// Synchronizer keeps an engine converged with its authoritative repository.
// Direct repository polling is the correctness path; external invalidation
// transports may call Observe to reduce propagation latency.
type Synchronizer struct {
	repository   Repository
	compiler     *Compiler
	engine       *authorization.Engine
	interval     time.Duration
	maxStaleness time.Duration
	clock        func() time.Time
	reloadMu     sync.Mutex
	verifiedMu   sync.RWMutex
	verifiedAt   time.Time
}

func NewSynchronizer(
	repository Repository,
	compiler *Compiler,
	engine *authorization.Engine,
	options ...SynchronizerOption,
) (*Synchronizer, error) {
	if repository == nil {
		return nil, ErrNilRepository
	}
	if compiler == nil {
		return nil, ErrNilCompiler
	}
	if engine == nil {
		return nil, ErrNilEngine
	}
	synchronizer := &Synchronizer{
		repository:   repository,
		compiler:     compiler,
		engine:       engine,
		interval:     DefaultSyncInterval,
		maxStaleness: DefaultMaxStaleness,
		clock:        time.Now,
	}
	for _, option := range options {
		option(synchronizer)
	}
	if synchronizer.interval <= 0 {
		return nil, ErrInvalidSyncInterval
	}
	if synchronizer.maxStaleness <= 0 {
		return nil, ErrInvalidMaxStaleness
	}
	return synchronizer, nil
}

// Reload activates a newer repository manifest. Equal revisions are a no-op;
// older repository state fails rather than rolling the engine back.
func (synchronizer *Synchronizer) Reload(ctx context.Context) (bool, error) {
	synchronizer.reloadMu.Lock()
	defer synchronizer.reloadMu.Unlock()
	if err := ctx.Err(); err != nil {
		return false, err
	}
	manifest, err := synchronizer.repository.Load(ctx)
	if err != nil {
		return false, err
	}
	if err := manifest.Validate(); err != nil {
		return false, err
	}
	current := synchronizer.engine.Revision()
	if manifest.Revision < current {
		return false, ErrStaleManifest
	}
	if manifest.Revision == current {
		synchronizer.markVerified()
		return false, nil
	}
	snapshot, err := synchronizer.compiler.Compile(manifest)
	if err != nil {
		return false, err
	}
	if err := synchronizer.engine.ReplaceSnapshot(snapshot, current); err != nil {
		return false, err
	}
	synchronizer.markVerified()
	return true, nil
}

// Decide enforces the maximum age of the last successful repository
// verification before delegating to the active immutable engine snapshot.
func (synchronizer *Synchronizer) Decide(
	ctx context.Context,
	request authorization.Request,
) (authorization.Decision, error) {
	verifiedAt, verified := synchronizer.LastVerified()
	now := synchronizer.clock()
	if !verified || now.Before(verifiedAt) || now.Sub(verifiedAt) > synchronizer.maxStaleness {
		return authorization.Decision{
			Outcome:  authorization.Deny,
			Reason:   authorization.ReasonPolicyStale,
			Revision: synchronizer.engine.Revision(),
		}, ErrPolicyStale
	}
	return synchronizer.engine.Decide(ctx, request)
}

// LastVerified reports the time of the latest successful authoritative
// repository verification, including a same-revision verification.
func (synchronizer *Synchronizer) LastVerified() (time.Time, bool) {
	synchronizer.verifiedMu.RLock()
	defer synchronizer.verifiedMu.RUnlock()
	return synchronizer.verifiedAt, !synchronizer.verifiedAt.IsZero()
}

func (synchronizer *Synchronizer) markVerified() {
	synchronizer.verifiedMu.Lock()
	defer synchronizer.verifiedMu.Unlock()
	synchronizer.verifiedAt = synchronizer.clock()
}

// Observe handles an untrusted invalidation revision by reloading the source
// of truth. It rejects hints ahead of the repository state.
func (synchronizer *Synchronizer) Observe(
	ctx context.Context,
	revision authorization.Revision,
) error {
	if revision <= synchronizer.engine.Revision() {
		return nil
	}
	if _, err := synchronizer.Reload(ctx); err != nil {
		return err
	}
	if synchronizer.engine.Revision() < revision {
		return ErrStaleManifest
	}
	return nil
}

// Run performs an immediate repository check and then polls until cancellation
// or the first reload error. Returning errors prevents silent stale operation.
func (synchronizer *Synchronizer) Run(ctx context.Context) error {
	if _, err := synchronizer.Reload(ctx); err != nil {
		return err
	}
	ticker := time.NewTicker(synchronizer.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if _, err := synchronizer.Reload(ctx); err != nil {
				return err
			}
		}
	}
}
