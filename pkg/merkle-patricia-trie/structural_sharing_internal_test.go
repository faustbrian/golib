package mpt

import (
	"context"
	"errors"
	"testing"
)

func TestFinishedSnapshotsFreezeHashedChildrenForStructuralSharing(t *testing.T) {
	t.Parallel()

	trie, err := NewRawTrie(DefaultLimits())
	if err != nil {
		t.Fatalf("NewRawTrie() error = %v", err)
	}
	for _, key := range []byte{0x10, 0x20} {
		trie, err = trie.Update(
			context.Background(),
			[]byte{key},
			[]byte("a value long enough to require a hashed child reference"),
		)
		if err != nil {
			t.Fatalf("Update(%x) error = %v", key, err)
		}
		assertPendingCanonical(t, trie.snapshot)
	}
	if got := countHashNodes(trie.snapshot.root); got != 2 {
		t.Fatalf("finished snapshot hash nodes = %d, want 2", got)
	}
}

func TestStructurallySharedPendingNodesRemainCanonical(t *testing.T) {
	t.Parallel()

	trie, err := NewRawTrie(DefaultLimits())
	if err != nil {
		t.Fatalf("NewRawTrie() error = %v", err)
	}
	for index := range 256 {
		key := []byte{byte(index)}
		value := []byte("a value long enough to require hashed child references")
		trie, err = trie.Update(context.Background(), key, value)
		if err != nil {
			t.Fatalf("Update(%d) error = %v", index, err)
		}
		assertPendingCanonical(t, trie.snapshot)
	}
}

func TestSecureUpdatesRetainMaterializedReadSnapshot(t *testing.T) {
	t.Parallel()

	trie, err := NewSecureTrie(DefaultLimits())
	if err != nil {
		t.Fatalf("NewSecureTrie() error = %v", err)
	}
	trie, err = trie.Update(
		context.Background(),
		[]byte("key"),
		[]byte("a value long enough to require a hashed reference"),
	)
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if !trie.snapshot.materialized || trie.snapshot.readRoot == nil {
		t.Fatal("secure update lost its materialized read snapshot")
	}
}

func TestDeleteResolvesHashedOnlyChildBeforeCompaction(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	limits := DefaultLimits()
	value := []byte("a value long enough to require a hashed child reference")
	trie, err := NewRawTrie(limits)
	if err != nil {
		t.Fatalf("NewRawTrie() error = %v", err)
	}
	trie, err = trie.Update(ctx, []byte{0x10}, value)
	if err != nil {
		t.Fatalf("Update(10) error = %v", err)
	}
	trie, err = trie.Update(ctx, []byte{0x20}, value)
	if err != nil {
		t.Fatalf("Update(20) error = %v", err)
	}
	trie, err = trie.Delete(ctx, []byte{0x10})
	if err != nil {
		t.Fatalf("Delete(10) error = %v", err)
	}

	want, err := NewRawTrie(limits)
	if err != nil {
		t.Fatalf("NewRawTrie(want) error = %v", err)
	}
	want, err = want.Update(ctx, []byte{0x20}, value)
	if err != nil {
		t.Fatalf("Update(want 20) error = %v", err)
	}
	if trie.snapshot.hash != want.snapshot.hash {
		t.Fatalf(
			"compacted root = %x, want %x",
			trie.snapshot.hash,
			want.snapshot.hash,
		)
	}
}

func TestPendingOverlayHelpersCoverEmptyAndRemovedLayers(t *testing.T) {
	t.Parallel()

	var hash Root
	hash[0] = 1
	if snapshotPendingLayer(nil) != nil {
		t.Fatal("snapshotPendingLayer(nil) is non-nil")
	}
	if _, exists := lookupSnapshotPending(nil, hash); exists {
		t.Fatal("lookupSnapshotPending(nil) found a node")
	}
	if got := materializeSnapshotPending(nil); len(got) != 0 {
		t.Fatalf("materializeSnapshotPending(nil) length = %d, want 0", len(got))
	}
	parent := &pendingLayer{
		added: map[Root][]byte{hash: {1}},
		depth: 1,
	}
	if _, exists := lookupPending(
		nil,
		nil,
		&pendingLayer{
			removed: map[Root]struct{}{hash: {}},
			parent:  parent,
			depth:   2,
		},
		hash,
	); exists {
		t.Fatal("lookupPending() ignored parent tombstone")
	}
	removed := map[Root]struct{}{hash: {}}
	layer := snapshotPendingLayer(&trieSnapshot{
		parent:  parent,
		removed: removed,
	})
	if layer == nil {
		t.Fatal("removed-only snapshot produced no pending layer")
	}
	if layer == parent || layer.depth != parent.depth+1 {
		t.Fatalf("removed-only layer = %#v, want a new child layer", layer)
	}
	if _, exists := layer.removed[hash]; !exists {
		t.Fatal("removed-only layer lost its tombstone")
	}
}

func TestPendingLayersCompactAtExactMaximumDepth(t *testing.T) {
	t.Parallel()

	trie, err := NewRawTrie(DefaultLimits())
	if err != nil {
		t.Fatalf("NewRawTrie() error = %v", err)
	}
	for index := range maximumPendingLayerDepth {
		trie, err = trie.Update(
			context.Background(),
			[]byte{byte(index)},
			[]byte("a value long enough to require a hashed reference"),
		)
		if err != nil {
			t.Fatalf("Update(%d) error = %v", index, err)
		}
	}
	if trie.snapshot.parent == nil ||
		trie.snapshot.parent.depth != maximumPendingLayerDepth-1 {
		t.Fatalf(
			"pre-boundary pending layer = %#v, want depth %d",
			trie.snapshot.parent,
			maximumPendingLayerDepth-1,
		)
	}
	trie, err = trie.Update(
		context.Background(),
		[]byte{maximumPendingLayerDepth},
		[]byte("a value long enough to require a hashed reference"),
	)
	if err != nil {
		t.Fatalf("Update(boundary) error = %v", err)
	}
	if trie.snapshot.parent != nil || trie.snapshot.removed != nil {
		t.Fatalf(
			"boundary compaction retained layers: parent=%#v removed=%d",
			trie.snapshot.parent,
			len(trie.snapshot.removed),
		)
	}
}

