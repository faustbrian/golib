package mpt

import (
	"context"
	"errors"
	"testing"
)

func TestMultiProofBuilderRejectsInvalidRootSources(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	missing := Root{1}
	builder := newMultiProofBuilder(context.Background(), &trieSnapshot{
		root: hashNode(missing), hash: missing, limits: limits,
		reader: nodeReaderFunc(func(context.Context, Root) ([]byte, error) {
			return nil, ErrMissingNode
		}),
	})
	if err := builder.prepareRoot(); !errors.Is(err, ErrMissingNode) {
		t.Fatalf("prepareRoot(missing) error = %v", err)
	}

	invalidRoot := &trieSnapshot{
		root: struct{}{}, hash: missing, limits: limits,
	}
	if err := newMultiProofBuilder(
		context.Background(), invalidRoot,
	).prepareRoot(); !errors.Is(err, ErrMalformedNode) {
		t.Fatalf("prepareRoot(invalid node) error = %v", err)
	}

	leaf := &leafNode{path: nil, value: []byte("value")}
	encoded, _, err := encodeNode(leaf)
	if err != nil {
		t.Fatalf("encodeNode() error = %v", err)
	}
	hash := keccakRoot(encoded)

	noHashes := limits
	noHashes.MaxHashOperations = 0
	if err := newMultiProofBuilder(context.Background(), &trieSnapshot{
		root: leaf, hash: hash, limits: noHashes,
	}).prepareRoot(); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("prepareRoot(hash limit) error = %v", err)
	}
	if err := newMultiProofBuilder(context.Background(), &trieSnapshot{
		root: leaf, hash: Root{2}, limits: limits,
	}).prepareRoot(); !errors.Is(err, ErrMalformedNode) {
		t.Fatalf("prepareRoot(hash mismatch) error = %v", err)
	}

	valid := newMultiProofBuilder(context.Background(), &trieSnapshot{
		root: leaf, hash: hash, limits: limits,
	})
	if err := valid.prepareRoot(); err != nil {
		t.Fatalf("prepareRoot(materialized) error = %v", err)
	}
	if valid.root != leaf || len(valid.nodes) != 1 {
		t.Fatalf("materialized root = %T, nodes = %d", valid.root, len(valid.nodes))
	}

	pendingInvalid := newMultiProofBuilder(context.Background(), &trieSnapshot{
		root: hashNode(hash), hash: hash, limits: limits,
		pending: map[Root][]byte{hash: nil},
	})
	if err := pendingInvalid.prepareRoot(); !errors.Is(err, ErrMalformedNode) {
		t.Fatalf("prepareRoot(invalid pending root) error = %v", err)
	}
}

func TestMultiProofBuilderRejectsInvalidTraversalSources(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	builder := newMultiProofBuilder(context.Background(), &trieSnapshot{
		limits: limits,
	})
	builder.root = &extensionNode{
		path: []byte{1}, child: &branchNode{},
	}
	if err := builder.addPath([]byte{0x20}, false); err != nil {
		t.Fatalf("addPath(non-matching extension) error = %v", err)
	}
	if err := builder.addPath([]byte{0x10}, false); err != nil {
		t.Fatalf("addPath(embedded branch) error = %v", err)
	}

	builder.root = struct{}{}
	if err := builder.addPath(nil, false); !errors.Is(err, ErrMalformedNode) {
		t.Fatalf("addPath(invalid source) error = %v", err)
	}

	builder.root = &leafNode{value: []byte("value")}
	builder.state.nodesLeft = 0
	if err := builder.addPath(nil, false); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("addPath(traversal limit) error = %v", err)
	}

	builder = newMultiProofBuilder(context.Background(), &trieSnapshot{
		limits: limits,
	})
	builder.root = &leafNode{value: []byte("value")}
	builder.state.budget.hashesLeft = 0
	if err := builder.addPath(nil, true); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("addPath(secure hash limit) error = %v", err)
	}
}

