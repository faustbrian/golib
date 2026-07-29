package mpt

import (
	"bytes"
	"context"
	"fmt"

	"github.com/faustbrian/golib/pkg/merkle-patricia-trie/internal/rlp"
)

// SortedBuilder incrementally calculates a raw-trie root from strictly sorted
// key/value input. It retains only the open trie frontier and is not safe for
// concurrent mutation. Its zero value rejects use with ErrUninitialized.
type SortedBuilder struct {
	state *sortedBuilderState
}

type sortedBuilderState struct {
	limits      Limits
	frames      []builderFrame
	previousKey []byte
	previous    []byte
	hasPrevious bool
	closed      bool
	budget      builderBudget
}

type builderBudget struct {
	hashesLeft int
	nodesLeft  int
}

type builderFrame struct {
	children [16]*builderNode
	value    []byte
}

type builderNodeKind uint8

const (
	builderLeaf builderNodeKind = iota + 1
	builderExtension
	builderBranch
)

type builderNode struct {
	kind  builderNodeKind
	path  []byte
	value []byte
	child rlp.Value
	node  rlp.Value
}

// NewSortedBuilder constructs an empty raw-trie streaming root builder.
func NewSortedBuilder(limits Limits) (*SortedBuilder, error) {
	if err := validateTrieLimits(limits); err != nil {
		return nil, err
	}
	return &SortedBuilder{state: &sortedBuilderState{
		limits: limits,
		frames: []builderFrame{{}},
		budget: builderBudget{
			hashesLeft: limits.MaxHashOperations,
			nodesLeft:  limits.MaxEncodingNodes,
		},
	}}, nil
}

// Add consumes one key/value pair. Keys must be strictly increasing in byte
// lexicographic order and values must be non-empty. Rejected input leaves the
// builder unchanged.
func (builder *SortedBuilder) Add(
	ctx context.Context,
	key, value []byte,
) error {
	if builder == nil || builder.state == nil {
		return ErrUninitialized
	}
	state := builder.state
	if state.closed {
		return ErrClosedBuilder
	}
	if err := checkContext(ctx); err != nil {
		return err
	}
	if len(key) > state.limits.MaxKeyBytes {
		return fmt.Errorf("%w: key byte limit exceeded", ErrInvalidKey)
	}
	if len(value) == 0 || len(value) > state.limits.MaxValueBytes {
		return fmt.Errorf("%w: builder value must be non-empty and bounded", ErrInvalidValue)
	}
	if state.hasPrevious {
		switch bytes.Compare(key, state.previousKey) {
		case 0:
			return ErrDuplicateBuilderKey
		case -1:
			return ErrOutOfOrderKey
		}
	}

	path := bytesToNibbles(key)
	if len(path)+1 > state.limits.MaxTraversalDepth {
		return fmt.Errorf("%w: builder path depth exceeded", ErrResourceLimit)
	}
	common := 0
	if state.hasPrevious {
		common = commonPrefixLength(path, state.previous)
	}
	frames := append([]builderFrame(nil), state.frames...)
	budget := state.budget
	var err error
	frames, err = closeBuilderFrames(
		ctx, frames, state.previous, common, &budget,
	)
	if err != nil {
		return err
	}
	for len(frames) < len(path)+1 {
		frames = append(frames, builderFrame{})
	}
	frames[len(path)].value = append([]byte(nil), value...)

	state.frames = frames
	state.previousKey = append([]byte(nil), key...)
	state.previous = path
	state.hasPrevious = true
	state.budget = budget
	return nil
}

// Finalize returns the canonical 32-byte root exactly once. An empty builder
// returns EmptyRoot.
func (builder *SortedBuilder) Finalize(ctx context.Context) (Root, error) {
	if builder == nil || builder.state == nil {
		return Root{}, ErrUninitialized
	}
	state := builder.state
	if state.closed {
		return Root{}, ErrClosedBuilder
	}
	if err := checkContext(ctx); err != nil {
		return Root{}, err
	}
	if !state.hasPrevious {
		state.closed = true
		return EmptyRoot(), nil
	}

	frames := append([]builderFrame(nil), state.frames...)
	budget := state.budget
	frames, err := closeBuilderFrames(ctx, frames, state.previous, 0, &budget)
	if err != nil {
		return Root{}, err
	}
	rootNode, err := finishBuilderFrame(ctx, frames[0], &budget)
	if err != nil {
		return Root{}, err
	}
	encoded, err := rootNode.encoded()
	if err != nil {
		return Root{}, err
	}
	root, err := budget.hash(encoded)
	if err != nil {
		return Root{}, err
	}
	state.closed = true
	state.budget = budget
	return root, nil
}

