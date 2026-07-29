// Package memory provides an atomic in-memory node store for MPT snapshots.
package memory

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"sync"

	mpt "github.com/faustbrian/golib/pkg/merkle-patricia-trie"
)

// Store retains immutable encoded nodes and publishes one root at a time.
// Concurrent reads are safe. Commits use compare-and-swap root semantics.
type Store struct {
	mutex    sync.RWMutex
	state    *storeState
	retained *retainedRoots
}

type storeState struct {
	root  mpt.Root
	nodes map[mpt.Root][]byte
}

type retainedRoots struct {
	leases map[*rootRetention]mpt.Root
}

// New constructs an empty store whose published root is the canonical empty
// trie root.
func New() *Store {
	return &Store{
		state: &storeState{
			root:  mpt.EmptyRoot(),
			nodes: make(map[mpt.Root][]byte),
		},
		retained: &retainedRoots{leases: make(map[*rootRetention]mpt.Root)},
	}
}

// Root returns the currently published root.
func (store *Store) Root() mpt.Root {
	if store == nil {
		return mpt.Root{}
	}
	store.mutex.RLock()
	state := store.state
	store.mutex.RUnlock()
	if state == nil {
		return mpt.EmptyRoot()
	}
	return state.root
}

// GetNode returns an owned copy of the canonical node stored under hash.
func (store *Store) GetNode(ctx context.Context, hash mpt.Root) ([]byte, error) {
	if store == nil {
		return nil, mpt.ErrInvalidStore
	}
	if err := checkContext(ctx); err != nil {
		return nil, err
	}

	store.mutex.RLock()
	state := store.state
	var encoded []byte
	var exists bool
	if state != nil {
		encoded, exists = state.nodes[hash]
	}
	if exists {
		encoded = append([]byte(nil), encoded...)
	}
	store.mutex.RUnlock()

	if !exists {
		return nil, &mpt.MissingNodeError{Hash: hash, Cause: mpt.ErrMissingNode}
	}
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	return encoded, nil
}

// CommitTrie atomically persists all supplied nodes and publishes the new root
// only when the store still exposes the commit's previous root.
func (store *Store) CommitTrie(ctx context.Context, commit mpt.StoreCommit) error {
	if store == nil {
		return mpt.ErrInvalidStore
	}
	if err := checkContext(ctx); err != nil {
		return err
	}

	nodes := commit.Nodes()
	validated := make(map[mpt.Root][]byte, len(nodes))
	for _, stored := range nodes {
		if err := checkContext(ctx); err != nil {
			return err
		}
		validated[stored.Hash()] = stored.Encoded()
	}

	store.mutex.RLock()
	base := store.state
	store.mutex.RUnlock()
	baseRoot := mpt.EmptyRoot()
	var baseNodes map[mpt.Root][]byte
	if base != nil {
		baseRoot = base.root
		baseNodes = base.nodes
	}
	if baseRoot != commit.PreviousRoot() {
		return mpt.ErrStaleRoot
	}

	next := make(map[mpt.Root][]byte)
	for hash, encoded := range baseNodes {
		if err := checkContext(ctx); err != nil {
			return err
		}
		next[hash] = encoded
	}
	for hash, encoded := range validated {
		next[hash] = encoded
	}
	if err := checkContext(ctx); err != nil {
		return err
	}

	store.mutex.Lock()
	defer store.mutex.Unlock()
	if err := checkContext(ctx); err != nil {
		return err
	}
	if store.state != base {
		return mpt.ErrStaleRoot
	}
	store.state = &storeState{root: commit.Root(), nodes: next}
	return nil
}

// IterateNodes visits an immutable store snapshot in ascending hash order.
func (store *Store) IterateNodes(
	ctx context.Context,
	maximum int,
	yield func(hash mpt.Root, encoded []byte) error,
) error {
	if store == nil {
		return mpt.ErrInvalidStore
	}
	if err := checkContext(ctx); err != nil {
		return err
	}
	if maximum <= 0 || yield == nil {
		return mpt.ErrInvalidIterator
	}

	store.mutex.RLock()
	state := store.state
	store.mutex.RUnlock()
	if state == nil {
		return nil
	}
	if len(state.nodes) > maximum {
		return fmt.Errorf("%w: node iteration bound exceeded", mpt.ErrResourceLimit)
	}

	hashes := make([]mpt.Root, 0, len(state.nodes))
	for hash := range state.nodes {
		if err := checkContext(ctx); err != nil {
			return err
		}
		hashes = append(hashes, hash)
	}
	slices.SortFunc(hashes, func(left, right mpt.Root) int {
		return bytes.Compare(left[:], right[:])
	})
	for _, hash := range hashes {
		if err := checkContext(ctx); err != nil {
			return err
		}
		if err := yield(hash, append([]byte(nil), state.nodes[hash]...)); err != nil {
			return err
		}
	}
	return nil
}

