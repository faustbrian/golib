package policy

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	authorization "github.com/faustbrian/golib/pkg/authorization"
)

type repositoryStub struct {
	mu        sync.Mutex
	manifests []Manifest
	err       error
	loads     int
}

func (repository *repositoryStub) Load(context.Context) (Manifest, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	repository.loads++
	if repository.err != nil {
		return Manifest{}, repository.err
	}
	manifest := repository.manifests[0]
	if len(repository.manifests) > 1 {
		repository.manifests = repository.manifests[1:]
	}
	return manifest, nil
}

func (*repositoryStub) Update(context.Context, authorization.Revision, Manifest) (Manifest, error) {
	return Manifest{}, errors.New("not implemented")
}

func TestSynchronizerReloadsNewerManifest(t *testing.T) {
	t.Parallel()

	engine := synchronizerEngine(t, 1)
	repository := &repositoryStub{manifests: []Manifest{synchronizerManifest(2)}}
	synchronizer, err := NewSynchronizer(repository, synchronizerCompiler(t), engine)
	if err != nil {
		t.Fatalf("NewSynchronizer() error = %v", err)
	}
	changed, err := synchronizer.Reload(context.Background())
	if err != nil || !changed {
		t.Fatalf("Reload() = (%v, %v), want changed", changed, err)
	}
	if engine.Revision() != 2 {
		t.Errorf("engine revision = %d, want 2", engine.Revision())
	}

	repository.manifests = []Manifest{synchronizerManifest(2)}
	changed, err = synchronizer.Reload(context.Background())
	if err != nil || changed {
		t.Fatalf("same-revision Reload() = (%v, %v), want unchanged", changed, err)
	}
}

func TestSynchronizerAuthorizerFailsClosedOutsideFreshnessWindow(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	snapshot, err := authorization.NewSnapshot(
		1,
		authorization.DenyOverrides,
		authorization.PolicyDefinition{
			ID: "allow",
			Evaluator: fixedEvaluator{decision: authorization.Decision{
				Outcome: authorization.Allow,
			}},
		},
	)
	if err != nil {
		t.Fatalf("authorization.NewSnapshot() error = %v", err)
	}
	engine, err := authorization.NewEngine(snapshot)
	if err != nil {
		t.Fatalf("authorization.NewEngine() error = %v", err)
	}
	repository := &repositoryStub{manifests: []Manifest{synchronizerManifest(1)}}
	synchronizer, err := NewSynchronizer(
		repository,
		synchronizerCompiler(t),
		engine,
		WithMaxStaleness(time.Minute),
		WithSynchronizerClock(clock),
	)
	if err != nil {
		t.Fatalf("NewSynchronizer() error = %v", err)
	}
	request := authorization.Request{
		Subject:  authorization.Subject{Kind: authorization.SubjectUser, ID: "alice"},
		Action:   "read",
		Resource: authorization.Resource{Type: "document", ID: "one"},
	}

	decision, err := synchronizer.Decide(context.Background(), request)
	if !errors.Is(err, ErrPolicyStale) {
		t.Fatalf("Decide(before verification) error = %v, want ErrPolicyStale", err)
	}
	if decision.Outcome != authorization.Deny || decision.Reason != authorization.ReasonPolicyStale {
		t.Errorf("Decide(before verification) = %+v, want stale deny", decision)
	}

	changed, err := synchronizer.Reload(context.Background())
	if err != nil || changed {
		t.Fatalf("Reload(same revision) = (%v, %v), want (false, nil)", changed, err)
	}
	verifiedAt, verified := synchronizer.LastVerified()
	if !verified || !verifiedAt.Equal(now) {
		t.Fatalf("LastVerified() = (%v, %v), want (%v, true)", verifiedAt, verified, now)
	}

	now = now.Add(time.Minute)
	decision, err = synchronizer.Decide(context.Background(), request)
	if err != nil || decision.Outcome != authorization.Allow {
		t.Fatalf("Decide(at freshness boundary) = (%+v, %v), want allow", decision, err)
	}

	now = now.Add(time.Nanosecond)
	decision, err = synchronizer.Decide(context.Background(), request)
	if !errors.Is(err, ErrPolicyStale) || decision.Outcome != authorization.Deny {
		t.Fatalf("Decide(after freshness boundary) = (%+v, %v), want stale deny", decision, err)
	}

	repository.err = errors.New("repository unavailable")
	if _, err := synchronizer.Reload(context.Background()); err == nil {
		t.Fatal("Reload(repository failure) error = nil")
	}
	gotVerifiedAt, verified := synchronizer.LastVerified()
	if !verified || !gotVerifiedAt.Equal(verifiedAt) {
		t.Errorf("LastVerified() after failed reload = (%v, %v), want (%v, true)", gotVerifiedAt, verified, verifiedAt)
	}

	now = verifiedAt.Add(-time.Nanosecond)
	decision, err = synchronizer.Decide(context.Background(), request)
	if !errors.Is(err, ErrPolicyStale) || decision.Outcome != authorization.Deny {
		t.Fatalf("Decide(after clock rollback) = (%+v, %v), want stale deny", decision, err)
	}
}

