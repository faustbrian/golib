package mpt

import (
	"bytes"
	"context"
	"fmt"
	"slices"
)

// MultiProof is an immutable deterministic sequence of canonical RLP nodes
// shared by multiple proof paths. Each hashed node appears exactly once.
type MultiProof struct {
	nodes [][]byte
}

// MultiProofFromNodes copies transport-decoded multi-proof nodes after
// enforcing count and byte limits. Canonicality, ordering, and claim binding
// are checked during verification.
func MultiProofFromNodes(nodes [][]byte, limits Limits) (MultiProof, error) {
	proof, err := ProofFromNodes(nodes, limits)
	if err != nil {
		return MultiProof{}, err
	}
	return MultiProof(proof), nil
}

// Nodes returns owned copies of the ordered deduplicated encoded nodes.
func (proof MultiProof) Nodes() [][]byte {
	return Proof(proof).Nodes()
}

// ProofClaim is one immutable membership or absence claim. Construct claims
// with MembershipClaim or AbsenceClaim.
type ProofClaim struct {
	key     []byte
	value   []byte
	present bool
	valid   bool
}

// MembershipClaim constructs an exact key/value membership claim.
func MembershipClaim(key, value []byte) ProofClaim {
	return ProofClaim{
		key: append([]byte(nil), key...), value: append([]byte(nil), value...),
		present: true, valid: true,
	}
}

// AbsenceClaim constructs an exact key absence claim.
func AbsenceClaim(key []byte) ProofClaim {
	return ProofClaim{key: append([]byte(nil), key...), valid: true}
}

// ProveMany returns one deterministic deduplicated proof for raw keys.
func (trie RawTrie) ProveMany(
	ctx context.Context,
	keys [][]byte,
) (MultiProof, error) {
	return proveManySnapshot(ctx, trie.snapshot, keys, false)
}

// ProveMany returns one deterministic deduplicated proof for secure keys.
func (trie SecureTrie) ProveMany(
	ctx context.Context,
	keys [][]byte,
) (MultiProof, error) {
	return proveManySnapshot(ctx, trie.snapshot, keys, true)
}

func proveManySnapshot(
	ctx context.Context,
	snapshot *trieSnapshot,
	keys [][]byte,
	secure bool,
) (MultiProof, error) {
	if snapshot == nil {
		return MultiProof{}, ErrUninitialized
	}
	if err := checkContext(ctx); err != nil {
		return MultiProof{}, err
	}
	if len(keys) == 0 || len(keys) > snapshot.limits.MaxProofKeys {
		return MultiProof{}, fmt.Errorf("%w: invalid proof key count", ErrInvalidProofClaim)
	}
	ordered := copyAndSortProofKeys(keys)
	for index, key := range ordered {
		if len(key) > snapshot.limits.MaxKeyBytes {
			return MultiProof{}, fmt.Errorf("%w: key byte limit exceeded", ErrInvalidKey)
		}
		if index != 0 && bytes.Equal(key, ordered[index-1]) {
			return MultiProof{}, ErrDuplicateProofKey
		}
	}

	builder := newMultiProofBuilder(ctx, snapshot)
	if err := builder.prepareRoot(); err != nil {
		return MultiProof{}, err
	}
	for _, key := range ordered {
		if err := builder.addPath(key, secure); err != nil {
			return MultiProof{}, err
		}
	}
	return MultiProof{nodes: builder.nodes}, nil
}

func copyAndSortProofKeys(keys [][]byte) [][]byte {
	ordered := make([][]byte, len(keys))
	for index, key := range keys {
		ordered[index] = append([]byte(nil), key...)
	}
	slices.SortFunc(ordered, bytes.Compare)
	return ordered
}

type multiProofBuilder struct {
	ctx      context.Context
	snapshot *trieSnapshot
	state    traversalState
	pending  map[Root][]byte
	decoded  map[Root]node
	seen     map[Root]struct{}
	nodes    [][]byte
	total    int
	root     node
}

