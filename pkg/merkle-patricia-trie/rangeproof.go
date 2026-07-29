package mpt

import (
	"bytes"
	"context"
	"fmt"
	"slices"
)

// RangeProof is an immutable deterministic sequence of canonical RLP nodes
// sufficient to establish every leaf in one explicit key interval.
type RangeProof struct {
	nodes [][]byte
}

// RangeProofFromNodes copies transport-decoded range-proof nodes after
// enforcing count and byte limits. Canonicality, ordering, and interval
// completeness are checked during verification.
func RangeProofFromNodes(nodes [][]byte, limits Limits) (RangeProof, error) {
	proof, err := ProofFromNodes(nodes, limits)
	if err != nil {
		return RangeProof{}, err
	}
	return RangeProof(proof), nil
}

// Nodes returns owned copies of the ordered deduplicated encoded nodes.
func (proof RangeProof) Nodes() [][]byte {
	return Proof(proof).Nodes()
}

// RangeItem is one immutable key/value leaf claimed by a range proof.
type RangeItem struct {
	key   []byte
	value []byte
	valid bool
}

// NewRangeItem copies one key/value leaf for range-proof verification.
func NewRangeItem(key, value []byte) RangeItem {
	return RangeItem{
		key:   append([]byte(nil), key...),
		value: append([]byte(nil), value...),
		valid: true,
	}
}

// Key returns an owned copy of the item's key.
func (item RangeItem) Key() []byte {
	return append([]byte(nil), item.key...)
}

// Value returns an owned copy of the item's value.
func (item RangeItem) Value() []byte {
	return append([]byte(nil), item.value...)
}

// ProveRange returns every raw-key leaf in [start,end) and the ordered proof
// nodes required to establish that the interval is complete. An empty end is
// an unbounded upper endpoint.
func (trie RawTrie) ProveRange(
	ctx context.Context,
	start, end []byte,
) (RangeProof, []RangeItem, error) {
	return proveRangeSnapshot(ctx, trie.snapshot, start, end, false)
}

// ProveHashedRange returns every transformed-key leaf in [start,end) and the
// ordered proof nodes required to establish that the secure-trie interval is
// complete. Non-empty endpoints must be exact 32-byte Keccak paths.
func (trie SecureTrie) ProveHashedRange(
	ctx context.Context,
	start, end []byte,
) (RangeProof, []RangeItem, error) {
	return proveRangeSnapshot(ctx, trie.snapshot, start, end, true)
}

// VerifyRawRange verifies the exact consecutive raw-key leaves in [start,end)
// under root and rejects unused, duplicated, reordered, or missing nodes.
func VerifyRawRange(
	ctx context.Context,
	root Root,
	start, end []byte,
	items []RangeItem,
	proof RangeProof,
	limits Limits,
) error {
	return verifyRange(ctx, root, start, end, items, proof, limits, false)
}

// VerifySecureHashedRange verifies the exact consecutive transformed-key
// leaves in [start,end) under a secure-trie root. It does not hash endpoints or
// item keys, preventing accidental double hashing.
func VerifySecureHashedRange(
	ctx context.Context,
	root Root,
	start, end []byte,
	items []RangeItem,
	proof RangeProof,
	limits Limits,
) error {
	return verifyRange(ctx, root, start, end, items, proof, limits, true)
}

type rangeBounds struct {
	start     []byte
	end       []byte
	startPath []byte
	endPath   []byte
	secure    bool
}