func TestMultiProofBuilderRejectsInvalidHashedChildren(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	leafEncoded, _, err := encodeNode(
		&leafNode{path: nil, value: make([]byte, RootBytes)},
	)
	if err != nil {
		t.Fatalf("encodeNode() error = %v", err)
	}
	leafHash := keccakRoot(leafEncoded)

	builder := newMultiProofBuilder(context.Background(), &trieSnapshot{
		limits:  limits,
		pending: map[Root][]byte{leafHash: leafEncoded},
	})
	builder.root = &extensionNode{path: []byte{1}, child: hashNode(leafHash)}
	if err := builder.addPath(
		[]byte{0x10}, false,
	); !errors.Is(err, ErrCorruptNode) {
		t.Fatalf("addPath(compact extension child) error = %v", err)
	}

	missing := Root{3}
	builder = newMultiProofBuilder(context.Background(), &trieSnapshot{
		limits: limits,
		reader: nodeReaderFunc(func(context.Context, Root) ([]byte, error) {
			return nil, ErrMissingNode
		}),
	})
	builder.root = &extensionNode{path: []byte{1}, child: hashNode(missing)}
	if err := builder.addPath(
		[]byte{0x10}, false,
	); !errors.Is(err, ErrMissingNode) {
		t.Fatalf("addPath(missing extension child) error = %v", err)
	}

	branch := &branchNode{}
	branch.children[1] = hashNode(missing)
	builder.root = branch
	if err := builder.addPath(
		[]byte{0x10}, false,
	); !errors.Is(err, ErrMissingNode) {
		t.Fatalf("addPath(missing branch child) error = %v", err)
	}
}

func TestMultiProofBuilderLoadAndDeduplicationBounds(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	encoded, _, err := encodeNode(
		&leafNode{path: nil, value: []byte("value")},
	)
	if err != nil {
		t.Fatalf("encodeNode() error = %v", err)
	}
	hash := keccakRoot(encoded)

	builder := newMultiProofBuilder(context.Background(), &trieSnapshot{
		limits: limits,
	})
	builder.decoded[hash] = &leafNode{value: []byte("cached")}
	if _, err := builder.load(hash); err != nil {
		t.Fatalf("load(cached) error = %v", err)
	}
	if err := builder.appendNode(hash, encoded); err != nil {
		t.Fatalf("appendNode(first) error = %v", err)
	}
	if err := builder.appendNode(hash, encoded); err != nil {
		t.Fatalf("appendNode(duplicate) error = %v", err)
	}
	if len(builder.nodes) != 1 {
		t.Fatalf("deduplicated node count = %d, want 1", len(builder.nodes))
	}
	byteLimited := newMultiProofBuilder(context.Background(), &trieSnapshot{
		limits: limits,
	})
	byteLimited.snapshot.limits.MaxProofBytes = len(encoded) - 1
	if err := byteLimited.appendNode(
		hash, encoded,
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("appendNode(byte limit) error = %v", err)
	}

	badPending := newMultiProofBuilder(context.Background(), &trieSnapshot{
		limits: limits, pending: map[Root][]byte{hash: nil},
	})
	if _, err := badPending.load(hash); !errors.Is(err, ErrMalformedNode) {
		t.Fatalf("load(invalid pending) error = %v", err)
	}

	nodeLimited := limits
	nodeLimited.MaxProofNodes = 0
	longEncoded, _, err := encodeNode(
		&leafNode{path: nil, value: make([]byte, RootBytes)},
	)
	if err != nil {
		t.Fatalf("encodeNode(long) error = %v", err)
	}
	longHash := keccakRoot(longEncoded)
	pendingLimited := newMultiProofBuilder(
		context.Background(),
		&trieSnapshot{
			limits: nodeLimited, pending: map[Root][]byte{longHash: longEncoded},
		},
	)
	if _, err := pendingLimited.load(longHash); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("load(pending node limit) error = %v", err)
	}

	missing := newMultiProofBuilder(context.Background(), &trieSnapshot{
		limits: limits,
		reader: nodeReaderFunc(func(context.Context, Root) ([]byte, error) {
			return nil, ErrMissingNode
		}),
	})
	if _, err := missing.load(hash); !errors.Is(err, ErrMissingNode) {
		t.Fatalf("load(missing reader node) error = %v", err)
	}

	readLimited := newMultiProofBuilder(context.Background(), &trieSnapshot{
		limits: nodeLimited,
		reader: nodeReaderFunc(func(context.Context, Root) ([]byte, error) {
			return longEncoded, nil
		}),
	})
	if _, err := readLimited.load(longHash); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("load(reader node limit) error = %v", err)
	}
}

