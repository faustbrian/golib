package mpt

import (
	"context"
	"fmt"

	"github.com/faustbrian/golib/pkg/merkle-patricia-trie/internal/rlp"
)

type node = any

type leafNode struct {
	path  []byte
	value []byte
}

type extensionNode struct {
	path  []byte
	child node
}

type branchNode struct {
	children [16]node
	value    []byte
}

type hashNode Root

func newLeaf(path, value []byte) (*leafNode, error) {
	if len(value) == 0 {
		return nil, fmt.Errorf("%w: leaf value is empty", ErrMalformedNode)
	}
	if err := validateNibbles(path); err != nil {
		return nil, err
	}
	return &leafNode{
		path:  append([]byte(nil), path...),
		value: append([]byte(nil), value...),
	}, nil
}

func newExtension(path []byte, child node) (*extensionNode, error) {
	if len(path) == 0 {
		return nil, fmt.Errorf("%w: extension path is empty", ErrMalformedNode)
	}
	if err := validateNibbles(path); err != nil {
		return nil, err
	}
	if child == nil {
		return nil, fmt.Errorf("%w: extension child is null", ErrMalformedNode)
	}
	switch child.(type) {
	case *leafNode, *extensionNode:
		return nil, fmt.Errorf("%w: adjacent compact nodes", ErrMalformedNode)
	}
	return &extensionNode{path: append([]byte(nil), path...), child: child}, nil
}

func newBranch(children [16]node, value []byte) (*branchNode, error) {
	childCount := 0
	for _, child := range children {
		if child != nil {
			childCount++
		}
	}
	if len(value) == 0 && childCount < 2 {
		return nil, fmt.Errorf("%w: branch without value has fewer than two children", ErrMalformedNode)
	}
	if len(value) != 0 && childCount == 0 {
		return nil, fmt.Errorf("%w: value-only branch", ErrMalformedNode)
	}
	return &branchNode{children: children, value: append([]byte(nil), value...)}, nil
}

func validateNibbles(path []byte) error {
	if len(path) > MaxCompactPathNibbles {
		return fmt.Errorf("%w: path limit exceeded", ErrMalformedNode)
	}
	for _, nibble := range path {
		if nibble > 0x0f {
			return fmt.Errorf("%w: path nibble outside 0..15", ErrMalformedNode)
		}
	}
	return nil
}

type nodeEncoding struct {
	value     rlp.Value
	bytes     []byte
	persisted map[Root][]byte
}

func encodeNode(current node) ([]byte, map[Root][]byte, error) {
	maximum := int(^uint(0) >> 1)
	budget := workBudget{hashesLeft: maximum}
	return encodeNodeBounded(context.Background(), current, maximum, &budget)
}

func encodeNodeBounded(
	ctx context.Context,
	current node,
	maximumNodes int,
	budget *workBudget,
) ([]byte, map[Root][]byte, error) {
	state := encodingState{ctx: ctx, nodesLeft: maximumNodes, budget: budget}
	encoded, err := encodeNodeValue(current, &state)
	if err != nil {
		return nil, nil, err
	}
	return encoded.bytes, encoded.persisted, nil
}

type encodingState struct {
	ctx       context.Context
	nodesLeft int
	budget    *workBudget
}

func (state *encodingState) visit() error {
	if err := checkContext(state.ctx); err != nil {
		return err
	}
	if state.nodesLeft == 0 {
		return fmt.Errorf("%w: encoding node bound exceeded", ErrResourceLimit)
	}
	state.nodesLeft--
	return nil
}

func encodeNodeValue(current node, state *encodingState) (nodeEncoding, error) {
	if err := state.visit(); err != nil {
		return nodeEncoding{}, err
	}
	switch current := current.(type) {
	case nil:
		return encodeRLPValue(rlp.String(nil), nil)
	case *leafNode:
		compact, err := EncodeCompactPath(current.path, true)
		if err != nil {
			return nodeEncoding{}, fmt.Errorf("%w: encode leaf path: %v", ErrMalformedNode, err)
		}
		return encodeRLPValue(
			rlp.List(rlp.String(compact), rlp.String(current.value)),
			nil,
		)
	case *extensionNode:
		reference, persisted, err := encodeChildReference(current.child, state)
		if err != nil {
			return nodeEncoding{}, err
		}
		compact, err := EncodeCompactPath(current.path, false)
		if err != nil {
			return nodeEncoding{}, fmt.Errorf("%w: encode extension path: %v", ErrMalformedNode, err)
		}
		return encodeRLPValue(
			rlp.List(rlp.String(compact), reference),
			persisted,
		)
	case *branchNode:
		values := make([]rlp.Value, 0, 17)
		persisted := make(map[Root][]byte)
		for _, childNode := range current.children {
			if childNode == nil {
				values = append(values, rlp.String(nil))
				continue
			}
			reference, childPersisted, err := encodeChildReference(childNode, state)
			if err != nil {
				return nodeEncoding{}, err
			}
			values = append(values, reference)
			mergePersisted(persisted, childPersisted)
		}
		values = append(values, rlp.String(current.value))
		return encodeRLPValue(rlp.List(values...), persisted)
	default:
		return nodeEncoding{}, fmt.Errorf("%w: unsupported in-memory node", ErrMalformedNode)
	}
}