func newRangeBounds(
	start, end []byte,
	limits Limits,
	secure bool,
) (rangeBounds, error) {
	maximum := limits.MaxKeyBytes
	if secure {
		maximum = RootBytes
		if (len(start) != 0 && len(start) != RootBytes) ||
			(len(end) != 0 && len(end) != RootBytes) {
			return rangeBounds{}, fmt.Errorf(
				"%w: secure range endpoints must be 32 bytes",
				ErrInvalidProofClaim,
			)
		}
	}
	if len(start) > maximum || len(end) > maximum {
		return rangeBounds{}, fmt.Errorf(
			"%w: range endpoint byte bound exceeded", ErrInvalidKey,
		)
	}
	if len(end) != 0 && bytes.Compare(start, end) >= 0 {
		return rangeBounds{}, fmt.Errorf(
			"%w: range start is not before end", ErrInvalidProofClaim,
		)
	}
	return rangeBounds{
		start:     append([]byte(nil), start...),
		end:       append([]byte(nil), end...),
		startPath: bytesToNibbles(start),
		endPath:   bytesToNibbles(end),
		secure:    secure,
	}, nil
}

func proveRangeSnapshot(
	ctx context.Context,
	snapshot *trieSnapshot,
	start, end []byte,
	secure bool,
) (RangeProof, []RangeItem, error) {
	if snapshot == nil {
		return RangeProof{}, nil, ErrUninitialized
	}
	if err := checkContext(ctx); err != nil {
		return RangeProof{}, nil, err
	}
	bounds, err := newRangeBounds(start, end, snapshot.limits, secure)
	if err != nil {
		return RangeProof{}, nil, err
	}
	if snapshot.root == nil {
		return RangeProof{}, nil, nil
	}
	builder := newMultiProofBuilder(ctx, snapshot)
	if err := builder.prepareRoot(); err != nil {
		return RangeProof{}, nil, err
	}
	state := rangeGenerationState{
		builder: builder,
		bounds:  bounds,
		items:   make([]RangeItem, 0),
	}
	if err := state.walk(builder.root, nil, 0); err != nil {
		return RangeProof{}, nil, err
	}
	return RangeProof{nodes: builder.nodes}, state.items, nil
}

type rangeGenerationState struct {
	builder *multiProofBuilder
	bounds  rangeBounds
	items   []RangeItem
}