// RetainRoot validates a complete historical root and returns a lease that
// keeps its reachable nodes eligible during pruning.
func (store *Store) RetainRoot(
	ctx context.Context,
	root mpt.Root,
	limits mpt.ReachabilityLimits,
) (mpt.RootRetention, error) {
	if store == nil {
		return nil, mpt.ErrInvalidStore
	}
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	store.mutex.RLock()
	base := store.state
	store.mutex.RUnlock()
	reader := stateReader{state: base}
	if _, err := mpt.CollectReachableNodes(
		ctx, []mpt.Root{root}, reader, limits,
	); err != nil {
		return nil, err
	}

	store.mutex.Lock()
	defer store.mutex.Unlock()
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if store.state != base {
		return nil, mpt.ErrStaleRoot
	}
	retained := store.retained
	if retained == nil {
		retained = &retainedRoots{leases: make(map[*rootRetention]mpt.Root)}
	}
	if len(retained.leases) == limits.MaxRetentions {
		return nil, fmt.Errorf("%w: retained root bound exceeded", mpt.ErrResourceLimit)
	}
	lease := &rootRetention{store: store, root: root}
	leases := make(map[*rootRetention]mpt.Root, len(retained.leases)+1)
	for existing, retainedRoot := range retained.leases {
		leases[existing] = retainedRoot
	}
	leases[lease] = root
	store.retained = &retainedRoots{leases: leases}
	return lease, nil
}

// Prune atomically removes nodes unreachable from the published root and all
// active historical-root leases.
func (store *Store) Prune(
	ctx context.Context,
	limits mpt.ReachabilityLimits,
) (mpt.PruneResult, error) {
	if store == nil {
		return mpt.PruneResult{}, mpt.ErrInvalidStore
	}
	if err := checkContext(ctx); err != nil {
		return mpt.PruneResult{}, err
	}
	store.mutex.RLock()
	base := store.state
	retained := store.retained
	retentionCount := 0
	if retained != nil {
		retentionCount = len(retained.leases)
	}
	roots := make([]mpt.Root, 0, retentionCount+1)
	if base == nil {
		roots = append(roots, mpt.EmptyRoot())
	} else {
		roots = append(roots, base.root)
	}
	if retained != nil {
		for _, root := range retained.leases {
			roots = append(roots, root)
		}
	}
	store.mutex.RUnlock()
	roots = uniqueRoots(roots)

	reachable, err := mpt.CollectReachableNodes(
		ctx, roots, stateReader{state: base}, limits,
	)
	if err != nil {
		return mpt.PruneResult{}, err
	}
	next := make(map[mpt.Root][]byte, len(reachable))
	for _, stored := range reachable {
		next[stored.Hash()] = stored.Encoded()
	}
	before := 0
	var removedBytes uint64
	if base != nil {
		before = len(base.nodes)
		for hash, encoded := range base.nodes {
			if _, retained := next[hash]; !retained {
				removedBytes += uint64(len(encoded))
			}
		}
	}
	if err := checkContext(ctx); err != nil {
		return mpt.PruneResult{}, err
	}

	store.mutex.Lock()
	defer store.mutex.Unlock()
	if err := checkContext(ctx); err != nil {
		return mpt.PruneResult{}, err
	}
	if store.state != base || store.retained != retained {
		return mpt.PruneResult{}, mpt.ErrStaleRoot
	}
	root := mpt.EmptyRoot()
	if base != nil {
		root = base.root
	}
	store.state = &storeState{root: root, nodes: next}
	return mpt.NewPruneResult(before, len(next), removedBytes), nil
}

type stateReader struct {
	state *storeState
}

func uniqueRoots(roots []mpt.Root) []mpt.Root {
	unique := make([]mpt.Root, 0, len(roots))
	seen := make(map[mpt.Root]struct{}, len(roots))
	for _, root := range roots {
		if _, exists := seen[root]; exists {
			continue
		}
		seen[root] = struct{}{}
		unique = append(unique, root)
	}
	return unique
}

func (reader stateReader) GetNode(
	ctx context.Context,
	hash mpt.Root,
) ([]byte, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if reader.state != nil {
		if encoded, exists := reader.state.nodes[hash]; exists {
			return append([]byte(nil), encoded...), nil
		}
	}
	return nil, &mpt.MissingNodeError{Hash: hash, Cause: mpt.ErrMissingNode}
}

type rootRetention struct {
	store *Store
	root  mpt.Root
}

func (retention *rootRetention) Root() mpt.Root {
	if retention == nil {
		return mpt.Root{}
	}
	return retention.root
}

func (retention *rootRetention) Release(ctx context.Context) error {
	if retention == nil || retention.store == nil {
		return mpt.ErrReleasedRetention
	}
	if err := checkContext(ctx); err != nil {
		return err
	}
	retention.store.mutex.Lock()
	defer retention.store.mutex.Unlock()
	if err := checkContext(ctx); err != nil {
		return err
	}
	retained := retention.store.retained
	if retained == nil {
		return mpt.ErrReleasedRetention
	}
	if _, exists := retained.leases[retention]; !exists {
		return mpt.ErrReleasedRetention
	}
	leases := make(map[*rootRetention]mpt.Root, len(retained.leases)-1)
	for existing, root := range retained.leases {
		if existing != retention {
			leases[existing] = root
		}
	}
	retention.store.retained = &retainedRoots{leases: leases}
	return nil
}

func checkContext(ctx context.Context) error {
	if ctx == nil {
		return mpt.ErrInvalidContext
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%w: %w", mpt.ErrCanceled, err)
	}
	return nil
}