func encodeChildReference(
	current node,
	state *encodingState,
) (rlp.Value, map[Root][]byte, error) {
	if hashed, ok := current.(hashNode); ok {
		root := Root(hashed)
		return rlp.String(root[:]), make(map[Root][]byte), nil
	}
	child, err := encodeNodeValue(current, state)
	if err != nil {
		return rlp.Value{}, nil, err
	}
	return childReference(child, state)
}

func childReference(
	child nodeEncoding,
	state *encodingState,
) (rlp.Value, map[Root][]byte, error) {
	persisted := make(map[Root][]byte, len(child.persisted)+1)
	mergePersisted(persisted, child.persisted)
	if len(child.bytes) < RootBytes {
		return child.value, persisted, nil
	}
	root, err := state.budget.hash(child.bytes)
	if err != nil {
		return rlp.Value{}, nil, err
	}
	persisted[root] = append([]byte(nil), child.bytes...)
	return rlp.String(root[:]), persisted, nil
}

func encodeRLPValue(value rlp.Value, persisted map[Root][]byte) (nodeEncoding, error) {
	encoded, err := rlp.Encode(value, rlp.DefaultLimits())
	if err != nil {
		return nodeEncoding{}, fmt.Errorf("%w: encode RLP: %v", ErrMalformedNode, err)
	}
	if persisted == nil {
		persisted = make(map[Root][]byte)
	}
	return nodeEncoding{value: value, bytes: encoded, persisted: persisted}, nil
}

func mergePersisted(target, source map[Root][]byte) {
	for root, encoded := range source {
		target[root] = append([]byte(nil), encoded...)
	}
}

func decodeNode(encoded []byte) (node, error) {
	value, err := rlp.Decode(encoded, rlp.DefaultLimits())
	if err != nil {
		return nil, fmt.Errorf("%w: RLP: %v", ErrMalformedNode, err)
	}
	return decodeNodeValue(value)
}

func decodeNodeValue(value rlp.Value) (node, error) {
	if value.Kind() == rlp.KindString {
		if len(value.Bytes()) == 0 {
			return nil, nil
		}
		return nil, fmt.Errorf("%w: root node is not a list", ErrMalformedNode)
	}

	elements := value.Elements()
	switch len(elements) {
	case 2:
		return decodeShortNode(elements)
	case 17:
		return decodeBranchNode(elements)
	default:
		return nil, fmt.Errorf("%w: unsupported list arity %d", ErrMalformedNode, len(elements))
	}
}

func decodeShortNode(elements []rlp.Value) (node, error) {
	if elements[0].Kind() != rlp.KindString {
		return nil, fmt.Errorf("%w: compact path is not a string", ErrMalformedNode)
	}
	compact, err := DecodeCompactPath(elements[0].Bytes())
	if err != nil {
		return nil, fmt.Errorf("%w: compact path: %v", ErrMalformedNode, err)
	}
	if compact.Leaf() {
		if elements[1].Kind() != rlp.KindString {
			return nil, fmt.Errorf("%w: leaf value is not a string", ErrMalformedNode)
		}
		leaf, leafErr := newLeaf(compact.Nibbles(), elements[1].Bytes())
		if leafErr != nil {
			return nil, leafErr
		}
		return leaf, nil
	}

	child, err := decodeChildReference(elements[1])
	if err != nil {
		return nil, err
	}
	extension, extensionErr := newExtension(compact.Nibbles(), child)
	if extensionErr != nil {
		return nil, extensionErr
	}
	return extension, nil
}

func decodeBranchNode(elements []rlp.Value) (node, error) {
	var children [16]node
	for index := range children {
		child, err := decodeChildReference(elements[index])
		if err != nil {
			return nil, err
		}
		children[index] = child
	}
	if elements[16].Kind() != rlp.KindString {
		return nil, fmt.Errorf("%w: branch value is not a string", ErrMalformedNode)
	}
	branch, err := newBranch(children, elements[16].Bytes())
	if err != nil {
		return nil, err
	}
	return branch, nil
}

func decodeChildReference(value rlp.Value) (node, error) {
	if value.Kind() == rlp.KindString {
		reference := value.Bytes()
		switch len(reference) {
		case 0:
			return nil, nil
		case RootBytes:
			root, err := RootFromBytes(reference)
			if err != nil {
				return nil, fmt.Errorf("%w: child hash: %v", ErrMalformedNode, err)
			}
			return hashNode(root), nil
		default:
			return nil, fmt.Errorf(
				"%w: child reference has %d bytes",
				ErrMalformedNode,
				len(reference),
			)
		}
	}

	encoded, err := rlp.Encode(value, rlp.DefaultLimits())
	if err != nil {
		return nil, fmt.Errorf("%w: embedded child RLP: %v", ErrMalformedNode, err)
	}
	if len(encoded) >= RootBytes {
		return nil, fmt.Errorf("%w: oversized embedded child", ErrMalformedNode)
	}
	child, err := decodeNodeValue(value)
	if err != nil {
		return nil, err
	}
	if child == nil {
		return nil, fmt.Errorf("%w: embedded null child", ErrMalformedNode)
	}
	return child, nil
}
