package mpt

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/faustbrian/golib/pkg/merkle-patricia-trie/internal/rlp"
)

func TestCompactAndNodePathLimitsAcceptTheirExactMaximum(t *testing.T) {
	t.Parallel()

	const ethereumMaximumPathNibbles = 8192
	if MaxCompactPathNibbles != ethereumMaximumPathNibbles {
		t.Fatalf("MaxCompactPathNibbles = %d", MaxCompactPathNibbles)
	}
	nibbles := make([]byte, ethereumMaximumPathNibbles)
	for index := range nibbles {
		nibbles[index] = byte(index % 16)
	}
	encoded, err := EncodeCompactPath(nibbles, true)
	if err != nil {
		t.Fatalf("EncodeCompactPath(maximum) error = %v", err)
	}
	if len(encoded) != ethereumMaximumPathNibbles/2+1 {
		t.Fatalf("encoded maximum length = %d", len(encoded))
	}
	decoded, err := DecodeCompactPath(encoded)
	if err != nil {
		t.Fatalf("DecodeCompactPath(maximum) error = %v", err)
	}
	if !decoded.Leaf() || !slices.Equal(decoded.Nibbles(), nibbles) {
		t.Fatal("maximum compact path did not round trip exactly")
	}
	if err := validateNibbles(nibbles); err != nil {
		t.Fatalf("validateNibbles(maximum) error = %v", err)
	}
}

func TestEveryTrieLimitRejectsZeroIndependently(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		invalidate func(*Limits)
	}{
		{"key bytes", func(limits *Limits) { limits.MaxKeyBytes = 0 }},
		{"value bytes", func(limits *Limits) { limits.MaxValueBytes = 0 }},
		{"traversal depth", func(limits *Limits) { limits.MaxTraversalDepth = 0 }},
		{"traversal nodes", func(limits *Limits) { limits.MaxTraversalNodes = 0 }},
		{"encoding nodes", func(limits *Limits) { limits.MaxEncodingNodes = 0 }},
		{"hash operations", func(limits *Limits) { limits.MaxHashOperations = 0 }},
		{"node reads", func(limits *Limits) { limits.MaxNodeReads = 0 }},
		{"iterator results", func(limits *Limits) { limits.MaxIteratorResults = 0 }},
		{"iteration nodes", func(limits *Limits) { limits.MaxIterationNodes = 0 }},
		{"rebuild nodes", func(limits *Limits) { limits.MaxRebuildNodes = 0 }},
		{"batch operations", func(limits *Limits) { limits.MaxBatchOperations = 0 }},
		{"proof nodes", func(limits *Limits) { limits.MaxProofNodes = 0 }},
		{"proof bytes", func(limits *Limits) { limits.MaxProofBytes = 0 }},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			limits := DefaultLimits()
			test.invalidate(&limits)
			if err := validateTrieLimits(limits); !errors.Is(err, ErrResourceLimit) {
				t.Fatalf("validateTrieLimits() error = %v", err)
			}
		})
	}
}

func TestTrieLimitUpperBoundsAreInclusive(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	const (
		maximumKeyBytes       = 4096
		maximumValueBytes     = 16_777_200
		maximumTraversalDepth = 8193
	)
	if rlpMaxValueBytes != maximumValueBytes {
		t.Fatalf("rlpMaxValueBytes = %d", rlpMaxValueBytes)
	}
	limits.MaxKeyBytes = maximumKeyBytes
	limits.MaxValueBytes = maximumValueBytes
	limits.MaxTraversalDepth = maximumTraversalDepth
	if err := validateTrieLimits(limits); err != nil {
		t.Fatalf("validateTrieLimits(exact maxima) error = %v", err)
	}

	tests := []struct {
		name     string
		overflow func(*Limits)
	}{
		{"key bytes", func(limits *Limits) {
			limits.MaxKeyBytes = maximumKeyBytes + 1
		}},
		{"value bytes", func(limits *Limits) {
			limits.MaxValueBytes = maximumValueBytes + 1
		}},
		{"traversal depth", func(limits *Limits) {
			limits.MaxTraversalDepth = maximumTraversalDepth + 1
		}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			overflow := DefaultLimits()
			test.overflow(&overflow)
			if err := validateTrieLimits(overflow); !errors.Is(err, ErrResourceLimit) {
				t.Fatalf("validateTrieLimits() error = %v", err)
			}
		})
	}
}

