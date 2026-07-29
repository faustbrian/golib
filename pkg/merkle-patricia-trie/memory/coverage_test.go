package memory_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	mpt "github.com/faustbrian/golib/pkg/merkle-patricia-trie"
	"github.com/faustbrian/golib/pkg/merkle-patricia-trie/memory"
)

func TestStoreObservesCancellationAtReadAndCommitBoundaries(t *testing.T) {
	t.Parallel()

	capture := newCaptureStore()
	baseTrie := mustMemoryTrie(t, map[string]string{
		"alpha": "a long persisted value for cancellation coverage",
		"beta":  "a second long persisted value for cancellation coverage",
	})
	committed, first := captureTrieCommit(t, baseTrie, capture)

	populated := memory.New()
	if err := populated.CommitTrie(context.Background(), first); err != nil {
		t.Fatalf("initial CommitTrie() error = %v", err)
	}
	root := first.Root()
	if _, err := populated.GetNode(&stepContext{cancelAt: 2}, root); !errors.Is(err, mpt.ErrCanceled) {
		t.Fatalf("GetNode(post-read cancellation) error = %v", err)
	}

	updated, err := committed.Update(
		context.Background(), []byte("alpha"), []byte("replacement"),
	)
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	_, second := captureTrieCommit(t, updated, capture)

	tests := []struct {
		name     string
		cancelAt func(existing int) int
	}{
		{
			name: "node validation",
			cancelAt: func(int) int {
				return 2
			},
		},
		{
			name: "existing node copy",
			cancelAt: func(int) int {
				return 2 + len(second.Nodes())
			},
		},
		{
			name: "before publication lock",
			cancelAt: func(existing int) int {
				return 2 + len(second.Nodes()) + existing
			},
		},
		{
			name: "after publication lock",
			cancelAt: func(existing int) int {
				return 3 + len(second.Nodes()) + existing
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			store := memory.New()
			if err := store.CommitTrie(context.Background(), first); err != nil {
				t.Fatalf("initial CommitTrie() error = %v", err)
			}
			existing := countStoreNodes(t, store)
			ctx := &stepContext{cancelAt: test.cancelAt(existing)}
			if err := store.CommitTrie(ctx, second); !errors.Is(err, mpt.ErrCanceled) {
				t.Fatalf("CommitTrie() error = %v", err)
			}
			if store.Root() != first.Root() {
				t.Fatalf("canceled commit published %x", store.Root())
			}
		})
	}
}

func TestStoreDetectsConcurrentPublication(t *testing.T) {
	t.Parallel()

	capture := newCaptureStore()
	baseTrie := mustMemoryTrie(t, map[string]string{"base": "value"})
	committed, first := captureTrieCommit(t, baseTrie, capture)
	left, err := committed.Update(context.Background(), []byte("left"), []byte("1"))
	if err != nil {
		t.Fatalf("left Update() error = %v", err)
	}
	_, leftCommit := captureTrieCommit(t, left, capture)
	right, err := committed.Update(context.Background(), []byte("right"), []byte("2"))
	if err != nil {
		t.Fatalf("right Update() error = %v", err)
	}
	_, rightCommit := captureTrieCommit(t, right, capture)

	store := memory.New()
	if err := store.CommitTrie(context.Background(), first); err != nil {
		t.Fatalf("initial CommitTrie() error = %v", err)
	}
	existing := countStoreNodes(t, store)
	hookAt := 2 + len(leftCommit.Nodes()) + existing
	ctx := &stepContext{
		hookAt: hookAt,
		hook: func() {
			if err := store.CommitTrie(context.Background(), rightCommit); err != nil {
				t.Errorf("concurrent CommitTrie() error = %v", err)
			}
		},
	}
	if err := store.CommitTrie(ctx, leftCommit); !errors.Is(err, mpt.ErrStaleRoot) {
		t.Fatalf("raced CommitTrie() error = %v", err)
	}
	if store.Root() != rightCommit.Root() {
		t.Fatalf("published root = %x, want concurrent root %x", store.Root(), rightCommit.Root())
	}
}