func TestMultiProofBuilderRejectsInvalidPendingNodes(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	encoded, _, err := encodeNode(
		&leafNode{path: nil, value: []byte("value")},
	)
	if err != nil {
		t.Fatalf("encodeNode() error = %v", err)
	}
	hash := keccakRoot(encoded)

	builder := newMultiProofBuilder(context.Background(), &trieSnapshot{
		limits: limits,
	})
	builder.state.budget.hashesLeft = 0
	if _, err := builder.decodePending(
		hash, encoded, false,
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("decodePending(hash limit) error = %v", err)
	}

	builder = newMultiProofBuilder(context.Background(), &trieSnapshot{
		limits: limits,
	})
	if _, err := builder.decodePending(
		Root{4}, encoded, false,
	); !errors.Is(err, ErrMalformedNode) {
		t.Fatalf("decodePending(hash mismatch) error = %v", err)
	}

	malformed := []byte{0xff}
	if _, err := builder.decodePending(
		keccakRoot(malformed), malformed, false,
	); !errors.Is(err, ErrMalformedNode) {
		t.Fatalf("decodePending(malformed node) error = %v", err)
	}
}

func TestMultiProofGenerationRejectsInvalidStateAndBounds(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	if _, err := proveManySnapshot(
		context.Background(), nil, [][]byte{nil}, false,
	); !errors.Is(err, ErrUninitialized) {
		t.Fatalf("proveManySnapshot(uninitialized) error = %v", err)
	}
	if _, err := proveManySnapshot(
		nilContext(), &trieSnapshot{limits: limits}, [][]byte{nil}, false,
	); !errors.Is(err, ErrInvalidContext) {
		t.Fatalf("proveManySnapshot(nil context) error = %v", err)
	}
	if _, err := proveManySnapshot(
		context.Background(),
		&trieSnapshot{limits: limits},
		[][]byte{make([]byte, limits.MaxKeyBytes+1)},
		false,
	); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("proveManySnapshot(oversized key) error = %v", err)
	}
	badRoot := Root{9}
	if _, err := proveManySnapshot(
		context.Background(),
		&trieSnapshot{
			root: hashNode(badRoot), hash: badRoot, limits: limits,
			pending: map[Root][]byte{badRoot: nil},
		},
		[][]byte{nil},
		false,
	); !errors.Is(err, ErrMalformedNode) {
		t.Fatalf("proveManySnapshot(invalid root) error = %v", err)
	}

	constrained := limits
	constrained.MaxTraversalNodes = 1
	branch := &branchNode{}
	branch.children[0] = &leafNode{path: []byte{0}, value: []byte("value")}
	branch.children[1] = &leafNode{path: []byte{0}, value: []byte("other")}
	encoded, _, err := encodeNode(branch)
	if err != nil {
		t.Fatalf("encodeNode() error = %v", err)
	}
	root := keccakRoot(encoded)
	if _, err := proveManySnapshot(
		context.Background(),
		&trieSnapshot{
			root: branch, hash: root, limits: constrained,
			pending: map[Root][]byte{root: encoded},
		},
		[][]byte{{0}},
		false,
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("proveManySnapshot(traversal limit) error = %v", err)
	}

	if _, err := MultiProofFromNodes(
		[][]byte{{0x80}}, Limits{},
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("MultiProofFromNodes(invalid limits) error = %v", err)
	}

	exact := limits
	exact.MaxProofKeys = 2
	exact.MaxKeyBytes = 2
	if _, err := proveManySnapshot(
		context.Background(), &trieSnapshot{limits: exact},
		[][]byte{{0, 0}, {0, 1}}, false,
	); err != nil {
		t.Fatalf("proveManySnapshot(exact bounds) error = %v", err)
	}
}