func TestHasReturnsFalseWithOperationErrors(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	limits.MaxKeyBytes = 1
	trie, err := NewRawTrie(limits)
	if err != nil {
		t.Fatalf("NewRawTrie() error = %v", err)
	}
	has, err := trie.Has(context.Background(), []byte{1, 2})
	if !errors.Is(err, ErrInvalidKey) || has {
		t.Fatalf("Has(oversized) = (%t, %v)", has, err)
	}
}

func TestValueAndTraversalBoundsAcceptTheExactLimit(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	limits.MaxValueBytes = 1
	trie, err := NewRawTrie(limits)
	if err != nil {
		t.Fatalf("NewRawTrie() error = %v", err)
	}
	if _, err := trie.Update(context.Background(), nil, []byte{1}); err != nil {
		t.Fatalf("Update(exact value limit) error = %v", err)
	}
	if _, err := trie.Update(context.Background(), nil, []byte{1, 2}); !errors.Is(err, ErrInvalidValue) {
		t.Fatalf("Update(over value limit) error = %v", err)
	}

	state := traversalState{
		ctx: context.Background(), maxDepth: 1, nodesLeft: 2,
	}
	if err := state.visit(1); err != nil {
		t.Fatalf("visit(exact depth) error = %v", err)
	}
	if err := state.visit(2); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("visit(over depth) error = %v", err)
	}
}

func TestRecursiveTrieOperationsChargeOneDepthPerEdge(t *testing.T) {
	t.Parallel()

	newState := func() traversalState {
		return traversalState{
			ctx: context.Background(), maxDepth: 0, nodesLeft: 8,
			budget: &workBudget{hashesLeft: 8},
		}
	}
	assertLimited := func(t *testing.T, err error) {
		t.Helper()
		if !errors.Is(err, ErrResourceLimit) {
			t.Fatalf("operation error = %v, want ErrResourceLimit", err)
		}
	}

	t.Run("get extension", func(t *testing.T) {
		state := newState()
		_, _, err := getNode(
			&extensionNode{path: []byte{1}, child: &branchNode{}},
			[]byte{1}, 0, &state,
		)
		assertLimited(t, err)
	})
	t.Run("get branch", func(t *testing.T) {
		state := newState()
		var children [16]node
		children[1] = &leafNode{value: []byte{1}}
		_, _, err := getNode(&branchNode{children: children}, []byte{1}, 0, &state)
		assertLimited(t, err)
	})
	t.Run("insert branch", func(t *testing.T) {
		state := newState()
		var children [16]node
		children[1] = &leafNode{value: []byte{1}}
		_, err := insertNode(
			&branchNode{children: children}, []byte{1}, []byte{2}, 0, &state,
		)
		assertLimited(t, err)
	})
	t.Run("insert extension", func(t *testing.T) {
		state := newState()
		_, err := insertExtension(
			&extensionNode{path: []byte{1}, child: &branchNode{}},
			[]byte{1, 2}, []byte{1}, 0, &state,
		)
		assertLimited(t, err)
	})
	t.Run("delete extension", func(t *testing.T) {
		state := newState()
		_, _, err := deleteNode(
			&extensionNode{path: []byte{1}, child: &branchNode{}},
			[]byte{1}, 0, &state,
		)
		assertLimited(t, err)
	})
	t.Run("delete branch", func(t *testing.T) {
		state := newState()
		var children [16]node
		children[1] = &leafNode{value: []byte{1}}
		_, _, err := deleteNode(
			&branchNode{children: children}, []byte{1}, 0, &state,
		)
		assertLimited(t, err)
	})
}