func TestStoreIterationZeroStateNilStoreAndCancellationBoundaries(t *testing.T) {
	t.Parallel()

	var zero memory.Store
	if err := zero.IterateNodes(
		context.Background(), 1, func(mpt.Root, []byte) error { return nil },
	); err != nil {
		t.Fatalf("zero IterateNodes() error = %v", err)
	}
	var nilStore *memory.Store
	if err := nilStore.IterateNodes(
		context.Background(), 1, func(mpt.Root, []byte) error { return nil },
	); !errors.Is(err, mpt.ErrInvalidStore) {
		t.Fatalf("nil IterateNodes() error = %v", err)
	}

	store := memory.New()
	trie := mustMemoryTrie(t, map[string]string{
		"alpha": "a long persisted value for iteration coverage",
		"beta":  "a second long persisted value for iteration coverage",
	})
	if _, err := trie.Commit(context.Background(), store); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	count := countStoreNodes(t, store)
	if err := store.IterateNodes(
		&stepContext{cancelAt: 2},
		count,
		func(mpt.Root, []byte) error { return nil },
	); !errors.Is(err, mpt.ErrCanceled) {
		t.Fatalf("hash-loop cancellation error = %v", err)
	}
	if err := store.IterateNodes(
		&stepContext{cancelAt: count + 2},
		count,
		func(mpt.Root, []byte) error { return nil },
	); !errors.Is(err, mpt.ErrCanceled) {
		t.Fatalf("yield-loop cancellation error = %v", err)
	}
}

