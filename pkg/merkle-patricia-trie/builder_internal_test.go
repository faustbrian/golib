package mpt

import (
	"context"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/merkle-patricia-trie/internal/rlp"
)

func TestSortedBuilderRetainsOnlyBoundedFrontier(t *testing.T) {
	t.Parallel()
	builder, err := NewSortedBuilder(DefaultLimits())
	if err != nil {
		t.Fatalf("NewSortedBuilder() error = %v", err)
	}
	for index := range 4096 {
		key := make([]byte, 2)
		binary.BigEndian.PutUint16(key, uint16(index))
		if err := builder.Add(context.Background(), key, []byte{1}); err != nil {
			t.Fatalf("Add(%d) error = %v", index, err)
		}
		if got, maximum := retainedBuilderNodes(builder.state.frames), 16*5; got > maximum {
			t.Fatalf("retained nodes = %d, want <= %d", got, maximum)
		}
	}
}

func retainedBuilderNodes(frames []builderFrame) int {
	count := 0
	for _, frame := range frames {
		for _, child := range frame.children {
			if child != nil {
				count++
			}
		}
	}
	return count
}

func TestSortedBuilderInternalFailurePaths(t *testing.T) {
	t.Parallel()
	invalid := DefaultLimits()
	invalid.MaxKeyBytes = 0
	if _, err := NewSortedBuilder(invalid); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("NewSortedBuilder(invalid) error = %v", err)
	}

	depthLimits := DefaultLimits()
	depthLimits.MaxTraversalDepth = 1
	builder, err := NewSortedBuilder(depthLimits)
	if err != nil {
		t.Fatalf("NewSortedBuilder() error = %v", err)
	}
	if err := builder.Add(
		context.Background(), []byte{1}, []byte{1},
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("depth Add() error = %v", err)
	}
	exactLimits := DefaultLimits()
	exactLimits.MaxKeyBytes = 1
	exactLimits.MaxValueBytes = 1
	exactLimits.MaxTraversalDepth = 3
	exact, err := NewSortedBuilder(exactLimits)
	if err != nil {
		t.Fatalf("NewSortedBuilder(exact) error = %v", err)
	}
	if err := exact.Add(
		context.Background(), []byte{1}, []byte{1},
	); err != nil {
		t.Fatalf("exact-bound Add() error = %v", err)
	}

	builder, _ = NewSortedBuilder(DefaultLimits())
	if err := builder.Add(context.Background(), []byte{1}, []byte{1}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := builder.Finalize(canceled); !errors.Is(err, ErrCanceled) {
		t.Fatalf("canceled Finalize() error = %v", err)
	}

	frames := []builderFrame{{}, {value: []byte{1}}}
	budget := builderBudget{nodesLeft: 2, hashesLeft: 2}
	if _, err := closeBuilderFrames(
		canceled, frames, []byte{1}, 0, &budget,
	); !errors.Is(err, ErrCanceled) {
		t.Fatalf("canceled closeBuilderFrames() error = %v", err)
	}
	if _, err := finishBuilderFrame(
		canceled, builderFrame{value: []byte{1}}, &budget,
	); !errors.Is(err, ErrCanceled) {
		t.Fatalf("canceled finishBuilderFrame() error = %v", err)
	}
	emptyBudget := builderBudget{}
	if _, err := finishBuilderFrame(
		context.Background(), builderFrame{value: []byte{1}}, &emptyBudget,
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("bounded finishBuilderFrame() error = %v", err)
	}
	budget = builderBudget{nodesLeft: 1, hashesLeft: 1}
	if _, err := finishBuilderFrame(
		context.Background(), builderFrame{}, &budget,
	); !errors.Is(err, ErrMalformedNode) {
		t.Fatalf("empty finishBuilderFrame() error = %v", err)
	}
	budget = builderBudget{nodesLeft: 2, hashesLeft: 2}
	if _, err := finishBuilderFrame(
		context.Background(), builderFrame{value: []byte{1}}, &budget,
	); err != nil {
		t.Fatalf("finishBuilderFrame() error = %v", err)
	}
	if budget.nodesLeft != 1 {
		t.Fatalf("nodes left = %d, want 1", budget.nodesLeft)
	}
	if _, err := budget.hash([]byte{1}); err != nil {
		t.Fatalf("hash() error = %v", err)
	}
	if budget.hashesLeft != 1 {
		t.Fatalf("hashes left = %d, want 1", budget.hashesLeft)
	}

	builder, _ = NewSortedBuilder(DefaultLimits())
	if err := builder.Add(context.Background(), []byte{1}, []byte{1}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	builder.state.budget.nodesLeft = 0
	if err := builder.Add(
		context.Background(), []byte{2}, []byte{1},
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("Add() close failure = %v", err)
	}
	if _, err := builder.Finalize(
		context.Background(),
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("Finalize() close failure = %v", err)
	}

	builder = &SortedBuilder{state: &sortedBuilderState{
		limits: DefaultLimits(), frames: []builderFrame{{value: []byte{1}}},
		previous: []byte{}, hasPrevious: true,
		budget: builderBudget{nodesLeft: 0, hashesLeft: 1},
	}}
	if _, err := builder.Finalize(
		context.Background(),
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("Finalize() root-frame failure = %v", err)
	}

	builder = &SortedBuilder{state: &sortedBuilderState{
		limits: DefaultLimits(),
		frames: []builderFrame{{
			value: make([]byte, rlp.DefaultLimits().MaxEncodedBytes),
		}},
		hasPrevious: true,
		budget:      builderBudget{nodesLeft: 1, hashesLeft: 1},
	}}
	if _, err := builder.Finalize(
		context.Background(),
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("Finalize() encoding failure = %v", err)
	}

	builder = &SortedBuilder{state: &sortedBuilderState{
		limits: DefaultLimits(), frames: []builderFrame{{value: []byte{1}}},
		hasPrevious: true,
		budget:      builderBudget{nodesLeft: 1, hashesLeft: 0},
	}}
	if _, err := builder.Finalize(
		context.Background(),
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("Finalize() hash failure = %v", err)
	}
}

func TestBuilderNodeInternalFailurePaths(t *testing.T) {
	t.Parallel()
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	child := &builderNode{kind: builderLeaf, value: []byte{1}}
	frame := builderFrame{}
	frame.children[0] = child
	frame.children[1] = child
	budget := builderBudget{nodesLeft: 8, hashesLeft: 8}
	if _, err := newBuilderBranch(canceled, frame, &budget); !errors.Is(err, ErrCanceled) {
		t.Fatalf("canceled newBuilderBranch() error = %v", err)
	}

	extension := &builderNode{
		kind: builderExtension, path: []byte{2}, child: rlp.String([]byte{1}),
	}
	got, err := prependBuilderNibble(
		context.Background(), 1, extension, &budget,
	)
	if err != nil || got.kind != builderExtension ||
		len(got.path) != 2 || got.path[0] != 1 || got.path[1] != 2 {
		t.Fatalf("prepend extension = %#v, %v", got, err)
	}
	if _, err := prependBuilderNibble(
		context.Background(), 1, &builderNode{}, &budget,
	); !errors.Is(err, ErrMalformedNode) {
		t.Fatalf("prepend invalid error = %v", err)
	}

	branch := &builderNode{
		kind: builderBranch,
		node: rlp.List(
			rlp.String(nil), rlp.String(nil), rlp.String(nil),
			rlp.String(nil), rlp.String(nil), rlp.String(nil),
			rlp.String(nil), rlp.String(nil), rlp.String(nil),
			rlp.String(nil), rlp.String(nil), rlp.String(nil),
			rlp.String(nil), rlp.String(nil), rlp.String(nil),
			rlp.String(nil), rlp.String([]byte{1}),
		),
	}
	if _, err := prependBuilderNibble(
		canceled, 1, branch, &budget,
	); !errors.Is(err, ErrCanceled) {
		t.Fatalf("prepend canceled branch error = %v", err)
	}
	zeroHashes := builderBudget{hashesLeft: 0}
	largeBranch := &builderNode{
		kind: builderBranch,
		node: rlp.List(rlp.String(make([]byte, 64))),
	}
	failingFrame := builderFrame{}
	failingFrame.children[0] = largeBranch
	failingFrame.children[1] = child
	if _, err := newBuilderBranch(
		context.Background(), failingFrame, &zeroHashes,
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("newBuilderBranch() reference error = %v", err)
	}
	if _, err := prependBuilderNibble(
		context.Background(), 1, largeBranch, &zeroHashes,
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("prepend branch reference error = %v", err)
	}
	if _, err := largeBranch.reference(&zeroHashes); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("bounded reference error = %v", err)
	}
	if _, err := zeroHashes.hash([]byte{1}); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("bounded hash error = %v", err)
	}
}