func TestResolveConsumesReadsAndClassifiesStoredNull(t *testing.T) {
	t.Parallel()

	encoded, _, err := encodeNode(&leafNode{value: []byte{1}})
	if err != nil {
		t.Fatalf("encodeNode() error = %v", err)
	}
	root := keccakRoot(encoded)
	reader := nodeReaderFunc(func(context.Context, Root) ([]byte, error) {
		return append([]byte(nil), encoded...), nil
	})
	state := traversalState{
		ctx: context.Background(), maxDepth: 1, nodesLeft: 2,
		readsLeft: 1, reader: reader, budget: &workBudget{hashesLeft: 2},
	}
	if _, err := state.resolve(root); err != nil {
		t.Fatalf("resolve(first read) error = %v", err)
	}
	if _, err := state.resolve(root); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("resolve(second read) error = %v", err)
	}

	nullEncoding := []byte{0x80}
	nullRoot := keccakRoot(nullEncoding)
	state.reader = nodeReaderFunc(func(context.Context, Root) ([]byte, error) {
		return append([]byte(nil), nullEncoding...), nil
	})
	state.readsLeft = 1
	state.budget.hashesLeft = 1
	_, err = state.resolve(nullRoot)
	var corrupt *CorruptNodeError
	if !errors.As(err, &corrupt) || !errors.Is(corrupt.Cause, ErrMalformedNode) {
		t.Fatalf("resolve(stored null) error = %v", err)
	}
}

func TestIteratorOptionBoundsAreExactAndIndependent(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	limits.MaxKeyBytes = 1
	limits.MaxIteratorResults = 1
	trie, err := NewRawTrie(limits)
	if err != nil {
		t.Fatalf("NewRawTrie() error = %v", err)
	}
	trie, err = trie.Update(context.Background(), []byte{'a'}, []byte{1})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	yield := func(Entry) error { return nil }
	for _, options := range []IterationOptions{
		{Limit: 1},
		{Prefix: []byte{'a'}, Limit: 1},
		{Start: []byte{'a'}, Limit: 1},
		{End: []byte{'b'}, Limit: 1},
	} {
		if err := trie.Iterate(context.Background(), options, yield); err != nil {
			t.Fatalf("Iterate(%+v) error = %v", options, err)
		}
	}
	if err := trie.Iterate(
		context.Background(),
		IterationOptions{Start: []byte{'a'}, End: []byte{'a'}, Limit: 1},
		yield,
	); !errors.Is(err, ErrInvalidIterator) {
		t.Fatalf("Iterate(equal range) error = %v", err)
	}
	for _, options := range []IterationOptions{
		{Limit: 2},
		{Prefix: []byte{1, 2}, Limit: 1},
		{Start: []byte{1, 2}, Limit: 1},
		{End: []byte{1, 2}, Limit: 1},
	} {
		if err := trie.Iterate(context.Background(), options, yield); !errors.Is(err, ErrResourceLimit) &&
			!errors.Is(err, ErrInvalidIterator) {
			t.Fatalf("Iterate(%+v) error = %v", options, err)
		}
	}
}

func TestIteratorChargesOneDepthPerExtensionAndBranchEdge(t *testing.T) {
	t.Parallel()

	newState := func() iterationState {
		return iterationState{
			traversal: traversalState{
				ctx: context.Background(), maxDepth: 0, nodesLeft: 8,
				budget: &workBudget{hashesLeft: 8},
			},
			yield:   func(Entry) error { return nil },
			hardMax: 8,
		}
	}
	t.Run("extension", func(t *testing.T) {
		state := newState()
		err := state.walk(
			&extensionNode{path: []byte{1}, child: &branchNode{}}, nil, 0,
		)
		if !errors.Is(err, ErrResourceLimit) {
			t.Fatalf("walk(extension) error = %v", err)
		}
	})
	t.Run("branch", func(t *testing.T) {
		state := newState()
		var children [16]node
		children[1] = &leafNode{path: []byte{0}, value: []byte{1}}
		err := state.walk(&branchNode{children: children}, nil, 0)
		if !errors.Is(err, ErrResourceLimit) {
			t.Fatalf("walk(branch) error = %v", err)
		}
	})
}