func closeBuilderFrames(
	ctx context.Context,
	frames []builderFrame,
	previous []byte,
	keepDepth int,
	budget *builderBudget,
) ([]builderFrame, error) {
	for len(frames)-1 > keepDepth {
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		depth := len(frames) - 1
		child, err := finishBuilderFrame(ctx, frames[depth], budget)
		if err != nil {
			return nil, err
		}
		frames = frames[:depth]
		frames[depth-1].children[previous[depth-1]] = child
	}
	return frames, nil
}

func finishBuilderFrame(
	ctx context.Context,
	frame builderFrame,
	budget *builderBudget,
) (*builderNode, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if budget.nodesLeft == 0 {
		return nil, fmt.Errorf("%w: builder node bound exceeded", ErrResourceLimit)
	}
	budget.nodesLeft--

	count := 0
	only := 0
	for index, child := range frame.children {
		if child != nil {
			count++
			only = index
		}
	}
	if len(frame.value) != 0 {
		if count == 0 {
			return &builderNode{
				kind: builderLeaf, value: append([]byte(nil), frame.value...),
			}, nil
		}
		return newBuilderBranch(ctx, frame, budget)
	}
	switch count {
	case 0:
		return nil, fmt.Errorf("%w: empty streaming frame", ErrMalformedNode)
	case 1:
		return prependBuilderNibble(
			ctx, byte(only), frame.children[only], budget,
		)
	default:
		return newBuilderBranch(ctx, frame, budget)
	}
}

func newBuilderBranch(
	ctx context.Context,
	frame builderFrame,
	budget *builderBudget,
) (*builderNode, error) {
	values := make([]rlp.Value, 17)
	for index, child := range frame.children {
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		if child == nil {
			values[index] = rlp.String(nil)
			continue
		}
		reference, err := child.reference(budget)
		if err != nil {
			return nil, err
		}
		values[index] = reference
	}
	values[16] = rlp.String(frame.value)
	return &builderNode{kind: builderBranch, node: rlp.List(values...)}, nil
}

func prependBuilderNibble(
	ctx context.Context,
	nibble byte,
	child *builderNode,
	budget *builderBudget,
) (*builderNode, error) {
	switch child.kind {
	case builderLeaf:
		return &builderNode{
			kind:  builderLeaf,
			path:  append([]byte{nibble}, child.path...),
			value: append([]byte(nil), child.value...),
		}, nil
	case builderExtension:
		return &builderNode{
			kind:  builderExtension,
			path:  append([]byte{nibble}, child.path...),
			child: child.child,
		}, nil
	case builderBranch:
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		reference, err := child.reference(budget)
		if err != nil {
			return nil, err
		}
		return &builderNode{
			kind: builderExtension, path: []byte{nibble}, child: reference,
		}, nil
	default:
		return nil, fmt.Errorf("%w: invalid streaming node", ErrMalformedNode)
	}
}

func (current *builderNode) valueRLP() (rlp.Value, error) {
	switch current.kind {
	case builderLeaf:
		compact, err := EncodeCompactPath(current.path, true)
		if err != nil {
			return rlp.Value{}, fmt.Errorf("%w: streaming leaf path", ErrMalformedNode)
		}
		return rlp.List(rlp.String(compact), rlp.String(current.value)), nil
	case builderExtension:
		compact, err := EncodeCompactPath(current.path, false)
		if err != nil {
			return rlp.Value{}, fmt.Errorf("%w: streaming extension path", ErrMalformedNode)
		}
		return rlp.List(rlp.String(compact), current.child), nil
	case builderBranch:
		return current.node, nil
	default:
		return rlp.Value{}, fmt.Errorf("%w: invalid streaming node", ErrMalformedNode)
	}
}

func (current *builderNode) encoded() ([]byte, error) {
	value, err := current.valueRLP()
	if err != nil {
		return nil, err
	}
	encoded, err := rlp.Encode(value, rlp.DefaultLimits())
	if err != nil {
		return nil, fmt.Errorf("%w: streaming node encoding", ErrResourceLimit)
	}
	return encoded, nil
}

func (current *builderNode) reference(
	budget *builderBudget,
) (rlp.Value, error) {
	value, err := current.valueRLP()
	if err != nil {
		return rlp.Value{}, err
	}
	encoded, err := current.encoded()
	if err != nil {
		return rlp.Value{}, err
	}
	if len(encoded) < RootBytes {
		return value, nil
	}
	root, err := budget.hash(encoded)
	if err != nil {
		return rlp.Value{}, err
	}
	return rlp.String(root[:]), nil
}

func (budget *builderBudget) hash(value []byte) (Root, error) {
	if budget.hashesLeft == 0 {
		return Root{}, fmt.Errorf("%w: builder hash bound exceeded", ErrResourceLimit)
	}
	budget.hashesLeft--
	return keccakRoot(value), nil
}