func TestBuilderNodeEncodingFailurePaths(t *testing.T) {
	t.Parallel()
	invalid := &builderNode{}
	if _, err := invalid.valueRLP(); !errors.Is(err, ErrMalformedNode) {
		t.Fatalf("invalid valueRLP() error = %v", err)
	}
	if _, err := invalid.encoded(); !errors.Is(err, ErrMalformedNode) {
		t.Fatalf("invalid encoded() error = %v", err)
	}

	badLeaf := &builderNode{
		kind: builderLeaf, path: []byte{16}, value: []byte{1},
	}
	if _, err := badLeaf.valueRLP(); !errors.Is(err, ErrMalformedNode) {
		t.Fatalf("invalid leaf valueRLP() error = %v", err)
	}
	badExtension := &builderNode{
		kind: builderExtension, path: []byte{16}, child: rlp.String([]byte{1}),
	}
	if _, err := badExtension.valueRLP(); !errors.Is(err, ErrMalformedNode) {
		t.Fatalf("invalid extension valueRLP() error = %v", err)
	}

	oversized := &builderNode{
		kind:  builderLeaf,
		value: make([]byte, rlp.DefaultLimits().MaxEncodedBytes),
	}
	if _, err := oversized.encoded(); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("oversized encoded() error = %v", err)
	}
	if _, err := invalid.reference(
		&builderBudget{hashesLeft: 1},
	); !errors.Is(err, ErrMalformedNode) {
		t.Fatalf("invalid reference() error = %v", err)
	}
	if _, err := oversized.reference(
		&builderBudget{hashesLeft: 1},
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("oversized reference() error = %v", err)
	}
}
