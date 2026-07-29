package mpt

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"slices"
)

// NodeReader retrieves canonical encoded nodes by exact legacy Keccak hash.
// Implementations must return ErrMissingNode when a hash is unavailable.
type NodeReader interface {
	GetNode(ctx context.Context, hash Root) ([]byte, error)
}

// NodeStore atomically writes a complete node batch and publishes its root.
// CommitTrie must leave the previous root and nodes observable on failure.
type NodeStore interface {
	NodeReader
	CommitTrie(ctx context.Context, commit StoreCommit) error
}

// NodeIterator is an optional deterministic audit and rebuild capability.
// IterateNodes must visit at most maximum immutable nodes in ascending hash
// order and pass owned encoded bytes to yield.
type NodeIterator interface {
	IterateNodes(
		ctx context.Context,
		maximum int,
		yield func(hash Root, encoded []byte) error,
	) error
}

// StoredNode is one immutable hash-addressed canonical node.
type StoredNode struct {
	hash    Root
	encoded []byte
}

// Hash returns the node's storage key.
func (stored StoredNode) Hash() Root {
	return stored.hash
}

// Encoded returns a copy of the canonical RLP node.
func (stored StoredNode) Encoded() []byte {
	return append([]byte(nil), stored.encoded...)
}

// StoreCommit is an immutable atomic node-write and root-publication request.
type StoreCommit struct {
	previous Root
	root     Root
	nodes    []StoredNode
}

// PreviousRoot returns the root that must still be published when the atomic
// commit begins.
func (commit StoreCommit) PreviousRoot() Root {
	return commit.previous
}

// Root returns the new root to publish after all nodes are durable.
func (commit StoreCommit) Root() Root {
	return commit.root
}

// Nodes returns an owned copy of the nodes that must become durable before Root.
func (commit StoreCommit) Nodes() []StoredNode {
	nodes := make([]StoredNode, len(commit.nodes))
	for index, stored := range commit.nodes {
		nodes[index] = StoredNode{
			hash:    stored.hash,
			encoded: stored.Encoded(),
		}
	}
	return nodes
}

// MissingNodeError identifies the exact unavailable hash without exposing key
// or value material.
type MissingNodeError struct {
	Hash  Root
	Cause error
}

func (err *MissingNodeError) Error() string {
	return fmt.Sprintf("mpt: missing node %x", err.Hash)
}

// Unwrap preserves the store's underlying missing-node cause.
func (err *MissingNodeError) Unwrap() error {
	return err.Cause
}

// Is classifies MissingNodeError as ErrMissingNode.
func (err *MissingNodeError) Is(target error) bool {
	return target == ErrMissingNode
}

// CorruptNodeError identifies the exact hash whose stored bytes failed
// integrity or canonicality validation.
type CorruptNodeError struct {
	Hash  Root
	Cause error
}

func (err *CorruptNodeError) Error() string {
	return fmt.Sprintf("mpt: corrupt node %x", err.Hash)
}

// Unwrap preserves the canonicality or integrity cause.
func (err *CorruptNodeError) Unwrap() error {
	return err.Cause
}

// Is classifies CorruptNodeError as ErrCorruptNode.
func (err *CorruptNodeError) Is(target error) bool {
	return target == ErrCorruptNode
}

func newStoreCommit(previous, root Root, pending map[Root][]byte) StoreCommit {
	hashes := make([]Root, 0, len(pending))
	for hash := range pending {
		hashes = append(hashes, hash)
	}
	slices.SortFunc(hashes, func(left, right Root) int {
		return bytes.Compare(left[:], right[:])
	})
	nodes := make([]StoredNode, 0, len(hashes))
	for _, hash := range hashes {
		nodes = append(nodes, StoredNode{
			hash:    hash,
			encoded: append([]byte(nil), pending[hash]...),
		})
	}
	return StoreCommit{previous: previous, root: root, nodes: nodes}
}

func validStore(store any) bool {
	if store == nil {
		return false
	}
	value := reflect.ValueOf(store)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map,
		reflect.Pointer, reflect.Slice:
		return !value.IsNil()
	default:
		return true
	}
}

func sameStore(left, right any) bool {
	if left == nil || right == nil {
		return false
	}
	leftValue := reflect.ValueOf(left)
	rightValue := reflect.ValueOf(right)
	if leftValue.Type() != rightValue.Type() {
		return false
	}
	switch leftValue.Kind() {
	case reflect.Chan, reflect.Func, reflect.Map, reflect.Pointer,
		reflect.UnsafePointer:
		return leftValue.Pointer() == rightValue.Pointer()
	default:
		if !leftValue.Type().Comparable() {
			return false
		}
		return leftValue.Interface() == rightValue.Interface()
	}
}
