package mpt

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
)

// ReachabilityLimits bounds retained-root validation and pruning mark work.
type ReachabilityLimits struct {
	MaxRoots          int
	MaxRetentions     int
	MaxNodes          int
	MaxBytes          int
	MaxDepth          int
	MaxNodeReads      int
	MaxHashOperations int
}

// DefaultReachabilityLimits returns conservative bounds for store audits and
// pruning.
func DefaultReachabilityLimits() ReachabilityLimits {
	return ReachabilityLimits{
		MaxRoots:          1025,
		MaxRetentions:     1024,
		MaxNodes:          1 << 20,
		MaxBytes:          256 << 20,
		MaxDepth:          2048,
		MaxNodeReads:      1 << 20,
		MaxHashOperations: 1 << 20,
	}
}

// RootRetention keeps one historical root eligible for loading until Release.
// Root does not transfer ownership of internal storage. Release makes the root
// eligible for a later prune; it does not itself delete nodes. Repeated release
// returns ErrReleasedRetention.
type RootRetention interface {
	Root() Root
	Release(ctx context.Context) error
}

// RootRetainer is an optional store capability for retaining historical roots.
// Implementations must document whether leases survive restart and how callers
// reconcile an error reported after durable lease publication.
type RootRetainer interface {
	RetainRoot(
		ctx context.Context,
		root Root,
		limits ReachabilityLimits,
	) (RootRetention, error)
}

// NodePruner is an optional store capability for atomically deleting nodes
// unreachable from its published root and retained historical roots. A
// non-zero PruneResult returned with an error means deletion committed and the
// result describes it; a zero result with an error means deletion did not
// commit. Implementations must document recovery for interrupted operations.
type NodePruner interface {
	Prune(ctx context.Context, limits ReachabilityLimits) (PruneResult, error)
}

// PruneResult describes one completed atomic pruning operation.
type PruneResult struct {
	storedBefore int
	storedAfter  int
	removedBytes uint64
}

// StoredBefore returns the node count before pruning.
func (result PruneResult) StoredBefore() int {
	return result.storedBefore
}

// StoredAfter returns the node count after pruning.
func (result PruneResult) StoredAfter() int {
	return result.storedAfter
}

// RemovedNodes returns the number of unreachable nodes deleted.
func (result PruneResult) RemovedNodes() int {
	return result.storedBefore - result.storedAfter
}

// RemovedBytes returns the encoded bytes deleted.
func (result PruneResult) RemovedBytes() uint64 {
	return result.removedBytes
}

// NewPruneResult constructs an immutable store pruning result.
func NewPruneResult(
	storedBefore, storedAfter int,
	removedBytes uint64,
) PruneResult {
	return PruneResult{
		storedBefore: storedBefore,
		storedAfter:  storedAfter,
		removedBytes: removedBytes,
	}
}

// CollectReachableNodes validates and returns every hash-addressed node
// reachable from roots in ascending hash order. Embedded nodes are validated
// but are not returned because they have no independent storage key.
func CollectReachableNodes(
	ctx context.Context,
	roots []Root,
	reader NodeReader,
	limits ReachabilityLimits,
) ([]StoredNode, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if err := validateReachabilityLimits(limits); err != nil {
		return nil, err
	}
	if !validStore(reader) {
		return nil, ErrInvalidStore
	}
	if len(roots) > limits.MaxRoots {
		return nil, fmt.Errorf("%w: retained root bound exceeded", ErrResourceLimit)
	}
	roots = append([]Root(nil), roots...)
	state := reachabilityState{
		ctx:        ctx,
		reader:     reader,
		limits:     limits,
		readsLeft:  limits.MaxNodeReads,
		hashesLeft: limits.MaxHashOperations,
		nodesLeft:  limits.MaxNodes,
		bytesLeft:  limits.MaxBytes,
		active:     make(map[Root]struct{}),
		nodes:      make(map[Root][]byte),
		decoded:    make(map[Root]node),
	}
	for _, root := range roots {
		if root == EmptyRoot() {
			continue
		}
		if _, err := state.collectHash(root, 0); err != nil {
			return nil, err
		}
	}
	hashes := make([]Root, 0, len(state.nodes))
	for hash := range state.nodes {
		hashes = append(hashes, hash)
	}
	slices.SortFunc(hashes, func(left, right Root) int {
		return bytes.Compare(left[:], right[:])
	})
	nodes := make([]StoredNode, 0, len(hashes))
	for _, hash := range hashes {
		nodes = append(nodes, StoredNode{
			hash: hash, encoded: append([]byte(nil), state.nodes[hash]...),
		})
	}
	return nodes, nil
}

type reachabilityState struct {
	ctx        context.Context
	reader     NodeReader
	limits     ReachabilityLimits
	readsLeft  int
	hashesLeft int
	nodesLeft  int
	bytesLeft  int
	active     map[Root]struct{}
	nodes      map[Root][]byte
	decoded    map[Root]node
}