func newMultiProofBuilder(
	ctx context.Context,
	snapshot *trieSnapshot,
) *multiProofBuilder {
	budget := &workBudget{hashesLeft: snapshot.limits.MaxHashOperations}
	pending := make(map[Root][]byte, len(snapshot.pending))
	mergePersisted(pending, snapshot.pending)
	return &multiProofBuilder{
		ctx: ctx, snapshot: snapshot, pending: pending,
		decoded: make(map[Root]node), seen: make(map[Root]struct{}),
		state: traversalState{
			ctx: ctx, maxDepth: snapshot.limits.MaxTraversalDepth,
			nodesLeft: snapshot.limits.MaxTraversalNodes,
			readsLeft: snapshot.limits.MaxNodeReads,
			reader:    snapshot.reader, budget: budget,
			pending: snapshot.pending,
		},
	}
}

func (builder *multiProofBuilder) prepareRoot() error {
	if builder.snapshot.root == nil {
		return nil
	}
	rootHash := builder.snapshot.hash
	if encoded, exists := builder.pending[rootHash]; exists {
		decoded, err := builder.decodePending(rootHash, encoded, true)
		if err != nil {
			return err
		}
		builder.root = decoded
		return builder.appendNode(rootHash, encoded)
	}
	if _, hashed := builder.snapshot.root.(hashNode); hashed {
		decoded, encoded, err := builder.state.resolveEncoded(rootHash)
		if err != nil {
			return err
		}
		builder.decoded[rootHash] = decoded
		builder.root = decoded
		return builder.appendNode(rootHash, encoded)
	}
	encoded, persisted, err := encodeNodeBounded(
		builder.ctx,
		builder.snapshot.root,
		builder.snapshot.limits.MaxEncodingNodes,
		builder.state.budget,
	)
	if err != nil {
		return err
	}
	actual, err := builder.state.budget.hash(encoded)
	if err != nil {
		return err
	}
	if actual != rootHash {
		return fmt.Errorf("%w: snapshot root encoding mismatch", ErrMalformedNode)
	}
	mergePersisted(builder.pending, persisted)
	builder.decoded[rootHash] = builder.snapshot.root
	builder.root = builder.snapshot.root
	return builder.appendNode(rootHash, encoded)
}

func (builder *multiProofBuilder) addPath(key []byte, secure bool) error {
	path, err := keyPath(key, secure, builder.state.budget)
	if err != nil {
		return err
	}
	current := builder.root
	depth := 0
	for current != nil {
		if err := builder.state.visit(depth); err != nil {
			return err
		}
		switch typed := current.(type) {
		case *leafNode:
			return nil
		case *extensionNode:
			if !hasPrefix(path, typed.path) {
				return nil
			}
			path = path[len(typed.path):]
			if hashed, ok := typed.child.(hashNode); ok {
				childHash := Root(hashed)
				child, loadErr := builder.load(childHash)
				if loadErr != nil {
					return loadErr
				}
				if _, branch := child.(*branchNode); !branch {
					return &CorruptNodeError{
						Hash: childHash,
						Cause: fmt.Errorf(
							"%w: extension child is a compact node",
							ErrMalformedNode,
						),
					}
				}
				current = child
			} else {
				current = typed.child
			}
			depth++
		case *branchNode:
			if len(path) == 0 {
				return nil
			}
			current = typed.children[path[0]]
			path = path[1:]
			if hashed, ok := current.(hashNode); ok {
				current, err = builder.load(Root(hashed))
				if err != nil {
					return err
				}
			}
			depth++
		default:
			return fmt.Errorf("%w: invalid proof source node", ErrMalformedNode)
		}
	}
	return nil
}

func (builder *multiProofBuilder) load(hash Root) (node, error) {
	if decoded, exists := builder.decoded[hash]; exists {
		return decoded, nil
	}
	if encoded, exists := builder.pending[hash]; exists {
		decoded, err := builder.decodePending(hash, encoded, false)
		if err != nil {
			return nil, err
		}
		if err := builder.appendNode(hash, encoded); err != nil {
			return nil, err
		}
		return decoded, nil
	}
	decoded, encoded, err := builder.state.resolveEncodedChild(hash)
	if err != nil {
		return nil, err
	}
	builder.decoded[hash] = decoded
	if err := builder.appendNode(hash, encoded); err != nil {
		return nil, err
	}
	return decoded, nil
}