func TestMultiProofVerificationRejectsAllInputBoundaries(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	claim := AbsenceClaim(nil)
	if err := verifyMultiProof(
		context.Background(), EmptyRoot(), []ProofClaim{claim},
		MultiProof{}, Limits{}, false,
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("verifyMultiProof(invalid limits) error = %v", err)
	}
	if err := verifyMultiProof(
		nilContext(), EmptyRoot(), []ProofClaim{claim},
		MultiProof{}, limits, false,
	); !errors.Is(err, ErrInvalidContext) {
		t.Fatalf("verifyMultiProof(nil context) error = %v", err)
	}

	proofLimits := limits
	proofLimits.MaxProofNodes = 1
	if err := verifyMultiProof(
		context.Background(), Root{1}, []ProofClaim{claim},
		MultiProof{nodes: [][]byte{{0x80}, {0x80}}},
		proofLimits, false,
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("verifyMultiProof(proof limit) error = %v", err)
	}
	if err := verifyMultiProof(
		context.Background(), EmptyRoot(),
		[]ProofClaim{AbsenceClaim(make([]byte, limits.MaxKeyBytes+1))},
		MultiProof{}, limits, false,
	); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("verifyMultiProof(key limit) error = %v", err)
	}
	if err := verifyMultiProof(
		context.Background(), EmptyRoot(),
		[]ProofClaim{MembershipClaim(nil, nil)},
		MultiProof{}, limits, false,
	); !errors.Is(err, ErrInvalidValue) {
		t.Fatalf("verifyMultiProof(empty value) error = %v", err)
	}
	if err := verifyMultiProof(
		context.Background(), EmptyRoot(), []ProofClaim{claim},
		MultiProof{nodes: [][]byte{{0x80}}}, limits, false,
	); !errors.Is(err, ErrMalformedProof) {
		t.Fatalf("verifyMultiProof(empty surplus) error = %v", err)
	}
	if err := verifyMultiProof(
		context.Background(), Root{1}, []ProofClaim{claim},
		MultiProof{}, limits, false,
	); !errors.Is(err, ErrIncompleteProof) {
		t.Fatalf("verifyMultiProof(missing proof) error = %v", err)
	}

	exact := limits
	exact.MaxProofKeys = 2
	exact.MaxKeyBytes = 2
	if err := verifyMultiProof(
		context.Background(), EmptyRoot(),
		[]ProofClaim{AbsenceClaim([]byte{0, 0}), AbsenceClaim([]byte{0, 1})},
		MultiProof{}, exact, false,
	); err != nil {
		t.Fatalf("verifyMultiProof(exact key bounds) error = %v", err)
	}
	exact.MaxValueBytes = 2
	if err := verifyMultiProof(
		context.Background(), Root{1},
		[]ProofClaim{MembershipClaim(nil, []byte{0, 1})},
		MultiProof{}, exact, false,
	); !errors.Is(err, ErrIncompleteProof) {
		t.Fatalf("verifyMultiProof(exact value bound) error = %v", err)
	}

	invalidLimits := limits
	invalidLimits.MaxProofKeys = 0
	if err := validateTrieLimits(
		invalidLimits,
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("validateTrieLimits(zero proof keys) error = %v", err)
	}
}

func nilContext() context.Context {
	var ctx context.Context
	return ctx
}

