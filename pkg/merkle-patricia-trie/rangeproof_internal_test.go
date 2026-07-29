package mpt

import (
	"context"
	"errors"
	"slices"
	"testing"
)

func TestRangeProofGenerationInternalFailureContracts(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	all, err := newRangeBounds(nil, nil, limits, false)
	if err != nil {
		t.Fatalf("newRangeBounds() error = %v", err)
	}
	newState := func() *rangeGenerationState {
		return &rangeGenerationState{
			builder: newMultiProofBuilder(
				context.Background(), &trieSnapshot{limits: limits},
			),
			bounds: all,
		}
	}

	outside, err := newRangeBounds(
		[]byte("b"), []byte("c"), limits, false,
	)
	if err != nil {
		t.Fatalf("newRangeBounds(outside) error = %v", err)
	}
	outsideState := newState()
	outsideState.bounds = outside
	if err := outsideState.walk(
		&leafNode{value: []byte("value")},
		bytesToNibbles([]byte("a")),
		0,
	); err != nil {
		t.Fatalf("walk(outside) error = %v", err)
	}

	exhausted := newState()
	exhausted.builder.state.nodesLeft = 0
	if err := exhausted.walk(
		&leafNode{value: []byte("value")}, nil, 0,
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("walk(exhausted) error = %v", err)
	}
	if err := newState().walk(nil, nil, 0); err != nil {
		t.Fatalf("walk(nil) error = %v", err)
	}
	if err := newState().walk(hashNode(Root{1}), nil, 0); err == nil {
		t.Fatal("walk(missing hash) succeeded")
	}

	encodedLeaf, _, err := encodeNode(&leafNode{
		path:  nil,
		value: []byte("a persisted leaf value long enough to hash"),
	})
	if err != nil {
		t.Fatalf("encodeNode() error = %v", err)
	}
	leafHash := keccakRoot(encodedLeaf)
	hashed := newState()
	hashed.builder.pending[leafHash] = encodedLeaf
	if err := hashed.walk(hashNode(leafHash), nil, 0); err != nil {
		t.Fatalf("walk(hashed leaf) error = %v", err)
	}

	extensionOutside := newState()
	extensionOutside.bounds = outside
	if err := extensionOutside.walk(&extensionNode{
		path:  bytesToNibbles([]byte("a")),
		child: &branchNode{},
	}, nil, 0); err != nil {
		t.Fatalf("walk(extension outside) error = %v", err)
	}
	invalidExtension := newState()
	invalidExtension.builder.pending[leafHash] = encodedLeaf
	if err := invalidExtension.walk(&extensionNode{
		path:  []byte{1},
		child: hashNode(leafHash),
	}, nil, 0); !errors.Is(err, ErrMalformedNode) {
		t.Fatalf("walk(extension to leaf) error = %v", err)
	}

	oddValue := newState()
	if err := oddValue.walk(
		&branchNode{value: []byte("value")}, []byte{1}, 0,
	); !errors.Is(err, ErrMalformedNode) {
		t.Fatalf("walk(odd branch value) error = %v", err)
	}
	missingChild := newState()
	missingChildBranch := &branchNode{}
	missingChildBranch.children[0] = hashNode(Root{2})
	if err := missingChild.walk(
		missingChildBranch, nil, 0,
	); err == nil {
		t.Fatal("walk(missing branch child) succeeded")
	}
	invalidChild := newState()
	invalidChildBranch := &branchNode{}
	invalidChildBranch.children[0] = &leafNode{
		path:  []byte{1, 2},
		value: []byte("value"),
	}
	if err := invalidChild.walk(invalidChildBranch, nil, 0); !errors.Is(
		err, ErrMalformedNode,
	) {
		t.Fatalf("walk(invalid branch child) error = %v", err)
	}
	if err := newState().walk(struct{}{}, nil, 0); !errors.Is(
		err, ErrMalformedNode,
	) {
		t.Fatalf("walk(unsupported) error = %v", err)
	}

	limited := newState()
	limited.builder.snapshot.limits.MaxProofKeys = 0
	if err := limited.emit(nil, []byte("value")); !errors.Is(
		err, ErrResourceLimit,
	) {
		t.Fatalf("emit(item limit) error = %v", err)
	}
	if err := newState().emit([]byte{1}, []byte("value")); !errors.Is(
		err, ErrMalformedNode,
	) {
		t.Fatalf("emit(odd path) error = %v", err)
	}
	secureState := newState()
	secureState.bounds.secure = true
	if err := secureState.emit(nil, []byte("value")); !errors.Is(
		err, ErrMalformedNode,
	) {
		t.Fatalf("emit(short secure path) error = %v", err)
	}

	if _, _, err := proveRangeSnapshot(
		context.Background(),
		&trieSnapshot{root: struct{}{}, limits: limits},
		nil,
		nil,
		false,
	); !errors.Is(err, ErrMalformedNode) {
		t.Fatalf("proveRangeSnapshot(invalid root) error = %v", err)
	}
	oddLeaf := &leafNode{path: []byte{1}, value: []byte("value")}
	oddEncoded, _, err := encodeNode(oddLeaf)
	if err != nil {
		t.Fatalf("encodeNode(odd leaf) error = %v", err)
	}
	if _, _, err := proveRangeSnapshot(
		context.Background(),
		&trieSnapshot{
			root: oddLeaf, hash: keccakRoot(oddEncoded), limits: limits,
		},
		nil,
		nil,
		false,
	); !errors.Is(err, ErrMalformedNode) {
		t.Fatalf("proveRangeSnapshot(odd leaf) error = %v", err)
	}
}