func TestMaterializedReadRootFailureContracts(t *testing.T) {
	t.Parallel()

	state := traversalState{
		ctx:       context.Background(),
		maxDepth:  1,
		nodesLeft: 0,
		budget:    &workBudget{hashesLeft: 1},
	}
	leaf := &leafNode{path: []byte{1}, value: []byte("value")}
	if _, err := insertReadRoot(
		leaf,
		[]byte{1},
		nil,
		true,
		&state,
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("insertReadRoot(limit) error = %v", err)
	}

	state.nodesLeft = 1
	if _, err := insertReadRoot(
		leaf,
		[]byte{2},
		nil,
		true,
		&state,
	); !errors.Is(err, ErrMalformedNode) {
		t.Fatalf("insertReadRoot(divergence) error = %v", err)
	}

	snapshot := &trieSnapshot{
		readRoot:     struct{}{},
		hash:         EmptyRoot(),
		limits:       DefaultLimits(),
		base:         EmptyRoot(),
		materialized: true,
	}
	if _, err := updateSnapshot(
		context.Background(),
		snapshot,
		[]byte{0x10},
		[]byte("value"),
		false,
	); !errors.Is(err, ErrMalformedNode) {
		t.Fatalf("updateSnapshot(read root) error = %v", err)
	}

	snapshot.root = &leafNode{
		path:  []byte{1, 0},
		value: []byte("value"),
	}
	if _, err := deleteSnapshot(
		context.Background(),
		snapshot,
		[]byte{0x10},
		false,
	); !errors.Is(err, ErrMalformedNode) {
		t.Fatalf("deleteSnapshot(read root) error = %v", err)
	}
}

func TestBatchMutationFailurePropagation(t *testing.T) {
	t.Parallel()

	snapshot := &trieSnapshot{
		root:   struct{}{},
		hash:   EmptyRoot(),
		limits: DefaultLimits(),
		base:   EmptyRoot(),
	}
	if _, err := applyBatch(
		context.Background(),
		snapshot,
		[]Mutation{Put([]byte{1}, []byte("value"))},
		false,
	); !errors.Is(err, ErrMalformedNode) {
		t.Fatalf("applyBatch(put failure) error = %v", err)
	}
	if _, err := applyBatch(
		context.Background(),
		snapshot,
		[]Mutation{Remove([]byte{1})},
		false,
	); !errors.Is(err, ErrMalformedNode) {
		t.Fatalf("applyBatch(remove failure) error = %v", err)
	}
}

func TestCollapsibleHashedChildResolutionFailurePropagates(t *testing.T) {
	t.Parallel()

	var hash Root
	hash[0] = 1
	var children [16]node
	children[1] = hashNode(hash)
	state := traversalState{
		ctx:       context.Background(),
		maxDepth:  2,
		nodesLeft: 2,
		readsLeft: 1,
		budget:    &workBudget{hashesLeft: 1},
	}
	if _, err := resolveCollapsibleBranchChild(
		children,
		nil,
		&state,
	); !errors.Is(err, ErrMalformedNode) {
		t.Fatalf("resolveCollapsibleBranchChild() error = %v", err)
	}

	children[0] = &leafNode{path: nil, value: []byte("deleted")}
	branch := &branchNode{children: children}
	state.nodesLeft = 3
	state.readsLeft = 1
	if _, _, err := deleteNode(
		branch,
		[]byte{0},
		0,
		&state,
	); !errors.Is(err, ErrMalformedNode) {
		t.Fatalf("deleteNode(collapsible child) error = %v", err)
	}
}

func TestFinishSnapshotRejectsNonCanonicalEncodedRoot(t *testing.T) {
	t.Parallel()

	_, err := finishSnapshotWithPending(
		context.Background(),
		&branchNode{value: []byte("value")},
		nil,
		DefaultLimits(),
		EmptyRoot(),
		nil,
		nil,
		nil,
		false,
		&workBudget{hashesLeft: DefaultLimits().MaxHashOperations},
	)
	if !errors.Is(err, ErrMalformedNode) {
		t.Fatalf("finishSnapshotWithPending() error = %v", err)
	}
}

func assertPendingCanonical(t *testing.T, snapshot *trieSnapshot) {
	t.Helper()
	for hash, encoded := range materializeSnapshotPending(snapshot) {
		if actual := keccakRoot(encoded); actual != hash {
			t.Fatalf("pending hash %x contains bytes for %x", hash, actual)
		}
		if decoded, err := decodeNode(encoded); err != nil || decoded == nil {
			t.Fatalf("pending node %x is malformed: %v", hash, err)
		}
	}
}

func countHashNodes(current node) int {
	switch current := current.(type) {
	case nil, *leafNode:
		return 0
	case hashNode:
		return 1
	case *extensionNode:
		return countHashNodes(current.child)
	case *branchNode:
		total := 0
		for _, child := range current.children {
			total += countHashNodes(child)
		}
		return total
	default:
		panic("unsupported test node")
	}
}