func TestMultiProofLookupRejectsResourceAndCanonicalityFailures(t *testing.T) {
	t.Parallel()

	if _, err := newMultiProofLookup(
		Root{}, MultiProof{nodes: [][]byte{{0x80}}},
		&workBudget{},
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("newMultiProofLookup(hash limit) error = %v", err)
	}
	malformed := []byte{0xff}
	if _, err := newMultiProofLookup(
		keccakRoot(malformed),
		MultiProof{nodes: [][]byte{malformed}},
		&workBudget{hashesLeft: 1},
	); !errors.Is(err, ErrMalformedProof) {
		t.Fatalf("newMultiProofLookup(malformed node) error = %v", err)
	}

	expected := Root{1}
	other := Root{2}
	lookup := &multiProofLookup{
		nodes: map[Root]multiProofNode{
			expected: {
				decoded: &leafNode{value: []byte("value")},
				size:    RootBytes,
			},
		},
		order: []Root{other}, used: make(map[Root]struct{}),
	}
	if _, err := lookup.resolve(
		expected, true,
	); !errors.Is(err, ErrWrongRoot) {
		t.Fatalf("resolve(reordered root) error = %v", err)
	}
	lookup.order = nil
	if _, err := lookup.resolve(
		expected, false,
	); !errors.Is(err, ErrMalformedProof) {
		t.Fatalf("resolve(exhausted order) error = %v", err)
	}
}