func TestRangeProofVerificationInternalFailureContracts(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	all, err := newRangeBounds(nil, nil, limits, false)
	if err != nil {
		t.Fatalf("newRangeBounds() error = %v", err)
	}
	lookup := func() *multiProofLookup {
		return &multiProofLookup{
			nodes:  make(map[Root]multiProofNode),
			used:   make(map[Root]struct{}),
			budget: &workBudget{hashesLeft: limits.MaxHashOperations},
		}
	}
	newState := func() *rangeVerificationState {
		return &rangeVerificationState{
			ctx:       context.Background(),
			lookup:    lookup(),
			bounds:    all,
			nodesLeft: limits.MaxTraversalNodes,
			maxDepth:  limits.MaxTraversalDepth,
		}
	}

	outside, err := newRangeBounds(
		[]byte("b"), []byte("c"), limits, false,
	)
	if err != nil {
		t.Fatalf("newRangeBounds(outside) error = %v", err)
	}
	outsideState := newState()
	outsideState.bounds = outside
	if err := outsideState.walk(
		&leafNode{value: []byte("value")},
		bytesToNibbles([]byte("a")),
		0,
	); err != nil {
		t.Fatalf("walk(outside) error = %v", err)
	}

	canceled := newState()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	canceled.ctx = ctx
	if err := canceled.walk(
		&leafNode{value: []byte("value")}, nil, 0,
	); !errors.Is(err, ErrCanceled) {
		t.Fatalf("walk(canceled) error = %v", err)
	}
	exhausted := newState()
	exhausted.nodesLeft = 0
	if err := exhausted.walk(
		&leafNode{value: []byte("value")}, nil, 0,
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("walk(exhausted) error = %v", err)
	}
	if err := newState().walk(nil, nil, 0); err != nil {
		t.Fatalf("walk(nil) error = %v", err)
	}
	if err := newState().walk(hashNode(Root{1}), nil, 0); !errors.Is(
		err, ErrIncompleteProof,
	) {
		t.Fatalf("walk(missing hash) error = %v", err)
	}

	encodedLeaf, _, err := encodeNode(&leafNode{
		value: []byte("a persisted leaf value long enough to hash"),
	})
	if err != nil {
		t.Fatalf("encodeNode() error = %v", err)
	}
	leafHash := keccakRoot(encodedLeaf)
	hashed := newState()
	hashed.lookup.nodes[leafHash] = multiProofNode{
		decoded: &leafNode{value: []byte("value")},
		size:    len(encodedLeaf),
	}
	hashed.lookup.order = []Root{leafHash}
	hashed.items = []RangeItem{NewRangeItem(nil, []byte("value"))}
	if err := hashed.walk(hashNode(leafHash), nil, 0); err != nil {
		t.Fatalf("walk(hashed leaf) error = %v", err)
	}

	extensionOutside := newState()
	extensionOutside.bounds = outside
	if err := extensionOutside.walk(&extensionNode{
		path:  bytesToNibbles([]byte("a")),
		child: &branchNode{},
	}, nil, 0); err != nil {
		t.Fatalf("walk(extension outside) error = %v", err)
	}
	missingExtension := newState()
	if err := missingExtension.walk(&extensionNode{
		path: []byte{1}, child: hashNode(Root{2}),
	}, nil, 0); !errors.Is(err, ErrIncompleteProof) {
		t.Fatalf("walk(missing extension) error = %v", err)
	}
	invalidExtension := newState()
	invalidExtension.lookup.nodes[leafHash] = multiProofNode{
		decoded: &leafNode{value: []byte("value")},
		size:    len(encodedLeaf),
	}
	invalidExtension.lookup.order = []Root{leafHash}
	if err := invalidExtension.walk(&extensionNode{
		path: []byte{1}, child: hashNode(leafHash),
	}, nil, 0); !errors.Is(err, ErrMalformedProof) {
		t.Fatalf("walk(extension to leaf) error = %v", err)
	}

	oddValue := newState()
	oddValue.items = []RangeItem{NewRangeItem(nil, []byte("value"))}
	if err := oddValue.walk(
		&branchNode{value: []byte("value")}, []byte{1}, 0,
	); !errors.Is(err, ErrMalformedProof) {
		t.Fatalf("walk(odd branch value) error = %v", err)
	}
	missingChild := newState()
	missingChildBranch := &branchNode{}
	missingChildBranch.children[0] = hashNode(Root{3})
	if err := missingChild.walk(
		missingChildBranch, nil, 0,
	); !errors.Is(err, ErrIncompleteProof) {
		t.Fatalf("walk(missing branch child) error = %v", err)
	}
	invalidChild := newState()
	invalidChild.items = []RangeItem{NewRangeItem(nil, []byte("value"))}
	invalidChildBranch := &branchNode{}
	invalidChildBranch.children[0] = &leafNode{
		path: []byte{1, 2}, value: []byte("value"),
	}
	if err := invalidChild.walk(invalidChildBranch, nil, 0); !errors.Is(
		err, ErrMalformedProof,
	) {
		t.Fatalf("walk(invalid branch child) error = %v", err)
	}
	if err := newState().walk(struct{}{}, nil, 0); !errors.Is(
		err, ErrMalformedProof,
	) {
		t.Fatalf("walk(unsupported) error = %v", err)
	}

	if err := newState().emit(nil, []byte("value")); !errors.Is(
		err, ErrFailedProof,
	) {
		t.Fatalf("emit(no item) error = %v", err)
	}
	oddLeaf := newState()
	oddLeaf.items = []RangeItem{NewRangeItem(nil, []byte("value"))}
	if err := oddLeaf.emit([]byte{1}, []byte("value")); !errors.Is(
		err, ErrMalformedProof,
	) {
		t.Fatalf("emit(odd path) error = %v", err)
	}
	mismatch := newState()
	mismatch.items = []RangeItem{NewRangeItem(nil, []byte("wrong"))}
	if err := mismatch.emit(nil, []byte("value")); !errors.Is(
		err, ErrFailedProof,
	) {
		t.Fatalf("emit(mismatch) error = %v", err)
	}

	secure := all
	secure.secure = true
	if err := validateRangeItems(
		[]RangeItem{NewRangeItem([]byte{1}, []byte("value"))},
		secure,
		limits,
	); !errors.Is(err, ErrInvalidProofClaim) {
		t.Fatalf("validateRangeItems(short secure key) error = %v", err)
	}
	if _, err := rangePathKey([]byte{0, 0}, true); !errors.Is(
		err, ErrMalformedNode,
	) {
		t.Fatalf("rangePathKey(short secure key) error = %v", err)
	}
}