func (state *reachabilityState) collectHash(hash Root, depth int) (node, error) {
	if _, cycle := state.active[hash]; cycle {
		return nil, &CorruptNodeError{
			Hash: hash, Cause: fmt.Errorf("%w: cyclic node graph", ErrCorruptNode),
		}
	}
	if decoded, visited := state.decoded[hash]; visited {
		return decoded, nil
	}
	if err := state.checkTraversal(depth); err != nil {
		return nil, err
	}
	if state.readsLeft == 0 {
		return nil, fmt.Errorf("%w: node read bound exceeded", ErrResourceLimit)
	}
	state.readsLeft--
	encoded, err := state.reader.GetNode(state.ctx, hash)
	if err != nil {
		if errors.Is(err, ErrMissingNode) {
			return nil, &MissingNodeError{Hash: hash, Cause: err}
		}
		return nil, fmt.Errorf("%w: %w", ErrStorageRead, err)
	}
	encoded = append([]byte(nil), encoded...)
	if len(encoded) > state.bytesLeft {
		return nil, fmt.Errorf("%w: reachable byte bound exceeded", ErrResourceLimit)
	}
	if state.hashesLeft == 0 {
		return nil, fmt.Errorf("%w: hash operation bound exceeded", ErrResourceLimit)
	}
	state.hashesLeft--
	if actual := keccakRoot(encoded); actual != hash {
		return nil, &CorruptNodeError{
			Hash: hash, Cause: fmt.Errorf("%w: hash mismatch", ErrCorruptNode),
		}
	}
	decoded, err := decodeNode(encoded)
	if err != nil || decoded == nil {
		if err == nil {
			err = fmt.Errorf("%w: stored null node", ErrMalformedNode)
		}
		return nil, &CorruptNodeError{Hash: hash, Cause: err}
	}
	state.nodes[hash] = encoded
	state.decoded[hash] = decoded
	state.bytesLeft -= len(encoded)
	state.active[hash] = struct{}{}
	err = state.collectNode(decoded, depth)
	delete(state.active, hash)
	if err != nil {
		return nil, err
	}
	return decoded, nil
}

func (state *reachabilityState) collectNode(current node, depth int) error {
	if err := checkContext(state.ctx); err != nil {
		return err
	}
	switch current := current.(type) {
	case nil:
		return nil
	case hashNode:
		_, err := state.collectChildHash(Root(current), depth)
		return err
	}
	if err := state.visit(depth); err != nil {
		return err
	}
	switch current := current.(type) {
	case *leafNode:
		return nil
	case *extensionNode:
		if childHash, hashed := current.child.(hashNode); hashed {
			child, err := state.collectChildHash(Root(childHash), depth+1)
			if err != nil {
				return err
			}
			if _, branch := child.(*branchNode); !branch {
				return &CorruptNodeError{
					Hash: Root(childHash),
					Cause: fmt.Errorf(
						"%w: extension child is a compact node",
						ErrMalformedNode,
					),
				}
			}
			return nil
		}
		return state.collectNode(current.child, depth+1)
	case *branchNode:
		for _, child := range current.children {
			if child == nil {
				continue
			}
			if err := state.collectNode(child, depth+1); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("%w: unsupported reachable node", ErrMalformedNode)
	}
}

func (state *reachabilityState) collectChildHash(
	hash Root,
	depth int,
) (node, error) {
	child, err := state.collectHash(hash, depth)
	if err != nil {
		return nil, err
	}
	if len(state.nodes[hash]) < RootBytes {
		return nil, &CorruptNodeError{
			Hash: hash,
			Cause: fmt.Errorf(
				"%w: embedded-size child referenced by hash",
				ErrMalformedNode,
			),
		}
	}
	return child, nil
}

func (state *reachabilityState) visit(depth int) error {
	if err := state.checkTraversal(depth); err != nil {
		return err
	}
	state.nodesLeft--
	return nil
}

func (state *reachabilityState) checkTraversal(depth int) error {
	if err := checkContext(state.ctx); err != nil {
		return err
	}
	if depth > state.limits.MaxDepth || state.nodesLeft == 0 {
		return fmt.Errorf("%w: reachable node bound exceeded", ErrResourceLimit)
	}
	return nil
}

func validateReachabilityLimits(limits ReachabilityLimits) error {
	if limits.MaxRoots <= 0 ||
		limits.MaxRetentions <= 0 ||
		limits.MaxNodes <= 0 ||
		limits.MaxBytes <= 0 ||
		limits.MaxDepth <= 0 ||
		limits.MaxDepth > MaxCompactPathNibbles+1 ||
		limits.MaxNodeReads <= 0 ||
		limits.MaxHashOperations <= 0 {
		return fmt.Errorf("%w: invalid reachability limits", ErrResourceLimit)
	}
	return nil
}
