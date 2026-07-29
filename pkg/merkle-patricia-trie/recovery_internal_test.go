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

func TestRecoveryPendingLayersRemainBounded(t *testing.T) {
	t.Parallel()

	reader := nodeReaderFunc(func(context.Context, Root) ([]byte, error) {
		return nil, ErrMissingNode
	})
	snapshot := &trieSnapshot{
		limits: DefaultLimits(),
		reader: reader,
	}
	for index := range maximumPendingLayerDepth + 1 {
		encoded, _, err := encodeNode(
			&leafNode{
				path:  []byte{byte(index >> 4), byte(index & 0x0f)},
				value: []byte{byte(index + 1)},
			},
		)
		if err != nil {
			t.Fatalf("encodeNode(%d) error = %v", index, err)
		}
		snapshot, err = recoverSnapshot(
			context.Background(),
			snapshot,
			keccakRoot(encoded),
			encoded,
		)
		if err != nil {
			t.Fatalf("recoverSnapshot(%d) error = %v", index, err)
		}
		if index == maximumPendingLayerDepth-1 {
			if snapshot.parent == nil ||
				snapshot.parent.depth != maximumPendingLayerDepth-1 {
				t.Fatalf(
					"pre-boundary recovery layer = %#v, want depth %d",
					snapshot.parent,
					maximumPendingLayerDepth-1,
				)
			}
		}
	}
	if snapshot.parent != nil {
		t.Fatalf("boundary recovery retained parent layer %#v", snapshot.parent)
	}
	if got := len(materializeSnapshotPending(snapshot)); got != maximumPendingLayerDepth+1 {
		t.Fatalf("recovered pending nodes = %d, want %d", got, maximumPendingLayerDepth+1)
	}
}

func TestRecoveryInheritanceRetainsOnlyReachableNodesAndExactLimits(t *testing.T) {
	t.Parallel()

	firstEncoded, _, err := encodeNode(
		&leafNode{path: []byte{1}, value: []byte("first")},
	)
	if err != nil {
		t.Fatalf("encodeNode(first) error = %v", err)
	}
	secondEncoded, _, err := encodeNode(
		&leafNode{path: []byte{2}, value: []byte("second")},
	)
	if err != nil {
		t.Fatalf("encodeNode(second) error = %v", err)
	}
	firstHash := keccakRoot(firstEncoded)
	secondHash := keccakRoot(secondEncoded)
	thirdEncoded, _, err := encodeNode(
		&leafNode{path: []byte{3}, value: []byte("third value")},
	)
	if err != nil {
		t.Fatalf("encodeNode(third) error = %v", err)
	}
	thirdHash := keccakRoot(thirdEncoded)
	previous := &trieSnapshot{
		recovered: map[Root][]byte{
			firstHash:  firstEncoded,
			secondHash: secondEncoded,
			thirdHash:  thirdEncoded,
		},
	}
	next := &trieSnapshot{
		root: &leafNode{path: []byte{1}, value: []byte("first")},
		pending: map[Root][]byte{
			firstHash:  firstEncoded,
			secondHash: secondEncoded,
		},
	}
	inherited := inheritRecovery(next, previous)
	if inherited != next {
		t.Fatal("inheritRecovery() replaced the next snapshot")
	}
	if len(inherited.recovered) != 2 {
		t.Fatalf("inherited recovery nodes = %d, want 2", len(inherited.recovered))
	}
	if _, exists := inherited.recovered[firstHash]; !exists {
		t.Fatal("inheritRecovery() discarded reachable node")
	}
	if _, exists := inherited.recovered[secondHash]; !exists {
		t.Fatal("inheritRecovery() discarded second reachable node")
	}
	if _, exists := inherited.recovered[thirdHash]; exists {
		t.Fatal("inheritRecovery() retained unreachable node")
	}
	if inherited.recoveryNodes != 2 {
		t.Fatalf("inherited recovery count = %d, want 2", inherited.recoveryNodes)
	}
	wantBytes := len(firstEncoded) + len(secondEncoded)
	if inherited.recoveryBytes != wantBytes {
		t.Fatalf(
			"inherited recovery bytes = %d, want %d",
			inherited.recoveryBytes,
			wantBytes,
		)
	}

	emptyRecovery := &trieSnapshot{}
	unchanged := &trieSnapshot{
		root: &leafNode{path: nil, value: []byte("value")},
	}
	if got := inheritRecovery(unchanged, emptyRecovery); got != unchanged ||
		got.recovered != nil {
		t.Fatal("empty recovery inheritance changed the snapshot")
	}
	emptyRoot := &trieSnapshot{}
	if got := inheritRecovery(emptyRoot, previous); got != emptyRoot ||
		got.recovered != nil {
		t.Fatal("empty-root recovery inheritance changed the snapshot")
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

	long := []byte("a value long enough to require a hashed child reference")
	trie, err = NewRawTrie(DefaultLimits())
	if err != nil {
		t.Fatalf("NewRawTrie(branch) error = %v", err)
	}
	trie, err = trie.Update(context.Background(), []byte{0x10}, long)
	if err != nil {
		t.Fatalf("Update(10) error = %v", err)
	}
	trie, err = trie.Update(context.Background(), []byte{0x20}, long)
	if err != nil {
		t.Fatalf("Update(20) error = %v", err)
	}
	branch, ok := trie.snapshot.root.(*branchNode)
	if !ok {
		t.Fatalf("root type = %T, want *branchNode", trie.snapshot.root)
	}
	superseded, ok := branch.children[1].(hashNode)
	if !ok {
		t.Fatalf("child type = %T, want hashNode", branch.children[1])
	}
	trie, err = trie.Update(
		context.Background(),
		[]byte{0x10},
		[]byte("a different value long enough to remain a hashed child"),
	)
	if err != nil {
		t.Fatalf("Update(replace 10) error = %v", err)
	}
	if _, retained := materializeSnapshotPending(trie.snapshot)[Root(superseded)]; retained {
		t.Fatalf("replacement retained superseded child %x", Root(superseded))
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