func (builder *multiProofBuilder) decodePending(
	hash Root,
	encoded []byte,
	root bool,
) (node, error) {
	actual, err := builder.state.budget.hash(encoded)
	if err != nil {
		return nil, err
	}
	if actual != hash {
		return nil, fmt.Errorf("%w: pending node hash mismatch", ErrMalformedNode)
	}
	if !root && len(encoded) < RootBytes {
		return nil, &CorruptNodeError{
			Hash: hash,
			Cause: fmt.Errorf(
				"%w: embedded-size child referenced by hash",
				ErrMalformedNode,
			),
		}
	}
	decoded, err := decodeNode(encoded)
	if err != nil || decoded == nil {
		return nil, fmt.Errorf("%w: invalid pending node", ErrMalformedNode)
	}
	builder.decoded[hash] = decoded
	return decoded, nil
}

func (builder *multiProofBuilder) appendNode(
	hash Root,
	encoded []byte,
) error {
	if _, duplicate := builder.seen[hash]; duplicate {
		return nil
	}
	if len(builder.nodes) == builder.snapshot.limits.MaxProofNodes {
		return fmt.Errorf("%w: proof node bound exceeded", ErrResourceLimit)
	}
	if len(encoded) > builder.snapshot.limits.MaxProofBytes-builder.total {
		return fmt.Errorf("%w: proof byte bound exceeded", ErrResourceLimit)
	}
	builder.seen[hash] = struct{}{}
	builder.total += len(encoded)
	builder.nodes = append(builder.nodes, append([]byte(nil), encoded...))
	return nil
}

// VerifyRawMultiProof verifies every raw-key claim and rejects unused,
// duplicated, reordered, or missing nodes.
func VerifyRawMultiProof(
	ctx context.Context,
	root Root,
	claims []ProofClaim,
	proof MultiProof,
	limits Limits,
) error {
	return verifyMultiProof(ctx, root, claims, proof, limits, false)
}

// VerifySecureMultiProof verifies every secure-key claim and rejects unused,
// duplicated, reordered, or missing nodes.
func VerifySecureMultiProof(
	ctx context.Context,
	root Root,
	claims []ProofClaim,
	proof MultiProof,
	limits Limits,
) error {
	return verifyMultiProof(ctx, root, claims, proof, limits, true)
}

type multiProofNode struct {
	decoded node
	size    int
}

type multiProofLookup struct {
	nodes  map[Root]multiProofNode
	order  []Root
	used   map[Root]struct{}
	next   int
	root   Root
	budget *workBudget
}

func verifyMultiProof(
	ctx context.Context,
	root Root,
	claims []ProofClaim,
	proof MultiProof,
	limits Limits,
	secure bool,
) error {
	if err := validateTrieLimits(limits); err != nil {
		return err
	}
	if err := checkContext(ctx); err != nil {
		return err
	}
	if len(claims) == 0 || len(claims) > limits.MaxProofKeys {
		return fmt.Errorf("%w: invalid proof claim count", ErrInvalidProofClaim)
	}
	if err := validateProofLimits(Proof(proof), limits); err != nil {
		return err
	}
	ordered := append([]ProofClaim(nil), claims...)
	slices.SortFunc(ordered, func(left, right ProofClaim) int {
		return bytes.Compare(left.key, right.key)
	})
	for index, claim := range ordered {
		if !claim.valid {
			return ErrInvalidProofClaim
		}
		if len(claim.key) > limits.MaxKeyBytes {
			return fmt.Errorf("%w: key byte limit exceeded", ErrInvalidKey)
		}
		if claim.present &&
			(len(claim.value) == 0 || len(claim.value) > limits.MaxValueBytes) {
			return fmt.Errorf("%w: invalid expected value", ErrInvalidValue)
		}
		if index != 0 && bytes.Equal(claim.key, ordered[index-1].key) {
			return ErrDuplicateProofKey
		}
	}
	if root == EmptyRoot() {
		if len(proof.nodes) != 0 {
			return fmt.Errorf("%w: surplus nodes for empty root", ErrMalformedProof)
		}
		for _, claim := range ordered {
			if claim.present {
				return ErrFailedProof
			}
		}
		return nil
	}
	if len(proof.nodes) == 0 {
		return ErrIncompleteProof
	}

	budget := workBudget{hashesLeft: limits.MaxHashOperations}
	lookup, err := newMultiProofLookup(root, proof, &budget)
	if err != nil {
		return err
	}
	for _, claim := range ordered {
		if err := verifyMultiClaim(
			ctx, lookup, claim, limits, secure,
		); err != nil {
			return err
		}
	}
	if lookup.next != len(lookup.order) {
		return fmt.Errorf("%w: surplus proof nodes", ErrMalformedProof)
	}
	return nil
}