func TestProofCountAndByteLimitsAreInclusiveAndCumulative(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	limits.MaxProofNodes = 2
	limits.MaxProofBytes = 2
	nodes := [][]byte{{1}, {2}}
	if _, err := ProofFromNodes(nodes, limits); err != nil {
		t.Fatalf("ProofFromNodes(exact limits) error = %v", err)
	}
	if err := validateProofLimits(Proof{nodes: nodes}, limits); err != nil {
		t.Fatalf("validateProofLimits(exact limits) error = %v", err)
	}
	if _, err := ProofFromNodes(
		[][]byte{{1}, {2}, {3}}, limits,
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("ProofFromNodes(over node limit) error = %v", err)
	}
	limits.MaxProofBytes = 1
	if _, err := ProofFromNodes(nodes, limits); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("ProofFromNodes(cumulative byte limit) error = %v", err)
	}
	if err := validateProofLimits(
		Proof{nodes: nodes}, limits,
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("validateProofLimits(cumulative byte limit) error = %v", err)
	}

	limits.MaxProofNodes = 3
	limits.MaxProofBytes = 4
	unequal := [][]byte{{1}, {2, 3}, {4, 5}}
	if _, err := ProofFromNodes(
		unequal, limits,
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("ProofFromNodes(unequal cumulative limit) error = %v", err)
	}
	if err := validateProofLimits(
		Proof{nodes: unequal}, limits,
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("validateProofLimits(unequal cumulative limit) error = %v", err)
	}
}

func TestLoadedMultiNodeProofTraversesEveryHashedReference(t *testing.T) {
	t.Parallel()

	trie, err := NewRawTrie(DefaultLimits())
	if err != nil {
		t.Fatalf("NewRawTrie() error = %v", err)
	}
	values := map[string][]byte{
		"alpha":  []byte("a long value that forces hashed child references 1"),
		"alpine": []byte("a long value that forces hashed child references 2"),
		"beta":   []byte("a long value that forces hashed child references 3"),
	}
	for key, value := range values {
		trie, err = trie.Update(context.Background(), []byte(key), value)
		if err != nil {
			t.Fatalf("Update(%q) error = %v", key, err)
		}
	}
	root, err := trie.Root()
	if err != nil {
		t.Fatalf("Root() error = %v", err)
	}
	reader := nodeReaderFunc(func(_ context.Context, hash Root) ([]byte, error) {
		encoded, ok := lookupSnapshotPending(trie.snapshot, hash)
		if !ok {
			return nil, ErrMissingNode
		}
		return append([]byte(nil), encoded...), nil
	})
	loaded := &trieSnapshot{
		root: hashNode(root), hash: root, base: root,
		limits: DefaultLimits(), reader: reader,
	}
	proof, err := proveSnapshot(
		context.Background(), loaded, []byte("alpha"), false,
	)
	if err != nil {
		t.Fatalf("proveSnapshot(loaded) error = %v", err)
	}
	if len(proof.nodes) < 2 {
		t.Fatalf("loaded proof nodes = %d", len(proof.nodes))
	}
	if err := VerifyRawMembership(
		context.Background(),
		root,
		[]byte("alpha"),
		values["alpha"],
		proof,
		DefaultLimits(),
	); err != nil {
		t.Fatalf("VerifyRawMembership() error = %v", err)
	}

	exact := loaded.limits
	exact.MaxProofNodes = len(proof.nodes)
	exact.MaxProofBytes = proofByteLength(proof)
	loaded.limits = exact
	if _, err := proveSnapshot(
		context.Background(), loaded, []byte("alpha"), false,
	); err != nil {
		t.Fatalf("proveSnapshot(exact proof limits) error = %v", err)
	}
	loaded.limits.MaxProofNodes--
	if _, err := proveSnapshot(
		context.Background(), loaded, []byte("alpha"), false,
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("proveSnapshot(over node limit) error = %v", err)
	}
	loaded.limits = exact
	loaded.limits.MaxProofBytes--
	if _, err := proveSnapshot(
		context.Background(), loaded, []byte("alpha"), false,
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("proveSnapshot(over byte limit) error = %v", err)
	}
}