func TestRangeProofVerificationCancelsWhileIndexingWitness(t *testing.T) {
	t.Parallel()

	leaf := &leafNode{value: []byte("value")}
	encoded, _, err := encodeNode(leaf)
	if err != nil {
		t.Fatalf("encodeNode() error = %v", err)
	}
	err = verifyRange(
		&nthErrorContext{at: 3},
		keccakRoot(encoded),
		nil,
		nil,
		[]RangeItem{NewRangeItem(nil, []byte("value"))},
		RangeProof{nodes: [][]byte{encoded, {0xff}}},
		DefaultLimits(),
		false,
	)
	if !errors.Is(err, ErrCanceled) {
		t.Fatalf("verifyRange(index cancellation) error = %v", err)
	}
}

func TestRangeProofDepthAndCountBudgetsAreInclusive(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	all, err := newRangeBounds(nil, nil, limits, false)
	if err != nil {
		t.Fatalf("newRangeBounds() error = %v", err)
	}
	newGeneration := func(maxDepth, nodes int) *rangeGenerationState {
		builder := newMultiProofBuilder(
			context.Background(), &trieSnapshot{limits: limits},
		)
		builder.state.maxDepth = maxDepth
		builder.state.nodesLeft = nodes
		return &rangeGenerationState{builder: builder, bounds: all}
	}
	extension := &extensionNode{path: []byte{0}, child: &branchNode{}}
	if err := newGeneration(1, 2).walk(extension, nil, 0); err != nil {
		t.Fatalf("generation extension exact depth error = %v", err)
	}
	if err := newGeneration(0, 2).walk(extension, nil, 0); !errors.Is(
		err, ErrResourceLimit,
	) {
		t.Fatalf("generation extension over depth error = %v", err)
	}
	branch := &branchNode{}
	branch.children[0] = &leafNode{
		path: []byte{0}, value: []byte("value"),
	}
	if err := newGeneration(1, 2).walk(branch, nil, 0); err != nil {
		t.Fatalf("generation branch exact depth error = %v", err)
	}
	if err := newGeneration(0, 2).walk(branch, nil, 0); !errors.Is(
		err, ErrResourceLimit,
	) {
		t.Fatalf("generation branch over depth error = %v", err)
	}

	newVerification := func(maxDepth, nodes int) *rangeVerificationState {
		return &rangeVerificationState{
			ctx:       context.Background(),
			lookup:    &multiProofLookup{},
			bounds:    all,
			nodesLeft: nodes,
			maxDepth:  maxDepth,
			items: []RangeItem{
				NewRangeItem([]byte{0}, []byte("value")),
			},
		}
	}
	exactLeaf := &leafNode{path: []byte{0, 0}, value: []byte("value")}
	if err := newVerification(1, 1).walk(exactLeaf, nil, 1); err != nil {
		t.Fatalf("verification exact depth error = %v", err)
	}
	if err := newVerification(1, 1).walk(extension, nil, 0); !errors.Is(
		err, ErrResourceLimit,
	) {
		t.Fatalf("verification node count error = %v", err)
	}
	if err := newVerification(0, 2).walk(extension, nil, 0); !errors.Is(
		err, ErrResourceLimit,
	) {
		t.Fatalf("verification extension over depth error = %v", err)
	}
	if err := newVerification(1, 2).walk(branch, nil, 0); err != nil {
		t.Fatalf("verification branch exact depth error = %v", err)
	}
	if err := newVerification(0, 2).walk(branch, nil, 0); !errors.Is(
		err, ErrResourceLimit,
	) {
		t.Fatalf("verification branch over depth error = %v", err)
	}
}

