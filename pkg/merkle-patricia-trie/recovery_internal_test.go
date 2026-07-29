package mpt

import (
	"context"
	"errors"
	"testing"
)

func TestRecoverSnapshotHonorsHashAndPostValidationCancellation(t *testing.T) {
	t.Parallel()

	encoded, _, err := encodeNode(
		&leafNode{path: nil, value: []byte("value")},
	)
	if err != nil {
		t.Fatalf("encodeNode() error = %v", err)
	}
	hash := keccakRoot(encoded)
	reader := nodeReaderFunc(func(context.Context, Root) ([]byte, error) {
		return nil, ErrMissingNode
	})
	exactLimits := DefaultLimits()
	exactLimits.MaxRecoveryBytes = len(encoded)
	if _, err := recoverSnapshot(
		context.Background(),
		&trieSnapshot{limits: exactLimits, reader: reader},
		hash,
		encoded,
	); err != nil {
		t.Fatalf("recoverSnapshot(exact byte bound) error = %v", err)
	}

	limits := DefaultLimits()
	limits.MaxHashOperations = 0
	if _, err := recoverSnapshot(
		context.Background(),
		&trieSnapshot{limits: limits, reader: reader},
		hash,
		encoded,
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("recoverSnapshot(hash limit) error = %v", err)
	}

	ctx := &nthErrorContext{at: 2}
	recovered, err := recoverSnapshot(
		ctx,
		&trieSnapshot{limits: DefaultLimits(), reader: reader},
		hash,
		encoded,
	)
	if !errors.Is(err, ErrCanceled) {
		t.Fatalf("recoverSnapshot(post-validation cancellation) error = %v", err)
	}
	if recovered != nil {
		t.Fatal("canceled recovery returned a snapshot")
	}

	recovered, err = recoverSnapshot(
		context.Background(),
		&trieSnapshot{limits: DefaultLimits(), reader: reader},
		hash,
		encoded,
	)
	if err != nil {
		t.Fatalf("recoverSnapshot() error = %v", err)
	}
	if recovered.recoveryNodes != 1 {
		t.Fatalf("recovery node count = %d, want 1", recovered.recoveryNodes)
	}
	if recovered.recoveryBytes != len(encoded) {
		t.Fatalf(
			"recovery byte count = %d, want %d",
			recovered.recoveryBytes,
			len(encoded),
		)
	}

	secondEncoded, _, err := encodeNode(
		&leafNode{path: nil, value: []byte("other value")},
	)
	if err != nil {
		t.Fatalf("encodeNode(second) error = %v", err)
	}
	recovered, err = recoverSnapshot(
		context.Background(),
		recovered,
		keccakRoot(secondEncoded),
		secondEncoded,
	)
	if err != nil {
		t.Fatalf("recoverSnapshot(second) error = %v", err)
	}
	if recovered.recoveryNodes != 2 {
		t.Fatalf("aggregate recovery node count = %d, want 2", recovered.recoveryNodes)
	}
	if recovered.recoveryBytes != len(encoded)+len(secondEncoded) {
		t.Fatalf(
			"aggregate recovery byte count = %d, want %d",
			recovered.recoveryBytes,
			len(encoded)+len(secondEncoded),
		)
	}
}

func TestPendingInheritancePropagatesFinalizationFailures(t *testing.T) {
	t.Parallel()

	trie, err := NewRawTrie(DefaultLimits())
	if err != nil {
		t.Fatalf("NewRawTrie() error = %v", err)
	}
	trie, err = trie.Update(context.Background(), []byte("a"), []byte("one"))
	if err != nil {
		t.Fatalf("Update(a) error = %v", err)
	}
	trie, err = trie.Update(context.Background(), []byte("b"), []byte("two"))
	if err != nil {
		t.Fatalf("Update(b) error = %v", err)
	}
	constrained := *trie.snapshot
	constrained.limits.MaxHashOperations = 0
	if _, err := deleteSnapshot(
		context.Background(), &constrained, []byte("a"), false,
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("deleteSnapshot(finalization limit) error = %v", err)
	}

	empty, err := NewRawTrie(DefaultLimits())
	if err != nil {
		t.Fatalf("NewRawTrie(empty) error = %v", err)
	}
	constrained = *empty.snapshot
	constrained.limits.MaxHashOperations = 0
	if _, err := applyBatch(
		context.Background(),
		&constrained,
		[]Mutation{Put([]byte("key"), []byte("value"))},
		false,
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("applyBatch(finalization limit) error = %v", err)
	}
}

func TestOrdinaryUpdatesDoNotRetainSupersededCommitNodes(t *testing.T) {
	t.Parallel()

	trie, err := NewRawTrie(DefaultLimits())
	if err != nil {
		t.Fatalf("NewRawTrie() error = %v", err)
	}
	trie, err = trie.Update(
		context.Background(), []byte("key"), []byte("first value"),
	)
	if err != nil {
		t.Fatalf("Update(first) error = %v", err)
	}
	firstRoot := trie.snapshot.hash
	trie, err = trie.Update(
		context.Background(), []byte("key"), []byte("replacement value"),
	)
	if err != nil {
		t.Fatalf("Update(replacement) error = %v", err)
	}
	if _, retained := trie.snapshot.pending[firstRoot]; retained {
		t.Fatalf("replacement retained superseded root %x", firstRoot)
	}
}

func TestDeletingFinalRecoveredKeyDiscardsUnreachableOverlay(t *testing.T) {
	t.Parallel()

	trie, err := NewRawTrie(DefaultLimits())
	if err != nil {
		t.Fatalf("NewRawTrie() error = %v", err)
	}
	trie, err = trie.Update(
		context.Background(), []byte("key"), []byte("value"),
	)
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	encoded := append([]byte(nil), trie.snapshot.pending[trie.snapshot.hash]...)
	reader := nodeReaderFunc(func(context.Context, Root) ([]byte, error) {
		return nil, ErrMissingNode
	})
	loaded, err := LoadRawTrie(trie.snapshot.hash, reader, DefaultLimits())
	if err != nil {
		t.Fatalf("LoadRawTrie() error = %v", err)
	}
	recovered, err := loaded.RecoverNode(
		context.Background(), trie.snapshot.hash, encoded,
	)
	if err != nil {
		t.Fatalf("RecoverNode() error = %v", err)
	}
	deleted, err := recovered.Delete(context.Background(), []byte("key"))
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if len(deleted.snapshot.recovered) != 0 || len(deleted.snapshot.pending) != 0 {
		t.Fatalf(
			"empty snapshot retained recovery state: recovered=%d pending=%d",
			len(deleted.snapshot.recovered),
			len(deleted.snapshot.pending),
		)
	}
}
