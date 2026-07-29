package mpt

import (
	"context"
	"errors"
	"fmt"
)

// Limits bounds trie traversal, encoding, hashing, storage reads, iteration,
// batches, and proofs.
type Limits struct {
	MaxKeyBytes        int
	MaxValueBytes      int
	MaxTraversalDepth  int
	MaxTraversalNodes  int
	MaxEncodingNodes   int
	MaxHashOperations  int
	MaxNodeReads       int
	MaxIteratorResults int
	MaxIterationNodes  int
	MaxRebuildNodes    int
	MaxBatchOperations int
	MaxProofKeys       int
	MaxProofNodes      int
	MaxProofBytes      int
	MaxRecoveryNodes   int
	MaxRecoveryBytes   int
}

// DefaultLimits returns conservative limits suitable for ordinary raw and
// secure tries.
func DefaultLimits() Limits {
	return Limits{
		MaxKeyBytes:        512,
		MaxValueBytes:      1 << 20,
		MaxTraversalDepth:  2048,
		MaxTraversalNodes:  8192,
		MaxEncodingNodes:   1 << 20,
		MaxHashOperations:  1 << 20,
		MaxNodeReads:       4096,
		MaxIteratorResults: 1 << 16,
		MaxIterationNodes:  1 << 20,
		MaxRebuildNodes:    1 << 22,
		MaxBatchOperations: 4096,
		MaxProofKeys:       256,
		MaxProofNodes:      1024,
		MaxProofBytes:      16 << 20,
		MaxRecoveryNodes:   1024,
		MaxRecoveryBytes:   16 << 20,
	}
}

type trieSnapshot struct {
	root    node
	hash    Root
	limits  Limits
	base    Root
	reader  NodeReader
	pending map[Root][]byte

	recovered     map[Root][]byte
	recoveryNodes int
	recoveryBytes int
}

// RawTrie is an immutable trie that uses caller bytes directly as its path.
// A zero RawTrie rejects use with ErrUninitialized.
type RawTrie struct {
	snapshot *trieSnapshot
}

// NewRawTrie constructs an empty raw-key trie.
func NewRawTrie(limits Limits) (RawTrie, error) {
	if err := validateTrieLimits(limits); err != nil {
		return RawTrie{}, err
	}
	return RawTrie{snapshot: &trieSnapshot{
		hash: EmptyRoot(), base: EmptyRoot(), limits: limits,
	}}, nil
}

// LoadRawTrie constructs a lazy immutable raw trie from a trusted root.
func LoadRawTrie(root Root, reader NodeReader, limits Limits) (RawTrie, error) {
	snapshot, err := loadSnapshot(root, reader, limits)
	if err != nil {
		return RawTrie{}, err
	}
	return RawTrie{snapshot: snapshot}, nil
}

// Root returns the snapshot's 32-byte commitment.
func (trie RawTrie) Root() (Root, error) {
	if trie.snapshot == nil {
		return Root{}, ErrUninitialized
	}
	return trie.snapshot.hash, nil
}

// Get returns an owned copy of the value stored under key.
func (trie RawTrie) Get(ctx context.Context, key []byte) ([]byte, error) {
	return getSnapshot(ctx, trie.snapshot, key, false)
}

// Has reports whether key is present.
func (trie RawTrie) Has(ctx context.Context, key []byte) (bool, error) {
	_, err := trie.Get(ctx, key)
	if errorsIsAbsent(err) {
		return false, nil
	}
	return err == nil, err
}

// Update returns a new snapshot containing key and value. An empty value has
// deletion semantics.
func (trie RawTrie) Update(ctx context.Context, key, value []byte) (RawTrie, error) {
	snapshot, err := updateSnapshot(ctx, trie.snapshot, key, value, false)
	if err != nil {
		return RawTrie{}, err
	}
	return RawTrie{snapshot: snapshot}, nil
}

// Delete returns a new snapshot without key.
func (trie RawTrie) Delete(ctx context.Context, key []byte) (RawTrie, error) {
	snapshot, err := deleteSnapshot(ctx, trie.snapshot, key, false)
	if err != nil {
		return RawTrie{}, err
	}
	return RawTrie{snapshot: snapshot}, nil
}

// Commit atomically writes every pending hashed node and publishes the root.
func (trie RawTrie) Commit(ctx context.Context, store NodeStore) (RawTrie, error) {
	snapshot, err := commitSnapshot(ctx, trie.snapshot, store)
	if err != nil {
		return RawTrie{}, err
	}
	return RawTrie{snapshot: snapshot}, nil
}