func TestRangeProofComparisonBoundariesAreExact(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	limits.MaxProofKeys = 1
	limits.MaxValueBytes = 1
	all, err := newRangeBounds(nil, nil, limits, false)
	if err != nil {
		t.Fatalf("newRangeBounds() error = %v", err)
	}
	if err := validateRangeItems(
		[]RangeItem{NewRangeItem(nil, []byte{1})}, all, limits,
	); err != nil {
		t.Fatalf("validateRangeItems(exact limits) error = %v", err)
	}

	endBound := rangeBounds{endPath: []byte{1}}
	if rangeSubtreeIntersects([]byte{1}, endBound) {
		t.Fatal("subtree beginning at the exclusive end intersects")
	}
	startBound := rangeBounds{startPath: []byte{1}}
	if rangeSubtreeIntersects([]byte{0}, startBound) {
		t.Fatal("subtree ending at the inclusive start intersects")
	}
	if next, ok := nibblePrefixSuccessor([]byte{0}); !ok ||
		!slices.Equal(next, []byte{1}) {
		t.Fatalf("successor(0) = (%v, %v)", next, ok)
	}
	if next, ok := nibblePrefixSuccessor([]byte{1, 15}); !ok ||
		!slices.Equal(next, []byte{2}) {
		t.Fatalf("successor(1f) = (%v, %v)", next, ok)
	}
	byteBound := rangeBounds{end: []byte{1}}
	if rangeBytesMatch([]byte{1}, byteBound) {
		t.Fatal("key equal to the exclusive byte end matches")
	}
}
