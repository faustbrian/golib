package mpt

import (
	"bytes"
	"context"
	"errors"
	"fmt"
)

// Entry is an immutable key/value pair returned during ordered iteration.
type Entry struct {
	key   []byte
	value []byte
}

// Key returns an owned copy of the entry key.
func (entry Entry) Key() []byte {
	return append([]byte(nil), entry.key...)
}

// Value returns an owned copy of the entry value.
func (entry Entry) Value() []byte {
	return append([]byte(nil), entry.value...)
}

// IterationOptions selects lexicographic byte-key order. Start is inclusive,
// End is exclusive, Prefix is conjunctive with the range, and Limit zero means
// complete iteration subject to the configured hard result bound.
type IterationOptions struct {
	Prefix []byte
	Start  []byte
	End    []byte
	Limit  int
}

// Iterate streams raw keys in deterministic lexicographic order. The callback
// runs synchronously without internal locks and must remain bounded.
func (trie RawTrie) Iterate(
	ctx context.Context,
	options IterationOptions,
	yield func(Entry) error,
) error {
	return iterateSnapshot(ctx, trie.snapshot, options, false, yield)
}

// IterateHashed streams secure-trie transformed 32-byte keys in deterministic
// lexicographic order. Original preimages are not available from SecureTrie.
func (trie SecureTrie) IterateHashed(
	ctx context.Context,
	options IterationOptions,
	yield func(Entry) error,
) error {
	return iterateSnapshot(ctx, trie.snapshot, options, true, yield)
}

var errIterationLimitReached = errors.New("mpt: requested iteration limit reached")

type iterationState struct {
	traversal traversalState
	options   IterationOptions
	secure    bool
	yield     func(Entry) error
	count     int
	hardMax   int
}

func iterateSnapshot(
	ctx context.Context,
	snapshot *trieSnapshot,
	options IterationOptions,
	secure bool,
	yield func(Entry) error,
) error {
	if snapshot == nil {
		return ErrUninitialized
	}
	if err := checkContext(ctx); err != nil {
		return err
	}
	if yield == nil {
		return ErrInvalidIterator
	}
	if options.Limit < 0 {
		return fmt.Errorf("%w: negative limit", ErrInvalidIterator)
	}
	if options.Limit > snapshot.limits.MaxIteratorResults {
		return fmt.Errorf("%w: iterator result bound exceeded", ErrResourceLimit)
	}
	if len(options.End) != 0 &&
		len(options.Start) != 0 &&
		bytes.Compare(options.Start, options.End) >= 0 {
		return fmt.Errorf("%w: start is not before end", ErrInvalidIterator)
	}
	maximumKeyBytes := snapshot.limits.MaxKeyBytes
	if secure {
		maximumKeyBytes = RootBytes
	}
	if len(options.Prefix) > maximumKeyBytes ||
		len(options.Start) > maximumKeyBytes ||
		len(options.End) > maximumKeyBytes {
		return fmt.Errorf("%w: iterator key bound exceeded", ErrInvalidIterator)
	}

	copied := IterationOptions{
		Prefix: append([]byte(nil), options.Prefix...),
		Start:  append([]byte(nil), options.Start...),
		End:    append([]byte(nil), options.End...),
		Limit:  options.Limit,
	}
	budget := workBudget{hashesLeft: snapshot.limits.MaxHashOperations}
	root := snapshot.root
	reader := snapshot.reader
	pending := snapshot.pending
	parent := snapshot.parent
	removed := snapshot.removed
	if snapshot.materialized {
		root = snapshot.readRoot
		reader = nil
		pending = nil
		parent = nil
		removed = nil
	}
	state := iterationState{
		traversal: traversalState{
			ctx:       ctx,
			maxDepth:  snapshot.limits.MaxTraversalDepth,
			nodesLeft: snapshot.limits.MaxIterationNodes,
			readsLeft: snapshot.limits.MaxNodeReads,
			reader:    reader,
			pending:   pending,
			parent:    parent,
			removed:   removed,
			budget:    &budget,
		},
		options: copied,
		secure:  secure,
		yield:   yield,
		hardMax: snapshot.limits.MaxIteratorResults,
	}
	err := state.walk(root, nil, 0)
	if errors.Is(err, errIterationLimitReached) {
		return nil
	}
	return err
}

func (state *iterationState) walk(current node, prefix []byte, depth int) error {
	if err := state.traversal.visit(depth); err != nil {
		return err
	}
	switch current := current.(type) {
	case nil:
		return nil
	case hashNode:
		var resolved node
		var err error
		if depth == 0 {
			resolved, err = state.traversal.resolve(Root(current))
		} else {
			resolved, err = state.traversal.resolveChild(Root(current))
		}
		if err != nil {
			return err
		}
		return state.walk(resolved, prefix, depth)
	case *leafNode:
		path := appendPath(prefix, current.path)
		return state.emit(path, current.value)
	case *extensionNode:
		child, err := state.traversal.extensionChild(current.child)
		if err != nil {
			return err
		}
		return state.walk(child, appendPath(prefix, current.path), depth+1)
	case *branchNode:
		if len(current.value) != 0 {
			if err := state.emit(prefix, current.value); err != nil {
				return err
			}
		}
		for index, child := range current.children {
			if child == nil {
				continue
			}
			if err := state.walk(
				child,
				appendPath(prefix, []byte{byte(index)}),
				depth+1,
			); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("%w: unsupported iteration node", ErrMalformedNode)
	}
}

func (state *iterationState) emit(path, value []byte) error {
	if len(path)%2 != 0 {
		return fmt.Errorf("%w: key path has odd nibble count", ErrMalformedNode)
	}
	key := nibblesToBytes(path)
	if state.secure && len(key) != RootBytes {
		return fmt.Errorf("%w: secure key path has %d bytes", ErrMalformedNode, len(key))
	}
	if !iterationMatches(key, state.options) {
		return nil
	}
	if state.options.Limit != 0 && state.count == state.options.Limit {
		return errIterationLimitReached
	}
	if state.options.Limit == 0 && state.count == state.hardMax {
		return fmt.Errorf("%w: iterator result bound exceeded", ErrResourceLimit)
	}
	if err := checkContext(state.traversal.ctx); err != nil {
		return err
	}
	entry := Entry{
		key: append([]byte(nil), key...), value: append([]byte(nil), value...),
	}
	if err := state.yield(entry); err != nil {
		return err
	}
	state.count++
	return nil
}

func iterationMatches(key []byte, options IterationOptions) bool {
	if len(options.Prefix) != 0 && !bytes.HasPrefix(key, options.Prefix) {
		return false
	}
	if len(options.Start) != 0 && bytes.Compare(key, options.Start) < 0 {
		return false
	}
	return len(options.End) == 0 || bytes.Compare(key, options.End) < 0
}

func appendPath(prefix, suffix []byte) []byte {
	path := make([]byte, len(prefix)+len(suffix))
	copy(path, prefix)
	copy(path[len(prefix):], suffix)
	return path
}

func nibblesToBytes(nibbles []byte) []byte {
	value := make([]byte, len(nibbles)/2)
	for index := range value {
		value[index] = nibbles[index*2]<<4 | nibbles[index*2+1]
	}
	return value
}