// SecureTrie is an immutable trie that applies legacy Keccak-256 exactly once
// to each caller key before path traversal. It has no pre-hashed-key method.
type SecureTrie struct {
	snapshot *trieSnapshot
}

// NewSecureTrie constructs an empty secure-key trie.
func NewSecureTrie(limits Limits) (SecureTrie, error) {
	if err := validateTrieLimits(limits); err != nil {
		return SecureTrie{}, err
	}
	return SecureTrie{snapshot: &trieSnapshot{
		hash: EmptyRoot(), base: EmptyRoot(), limits: limits,
	}}, nil
}

// LoadSecureTrie constructs a lazy immutable secure trie from a trusted root.
func LoadSecureTrie(root Root, reader NodeReader, limits Limits) (SecureTrie, error) {
	snapshot, err := loadSnapshot(root, reader, limits)
	if err != nil {
		return SecureTrie{}, err
	}
	return SecureTrie{snapshot: snapshot}, nil
}

// Root returns the snapshot's 32-byte commitment.
func (trie SecureTrie) Root() (Root, error) {
	if trie.snapshot == nil {
		return Root{}, ErrUninitialized
	}
	return trie.snapshot.hash, nil
}

// Get returns an owned copy of the value stored under the securely transformed
// key.
func (trie SecureTrie) Get(ctx context.Context, key []byte) ([]byte, error) {
	return getSnapshot(ctx, trie.snapshot, key, true)
}

// Has reports whether the securely transformed key is present.
func (trie SecureTrie) Has(ctx context.Context, key []byte) (bool, error) {
	_, err := trie.Get(ctx, key)
	if errorsIsAbsent(err) {
		return false, nil
	}
	return err == nil, err
}

// Update returns a new secure snapshot containing key and value. An empty value
// has deletion semantics.
func (trie SecureTrie) Update(ctx context.Context, key, value []byte) (SecureTrie, error) {
	snapshot, err := updateSnapshot(ctx, trie.snapshot, key, value, true)
	if err != nil {
		return SecureTrie{}, err
	}
	return SecureTrie{snapshot: snapshot}, nil
}

// Delete returns a new secure snapshot without key.
func (trie SecureTrie) Delete(ctx context.Context, key []byte) (SecureTrie, error) {
	snapshot, err := deleteSnapshot(ctx, trie.snapshot, key, true)
	if err != nil {
		return SecureTrie{}, err
	}
	return SecureTrie{snapshot: snapshot}, nil
}

// Commit atomically writes every pending hashed node and publishes the root.
func (trie SecureTrie) Commit(ctx context.Context, store NodeStore) (SecureTrie, error) {
	snapshot, err := commitSnapshot(ctx, trie.snapshot, store)
	if err != nil {
		return SecureTrie{}, err
	}
	return SecureTrie{snapshot: snapshot}, nil
}

func getSnapshot(
	ctx context.Context,
	snapshot *trieSnapshot,
	key []byte,
	secure bool,
) ([]byte, error) {
	if err := validateOperation(ctx, snapshot, key, nil, false); err != nil {
		return nil, err
	}
	budget := workBudget{hashesLeft: snapshot.limits.MaxHashOperations}
	path, _ := keyPath(key, secure, &budget)
	state := traversalState{
		ctx:       ctx,
		maxDepth:  snapshot.limits.MaxTraversalDepth,
		nodesLeft: snapshot.limits.MaxTraversalNodes,
		readsLeft: snapshot.limits.MaxNodeReads,
		reader:    snapshot.reader,
		pending:   snapshot.pending,
		budget:    &budget,
	}
	value, found, err := getNode(snapshot.root, path, 0, &state)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrAbsentKey
	}
	return append([]byte(nil), value...), nil
}