func TestMultiProofClaimTraversalRejectsHostileInternalNodes(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	rootHash := Root{1}
	base := func(root node) *multiProofLookup {
		return &multiProofLookup{
			nodes: map[Root]multiProofNode{
				rootHash: {decoded: root},
			},
			order: []Root{rootHash}, used: make(map[Root]struct{}),
			root: rootHash, budget: &workBudget{hashesLeft: 8},
		}
	}

	secure := base(&leafNode{value: []byte("value")})
	secure.budget.hashesLeft = 0
	if err := verifyMultiClaim(
		context.Background(), secure, AbsenceClaim(nil), limits, true,
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("verifyMultiClaim(hash limit) error = %v", err)
	}
	if err := verifyMultiClaim(
		context.Background(), base(&leafNode{value: []byte("value")}),
		AbsenceClaim(nil), limits, false,
	); !errors.Is(err, ErrFailedProof) {
		t.Fatalf("verifyMultiClaim(present leaf) error = %v", err)
	}
	if err := verifyMultiClaim(
		context.Background(),
		base(&extensionNode{
			path: []byte{1}, child: &branchNode{},
		}),
		AbsenceClaim([]byte{0x20}), limits, false,
	); err != nil {
		t.Fatalf("verifyMultiClaim(non-matching extension) error = %v", err)
	}
	if err := verifyMultiClaim(
		context.Background(),
		base(&extensionNode{
			path: []byte{1}, child: &branchNode{},
		}),
		AbsenceClaim([]byte{0x10}), limits, false,
	); err != nil {
		t.Fatalf("verifyMultiClaim(embedded extension child) error = %v", err)
	}

	childHash := Root{2}
	hashed := base(&extensionNode{
		path: []byte{1}, child: hashNode(childHash),
	})
	hashed.nodes[childHash] = multiProofNode{
		decoded: &leafNode{value: []byte("invalid")},
		size:    RootBytes,
	}
	hashed.order = append(hashed.order, childHash)
	if err := verifyMultiClaim(
		context.Background(), hashed,
		AbsenceClaim([]byte{0x10}), limits, false,
	); !errors.Is(err, ErrMalformedProof) {
		t.Fatalf("verifyMultiClaim(compact extension child) error = %v", err)
	}

	if err := verifyMultiClaim(
		context.Background(), base(struct{}{}),
		AbsenceClaim(nil), limits, false,
	); !errors.Is(err, ErrMalformedProof) {
		t.Fatalf("verifyMultiClaim(invalid node) error = %v", err)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := verifyMultiClaim(
		canceled, base(&leafNode{value: []byte("value")}),
		AbsenceClaim(nil), limits, false,
	); !errors.Is(err, ErrCanceled) {
		t.Fatalf("verifyMultiClaim(canceled) error = %v", err)
	}

	bounded := limits
	bounded.MaxTraversalNodes = 1
	branch := &branchNode{}
	branch.children[0] = &leafNode{path: nil, value: []byte("value")}
	if err := verifyMultiClaim(
		context.Background(), base(branch),
		AbsenceClaim([]byte{0}), bounded, false,
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("verifyMultiClaim(traversal limit) error = %v", err)
	}

	if err := finishMultiClaim(
		true, []byte("actual"),
		MembershipClaim(nil, []byte("expected")),
	); !errors.Is(err, ErrFailedProof) {
		t.Fatalf("finishMultiClaim(wrong value) error = %v", err)
	}

	depthBounded := limits
	depthBounded.MaxTraversalDepth = 0
	if err := verifyMultiClaim(
		context.Background(),
		base(&leafNode{path: nil, value: []byte("value")}),
		MembershipClaim(nil, []byte("value")), depthBounded, false,
	); err != nil {
		t.Fatalf("verifyMultiClaim(exact depth) error = %v", err)
	}
	if err := verifyMultiClaim(
		context.Background(),
		base(&extensionNode{
			path: []byte{0}, child: &branchNode{},
		}),
		AbsenceClaim([]byte{0}), depthBounded, false,
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("verifyMultiClaim(extension depth) error = %v", err)
	}
	deepBranch := &branchNode{}
	deepBranch.children[0] = &leafNode{path: nil, value: []byte("value")}
	if err := verifyMultiClaim(
		context.Background(), base(deepBranch),
		MembershipClaim([]byte{0}, []byte("value")),
		depthBounded, false,
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("verifyMultiClaim(branch depth) error = %v", err)
	}
}

func TestMultiProofBuilderDepthAndByteAccounting(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	builder := newMultiProofBuilder(context.Background(), &trieSnapshot{
		limits: limits,
	})
	builder.state.maxDepth = 0
	builder.root = &extensionNode{
		path: []byte{0},
		child: &leafNode{
			path: nil, value: []byte("value"),
		},
	}
	if err := builder.addPath(
		[]byte{0}, false,
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("addPath(extension depth) error = %v", err)
	}
	builder = newMultiProofBuilder(context.Background(), &trieSnapshot{
		limits: limits,
	})
	builder.state.maxDepth = 0
	branch := &branchNode{}
	branch.children[0] = &leafNode{path: nil, value: []byte("value")}
	builder.root = branch
	if err := builder.addPath(
		[]byte{0}, false,
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("addPath(branch depth) error = %v", err)
	}

	bytesLimited := limits
	bytesLimited.MaxProofBytes = 5
	builder = newMultiProofBuilder(context.Background(), &trieSnapshot{
		limits: bytesLimited,
	})
	first := []byte{1, 2}
	if err := builder.appendNode(Root{1}, first); err != nil {
		t.Fatalf("appendNode(first) error = %v", err)
	}
	if builder.total != len(first) {
		t.Fatalf("proof byte total = %d, want %d", builder.total, len(first))
	}
	if err := builder.appendNode(
		Root{2}, []byte{3, 4, 5},
	); err != nil {
		t.Fatalf("appendNode(exact byte limit) error = %v", err)
	}
	if builder.total != bytesLimited.MaxProofBytes {
		t.Fatalf(
			"proof byte total = %d, want %d",
			builder.total,
			bytesLimited.MaxProofBytes,
		)
	}
	if err := builder.appendNode(
		Root{3}, []byte{6},
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("appendNode(cumulative limit) error = %v", err)
	}
}

func TestMultiProofDecodersRejectCanonicalNullNode(t *testing.T) {
	t.Parallel()

	encodedNull := []byte{0x80}
	hash := keccakRoot(encodedNull)
	builder := newMultiProofBuilder(context.Background(), &trieSnapshot{
		limits: DefaultLimits(),
	})
	if _, err := builder.decodePending(
		hash, encodedNull, true,
	); !errors.Is(err, ErrMalformedNode) {
		t.Fatalf("decodePending(null node) error = %v", err)
	}
	if _, err := newMultiProofLookup(
		hash, MultiProof{nodes: [][]byte{encodedNull}},
		&workBudget{hashesLeft: 1},
	); !errors.Is(err, ErrMalformedProof) {
		t.Fatalf("newMultiProofLookup(null node) error = %v", err)
	}
}
