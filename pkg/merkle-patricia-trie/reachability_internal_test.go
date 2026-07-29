package mpt

import (
	"context"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/merkle-patricia-trie/internal/rlp"
)

func TestReachabilityLimitsRejectEachInvalidBound(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		invalidate func(*ReachabilityLimits)
	}{
		{"roots", func(limits *ReachabilityLimits) { limits.MaxRoots = 0 }},
		{"retentions", func(limits *ReachabilityLimits) { limits.MaxRetentions = 0 }},
		{"nodes", func(limits *ReachabilityLimits) { limits.MaxNodes = 0 }},
		{"bytes", func(limits *ReachabilityLimits) { limits.MaxBytes = 0 }},
		{"depth", func(limits *ReachabilityLimits) { limits.MaxDepth = 0 }},
		{"reads", func(limits *ReachabilityLimits) { limits.MaxNodeReads = 0 }},
		{"hashes", func(limits *ReachabilityLimits) { limits.MaxHashOperations = 0 }},
		{"depth overflow", func(limits *ReachabilityLimits) {
			limits.MaxDepth = MaxCompactPathNibbles + 2
		}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			limits := DefaultReachabilityLimits()
			test.invalidate(&limits)
			if err := validateReachabilityLimits(limits); !errors.Is(err, ErrResourceLimit) {
				t.Fatalf("validateReachabilityLimits() error = %v", err)
			}
		})
	}
}

func TestReachabilityExactBoundsAndFailureClassification(t *testing.T) {
	t.Parallel()

	encoded, _, err := encodeNode(&leafNode{path: []byte{1}, value: []byte{2}})
	if err != nil {
		t.Fatalf("encodeNode() error = %v", err)
	}
	root := keccakRoot(encoded)
	reader := nodeReaderFunc(func(context.Context, Root) ([]byte, error) {
		return encoded, nil
	})
	exact := ReachabilityLimits{
		MaxRoots: 1, MaxRetentions: 1, MaxNodes: 1, MaxBytes: len(encoded),
		MaxDepth: 1, MaxNodeReads: 1, MaxHashOperations: 1,
	}
	nodes, err := CollectReachableNodes(
		context.Background(), []Root{root}, reader, exact,
	)
	if err != nil || len(nodes) != 1 {
		t.Fatalf("CollectReachableNodes(exact) = (%d, %v)", len(nodes), err)
	}

	tests := []struct {
		name      string
		constrain func(*ReachabilityLimits)
	}{
		{"roots", func(limits *ReachabilityLimits) { limits.MaxRoots = 1 }},
		{"nodes", func(limits *ReachabilityLimits) { limits.MaxNodes = 0 }},
		{"bytes", func(limits *ReachabilityLimits) { limits.MaxBytes-- }},
		{"reads", func(limits *ReachabilityLimits) { limits.MaxNodeReads = 0 }},
		{"hashes", func(limits *ReachabilityLimits) { limits.MaxHashOperations = 0 }},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			limits := exact
			test.constrain(&limits)
			roots := []Root{root}
			if test.name == "roots" {
				roots = append(roots, root)
			}
			if _, collectErr := CollectReachableNodes(
				context.Background(), roots, reader, limits,
			); !errors.Is(collectErr, ErrResourceLimit) {
				t.Fatalf("CollectReachableNodes() error = %v", collectErr)
			}
		})
	}

	var unknown Root
	unknown[0] = 1
	if _, err = CollectReachableNodes(
		context.Background(),
		[]Root{unknown},
		nodeReaderFunc(func(context.Context, Root) ([]byte, error) {
			return nil, ErrMissingNode
		}),
		DefaultReachabilityLimits(),
	); !errors.Is(err, ErrMissingNode) {
		t.Fatalf("missing CollectReachableNodes() error = %v", err)
	}
	readFailure := errors.New("read failed")
	if _, err = CollectReachableNodes(
		context.Background(),
		[]Root{unknown},
		nodeReaderFunc(func(context.Context, Root) ([]byte, error) {
			return nil, readFailure
		}),
		DefaultReachabilityLimits(),
	); !errors.Is(err, ErrStorageRead) || !errors.Is(err, readFailure) {
		t.Fatalf("failed-read CollectReachableNodes() error = %v", err)
	}
	if _, err = CollectReachableNodes(
		context.Background(), []Root{unknown}, reader,
		DefaultReachabilityLimits(),
	); !errors.Is(err, ErrCorruptNode) {
		t.Fatalf("hash-mismatch CollectReachableNodes() error = %v", err)
	}
	malformedEncoding := []byte{0xc0}
	if _, err = CollectReachableNodes(
		context.Background(),
		[]Root{keccakRoot(malformedEncoding)},
		nodeReaderFunc(func(context.Context, Root) ([]byte, error) {
			return malformedEncoding, nil
		}),
		DefaultReachabilityLimits(),
	); !errors.Is(err, ErrCorruptNode) {
		t.Fatalf("malformed-node CollectReachableNodes() error = %v", err)
	}
}