func updateSnapshot(
	ctx context.Context,
	snapshot *trieSnapshot,
	key, value []byte,
	secure bool,
) (*trieSnapshot, error) {
	if err := validateOperation(ctx, snapshot, key, value, true); err != nil {
		return nil, err
	}
	if len(value) == 0 {
		return deleteSnapshot(ctx, snapshot, key, secure)
	}

	budget := workBudget{hashesLeft: snapshot.limits.MaxHashOperations}
	state := traversalState{
		ctx:       ctx,
		maxDepth:  snapshot.limits.MaxTraversalDepth,
		nodesLeft: snapshot.limits.MaxTraversalNodes,
		readsLeft: snapshot.limits.MaxNodeReads,
		reader:    snapshot.reader,
		pending:   snapshot.pending,
		budget:    &budget,
	}
	path, _ := keyPath(key, secure, &budget)
	root, err := insertNode(snapshot.root, path, value, 0, &state)
	if err != nil {
		return nil, err
	}
	finished, err := finishSnapshot(
		ctx, root, snapshot.limits, snapshot.base, snapshot.reader, &budget,
	)
	if err != nil {
		return nil, err
	}
	return inheritRecovery(finished, snapshot), nil
}

func deleteSnapshot(
	ctx context.Context,
	snapshot *trieSnapshot,
	key []byte,
	secure bool,
) (*trieSnapshot, error) {
	if err := validateOperation(ctx, snapshot, key, nil, false); err != nil {
		return nil, err
	}
	budget := workBudget{hashesLeft: snapshot.limits.MaxHashOperations}
	state := traversalState{
		ctx:       ctx,
		maxDepth:  snapshot.limits.MaxTraversalDepth,
		nodesLeft: snapshot.limits.MaxTraversalNodes,
		readsLeft: snapshot.limits.MaxNodeReads,
		reader:    snapshot.reader,
		pending:   snapshot.pending,
		budget:    &budget,
	}
	path, _ := keyPath(key, secure, &budget)
	root, deleted, err := deleteNode(snapshot.root, path, 0, &state)
	if err != nil {
		return nil, err
	}
	if !deleted {
		return nil, ErrAbsentKey
	}
	finished, err := finishSnapshot(
		ctx, root, snapshot.limits, snapshot.base, snapshot.reader, &budget,
	)
	if err != nil {
		return nil, err
	}
	return inheritRecovery(finished, snapshot), nil
}

func finishSnapshot(
	ctx context.Context,
	root node,
	limits Limits,
	base Root,
	reader NodeReader,
	budget *workBudget,
) (*trieSnapshot, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if root == nil {
		return &trieSnapshot{
			hash: EmptyRoot(), base: base, limits: limits, reader: reader,
		}, nil
	}
	encoded, pending, err := encodeNodeBounded(
		ctx,
		root,
		limits.MaxEncodingNodes,
		budget,
	)
	if err != nil {
		return nil, err
	}
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	hash, err := budget.hash(encoded)
	if err != nil {
		return nil, err
	}
	pending[hash] = append([]byte(nil), encoded...)
	return &trieSnapshot{
		root: root, hash: hash, limits: limits, base: base, reader: reader,
		pending: pending,
	}, nil
}

func validateTrieLimits(limits Limits) error {
	if limits.MaxKeyBytes <= 0 ||
		limits.MaxKeyBytes > MaxCompactPathNibbles/2 ||
		limits.MaxValueBytes <= 0 ||
		limits.MaxValueBytes > rlpMaxValueBytes ||
		limits.MaxTraversalDepth <= 0 ||
		limits.MaxTraversalDepth > MaxCompactPathNibbles+1 ||
		limits.MaxTraversalNodes <= 0 ||
		limits.MaxEncodingNodes <= 0 ||
		limits.MaxHashOperations <= 0 ||
		limits.MaxNodeReads <= 0 ||
		limits.MaxIteratorResults <= 0 ||
		limits.MaxIterationNodes <= 0 ||
		limits.MaxRebuildNodes <= 0 ||
		limits.MaxBatchOperations <= 0 ||
		limits.MaxProofKeys <= 0 ||
		limits.MaxProofNodes <= 0 ||
		limits.MaxProofBytes <= 0 ||
		limits.MaxRecoveryNodes <= 0 ||
		limits.MaxRecoveryBytes <= 0 {
		return fmt.Errorf("%w: invalid trie limits", ErrResourceLimit)
	}
	return nil
}

const rlpMaxValueBytes = (16 << 20) - 16

func validateOperation(
	ctx context.Context,
	snapshot *trieSnapshot,
	key, value []byte,
	checkValue bool,
) error {
	if snapshot == nil {
		return ErrUninitialized
	}
	if err := checkContext(ctx); err != nil {
		return err
	}
	if len(key) > snapshot.limits.MaxKeyBytes {
		return fmt.Errorf("%w: key byte limit exceeded", ErrInvalidKey)
	}
	if !checkValue {
		return nil
	}
	if len(value) > snapshot.limits.MaxValueBytes {
		return fmt.Errorf("%w: value byte limit exceeded", ErrInvalidValue)
	}
	return nil
}