func newMultiProofLookup(
	root Root,
	proof MultiProof,
	budget *workBudget,
) (*multiProofLookup, error) {
	lookup := &multiProofLookup{
		nodes: make(map[Root]multiProofNode, len(proof.nodes)),
		order: make([]Root, 0, len(proof.nodes)),
		used:  make(map[Root]struct{}, len(proof.nodes)),
		root:  root, budget: budget,
	}
	for _, encoded := range proof.nodes {
		hash, err := budget.hash(encoded)
		if err != nil {
			return nil, err
		}
		if _, duplicate := lookup.nodes[hash]; duplicate {
			return nil, fmt.Errorf("%w: duplicate proof node", ErrMalformedProof)
		}
		decoded, err := decodeNode(encoded)
		if err != nil || decoded == nil {
			return nil, fmt.Errorf("%w: invalid canonical node", ErrMalformedProof)
		}
		lookup.nodes[hash] = multiProofNode{
			decoded: decoded,
			size:    len(encoded),
		}
		lookup.order = append(lookup.order, hash)
	}
	return lookup, nil
}

func (lookup *multiProofLookup) resolve(
	expected Root,
	root bool,
) (node, error) {
	stored, exists := lookup.nodes[expected]
	if !exists {
		if root {
			return nil, ErrWrongRoot
		}
		return nil, ErrIncompleteProof
	}
	if !root && stored.size < RootBytes {
		return nil, fmt.Errorf(
			"%w: embedded-size child referenced by hash",
			ErrMalformedProof,
		)
	}
	if _, used := lookup.used[expected]; !used {
		if lookup.next >= len(lookup.order) ||
			lookup.order[lookup.next] != expected {
			if root {
				return nil, ErrWrongRoot
			}
			return nil, fmt.Errorf("%w: reordered proof node", ErrMalformedProof)
		}
		lookup.used[expected] = struct{}{}
		lookup.next++
	}
	return stored.decoded, nil
}

func verifyMultiClaim(
	ctx context.Context,
	lookup *multiProofLookup,
	claim ProofClaim,
	limits Limits,
	secure bool,
) error {
	current, err := lookup.resolve(lookup.root, true)
	if err != nil {
		return err
	}
	path, err := keyPath(claim.key, secure, lookup.budget)
	if err != nil {
		return err
	}
	depth := 0
	nodesLeft := limits.MaxTraversalNodes
	for {
		if err := checkContext(ctx); err != nil {
			return err
		}
		if depth > limits.MaxTraversalDepth || nodesLeft == 0 {
			return fmt.Errorf("%w: proof traversal bound exceeded", ErrResourceLimit)
		}
		nodesLeft--
		switch typed := current.(type) {
		case *leafNode:
			return finishMultiClaim(
				slices.Equal(typed.path, path), typed.value, claim,
			)
		case *extensionNode:
			if !hasPrefix(path, typed.path) {
				return finishMultiClaim(false, nil, claim)
			}
			path = path[len(typed.path):]
			if hashed, ok := typed.child.(hashNode); ok {
				child, childErr := lookup.resolve(Root(hashed), false)
				if childErr != nil {
					return childErr
				}
				if _, branch := child.(*branchNode); !branch {
					return fmt.Errorf(
						"%w: extension child is not a branch", ErrMalformedProof,
					)
				}
				current = child
			} else {
				current = typed.child
			}
			depth++
		case *branchNode:
			if len(path) == 0 {
				return finishMultiClaim(
					len(typed.value) != 0, typed.value, claim,
				)
			}
			child := typed.children[path[0]]
			path = path[1:]
			if child == nil {
				return finishMultiClaim(false, nil, claim)
			}
			if hashed, ok := child.(hashNode); ok {
				current, err = lookup.resolve(Root(hashed), false)
				if err != nil {
					return err
				}
			} else {
				current = child
			}
			depth++
		default:
			return fmt.Errorf("%w: invalid proof node", ErrMalformedProof)
		}
	}
}

func finishMultiClaim(present bool, value []byte, claim ProofClaim) error {
	if present != claim.present {
		return ErrFailedProof
	}
	if present && !slices.Equal(value, claim.value) {
		return ErrFailedProof
	}
	return nil
}
