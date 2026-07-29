package memory

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	mpt "github.com/faustbrian/golib/pkg/merkle-patricia-trie"
)

func TestInternalRetentionNilAndDetachedLifecycle(t *testing.T) {
	t.Parallel()

	var retention *rootRetention
	if retention.Root() != (mpt.Root{}) {
		t.Fatalf("nil retention root = %x", retention.Root())
	}
	if err := retention.Release(context.Background()); !errors.Is(
		err, mpt.ErrReleasedRetention,
	) {
		t.Fatalf("nil retention Release() error = %v", err)
	}
	detached := &rootRetention{}
	if err := detached.Release(context.Background()); !errors.Is(
		err, mpt.ErrReleasedRetention,
	) {
		t.Fatalf("detached retention Release() error = %v", err)
	}
	store := New()
	missing := &rootRetention{store: store}
	if err := missing.Release(context.Background()); !errors.Is(
		err, mpt.ErrReleasedRetention,
	) {
		t.Fatalf("missing retention Release() error = %v", err)
	}
	store.retained = nil
	if err := missing.Release(context.Background()); !errors.Is(
		err, mpt.ErrReleasedRetention,
	) {
		t.Fatalf("nil-state retention Release() error = %v", err)
	}
	lease, err := store.RetainRoot(
		context.Background(),
		mpt.EmptyRoot(),
		mpt.DefaultReachabilityLimits(),
	)
	if err != nil {
		t.Fatalf("RetainRoot() error = %v", err)
	}
	if err := lease.Release(
		&internalStepContext{cancelAt: 2},
	); !errors.Is(err, mpt.ErrCanceled) {
		t.Fatalf("Release(post-lock cancellation) error = %v", err)
	}
	if err := lease.Release(context.Background()); err != nil {
		t.Fatalf("Release(after cancellation) error = %v", err)
	}
}

func TestInternalStateReaderAndUniqueRoots(t *testing.T) {
	t.Parallel()

	var root mpt.Root
	root[0] = 1
	encoded := []byte{1, 2, 3}
	reader := stateReader{state: &storeState{
		nodes: map[mpt.Root][]byte{root: encoded},
	}}
	got, err := reader.GetNode(context.Background(), root)
	if err != nil {
		t.Fatalf("GetNode() error = %v", err)
	}
	got[0] = 9
	if reader.state.nodes[root][0] == 9 {
		t.Fatal("stateReader returned aliased bytes")
	}
	var unknown mpt.Root
	if _, err := reader.GetNode(
		context.Background(), unknown,
	); !errors.Is(err, mpt.ErrMissingNode) {
		t.Fatalf("GetNode(missing) error = %v", err)
	}
	if _, err := (stateReader{}).GetNode(
		context.Background(), unknown,
	); !errors.Is(err, mpt.ErrMissingNode) {
		t.Fatalf("zero GetNode() error = %v", err)
	}
	var nilContext context.Context
	if _, err := reader.GetNode(
		nilContext, root,
	); !errors.Is(err, mpt.ErrInvalidContext) {
		t.Fatalf("GetNode(nil context) error = %v", err)
	}
	roots := uniqueRoots([]mpt.Root{root, root, unknown})
	if len(roots) != 2 || roots[0] != root || roots[1] != unknown {
		t.Fatalf("uniqueRoots() = %x", roots)
	}
}

func TestInternalRetentionAndPrunePostValidationBoundaries(t *testing.T) {
	t.Parallel()

	var zero Store
	if _, err := zero.Prune(
		&internalStepContext{cancelAt: 3},
		mpt.DefaultReachabilityLimits(),
	); !errors.Is(err, mpt.ErrCanceled) {
		t.Fatalf("Prune(post-mark cancellation) error = %v", err)
	}
	if _, err := zero.Prune(
		&internalStepContext{cancelAt: 4},
		mpt.DefaultReachabilityLimits(),
	); !errors.Is(err, mpt.ErrCanceled) {
		t.Fatalf("Prune(post-lock cancellation) error = %v", err)
	}
	if _, err := zero.RetainRoot(
		&internalStepContext{cancelAt: 3},
		mpt.EmptyRoot(),
		mpt.DefaultReachabilityLimits(),
	); !errors.Is(err, mpt.ErrCanceled) {
		t.Fatalf("RetainRoot(post-validation cancellation) error = %v", err)
	}

	store := New()
	raceContext := &internalStepContext{
		hookAt: 2,
		hook: func() {
			if _, err := store.Prune(
				context.Background(), mpt.DefaultReachabilityLimits(),
			); err != nil {
				t.Errorf("racing Prune() error = %v", err)
			}
		},
	}
	if _, err := store.RetainRoot(
		raceContext,
		mpt.EmptyRoot(),
		mpt.DefaultReachabilityLimits(),
	); !errors.Is(err, mpt.ErrStaleRoot) {
		t.Fatalf("RetainRoot(publication race) error = %v", err)
	}
}

type internalStepContext struct {
	mutex    sync.Mutex
	calls    int
	cancelAt int
	hookAt   int
	hook     func()
}

func (ctx *internalStepContext) Deadline() (time.Time, bool) {
	return time.Time{}, false
}

func (ctx *internalStepContext) Done() <-chan struct{} {
	return nil
}

func (ctx *internalStepContext) Err() error {
	ctx.mutex.Lock()
	defer ctx.mutex.Unlock()
	ctx.calls++
	if ctx.hookAt != 0 && ctx.calls == ctx.hookAt {
		ctx.hook()
	}
	if ctx.cancelAt != 0 && ctx.calls >= ctx.cancelAt {
		return context.Canceled
	}
	return nil
}

func (*internalStepContext) Value(any) any {
	return nil
}