func checkContext(ctx context.Context) error {
	if ctx == nil {
		return ErrInvalidContext
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%w: %w", ErrCanceled, err)
	}
	return nil
}

type traversalState struct {
	ctx       context.Context
	maxDepth  int
	nodesLeft int
	readsLeft int
	reader    NodeReader
	pending   map[Root][]byte
	budget    *workBudget
}

func (state *traversalState) visit(depth int) error {
	if err := checkContext(state.ctx); err != nil {
		return err
	}
	if depth > state.maxDepth || state.nodesLeft == 0 {
		return fmt.Errorf("%w: traversal bound exceeded", ErrResourceLimit)
	}
	state.nodesLeft--
	return nil
}

type workBudget struct {
	hashesLeft int
}

func (budget *workBudget) hash(value []byte) (Root, error) {
	if budget.hashesLeft == 0 {
		return Root{}, fmt.Errorf("%w: hash operation bound exceeded", ErrResourceLimit)
	}
	budget.hashesLeft--
	return keccakRoot(value), nil
}

func keyPath(key []byte, secure bool, budget *workBudget) ([]byte, error) {
	if secure {
		root, err := budget.hash(key)
		if err != nil {
			return nil, err
		}
		return bytesToNibbles(root[:]), nil
	}
	return bytesToNibbles(key), nil
}

func bytesToNibbles(value []byte) []byte {
	nibbles := make([]byte, len(value)*2)
	for index, current := range value {
		nibbles[index*2] = current >> 4
		nibbles[index*2+1] = current & 0x0f
	}
	return nibbles
}

func getNode(
	current node,
	path []byte,
	depth int,
	state *traversalState,
) ([]byte, bool, error) {
	if err := state.visit(depth); err != nil {
		return nil, false, err
	}
	switch current := current.(type) {
	case nil:
		return nil, false, nil
	case *leafNode:
		if slicesEqual(current.path, path) {
			return current.value, true, nil
		}
		return nil, false, nil
	case *extensionNode:
		if !hasPrefix(path, current.path) {
			return nil, false, nil
		}
		child, err := state.extensionChild(current.child)
		if err != nil {
			return nil, false, err
		}
		return getNode(child, path[len(current.path):], depth+1, state)
	case *branchNode:
		if len(path) == 0 {
			return current.value, len(current.value) != 0, nil
		}
		return getNode(current.children[path[0]], path[1:], depth+1, state)
	case hashNode:
		resolved, err := state.resolve(Root(current))
		if err != nil {
			return nil, false, err
		}
		return getNode(resolved, path, depth, state)
	default:
		return nil, false, fmt.Errorf("%w: unresolved node", ErrMalformedNode)
	}
}

func insertNode(
	current node,
	path, value []byte,
	depth int,
	state *traversalState,
) (node, error) {
	if err := state.visit(depth); err != nil {
		return nil, err
	}
	switch current := current.(type) {
	case nil:
		return newLeaf(path, value)
	case *leafNode:
		return insertLeaf(current, path, value)
	case *extensionNode:
		child, err := state.extensionChild(current.child)
		if err != nil {
			return nil, err
		}
		resolved := &extensionNode{path: current.path, child: child}
		return insertExtension(resolved, path, value, depth, state)
	case *branchNode:
		children := current.children
		branchValue := current.value
		if len(path) == 0 {
			branchValue = append([]byte(nil), value...)
		} else {
			child, err := insertNode(children[path[0]], path[1:], value, depth+1, state)
			if err != nil {
				return nil, err
			}
			children[path[0]] = child
		}
		return newBranch(children, branchValue)
	case hashNode:
		resolved, err := state.resolve(Root(current))
		if err != nil {
			return nil, err
		}
		return insertNode(resolved, path, value, depth, state)
	default:
		return nil, fmt.Errorf("%w: unresolved node", ErrMalformedNode)
	}
}