func TestEIP1186IntegerBoundaries(t *testing.T) {
	t.Parallel()

	maximum := make([]byte, RootBytes)
	maximum[0] = 1
	if !canonicalUint256(nil) ||
		!canonicalUint256(maximum) ||
		canonicalUint256(append([]byte{1}, maximum...)) ||
		canonicalUint256([]byte{0, 1}) {
		t.Fatal("canonicalUint256() disagrees at an integer boundary")
	}
	account := Account{storageRoot: EmptyRoot(), verified: true}
	if err := VerifyStorageProof(
		context.Background(),
		account,
		[RootBytes]byte{},
		maximum,
		Proof{},
		DefaultLimits(),
	); !errors.Is(err, ErrFailedProof) {
		t.Fatalf("VerifyStorageProof(maximum) error = %v", err)
	}
}

func TestProofGenerationIncludesExactlyThirtyTwoByteChildren(t *testing.T) {
	t.Parallel()

	value := make([]byte, 29)
	for index := range value {
		value[index] = 0x80
	}
	child := &leafNode{path: []byte{0}, value: value}
	childEncoding, _, err := encodeNode(child)
	if err != nil {
		t.Fatalf("encodeNode(child) error = %v", err)
	}
	if len(childEncoding) != RootBytes {
		t.Fatalf("child encoding length = %d", len(childEncoding))
	}
	var children [16]node
	children[0] = child
	children[1] = &leafNode{value: []byte{1}}
	rootNode := &branchNode{children: children}
	rootEncoding, _, err := encodeNode(rootNode)
	if err != nil {
		t.Fatalf("encodeNode(root) error = %v", err)
	}
	snapshot := &trieSnapshot{
		root: rootNode, hash: keccakRoot(rootEncoding), limits: DefaultLimits(),
	}
	proof, err := proveSnapshot(
		context.Background(), snapshot, []byte{0}, false,
	)
	if err != nil {
		t.Fatalf("proveSnapshot() error = %v", err)
	}
	if len(proof.nodes) != 2 {
		t.Fatalf("proof node count = %d, want 2", len(proof.nodes))
	}
	if err := VerifyRawMembership(
		context.Background(),
		snapshot.hash,
		[]byte{0},
		value,
		proof,
		DefaultLimits(),
	); err != nil {
		t.Fatalf("VerifyRawMembership() error = %v", err)
	}
}

func TestProofClaimBoundsAcceptExactKeyValueAndDepth(t *testing.T) {
	t.Parallel()

	var children [16]node
	children[0] = &leafNode{path: []byte{0}, value: []byte{1}}
	children[1] = &leafNode{value: []byte{2}}
	proof, root := proofForInternalNode(t, &branchNode{children: children})
	limits := DefaultLimits()
	limits.MaxKeyBytes = 1
	limits.MaxValueBytes = 1
	limits.MaxTraversalDepth = 1
	if err := verifyClaim(
		context.Background(),
		root,
		[]byte{0},
		[]byte{1},
		true,
		false,
		proof,
		limits,
	); err != nil {
		t.Fatalf("verifyClaim(exact bounds) error = %v", err)
	}
}