func TestStoreRetentionAndPruningValidateLifecycleAndBounds(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	var zero memory.Store
	lease, err := zero.RetainRoot(
		ctx, mpt.EmptyRoot(), mpt.DefaultReachabilityLimits(),
	)
	if err != nil {
		t.Fatalf("zero RetainRoot(empty) error = %v", err)
	}
	if result, pruneErr := zero.Prune(
		ctx, mpt.DefaultReachabilityLimits(),
	); pruneErr != nil || result.StoredAfter() != 0 {
		t.Fatalf("zero Prune() = (%+v, %v)", result, pruneErr)
	}
	if err := lease.Release(ctx); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
	if err := lease.Release(ctx); !errors.Is(err, mpt.ErrReleasedRetention) {
		t.Fatalf("second Release() error = %v", err)
	}

	var nilStore *memory.Store
	if _, err := nilStore.RetainRoot(
		ctx, mpt.EmptyRoot(), mpt.DefaultReachabilityLimits(),
	); !errors.Is(err, mpt.ErrInvalidStore) {
		t.Fatalf("nil RetainRoot() error = %v", err)
	}
	if _, err := nilStore.Prune(
		ctx, mpt.DefaultReachabilityLimits(),
	); !errors.Is(err, mpt.ErrInvalidStore) {
		t.Fatalf("nil Prune() error = %v", err)
	}
	var nilContext context.Context
	store := memory.New()
	if _, err := store.RetainRoot(
		nilContext, mpt.EmptyRoot(), mpt.DefaultReachabilityLimits(),
	); !errors.Is(err, mpt.ErrInvalidContext) {
		t.Fatalf("RetainRoot(nil context) error = %v", err)
	}
	if _, err := store.Prune(
		nilContext, mpt.DefaultReachabilityLimits(),
	); !errors.Is(err, mpt.ErrInvalidContext) {
		t.Fatalf("Prune(nil context) error = %v", err)
	}
	limits := mpt.DefaultReachabilityLimits()
	limits.MaxRetentions = 1
	first, err := store.RetainRoot(ctx, mpt.EmptyRoot(), limits)
	if err != nil {
		t.Fatalf("bounded RetainRoot(first) error = %v", err)
	}
	if _, err := store.RetainRoot(
		ctx, mpt.EmptyRoot(), limits,
	); !errors.Is(err, mpt.ErrResourceLimit) {
		t.Fatalf("bounded RetainRoot(second) error = %v", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := first.Release(canceled); !errors.Is(err, mpt.ErrCanceled) {
		t.Fatalf("Release(canceled) error = %v", err)
	}
	if err := first.Release(ctx); err != nil {
		t.Fatalf("Release(after cancellation) error = %v", err)
	}
}

func TestStorePruneIsAtomicAcrossRetentionAndPublicationRaces(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := memory.New()
	trie := mustMemoryTrie(t, map[string]string{
		"alpha": "a long persisted value for pruning races",
		"beta":  "another long persisted value for pruning races",
	})
	trie, err := trie.Commit(ctx, store)
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	root, err := trie.Root()
	if err != nil {
		t.Fatalf("Root() error = %v", err)
	}
	lease, err := store.RetainRoot(
		ctx, root, mpt.DefaultReachabilityLimits(),
	)
	if err != nil {
		t.Fatalf("RetainRoot() error = %v", err)
	}
	before := countStoreNodes(t, store)
	raceContext := &stepContext{
		hookAt: 3,
		hook: func() {
			if releaseErr := lease.Release(ctx); releaseErr != nil {
				t.Errorf("racing Release() error = %v", releaseErr)
			}
		},
	}
	if _, err := store.Prune(
		raceContext, mpt.DefaultReachabilityLimits(),
	); !errors.Is(err, mpt.ErrStaleRoot) {
		t.Fatalf("Prune(retention race) error = %v", err)
	}
	if after := countStoreNodes(t, store); after != before {
		t.Fatalf("raced Prune() changed node count from %d to %d", before, after)
	}

	canceled := &stepContext{cancelAt: 3}
	if _, err := store.Prune(
		canceled, mpt.DefaultReachabilityLimits(),
	); !errors.Is(err, mpt.ErrCanceled) {
		t.Fatalf("Prune(canceled) error = %v", err)
	}
	if after := countStoreNodes(t, store); after != before {
		t.Fatalf("canceled Prune() changed node count from %d to %d", before, after)
	}

	updated, err := trie.Update(ctx, []byte("gamma"), []byte("published"))
	if err != nil {
		t.Fatalf("Update(gamma) error = %v", err)
	}
	var published mpt.RawTrie
	publicationContext := &stepContext{
		hookAt: 3,
		hook: func() {
			published, err = updated.Commit(ctx, store)
			if err != nil {
				t.Errorf("racing Commit() error = %v", err)
			}
		},
	}
	if _, pruneErr := store.Prune(
		publicationContext, mpt.DefaultReachabilityLimits(),
	); !errors.Is(pruneErr, mpt.ErrStaleRoot) {
		t.Fatalf("Prune(publication race) error = %v", pruneErr)
	}
	publishedRoot, err := published.Root()
	if err != nil {
		t.Fatalf("published Root() error = %v", err)
	}
	if store.Root() != publishedRoot {
		t.Fatalf("raced Prune() replaced published root %x", publishedRoot)
	}
}

type captureStore struct {
	root    mpt.Root
	nodes   map[mpt.Root][]byte
	commits []mpt.StoreCommit
}

func newCaptureStore() *captureStore {
	return &captureStore{
		root:  mpt.EmptyRoot(),
		nodes: make(map[mpt.Root][]byte),
	}
}

func (store *captureStore) GetNode(_ context.Context, hash mpt.Root) ([]byte, error) {
	encoded, exists := store.nodes[hash]
	if !exists {
		return nil, mpt.ErrMissingNode
	}
	return append([]byte(nil), encoded...), nil
}

func (store *captureStore) CommitTrie(_ context.Context, commit mpt.StoreCommit) error {
	for _, node := range commit.Nodes() {
		store.nodes[node.Hash()] = node.Encoded()
	}
	store.root = commit.Root()
	store.commits = append(store.commits, commit)
	return nil
}

func captureTrieCommit(
	t *testing.T,
	trie mpt.RawTrie,
	store *captureStore,
) (mpt.RawTrie, mpt.StoreCommit) {
	t.Helper()
	committed, err := trie.Commit(context.Background(), store)
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	return committed, store.commits[len(store.commits)-1]
}

func countStoreNodes(t *testing.T, store *memory.Store) int {
	t.Helper()
	count := 0
	err := store.IterateNodes(
		context.Background(),
		1<<20,
		func(mpt.Root, []byte) error {
			count++
			return nil
		},
	)
	if err != nil {
		t.Fatalf("IterateNodes() error = %v", err)
	}
	return count
}

type stepContext struct {
	mutex    sync.Mutex
	calls    int
	cancelAt int
	hookAt   int
	hook     func()
}

func (ctx *stepContext) Deadline() (time.Time, bool) {
	return time.Time{}, false
}

func (ctx *stepContext) Done() <-chan struct{} {
	return nil
}

func (ctx *stepContext) Err() error {
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

func (ctx *stepContext) Value(any) any {
	return nil
}