func insertLeaf(current *leafNode, path, value []byte) (node, error) {
	common := commonPrefixLength(current.path, path)
	if common == len(current.path) && common == len(path) {
		return newLeaf(path, value)
	}

	var children [16]node
	var branchValue []byte
	oldRemainder := current.path[common:]
	if len(oldRemainder) == 0 {
		branchValue = current.value
	} else {
		leaf, err := newLeaf(oldRemainder[1:], current.value)
		if err != nil {
			return nil, err
		}
		children[oldRemainder[0]] = leaf
	}
	newRemainder := path[common:]
	if len(newRemainder) == 0 {
		branchValue = value
	} else {
		leaf, err := newLeaf(newRemainder[1:], value)
		if err != nil {
			return nil, err
		}
		children[newRemainder[0]] = leaf
	}
	branch, _ := newBranch(children, branchValue)
	if common == 0 {
		return branch, nil
	}
	return makeExtension(path[:common], branch)
}

func insertExtension(
	current *extensionNode,
	path, value []byte,
	depth int,
	state *traversalState,
) (node, error) {
	common := commonPrefixLength(current.path, path)
	if common == len(current.path) {
		child, err := insertNode(
			current.child,
			path[common:],
			value,
			depth+1,
			state,
		)
		if err != nil {
			return nil, err
		}
		compacted, _ := makeExtension(current.path, child)
		return compacted, nil
	}

	var children [16]node
	oldRemainder := current.path[common:]
	if len(oldRemainder) == 1 {
		children[oldRemainder[0]] = current.child
	} else {
		child, err := makeExtension(oldRemainder[1:], current.child)
		if err != nil {
			return nil, err
		}
		children[oldRemainder[0]] = child
	}

	var branchValue []byte
	newRemainder := path[common:]
	if len(newRemainder) == 0 {
		branchValue = value
	} else {
		leaf, err := newLeaf(newRemainder[1:], value)
		if err != nil {
			return nil, err
		}
		children[newRemainder[0]] = leaf
	}
	branch, _ := newBranch(children, branchValue)
	if common == 0 {
		return branch, nil
	}
	compacted, _ := makeExtension(path[:common], branch)
	return compacted, nil
}

func deleteNode(
	current node,
	path []byte,
	depth int,
	state *traversalState,
) (node, bool, error) {
	if err := state.visit(depth); err != nil {
		return nil, false, err
	}
	switch current := current.(type) {
	case nil:
		return nil, false, nil
	case *leafNode:
		if slicesEqual(current.path, path) {
			return nil, true, nil
		}
		return current, false, nil
	case *extensionNode:
		if !hasPrefix(path, current.path) {
			return current, false, nil
		}
		resolvedChild, resolveErr := state.extensionChild(current.child)
		if resolveErr != nil {
			return nil, false, resolveErr
		}
		child, deleted, err := deleteNode(
			resolvedChild,
			path[len(current.path):],
			depth+1,
			state,
		)
		if err != nil || !deleted {
			return current, deleted, err
		}
		compacted, _ := makeExtension(current.path, child)
		return compacted, true, nil
	case *branchNode:
		children := current.children
		branchValue := current.value
		if len(path) == 0 {
			if len(branchValue) == 0 {
				return current, false, nil
			}
			branchValue = nil
		} else {
			child, deleted, err := deleteNode(
				children[path[0]],
				path[1:],
				depth+1,
				state,
			)
			if err != nil || !deleted {
				return current, deleted, err
			}
			children[path[0]] = child
		}
		compacted, err := compactBranch(children, branchValue)
		return compacted, true, err
	case hashNode:
		resolved, err := state.resolve(Root(current))
		if err != nil {
			return nil, false, err
		}
		return deleteNode(resolved, path, depth, state)
	default:
		return nil, false, fmt.Errorf("%w: unresolved node", ErrMalformedNode)
	}
}

func compactBranch(children [16]node, value []byte) (node, error) {
	count := 0
	index := 0
	for childIndex, child := range children {
		if child != nil {
			count++
			index = childIndex
		}
	}
	if len(value) != 0 {
		if count == 0 {
			return newLeaf(nil, value)
		}
		return newBranch(children, value)
	}
	switch count {
	case 0:
		return nil, nil
	case 1:
		return prependNibble(byte(index), children[index])
	default:
		return newBranch(children, nil)
	}
}

func prependNibble(nibble byte, child node) (node, error) {
	switch child := child.(type) {
	case *leafNode:
		return newLeaf(append([]byte{nibble}, child.path...), child.value)
	case *extensionNode:
		return makeExtension(append([]byte{nibble}, child.path...), child.child)
	default:
		return makeExtension([]byte{nibble}, child)
	}
}

func makeExtension(path []byte, child node) (node, error) {
	switch child := child.(type) {
	case *leafNode:
		return newLeaf(append(append([]byte(nil), path...), child.path...), child.value)
	case *extensionNode:
		return newExtension(
			append(append([]byte(nil), path...), child.path...),
			child.child,
		)
	default:
		return newExtension(path, child)
	}
}