func (state *rangeGenerationState) walk(
	current node,
	prefix []byte,
	depth int,
) error {
	if !rangeSubtreeIntersects(prefix, state.bounds) {
		return nil
	}
	if err := state.builder.state.visit(depth); err != nil {
		return err
	}
	switch typed := current.(type) {
	case nil:
		return nil
	case hashNode:
		resolved, err := state.builder.load(Root(typed))
		if err != nil {
			return err
		}
		return state.walk(resolved, prefix, depth)
	case *leafNode:
		return state.emit(appendPath(prefix, typed.path), typed.value)
	case *extensionNode:
		nextPrefix := appendPath(prefix, typed.path)
		if !rangeSubtreeIntersects(nextPrefix, state.bounds) {
			return nil
		}
		child := typed.child
		if hashed, ok := child.(hashNode); ok {
			resolved, err := state.builder.load(Root(hashed))
			if err != nil {
				return err
			}
			if _, branch := resolved.(*branchNode); !branch {
				return fmt.Errorf(
					"%w: extension child is not a branch", ErrMalformedNode,
				)
			}
			child = resolved
		}
		return state.walk(child, nextPrefix, depth+1)
	case *branchNode:
		if len(typed.value) != 0 {
			if err := state.emit(prefix, typed.value); err != nil {
				return err
			}
		}
		for index, child := range typed.children {
			if child == nil {
				continue
			}
			childPrefix := appendPath(prefix, []byte{byte(index)})
			if !rangeSubtreeIntersects(childPrefix, state.bounds) {
				continue
			}
			if hashed, ok := child.(hashNode); ok {
				resolved, err := state.builder.load(Root(hashed))
				if err != nil {
					return err
				}
				child = resolved
			}
			if err := state.walk(child, childPrefix, depth+1); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("%w: unsupported range-proof node", ErrMalformedNode)
	}
}

func (state *rangeGenerationState) emit(path, value []byte) error {
	if !rangePathMatches(path, state.bounds) {
		return nil
	}
	if len(state.items) == state.builder.snapshot.limits.MaxProofKeys {
		return fmt.Errorf("%w: range item bound exceeded", ErrResourceLimit)
	}
	key, err := rangePathKey(path, state.bounds.secure)
	if err != nil {
		return err
	}
	state.items = append(state.items, NewRangeItem(key, value))
	return nil
}

func verifyRange(
	ctx context.Context,
	root Root,
	start, end []byte,
	items []RangeItem,
	proof RangeProof,
	limits Limits,
	secure bool,
) error {
	if err := validateTrieLimits(limits); err != nil {
		return err
	}
	if err := checkContext(ctx); err != nil {
		return err
	}
	bounds, err := newRangeBounds(start, end, limits, secure)
	if err != nil {
		return err
	}
	if err := validateRangeItems(items, bounds, limits); err != nil {
		return err
	}
	if err := validateProofLimits(Proof(proof), limits); err != nil {
		return err
	}
	if root == EmptyRoot() {
		if len(proof.nodes) != 0 {
			return fmt.Errorf(
				"%w: surplus nodes for empty root", ErrMalformedProof,
			)
		}
		if len(items) != 0 {
			return ErrFailedProof
		}
		return nil
	}
	if len(proof.nodes) == 0 {
		return ErrIncompleteProof
	}
	budget := workBudget{hashesLeft: limits.MaxHashOperations}
	lookup, err := newMultiProofLookup(
		ctx, root, MultiProof(proof), &budget,
	)
	if err != nil {
		return err
	}
	current, err := lookup.resolve(root, true)
	if err != nil {
		return err
	}
	state := rangeVerificationState{
		ctx:       ctx,
		lookup:    lookup,
		bounds:    bounds,
		items:     items,
		nodesLeft: limits.MaxTraversalNodes,
		maxDepth:  limits.MaxTraversalDepth,
	}
	if err := state.walk(current, nil, 0); err != nil {
		return err
	}
	if state.index != len(items) {
		return ErrFailedProof
	}
	if lookup.next != len(lookup.order) {
		return fmt.Errorf("%w: surplus proof nodes", ErrMalformedProof)
	}
	return nil
}

type rangeVerificationState struct {
	ctx       context.Context
	lookup    *multiProofLookup
	bounds    rangeBounds
	items     []RangeItem
	index     int
	nodesLeft int
	maxDepth  int
}

func (state *rangeVerificationState) walk(
	current node,
	prefix []byte,
	depth int,
) error {
	if !rangeSubtreeIntersects(prefix, state.bounds) {
		return nil
	}
	if err := checkContext(state.ctx); err != nil {
		return err
	}
	if depth > state.maxDepth || state.nodesLeft == 0 {
		return fmt.Errorf(
			"%w: range-proof traversal bound exceeded", ErrResourceLimit,
		)
	}
	state.nodesLeft--
	switch typed := current.(type) {
	case nil:
		return nil
	case hashNode:
		resolved, err := state.lookup.resolve(Root(typed), false)
		if err != nil {
			return err
		}
		return state.walk(resolved, prefix, depth)
	case *leafNode:
		return state.emit(appendPath(prefix, typed.path), typed.value)
	case *extensionNode:
		nextPrefix := appendPath(prefix, typed.path)
		if !rangeSubtreeIntersects(nextPrefix, state.bounds) {
			return nil
		}
		child := typed.child
		if hashed, ok := child.(hashNode); ok {
			resolved, err := state.lookup.resolve(Root(hashed), false)
			if err != nil {
				return err
			}
			if _, branch := resolved.(*branchNode); !branch {
				return fmt.Errorf(
					"%w: extension child is not a branch", ErrMalformedProof,
				)
			}
			child = resolved
		}
		return state.walk(child, nextPrefix, depth+1)
	case *branchNode:
		if len(typed.value) != 0 {
			if err := state.emit(prefix, typed.value); err != nil {
				return err
			}
		}
		for index, child := range typed.children {
			if child == nil {
				continue
			}
			childPrefix := appendPath(prefix, []byte{byte(index)})
			if !rangeSubtreeIntersects(childPrefix, state.bounds) {
				continue
			}
			if hashed, ok := child.(hashNode); ok {
				resolved, err := state.lookup.resolve(Root(hashed), false)
				if err != nil {
					return err
				}
				child = resolved
			}
			if err := state.walk(child, childPrefix, depth+1); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("%w: invalid range-proof node", ErrMalformedProof)
	}
}

func (state *rangeVerificationState) emit(path, value []byte) error {
	if !rangePathMatches(path, state.bounds) {
		return nil
	}
	if state.index == len(state.items) {
		return ErrFailedProof
	}
	key, err := rangePathKey(path, state.bounds.secure)
	if err != nil {
		return fmt.Errorf("%w: invalid range leaf", ErrMalformedProof)
	}
	item := state.items[state.index]
	if !bytes.Equal(key, item.key) || !bytes.Equal(value, item.value) {
		return ErrFailedProof
	}
	state.index++
	return nil
}

func validateRangeItems(
	items []RangeItem,
	bounds rangeBounds,
	limits Limits,
) error {
	if len(items) > limits.MaxProofKeys {
		return fmt.Errorf("%w: range item bound exceeded", ErrResourceLimit)
	}
	for index, item := range items {
		if !item.valid || len(item.value) == 0 {
			return ErrInvalidProofClaim
		}
		maximum := limits.MaxKeyBytes
		if bounds.secure {
			maximum = RootBytes
			if len(item.key) != RootBytes {
				return ErrInvalidProofClaim
			}
		}
		if len(item.key) > maximum {
			return fmt.Errorf("%w: key byte limit exceeded", ErrInvalidKey)
		}
		if len(item.value) > limits.MaxValueBytes {
			return fmt.Errorf("%w: value byte limit exceeded", ErrInvalidValue)
		}
		if !rangeBytesMatch(item.key, bounds) {
			return ErrInvalidProofClaim
		}
		if index != 0 && bytes.Compare(items[index-1].key, item.key) >= 0 {
			return ErrInvalidProofClaim
		}
	}
	return nil
}

func rangeSubtreeIntersects(prefix []byte, bounds rangeBounds) bool {
	if len(bounds.endPath) != 0 &&
		slices.Compare(prefix, bounds.endPath) >= 0 {
		return false
	}
	upper, exists := nibblePrefixSuccessor(prefix)
	return !exists || slices.Compare(upper, bounds.startPath) > 0
}

func nibblePrefixSuccessor(prefix []byte) ([]byte, bool) {
	next := append([]byte(nil), prefix...)
	for index := len(next) - 1; index >= 0; index-- {
		if next[index] != 15 {
			next[index]++
			return next[:index+1], true
		}
	}
	return nil, false
}

func rangePathMatches(path []byte, bounds rangeBounds) bool {
	return slices.Compare(path, bounds.startPath) >= 0 &&
		(len(bounds.endPath) == 0 || slices.Compare(path, bounds.endPath) < 0)
}

func rangeBytesMatch(key []byte, bounds rangeBounds) bool {
	return bytes.Compare(key, bounds.start) >= 0 &&
		(len(bounds.end) == 0 || bytes.Compare(key, bounds.end) < 0)
}

func rangePathKey(path []byte, secure bool) ([]byte, error) {
	if len(path)%2 != 0 {
		return nil, fmt.Errorf("%w: key path has odd nibble count", ErrMalformedNode)
	}
	key := nibblesToBytes(path)
	if secure && len(key) != RootBytes {
		return nil, fmt.Errorf(
			"%w: secure key path has %d bytes", ErrMalformedNode, len(key),
		)
	}
	return key, nil
}
