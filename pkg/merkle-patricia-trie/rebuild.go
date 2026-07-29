package mpt

import (
	"context"
	"fmt"
)

// Rebuild reconstructs a fully materialized raw snapshot from ordered entries
// and verifies that its root matches the receiver. The result has no source
// store dependency and may be committed to a different store.
func (trie RawTrie) Rebuild(ctx context.Context) (RawTrie, error) {
	if trie.snapshot == nil {
		return RawTrie{}, ErrUninitialized
	}
	snapshot, err := rebuildSnapshot(ctx, trie.snapshot, false)
	if err != nil {
		return RawTrie{}, err
	}
	return RawTrie{snapshot: snapshot}, nil
}

// Rebuild reconstructs a fully materialized secure snapshot from transformed
// keys and verifies that its root matches the receiver. Key preimages are not
// required or recovered.
func (trie SecureTrie) Rebuild(ctx context.Context) (SecureTrie, error) {
	if trie.snapshot == nil {
		return SecureTrie{}, ErrUninitialized
	}
	snapshot, err := rebuildSnapshot(ctx, trie.snapshot, true)
	if err != nil {
		return SecureTrie{}, err
	}
	return SecureTrie{snapshot: snapshot}, nil
}

func rebuildSnapshot(
	ctx context.Context,
	source *trieSnapshot,
	secure bool,
) (*trieSnapshot, error) {
	var root node
	budget := workBudget{hashesLeft: source.limits.MaxHashOperations}
	buildState := traversalState{
		ctx:       ctx,
		maxDepth:  source.limits.MaxTraversalDepth,
		nodesLeft: source.limits.MaxRebuildNodes,
		readsLeft: source.limits.MaxNodeReads,
		budget:    &budget,
	}
	iterate := func(entry Entry) error {
		path := bytesToNibbles(entry.key)
		var err error
		root, err = insertNode(root, path, entry.value, 0, &buildState)
		return err
	}
	var err error
	if secure {
		err = SecureTrie{snapshot: source}.IterateHashed(
			ctx, IterationOptions{}, iterate,
		)
	} else {
		err = RawTrie{snapshot: source}.Iterate(
			ctx, IterationOptions{}, iterate,
		)
	}
	if err != nil {
		return nil, err
	}
	rebuilt, err := finishSnapshot(
		ctx, root, source.limits, EmptyRoot(), nil, &budget,
	)
	if err != nil {
		return nil, err
	}
	if rebuilt.hash != source.hash {
		return nil, fmt.Errorf(
			"%w: have %x, want %x",
			ErrRootMismatch,
			rebuilt.hash,
			source.hash,
		)
	}
	return rebuilt, nil
}