func commonPrefixLength(left, right []byte) int {
	maximum := min(len(left), len(right))
	for index := range maximum {
		if left[index] != right[index] {
			return index
		}
	}
	return maximum
}

func hasPrefix(value, prefix []byte) bool {
	return len(value) >= len(prefix) && slicesEqual(value[:len(prefix)], prefix)
}

func slicesEqual(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func errorsIsAbsent(err error) bool {
	return err == ErrAbsentKey
}

func loadSnapshot(root Root, reader NodeReader, limits Limits) (*trieSnapshot, error) {
	if err := validateTrieLimits(limits); err != nil {
		return nil, err
	}
	if !validStore(reader) {
		return nil, ErrInvalidStore
	}
	var rootNode node
	if root != EmptyRoot() {
		rootNode = hashNode(root)
	}
	return &trieSnapshot{
		root: rootNode, hash: root, base: root, limits: limits, reader: reader,
	}, nil
}

func commitSnapshot(
	ctx context.Context,
	snapshot *trieSnapshot,
	store NodeStore,
) (*trieSnapshot, error) {
	if snapshot == nil {
		return nil, ErrUninitialized
	}
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if !validStore(store) {
		return nil, ErrInvalidStore
	}
	if snapshot.reader != nil && !sameStore(snapshot.reader, store) {
		return nil, fmt.Errorf(
			"%w: loaded snapshots must commit to their source store",
			ErrInvalidStore,
		)
	}
	if snapshot.reader != nil &&
		snapshot.hash == snapshot.base &&
		len(snapshot.pending) == 0 {
		return snapshot, nil
	}
	commit := newStoreCommit(snapshot.base, snapshot.hash, snapshot.pending)
	if err := store.CommitTrie(ctx, commit); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrStorageCommit, err)
	}
	return &trieSnapshot{
		root: snapshot.root, hash: snapshot.hash, base: snapshot.hash,
		limits: snapshot.limits, reader: store,
	}, nil
}

func (state *traversalState) resolve(hash Root) (node, error) {
	resolved, _, err := state.resolveEncoded(hash)
	return resolved, err
}

func (state *traversalState) resolveEncoded(
	hash Root,
) (node, []byte, error) {
	encoded, recovered := state.pending[hash]
	if !recovered {
		if state.reader == nil {
			return nil, nil, fmt.Errorf("%w: unresolved hash without reader", ErrMalformedNode)
		}
		if state.readsLeft == 0 {
			return nil, nil, fmt.Errorf("%w: node read bound exceeded", ErrResourceLimit)
		}
		state.readsLeft--
		var err error
		encoded, err = state.reader.GetNode(state.ctx, hash)
		if err != nil {
			if errors.Is(err, ErrMissingNode) {
				return nil, nil, &MissingNodeError{Hash: hash, Cause: err}
			}
			return nil, nil, fmt.Errorf("%w: %w", ErrStorageRead, err)
		}
	}
	encoded = append([]byte(nil), encoded...)
	actual, err := state.budget.hash(encoded)
	if err != nil {
		return nil, nil, err
	}
	if actual != hash {
		return nil, nil, &CorruptNodeError{
			Hash: hash, Cause: fmt.Errorf("%w: hash mismatch", ErrCorruptNode),
		}
	}
	resolved, err := decodeNode(encoded)
	if err != nil || resolved == nil {
		if err == nil {
			err = fmt.Errorf("%w: stored null node", ErrMalformedNode)
		}
		return nil, nil, &CorruptNodeError{Hash: hash, Cause: err}
	}
	return resolved, append([]byte(nil), encoded...), nil
}

func (state *traversalState) extensionChild(child node) (node, error) {
	switch child := child.(type) {
	case *branchNode:
		return child, nil
	case hashNode:
		hash := Root(child)
		resolved, err := state.resolve(hash)
		if err != nil {
			return nil, err
		}
		if _, ok := resolved.(*branchNode); !ok {
			return nil, &CorruptNodeError{
				Hash: hash,
				Cause: fmt.Errorf(
					"%w: extension child is a compact node",
					ErrMalformedNode,
				),
			}
		}
		return resolved, nil
	default:
		return nil, fmt.Errorf(
			"%w: extension child is not a branch",
			ErrMalformedNode,
		)
	}
}
