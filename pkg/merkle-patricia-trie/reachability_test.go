package mpt_test

import (
	"bytes"
	"context"
	"slices"
	"testing"

	mpt "github.com/faustbrian/golib/pkg/merkle-patricia-trie"
	"github.com/faustbrian/golib/pkg/merkle-patricia-trie/memory"
)

func TestCollectReachableNodesReturnsCanonicalGraph(t *testing.T) {
	t.Parallel()

	store := memory.New()
	trie, err := mpt.NewRawTrie(mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("NewRawTrie() error = %v", err)
	}
	for key, value := range map[string]string{
		"alpha": "a long value that forces a persisted child node",
		"beta":  "another long value that forces a persisted child node",
	} {
		trie, err = trie.Update(context.Background(), []byte(key), []byte(value))
		if err != nil {
			t.Fatalf("Update(%q) error = %v", key, err)
		}
	}
	trie, err = trie.Commit(context.Background(), store)
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	root, err := trie.Root()
	if err != nil {
		t.Fatalf("Root() error = %v", err)
	}

	nodes, err := mpt.CollectReachableNodes(
		context.Background(),
		[]mpt.Root{root},
		store,
		mpt.DefaultReachabilityLimits(),
	)
	if err != nil {
		t.Fatalf("CollectReachableNodes() error = %v", err)
	}
	if len(nodes) < 2 {
		t.Fatalf("CollectReachableNodes() returned %d nodes", len(nodes))
	}
	if !slices.IsSortedFunc(nodes, func(left, right mpt.StoredNode) int {
		leftHash, rightHash := left.Hash(), right.Hash()
		return bytes.Compare(leftHash[:], rightHash[:])
	}) {
		t.Fatal("CollectReachableNodes() did not sort hashes")
	}
	if !slices.ContainsFunc(nodes, func(stored mpt.StoredNode) bool {
		return stored.Hash() == root
	}) {
		t.Fatalf("reachable nodes do not contain root %x", root)
	}
	owned := nodes[0].Encoded()
	owned[0] ^= 0xff
	if bytes.Equal(owned, nodes[0].Encoded()) {
		t.Fatal("StoredNode.Encoded() returned aliased bytes")
	}
}
