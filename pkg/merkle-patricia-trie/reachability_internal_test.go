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
		rlp.List(
			rlp.String([]byte{0x31}),
			rlp.String(make([]byte, RootBytes)),
		),
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
	var nilContext context.Context
	if _, err := CollectReachableNodes(
		nilContext, nil, readerNeverCalled{}, DefaultReachabilityLimits(),
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

func TestReachabilityTraversesSharedAndEmbeddedChildren(t *testing.T) {
	t.Parallel()

	child := &leafNode{path: []byte{1}, value: make([]byte, 40)}
	childEncoding, _, err := encodeNode(child)
	if err != nil {
		t.Fatalf("encode child: %v", err)
	}
	childRoot := keccakRoot(childEncoding)
	var children [16]node
	children[0] = hashNode(childRoot)
	children[1] = hashNode(childRoot)
	rootNode := &branchNode{children: children}
	rootEncoding, _, err := encodeNode(rootNode)
	if err != nil {
		t.Fatalf("encode root: %v", err)
	}
	root := keccakRoot(rootEncoding)
	stored := map[Root][]byte{root: rootEncoding, childRoot: childEncoding}
	nodes, err := CollectReachableNodes(
		context.Background(),
		[]Root{root},
		nodeReaderFunc(func(_ context.Context, hash Root) ([]byte, error) {
			return stored[hash], nil
		}),
		DefaultReachabilityLimits(),
	)
	if err != nil || len(nodes) != 2 {
		t.Fatalf("CollectReachableNodes(shared) = (%d, %v)", len(nodes), err)
	}

	embedded := &extensionNode{path: []byte{1}, child: &branchNode{
		children: [16]node{0: &leafNode{path: []byte{2}, value: []byte{3}}},
	}}
	state := reachabilityState{
		ctx:       context.Background(),
		limits:    DefaultReachabilityLimits(),
		nodesLeft: DefaultReachabilityLimits().MaxNodes,
	}
	if err := state.collectNode(embedded, 0); err != nil {
		t.Fatalf("collectNode(embedded extension) error = %v", err)
	}
	depthLimited := reachabilityState{
		ctx:       context.Background(),
		limits:    ReachabilityLimits{MaxDepth: 1},
		nodesLeft: 3,
	}
	if err := depthLimited.collectNode(
		embedded, 0,
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("collectNode(embedded depth) error = %v", err)
	}
	nodeLimited := reachabilityState{
		ctx:       context.Background(),
		limits:    DefaultReachabilityLimits(),
		nodesLeft: 2,
	}
	if err := nodeLimited.collectNode(
		embedded, 0,
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("collectNode(embedded nodes) error = %v", err)
	}
	if err := state.collectNode(nil, 0); err != nil {
		t.Fatalf("collectNode(nil) error = %v", err)
	}
	if err := state.collectNode(struct{}{}, 0); !errors.Is(err, ErrMalformedNode) {
		t.Fatalf("collectNode(unsupported) error = %v", err)
	}
}

func TestReachabilityInternalBudgetsAndCancellation(t *testing.T) {
	t.Parallel()

	encoding, _, err := encodeNode(&leafNode{path: []byte{1}, value: []byte{2}})
	if err != nil {
		t.Fatalf("encodeNode() error = %v", err)
	}
	root := keccakRoot(encoding)
	base := func() reachabilityState {
		return reachabilityState{
			ctx: context.Background(),
			reader: nodeReaderFunc(func(context.Context, Root) ([]byte, error) {
				return encoding, nil
			}),
			limits:     DefaultReachabilityLimits(),
			readsLeft:  1,
			hashesLeft: 1,
			nodesLeft:  1,
			bytesLeft:  len(encoding),
			active:     make(map[Root]struct{}),
			nodes:      make(map[Root][]byte),
			decoded:    make(map[Root]node),
		}
	}
	tests := []struct {
		name      string
		constrain func(*reachabilityState)
	}{
		{"nodes", func(state *reachabilityState) { state.nodesLeft = 0 }},
		{"reads", func(state *reachabilityState) { state.readsLeft = 0 }},
		{"hashes", func(state *reachabilityState) { state.hashesLeft = 0 }},
		{"bytes", func(state *reachabilityState) { state.bytesLeft-- }},
		{"depth", func(state *reachabilityState) {
			state.limits.MaxDepth = 1
		}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			state := base()
			test.constrain(&state)
			depth := 0
			if test.name == "depth" {
				depth = 2
			}
			if _, err := state.collectHash(root, depth); !errors.Is(err, ErrResourceLimit) {
				t.Fatalf("collectHash() error = %v", err)
			}
		})
	}

	nullEncoding := []byte{0x80}
	state := base()
	state.reader = nodeReaderFunc(func(context.Context, Root) ([]byte, error) {
		return nullEncoding, nil
	})
	state.bytesLeft = len(nullEncoding)
	if _, err := state.collectHash(
		keccakRoot(nullEncoding), 0,
	); !errors.Is(err, ErrCorruptNode) {
		t.Fatalf("collectHash(null) error = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	state = base()
	state.ctx = canceled
	if err := state.collectNode(&leafNode{}, 0); !errors.Is(err, ErrCanceled) {
		t.Fatalf("collectNode(canceled) error = %v", err)
	}
	if err := state.visit(0); !errors.Is(err, ErrCanceled) {
		t.Fatalf("visit(canceled) error = %v", err)
	}

	var missing Root
	missing[0] = 1
	missingState := reachabilityState{
		ctx: context.Background(),
		reader: nodeReaderFunc(func(context.Context, Root) ([]byte, error) {
			return nil, ErrMissingNode
		}),
		limits:     DefaultReachabilityLimits(),
		readsLeft:  2,
		hashesLeft: 2,
		nodesLeft:  2,
		bytesLeft:  2,
		active:     make(map[Root]struct{}),
		nodes:      make(map[Root][]byte),
		decoded:    make(map[Root]node),
	}
	if err := missingState.collectNode(
		&extensionNode{path: []byte{1}, child: hashNode(missing)}, 0,
	); !errors.Is(err, ErrMissingNode) {
		t.Fatalf("collectNode(missing extension child) error = %v", err)
	}
	missingState.readsLeft = 2
	missingState.nodesLeft = 2
	var children [16]node
	children[0] = hashNode(missing)
	if err := missingState.collectNode(
		&branchNode{children: children}, 0,
	); !errors.Is(err, ErrMissingNode) {
		t.Fatalf("collectNode(missing branch child) error = %v", err)
	}
}

func TestPruneResultReportsStoreCountsAndBytes(t *testing.T) {
	t.Parallel()

	result := NewPruneResult(5, 3, 42)
	if result.StoredBefore() != 5 ||
		result.StoredAfter() != 3 ||
		result.RemovedNodes() != 2 ||
		result.RemovedBytes() != 42 {
		t.Fatalf("PruneResult = %+v", result)
	}
}

func TestHashedEmbeddedSizeChildIsRejectedAcrossConsumers(t *testing.T) {
	t.Parallel()

	leafEncoding, err := rlp.Encode(
		rlp.List(rlp.String([]byte{0x32}), rlp.String([]byte("value"))),
		rlp.DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("encode leaf: %v", err)
	}
	if len(leafEncoding) >= RootBytes {
		t.Fatalf("leaf encoding has %d bytes, want embedded size", len(leafEncoding))
	}
	leafRoot := keccakRoot(leafEncoding)
	elements := make([]rlp.Value, 17)
	for index := range 16 {
		elements[index] = rlp.String(nil)
	}
	elements[1] = rlp.String(leafRoot[:])
	elements[16] = rlp.String([]byte("branch-value"))
	rootEncoding, err := rlp.Encode(rlp.List(elements...), rlp.DefaultLimits())
	if err != nil {
		t.Fatalf("encode branch: %v", err)
	}
	root := keccakRoot(rootEncoding)
	nodes := map[Root][]byte{root: rootEncoding, leafRoot: leafEncoding}
	reader := nodeReaderFunc(func(_ context.Context, hash Root) ([]byte, error) {
		encoded, exists := nodes[hash]
		if !exists {
			return nil, ErrMissingNode
		}
		return encoded, nil
	})
	trie, err := LoadRawTrie(root, reader, DefaultLimits())
	if err != nil {
		t.Fatalf("LoadRawTrie() error = %v", err)
	}
	key := []byte{0x12}

	t.Run("lookup", func(t *testing.T) {
		if _, err := trie.Get(
			context.Background(), key,
		); !errors.Is(err, ErrCorruptNode) {
			t.Fatalf("Get() error = %v", err)
		}
	})
	t.Run("update", func(t *testing.T) {
		if _, err := trie.Update(
			context.Background(), key, []byte("next"),
		); !errors.Is(err, ErrCorruptNode) {
			t.Fatalf("Update() error = %v", err)
		}
	})
	t.Run("delete", func(t *testing.T) {
		if _, err := trie.Delete(
			context.Background(), key,
		); !errors.Is(err, ErrCorruptNode) {
			t.Fatalf("Delete() error = %v", err)
		}
	})
	t.Run("iteration", func(t *testing.T) {
		err := trie.Iterate(
			context.Background(),
			IterationOptions{},
			func(Entry) error { return nil },
		)
		if !errors.Is(err, ErrCorruptNode) {
			t.Fatalf("Iterate() error = %v", err)
		}
	})
	t.Run("proof generation", func(t *testing.T) {
		if _, err := trie.Prove(
			context.Background(), key,
		); !errors.Is(err, ErrCorruptNode) {
			t.Fatalf("Prove() error = %v", err)
		}
		if _, err := trie.ProveMany(
			context.Background(), [][]byte{key},
		); !errors.Is(err, ErrCorruptNode) {
			t.Fatalf("ProveMany() error = %v", err)
		}
	})
	t.Run("rebuild", func(t *testing.T) {
		if _, err := trie.Rebuild(
			context.Background(),
		); !errors.Is(err, ErrCorruptNode) {
			t.Fatalf("Rebuild() error = %v", err)
		}
	})
	t.Run("reachability", func(t *testing.T) {
		if _, err := CollectReachableNodes(
			context.Background(),
			[]Root{root},
			reader,
			DefaultReachabilityLimits(),
		); !errors.Is(err, ErrCorruptNode) {
			t.Fatalf("CollectReachableNodes() error = %v", err)
		}
	})

	proof, err := ProofFromNodes(
		[][]byte{rootEncoding, leafEncoding}, DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("ProofFromNodes() error = %v", err)
	}
	t.Run("proof verification", func(t *testing.T) {
		if err := VerifyRawMembership(
			context.Background(),
			root,
			key,
			[]byte("value"),
			proof,
			DefaultLimits(),
		); !errors.Is(err, ErrMalformedProof) {
			t.Fatalf("VerifyRawMembership() error = %v", err)
		}
	})
	multiProof, err := MultiProofFromNodes(
		[][]byte{rootEncoding, leafEncoding}, DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("MultiProofFromNodes() error = %v", err)
	}
	t.Run("multi-proof verification", func(t *testing.T) {
		if err := VerifyRawMultiProof(
			context.Background(),
			root,
			[]ProofClaim{MembershipClaim(key, []byte("value"))},
			multiProof,
			DefaultLimits(),
		); !errors.Is(err, ErrMalformedProof) {
			t.Fatalf("VerifyRawMultiProof() error = %v", err)
		}
	})
}

type readerNeverCalled struct{}

func (readerNeverCalled) GetNode(context.Context, Root) ([]byte, error) {
	panic("empty root must not read storage")
}