func TestSynchronizerFailsClosed(t *testing.T) {
	t.Parallel()

	backendError := errors.New("repository unavailable")
	tests := map[string]struct {
		repository *repositoryStub
		compiler   *Compiler
		want       error
	}{
		"repository": {
			repository: &repositoryStub{err: backendError},
			compiler:   synchronizerCompiler(t),
			want:       backendError,
		},
		"stale": {
			repository: &repositoryStub{manifests: []Manifest{synchronizerManifest(1)}},
			compiler:   synchronizerCompiler(t),
			want:       ErrStaleManifest,
		},
		"compile": {
			repository: &repositoryStub{manifests: []Manifest{synchronizerManifest(3)}},
			compiler:   mustCompiler(t, map[Model]Decoder{}),
			want:       ErrMissingDecoder,
		},
		"invalid manifest": {
			repository: &repositoryStub{manifests: []Manifest{{}}},
			compiler:   synchronizerCompiler(t),
			want:       ErrInvalidManifest,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			synchronizer, err := NewSynchronizer(tt.repository, tt.compiler, synchronizerEngine(t, 2))
			if err != nil {
				t.Fatalf("NewSynchronizer() error = %v", err)
			}
			if _, err := synchronizer.Reload(context.Background()); !errors.Is(err, tt.want) {
				t.Errorf("Reload() error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestSynchronizerRejectsCanceledAndConcurrentReplacement(t *testing.T) {
	t.Parallel()

	engine := synchronizerEngine(t, 2)
	repository := &repositoryStub{manifests: []Manifest{synchronizerManifest(3)}}
	compiler := mustCompiler(t, map[Model]Decoder{
		ModelACL: DecoderFunc(func(json.RawMessage) (authorization.Evaluator, error) {
			replacement, err := authorization.NewSnapshot(3, authorization.DenyOverrides)
			if err != nil {
				return nil, err
			}
			if err := engine.ReplaceSnapshot(replacement, 2); err != nil {
				return nil, err
			}
			return fixedEvaluator{}, nil
		}),
	})
	synchronizer, err := NewSynchronizer(repository, compiler, engine)
	if err != nil {
		t.Fatalf("NewSynchronizer() error = %v", err)
	}
	if _, err := synchronizer.Reload(context.Background()); !errors.Is(err, authorization.ErrRevisionConflict) {
		t.Errorf("concurrent Reload() error = %v, want ErrRevisionConflict", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := synchronizer.Reload(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("canceled Reload() error = %v, want context.Canceled", err)
	}
}

func TestSynchronizerObserveVerifiesHintAgainstRepository(t *testing.T) {
	t.Parallel()

	engine := synchronizerEngine(t, 1)
	repository := &repositoryStub{manifests: []Manifest{synchronizerManifest(2)}}
	synchronizer, err := NewSynchronizer(repository, synchronizerCompiler(t), engine)
	if err != nil {
		t.Fatalf("NewSynchronizer() error = %v", err)
	}
	if err := synchronizer.Observe(context.Background(), 1); err != nil {
		t.Errorf("Observe(current) error = %v", err)
	}
	if repository.loads != 0 {
		t.Errorf("Observe(current) loads = %d, want 0", repository.loads)
	}
	if err := synchronizer.Observe(context.Background(), 3); !errors.Is(err, ErrStaleManifest) {
		t.Errorf("Observe(ahead) error = %v, want ErrStaleManifest", err)
	}
	if engine.Revision() != 2 {
		t.Errorf("engine revision = %d, want verified repository revision 2", engine.Revision())
	}

	success := &repositoryStub{manifests: []Manifest{synchronizerManifest(3)}}
	synchronizer, err = NewSynchronizer(success, synchronizerCompiler(t), synchronizerEngine(t, 1))
	if err != nil {
		t.Fatalf("NewSynchronizer() error = %v", err)
	}
	if err := synchronizer.Observe(context.Background(), 2); err != nil {
		t.Errorf("Observe(verified) error = %v", err)
	}

	failed := &repositoryStub{err: errors.New("repository unavailable")}
	synchronizer, err = NewSynchronizer(failed, synchronizerCompiler(t), synchronizerEngine(t, 1))
	if err != nil {
		t.Fatalf("NewSynchronizer() error = %v", err)
	}
	if err := synchronizer.Observe(context.Background(), 2); err == nil {
		t.Error("Observe(repository failure) returned nil")
	}
}

func TestSynchronizerRunPollsSourceOfTruth(t *testing.T) {
	t.Parallel()

	engine := synchronizerEngine(t, 1)
	repository := &repositoryStub{manifests: []Manifest{
		synchronizerManifest(1), synchronizerManifest(2),
	}}
	synchronizer, err := NewSynchronizer(
		repository, synchronizerCompiler(t), engine,
		WithSyncInterval(time.Millisecond),
	)
	if err != nil {
		t.Fatalf("NewSynchronizer() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- synchronizer.Run(ctx) }()
	deadline := time.After(time.Second)
	for engine.Revision() != 2 {
		select {
		case <-deadline:
			t.Fatal("Run() did not poll revision 2")
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Errorf("Run() error = %v, want context.Canceled", err)
	}
}

func TestSynchronizerRunReturnsReloadError(t *testing.T) {
	t.Parallel()

	want := errors.New("repository unavailable")
	synchronizer, err := NewSynchronizer(
		&repositoryStub{err: want}, synchronizerCompiler(t), synchronizerEngine(t, 1),
	)
	if err != nil {
		t.Fatalf("NewSynchronizer() error = %v", err)
	}
	if err := synchronizer.Run(context.Background()); !errors.Is(err, want) {
		t.Errorf("Run() error = %v, want repository error", err)
	}
}

func TestSynchronizerRunReturnsPollingError(t *testing.T) {
	t.Parallel()

	invalid := synchronizerManifest(2)
	invalid.Policies[0].Model = ModelRBAC
	repository := &repositoryStub{manifests: []Manifest{
		synchronizerManifest(1), invalid,
	}}
	synchronizer, err := NewSynchronizer(
		repository, synchronizerCompiler(t), synchronizerEngine(t, 1),
		WithSyncInterval(time.Millisecond),
	)
	if err != nil {
		t.Fatalf("NewSynchronizer() error = %v", err)
	}
	if err := synchronizer.Run(context.Background()); !errors.Is(err, ErrMissingDecoder) {
		t.Errorf("Run() error = %v, want ErrMissingDecoder", err)
	}
}

func TestNewSynchronizerValidatesDependencies(t *testing.T) {
	t.Parallel()

	repository := &repositoryStub{}
	compiler := synchronizerCompiler(t)
	engine := synchronizerEngine(t, 1)
	tests := []struct {
		repository Repository
		compiler   *Compiler
		engine     *authorization.Engine
		options    []SynchronizerOption
		want       error
	}{
		{compiler: compiler, engine: engine, want: ErrNilRepository},
		{repository: repository, engine: engine, want: ErrNilCompiler},
		{repository: repository, compiler: compiler, want: ErrNilEngine},
		{repository: repository, compiler: compiler, engine: engine, options: []SynchronizerOption{WithSyncInterval(-time.Second)}, want: ErrInvalidSyncInterval},
		{repository: repository, compiler: compiler, engine: engine, options: []SynchronizerOption{WithMaxStaleness(0)}, want: ErrInvalidMaxStaleness},
	}
	for _, test := range tests {
		if _, err := NewSynchronizer(test.repository, test.compiler, test.engine, test.options...); !errors.Is(err, test.want) {
			t.Errorf("NewSynchronizer() error = %v, want %v", err, test.want)
		}
	}
}

func synchronizerManifest(revision authorization.Revision) Manifest {
	return Manifest{
		Format: FormatV1, Revision: revision, Algorithm: AlgorithmDenyOverrides,
		Policies: []Record{{
			ID: "policy", Revision: revision, Model: ModelACL,
			Document: []byte(`{}`),
		}},
	}
}

func synchronizerCompiler(t *testing.T) *Compiler {
	t.Helper()
	return mustCompiler(t, map[Model]Decoder{
		ModelACL: DecoderFunc(func(json.RawMessage) (authorization.Evaluator, error) {
			return fixedEvaluator{}, nil
		}),
	})
}

func mustCompiler(t *testing.T, decoders map[Model]Decoder) *Compiler {
	t.Helper()
	compiler, err := NewCompiler(decoders)
	if err != nil {
		t.Fatalf("NewCompiler() error = %v", err)
	}
	return compiler
}

func synchronizerEngine(t *testing.T, revision authorization.Revision) *authorization.Engine {
	t.Helper()
	snapshot, err := authorization.NewSnapshot(revision, authorization.DenyOverrides)
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}
	engine, err := authorization.NewEngine(snapshot)
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}
	return engine
}
