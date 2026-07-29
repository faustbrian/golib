package mpt

import (
	"context"
	"fmt"
)

type mutationKind uint8

const (
	mutationPut mutationKind = iota + 1
	mutationRemove
)

// Mutation is one immutable explicit batch operation. Construct mutations with
// Put or Remove; its zero value is invalid.
type Mutation struct {
	kind  mutationKind
	key   []byte
	value []byte
}

// Put constructs a mutation that stores a non-empty value.
func Put(key, value []byte) Mutation {
	return Mutation{
		kind: mutationPut, key: append([]byte(nil), key...),
		value: append([]byte(nil), value...),
	}
}

// Remove constructs a mutation that deletes an existing key.
func Remove(key []byte) Mutation {
	return Mutation{kind: mutationRemove, key: append([]byte(nil), key...)}
}

// ApplyBatch validates every mutation and returns one atomically visible raw
// snapshot. Failure leaves the receiver unchanged.
func (trie RawTrie) ApplyBatch(
	ctx context.Context,
	mutations []Mutation,
) (RawTrie, error) {
	snapshot, err := applyBatch(ctx, trie.snapshot, mutations, false)
	if err != nil {
		return RawTrie{}, err
	}
	return RawTrie{snapshot: snapshot}, nil
}

// ApplyBatch validates every mutation and returns one atomically visible secure
// snapshot. Failure leaves the receiver unchanged.
func (trie SecureTrie) ApplyBatch(
	ctx context.Context,
	mutations []Mutation,
) (SecureTrie, error) {
	snapshot, err := applyBatch(ctx, trie.snapshot, mutations, true)
	if err != nil {
		return SecureTrie{}, err
	}
	return SecureTrie{snapshot: snapshot}, nil
}

func applyBatch(
	ctx context.Context,
	snapshot *trieSnapshot,
	mutations []Mutation,
	secure bool,
) (*trieSnapshot, error) {
	if snapshot == nil {
		return nil, ErrUninitialized
	}
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if len(mutations) > snapshot.limits.MaxBatchOperations {
		return nil, fmt.Errorf("%w: batch operation bound exceeded", ErrResourceLimit)
	}
	if len(mutations) == 0 {
		return snapshot, nil
	}

	seen := make(map[string]struct{}, len(mutations))
	for _, mutation := range mutations {
		switch mutation.kind {
		case mutationPut:
			if len(mutation.value) == 0 {
				return nil, fmt.Errorf("%w: empty put value", ErrInvalidValue)
			}
			if err := validateOperation(
				ctx, snapshot, mutation.key, mutation.value, true,
			); err != nil {
				return nil, err
			}
		case mutationRemove:
			if err := validateOperation(
				ctx, snapshot, mutation.key, nil, false,
			); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("%w: unknown mutation kind", ErrInvalidBatch)
		}
		identity := string(mutation.key)
		if _, duplicate := seen[identity]; duplicate {
			return nil, fmt.Errorf("%w: repeated key", ErrDuplicateBatchKey)
		}
		seen[identity] = struct{}{}
	}

	budget := workBudget{hashesLeft: snapshot.limits.MaxHashOperations}
	state := traversalState{
		ctx:       ctx,
		maxDepth:  snapshot.limits.MaxTraversalDepth,
		nodesLeft: snapshot.limits.MaxTraversalNodes,
		readsLeft: snapshot.limits.MaxNodeReads,
		reader:    snapshot.reader,
		pending:   snapshot.pending,
		parent:    snapshot.parent,
		removed:   snapshot.removed,
		budget:    &budget,
		resolved:  make(map[Root]struct{}),
	}
	root := snapshot.root
	readRoot := snapshot.readRoot
	for _, mutation := range mutations {
		path, err := keyPath(mutation.key, secure, &budget)
		if err != nil {
			return nil, err
		}
		switch mutation.kind {
		case mutationPut:
			root, err = insertNode(root, path, mutation.value, 0, &state)
			if err != nil {
				return nil, err
			}
			if snapshot.materialized {
				readRoot, err = insertReadRoot(
					readRoot,
					path,
					mutation.value,
					false,
					&state,
				)
			}
		case mutationRemove:
			var deleted bool
			root, deleted, err = deleteNode(root, path, 0, &state)
			if err != nil {
				return nil, err
			}
			if !deleted {
				return nil, ErrAbsentKey
			}
			if snapshot.materialized {
				readRoot, err = insertReadRoot(
					readRoot,
					path,
					nil,
					true,
					&state,
				)
			}
		}
		if err != nil {
			return nil, err
		}
	}
	finished, err := finishMutatedSnapshot(
		ctx,
		root,
		readRoot,
		snapshot,
		state.resolved,
		&budget,
	)
	if err != nil {
		return nil, err
	}
	return inheritRecovery(finished, snapshot), nil
}