func TestReachabilityRejectsHashedCompactExtensionChild(t *testing.T) {
	t.Parallel()

	leafEncoding, err := rlp.Encode(
		rlp.List(rlp.String([]byte{0x31}), rlp.String([]byte{1})),
		rlp.DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("encode leaf: %v", err)
	}
	leafRoot := keccakRoot(leafEncoding)
	rootEncoding, err := rlp.Encode(
		rlp.List(rlp.String([]byte{0x11}), rlp.String(leafRoot[:])),
		rlp.DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("encode extension: %v", err)
	}
	root := keccakRoot(rootEncoding)
	nodes := map[Root][]byte{root: rootEncoding, leafRoot: leafEncoding}
	_, err = CollectReachableNodes(
		context.Background(),
		[]Root{root},
		nodeReaderFunc(func(_ context.Context, hash Root) ([]byte, error) {
			return nodes[hash], nil
		}),
		DefaultReachabilityLimits(),
	)
	if !errors.Is(err, ErrCorruptNode) || !errors.Is(err, ErrMalformedNode) {
		t.Fatalf("CollectReachableNodes() error = %v", err)
	}
}

func TestReachabilityRejectsCycleAndInvalidUse(t *testing.T) {
	t.Parallel()

	var root Root
	state := reachabilityState{
		ctx:        context.Background(),
		reader:     nodeReaderFunc(func(context.Context, Root) ([]byte, error) { return nil, nil }),
		limits:     DefaultReachabilityLimits(),
		readsLeft:  1,
		hashesLeft: 1,
		nodesLeft:  1,
		bytesLeft:  1,
		active:     map[Root]struct{}{root: {}},
		nodes:      make(map[Root][]byte),
		decoded:    make(map[Root]node),
	}
	if _, err := state.collectHash(root, 0); !errors.Is(err, ErrCorruptNode) {
		t.Fatalf("collectHash(cycle) error = %v", err)
	}
	if _, err := CollectReachableNodes(
		nil, nil, readerNeverCalled{}, DefaultReachabilityLimits(),
	); !errors.Is(err, ErrInvalidContext) {
		t.Fatalf("CollectReachableNodes(nil context) error = %v", err)
	}
	if _, err := CollectReachableNodes(
		context.Background(), nil, nil, DefaultReachabilityLimits(),
	); !errors.Is(err, ErrInvalidStore) {
		t.Fatalf("CollectReachableNodes(nil reader) error = %v", err)
	}
	nodes, err := CollectReachableNodes(
		context.Background(),
		[]Root{EmptyRoot(), EmptyRoot()},
		readerNeverCalled{},
		DefaultReachabilityLimits(),
	)
	if err != nil || len(nodes) != 0 {
		t.Fatalf("CollectReachableNodes(empty roots) = (%d, %v)", len(nodes), err)
	}
}

type readerNeverCalled struct{}

func (readerNeverCalled) GetNode(context.Context, Root) ([]byte, error) {
	panic("empty root must not read storage")
}