func TestProofTraversalChargesOneDepthPerEdge(t *testing.T) {
	t.Parallel()

	leaf := &leafNode{value: []byte{1}}
	var nestedChildren [16]node
	nestedChildren[0] = leaf
	nestedChildren[1] = &leafNode{value: []byte{2}}
	nested := &branchNode{children: nestedChildren}
	var rootChildren [16]node
	rootChildren[0] = nested
	rootChildren[1] = &leafNode{value: []byte{3}}

	branchProof, branchRoot := proofForInternalNode(
		t, &branchNode{children: rootChildren},
	)
	limited := DefaultLimits()
	limited.MaxTraversalDepth = 1
	if err := verifyClaim(
		context.Background(),
		branchRoot,
		[]byte{0},
		[]byte{1},
		true,
		false,
		branchProof,
		limited,
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("verifyClaim(branch depth) error = %v", err)
	}
	branchEncoding, _, err := encodeNode(&branchNode{children: rootChildren})
	if err != nil {
		t.Fatalf("encodeNode(branch root) error = %v", err)
	}
	branchSnapshot := &trieSnapshot{
		root: &branchNode{children: rootChildren},
		hash: keccakRoot(branchEncoding), limits: limited,
	}
	if _, err := proveSnapshot(
		context.Background(), branchSnapshot, []byte{0}, false,
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("proveSnapshot(branch depth) error = %v", err)
	}

	var extensionChildren [16]node
	extensionChildren[1] = leaf
	extensionChildren[2] = &leafNode{value: []byte{2}}
	extension := &extensionNode{
		path: []byte{0}, child: &branchNode{children: extensionChildren},
	}
	extensionProof, extensionRoot := proofForInternalNode(t, extension)
	if err := verifyClaim(
		context.Background(),
		extensionRoot,
		[]byte{1},
		[]byte{1},
		true,
		false,
		extensionProof,
		limited,
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("verifyClaim(extension depth) error = %v", err)
	}
	extensionEncoding, _, err := encodeNode(extension)
	if err != nil {
		t.Fatalf("encodeNode(extension root) error = %v", err)
	}
	extensionSnapshot := &trieSnapshot{
		root: extension, hash: keccakRoot(extensionEncoding), limits: limited,
	}
	if _, err := proveSnapshot(
		context.Background(), extensionSnapshot, []byte{1}, false,
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("proveSnapshot(extension depth) error = %v", err)
	}
}

func TestEnvelopeSizeAndBatchBoundsAreInclusive(t *testing.T) {
	t.Parallel()

	legacyLimits := DefaultLimits()
	legacyLimits.MaxValueBytes = 1
	legacy, err := legacyEnvelopeValue([]byte{0xc0}, legacyLimits)
	if err != nil {
		t.Fatalf("legacyEnvelopeValue(exact limit) error = %v", err)
	}
	if _, err := legacyEnvelopeValue(
		[]byte{0xc1, 0x80}, legacyLimits,
	); !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("legacyEnvelopeValue(over limit) error = %v", err)
	}

	typedLimits := DefaultLimits()
	typedLimits.MaxValueBytes = 2
	typed, err := typedEnvelopeValue(PragueProfile, 4, []byte{0xc0}, typedLimits)
	if err != nil {
		t.Fatalf("typedEnvelopeValue(exact limit) error = %v", err)
	}
	if _, err := typedEnvelopeValue(
		PragueProfile, 4, []byte{0xc1, 0x80}, typedLimits,
	); !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("typedEnvelopeValue(over limit) error = %v", err)
	}

	batchLimits := DefaultLimits()
	batchLimits.MaxBatchOperations = 2
	batchLimits.MaxValueBytes = 2
	if _, err := indexedTrieRoot(
		context.Background(),
		[]encodedTrieValue{legacy, typed},
		batchLimits,
	); err != nil {
		t.Fatalf("indexedTrieRoot(exact batch limit) error = %v", err)
	}
	if _, err := indexedTrieRoot(
		context.Background(),
		[]encodedTrieValue{legacy, typed, typed},
		batchLimits,
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("indexedTrieRoot(over batch limit) error = %v", err)
	}
}

func TestDecodeAccountRejectsNonListRLP(t *testing.T) {
	t.Parallel()

	if _, err := decodeAccount(
		[]byte{0x81}, DefaultLimits(),
	); !errors.Is(err, ErrInvalidAccount) {
		t.Fatalf("decodeAccount(malformed) error = %v", err)
	}
	encoded, err := rlp.Encode(rlp.String([]byte{1}), rlp.DefaultLimits())
	if err != nil {
		t.Fatalf("encode account string: %v", err)
	}
	if _, err := decodeAccount(
		encoded, DefaultLimits(),
	); !errors.Is(err, ErrInvalidAccount) {
		t.Fatalf("decodeAccount(non-list) error = %v", err)
	}
}

func proofByteLength(proof Proof) int {
	total := 0
	for _, encoded := range proof.nodes {
		total += len(encoded)
	}
	return total
}
