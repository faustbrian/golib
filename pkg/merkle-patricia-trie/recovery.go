package mpt

import (
	"bytes"
	"context"
	"fmt"
)

// RecoverNode copies, hash-checks, and canonically decodes one node retrieved
// after a MissingNodeError, then returns a new raw snapshot that consults the
// bounded recovery overlay before its backing reader. The receiver is
// unchanged. Commit atomically persists recovered nodes to the source store.
func (trie RawTrie) RecoverNode(
	ctx context.Context,
	hash Root,
	encoded []byte,
) (RawTrie, error) {
	snapshot, err := recoverSnapshot(ctx, trie.snapshot, hash, encoded)
	if err != nil {
		return RawTrie{}, err
	}
	return RawTrie{snapshot: snapshot}, nil
}

// RecoverNode copies, hash-checks, and canonically decodes one node retrieved
// after a MissingNodeError, then returns a new secure snapshot that consults
// the bounded recovery overlay before its backing reader. The receiver is
// unchanged. Commit atomically persists recovered nodes to the source store.
func (trie SecureTrie) RecoverNode(
	ctx context.Context,
	hash Root,
	encoded []byte,
) (SecureTrie, error) {
	snapshot, err := recoverSnapshot(ctx, trie.snapshot, hash, encoded)
	if err != nil {
		return SecureTrie{}, err
	}
	return SecureTrie{snapshot: snapshot}, nil
}

func recoverSnapshot(
	ctx context.Context,
	snapshot *trieSnapshot,
	hash Root,
	encoded []byte,
) (*trieSnapshot, error) {
	if snapshot == nil {
		return nil, ErrUninitialized
	}
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if !validStore(snapshot.reader) {
		return nil, fmt.Errorf("%w: recovery requires a backing reader", ErrInvalidStore)
	}
	if existing, exists := lookupSnapshotPending(snapshot, hash); exists {
		if bytes.Equal(existing, encoded) {
			return snapshot, nil
		}
		return nil, &CorruptNodeError{
			Hash:  hash,
			Cause: fmt.Errorf("%w: conflicting recovered bytes", ErrCorruptNode),
		}
	}
	if snapshot.recoveryNodes == snapshot.limits.MaxRecoveryNodes {
		return nil, fmt.Errorf("%w: recovery node bound exceeded", ErrResourceLimit)
	}
	if len(encoded) > snapshot.limits.MaxRecoveryBytes-snapshot.recoveryBytes {
		return nil, fmt.Errorf("%w: recovery byte bound exceeded", ErrResourceLimit)
	}

	owned := append([]byte(nil), encoded...)
	budget := workBudget{hashesLeft: snapshot.limits.MaxHashOperations}
	actual, err := budget.hash(owned)
	if err != nil {
		return nil, err
	}
	if actual != hash {
		return nil, &CorruptNodeError{
			Hash:  hash,
			Cause: fmt.Errorf("%w: recovered node hash mismatch", ErrCorruptNode),
		}
	}
	decoded, err := decodeNode(owned)
	if err != nil || decoded == nil {
		return nil, fmt.Errorf("%w: invalid recovered node", ErrMalformedNode)
	}
	if err := checkContext(ctx); err != nil {
		return nil, err
	}

	recoveredNodes := make(map[Root][]byte)
	mergePersisted(recoveredNodes, snapshot.recovered)
	recoveredNodes[hash] = append([]byte(nil), owned...)
	recovered := *snapshot
	recovered.pending = map[Root][]byte{hash: owned}
	recovered.parent = snapshotPendingLayer(snapshot)
	recovered.removed = nil
	if recovered.parent != nil &&
		recovered.parent.depth >= maximumPendingLayerDepth {
		compacted := materializePendingLayer(recovered.parent)
		compacted[hash] = owned
		recovered.pending = compacted
		recovered.parent = nil
	}
	recovered.recovered = recoveredNodes
	recovered.recoveryNodes++
	recovered.recoveryBytes += len(owned)
	return &recovered, nil
}

func inheritRecovery(next, previous *trieSnapshot) *trieSnapshot {
	if next.root == nil || len(previous.recovered) == 0 {
		return next
	}
	next.recovered = make(map[Root][]byte, len(previous.recovered))
	recoveryBytes := 0
	for hash, encoded := range previous.recovered {
		if _, reachable := lookupSnapshotPending(next, hash); reachable {
			owned := append([]byte(nil), encoded...)
			next.pending[hash] = owned
			next.recovered[hash] = append([]byte(nil), owned...)
			recoveryBytes = recoveryBytes + len(owned)
		}
	}
	next.recoveryNodes = len(next.recovered)
	next.recoveryBytes = recoveryBytes
	return next
}
