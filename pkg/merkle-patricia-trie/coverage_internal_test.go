package mpt

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/merkle-patricia-trie/internal/rlp"
)

func TestInternalCompactAndNodeFailureContracts(t *testing.T) {
	t.Parallel()

	if _, err := EncodeCompactPath(
		make([]byte, MaxCompactPathNibbles+1), false,
	); !errors.Is(err, ErrInvalidCompactPath) {
		t.Fatalf("EncodeCompactPath(oversized) error = %v", err)
	}
	if _, err := DecodeCompactPath(
		make([]byte, MaxCompactPathNibbles/2+2),
	); !errors.Is(err, ErrInvalidCompactPath) {
		t.Fatalf("DecodeCompactPath(oversized) error = %v", err)
	}
	if _, err := newExtension([]byte{16}, &branchNode{}); !errors.Is(err, ErrMalformedNode) {
		t.Fatalf("newExtension(invalid nibble) error = %v", err)
	}
	if err := validateNibbles(
		make([]byte, MaxCompactPathNibbles+1),
	); !errors.Is(err, ErrMalformedNode) {
		t.Fatalf("validateNibbles(oversized) error = %v", err)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	state := encodingState{
		ctx: canceled, nodesLeft: 1, budget: &workBudget{hashesLeft: 1},
	}
	if _, err := encodeNodeValue(nil, &state); !errors.Is(err, ErrCanceled) {
		t.Fatalf("encodeNodeValue(canceled) error = %v", err)
	}
	if _, _, err := encodeNodeBounded(
		context.Background(),
		&leafNode{path: []byte{16}, value: []byte{1}},
		1,
		&workBudget{hashesLeft: 1},
	); !errors.Is(err, ErrMalformedNode) {
		t.Fatalf("encode invalid leaf error = %v", err)
	}
	if _, _, err := encodeNodeBounded(
		context.Background(),
		&extensionNode{path: []byte{1}, child: struct{}{}},
		2,
		&workBudget{hashesLeft: 1},
	); !errors.Is(err, ErrMalformedNode) {
		t.Fatalf("encode invalid extension child error = %v", err)
	}
	if _, _, err := encodeNodeBounded(
		context.Background(),
		&extensionNode{path: []byte{16}, child: hashNode{}},
		2,
		&workBudget{hashesLeft: 1},
	); !errors.Is(err, ErrMalformedNode) {
		t.Fatalf("encode invalid extension path error = %v", err)
	}
	var children [16]node
	children[0] = struct{}{}
	if _, _, err := encodeNodeBounded(
		context.Background(),
		&branchNode{children: children},
		2,
		&workBudget{hashesLeft: 1},
	); !errors.Is(err, ErrMalformedNode) {
		t.Fatalf("encode invalid branch child error = %v", err)
	}
	if _, _, err := encodeNodeBounded(
		context.Background(),
		struct{}{},
		1,
		&workBudget{hashesLeft: 1},
	); !errors.Is(err, ErrMalformedNode) {
		t.Fatalf("encode unsupported node error = %v", err)
	}
	if _, _, err := childReference(
		nodeEncoding{bytes: make([]byte, RootBytes), persisted: make(map[Root][]byte)},
		&encodingState{
			ctx: context.Background(), nodesLeft: 1,
			budget: &workBudget{},
		},
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("childReference(hash limit) error = %v", err)
	}
	if _, err := encodeRLPValue(
		rlp.String(make([]byte, rlp.DefaultLimits().MaxEncodedBytes+1)),
		nil,
	); !errors.Is(err, ErrMalformedNode) {
		t.Fatalf("encodeRLPValue(oversized) error = %v", err)
	}
}

func TestInternalNodeDecodeFailureContracts(t *testing.T) {
	t.Parallel()

	if _, err := decodeShortNode([]rlp.Value{
		rlp.String([]byte{0x00}),
		rlp.String([]byte{1}),
	}); !errors.Is(err, ErrMalformedNode) {
		t.Fatalf("decodeShortNode(empty extension) error = %v", err)
	}
	branchElements := make([]rlp.Value, 17)
	for index := range branchElements {
		branchElements[index] = rlp.String(nil)
	}
	branchElements[0] = rlp.String([]byte{1})
	if _, err := decodeBranchNode(branchElements); !errors.Is(err, ErrMalformedNode) {
		t.Fatalf("decodeBranchNode(invalid child) error = %v", err)
	}
	largeEmbedded := rlp.List(rlp.String(make([]byte, RootBytes)))
	if _, err := decodeChildReference(largeEmbedded); !errors.Is(err, ErrMalformedNode) {
		t.Fatalf("decodeChildReference(oversized embedded) error = %v", err)
	}
	if _, err := decodeChildReference(
		rlp.List(rlp.String(nil)),
	); !errors.Is(err, ErrMalformedNode) {
		t.Fatalf("decodeChildReference(invalid arity) error = %v", err)
	}
}

func TestInternalStoreHelpersAndTypedErrors(t *testing.T) {
	t.Parallel()

	var hash Root
	hash[0] = 1
	cause := errors.New("cause")
	missing := &MissingNodeError{Hash: hash, Cause: cause}
	if missing.Error() == "" || !errors.Is(missing, cause) {
		t.Fatalf("MissingNodeError = %q, unwrap %v", missing.Error(), errors.Unwrap(missing))
	}
	corrupt := &CorruptNodeError{Hash: hash, Cause: cause}
	if corrupt.Error() == "" || !errors.Is(corrupt, cause) {
		t.Fatalf("CorruptNodeError = %q, unwrap %v", corrupt.Error(), errors.Unwrap(corrupt))
	}

	var nilMap map[string]int
	if validStore(nilMap) {
		t.Fatal("validStore(nil map) = true")
	}
	if !validStore(map[string]int{}) || !validStore(struct{}{}) {
		t.Fatal("validStore(non-nil value) = false")
	}
	left := &struct{ value int }{value: 1}
	right := &struct{ value int }{value: 1}
	if sameStore(nil, left) || sameStore(left, struct{}{}) ||
		!sameStore(left, left) || sameStore(left, right) {
		t.Fatal("sameStore pointer identity mismatch")
	}
	if !sameStore(struct{ value int }{1}, struct{ value int }{1}) {
		t.Fatal("sameStore comparable values = false")
	}
	if sameStore([]int{1}, []int{1}) {
		t.Fatal("sameStore uncomparable values = true")
	}
}

func TestInternalBatchAndEnvelopeFailureContracts(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	limits.MaxKeyBytes = 1
	trie, err := NewRawTrie(limits)
	if err != nil {
		t.Fatalf("NewRawTrie() error = %v", err)
	}
	if _, err := trie.ApplyBatch(context.Background(), []Mutation{
		Put([]byte{1, 2}, []byte{1}),
	}); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("ApplyBatch(oversized put key) error = %v", err)
	}
	if _, err := trie.ApplyBatch(context.Background(), []Mutation{
		Remove([]byte{1, 2}),
	}); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("ApplyBatch(oversized remove key) error = %v", err)
	}
	trie.snapshot.limits.MaxHashOperations = 0
	if _, err := applyBatch(
		context.Background(),
		trie.snapshot,
		[]Mutation{Put([]byte{1}, []byte{1})},
		true,
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("applyBatch(hash limit) error = %v", err)
	}

	invalid := DefaultLimits()
	invalid.MaxProofBytes = 0
	if _, err := LegacyTrieValue([]byte{0xc0}, invalid); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("LegacyTrieValue(invalid limits) error = %v", err)
	}
	if _, err := TypedTrieValue(1, []byte{1}, invalid); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("TypedTrieValue(invalid limits) error = %v", err)
	}
	small := DefaultLimits()
	small.MaxValueBytes = 1
	if _, err := LegacyTrieValue(nil, small); !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("LegacyTrieValue(empty) error = %v", err)
	}
	valid, err := TypedTrieValue(1, []byte{1}, DefaultLimits())
	if err != nil {
		t.Fatalf("TypedTrieValue() error = %v", err)
	}
	valid.encoded = make([]byte, DefaultLimits().MaxValueBytes+1)
	if _, err := indexedTrieRoot(
		context.Background(),
		[]EncodedTrieValue{valid},
		DefaultLimits(),
	); !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("indexedTrieRoot(oversized encoded) error = %v", err)
	}
	limited := DefaultLimits()
	limited.MaxTraversalNodes = 1
	first, _ := TypedTrieValue(1, []byte{1}, limited)
	if _, err := indexedTrieRoot(
		context.Background(),
		[]EncodedTrieValue{first, first},
		limited,
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("indexedTrieRoot(operation limit) error = %v", err)
	}
	if _, err := indexedTrieRoot(
		context.Background(), nil, invalid,
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("indexedTrieRoot(invalid limits) error = %v", err)
	}
}

func TestInternalAccountDecodeFailureContracts(t *testing.T) {
	t.Parallel()

	if _, err := VerifyAccountProof(
		context.Background(),
		EmptyRoot(),
		[20]byte{},
		[]byte{0xc0},
		Proof{},
		DefaultLimits(),
	); !errors.Is(err, ErrFailedProof) {
		t.Fatalf("VerifyAccountProof(failed proof) error = %v", err)
	}
	if got := encodeStorageInteger([]byte{0x80}); len(got) != 2 ||
		got[0] != 0x81 || got[1] != 0x80 {
		t.Fatalf("encodeStorageInteger(0x80) = %x", got)
	}

	invalid := DefaultLimits()
	invalid.MaxNodeReads = 0
	if _, err := decodeAccount([]byte{0xc0}, invalid); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("decodeAccount(invalid limits) error = %v", err)
	}
	tests := []struct {
		name   string
		fields []rlp.Value
	}{
		{
			name: "field count",
			fields: []rlp.Value{
				rlp.String(nil), rlp.String(nil), rlp.String(nil),
			},
		},
		{
			name: "field kind",
			fields: []rlp.Value{
				rlp.List(), rlp.String(nil),
				rlp.String(make([]byte, RootBytes)),
				rlp.String(make([]byte, RootBytes)),
			},
		},
		{
			name: "integer leading zero",
			fields: []rlp.Value{
				rlp.String([]byte{0}), rlp.String(nil),
				rlp.String(make([]byte, RootBytes)),
				rlp.String(make([]byte, RootBytes)),
			},
		},
		{
			name: "storage root length",
			fields: []rlp.Value{
				rlp.String(nil), rlp.String(nil),
				rlp.String(make([]byte, RootBytes-1)),
				rlp.String(make([]byte, RootBytes)),
			},
		},
		{
			name: "code hash length",
			fields: []rlp.Value{
				rlp.String(nil), rlp.String(nil),
				rlp.String(make([]byte, RootBytes)),
				rlp.String(make([]byte, RootBytes-1)),
			},
		},
	}
	for _, test := range tests {
		encoded, err := rlp.Encode(rlp.List(test.fields...), rlp.DefaultLimits())
		if err != nil {
			t.Fatalf("%s encode: %v", test.name, err)
		}
		if _, err := decodeAccount(
			encoded, DefaultLimits(),
		); !errors.Is(err, ErrInvalidAccount) {
			t.Fatalf("%s decodeAccount() error = %v", test.name, err)
		}
	}
}

func TestInternalIterationFailureContracts(t *testing.T) {
	t.Parallel()

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	state := iterationState{
		traversal: traversalState{
			ctx: canceled, maxDepth: 2, nodesLeft: 2,
			budget: &workBudget{hashesLeft: 2},
		},
		yield:   func(Entry) error { return nil },
		hardMax: 2,
	}
	if err := state.walk(nil, nil, 0); !errors.Is(err, ErrCanceled) {
		t.Fatalf("walk(canceled) error = %v", err)
	}

	state = iterationState{
		traversal: traversalState{
			ctx: context.Background(), maxDepth: 2, nodesLeft: 4,
			budget: &workBudget{hashesLeft: 2},
		},
		yield:   func(Entry) error { return nil },
		hardMax: 2,
	}
	if err := state.walk(
		&extensionNode{path: []byte{1}, child: struct{}{}}, nil, 0,
	); !errors.Is(err, ErrMalformedNode) {
		t.Fatalf("walk(invalid extension) error = %v", err)
	}
	callbackErr := errors.New("callback")
	state.yield = func(Entry) error { return callbackErr }
	if err := state.walk(
		&branchNode{value: []byte{1}}, nil, 0,
	); !errors.Is(err, callbackErr) {
		t.Fatalf("walk(branch callback) error = %v", err)
	}
	state.yield = func(Entry) error { return nil }
	state.traversal.nodesLeft = 1
	var children [16]node
	children[0] = &leafNode{path: []byte{0}, value: []byte{1}}
	if err := state.walk(
		&branchNode{children: children}, nil, 0,
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("walk(child traversal bound) error = %v", err)
	}
	state.traversal.nodesLeft = 2
	if err := state.walk(struct{}{}, nil, 0); !errors.Is(err, ErrMalformedNode) {
		t.Fatalf("walk(unsupported) error = %v", err)
	}
	if err := state.emit([]byte{1}, []byte{1}); !errors.Is(err, ErrMalformedNode) {
		t.Fatalf("emit(odd path) error = %v", err)
	}
	state.secure = true
	if err := state.emit([]byte{1, 2}, []byte{1}); !errors.Is(err, ErrMalformedNode) {
		t.Fatalf("emit(short secure path) error = %v", err)
	}
	state.secure = false
	state.traversal.ctx = canceled
	if err := state.emit(nil, []byte{1}); !errors.Is(err, ErrCanceled) {
		t.Fatalf("emit(canceled) error = %v", err)
	}
}

func TestInternalRebuildFailureContracts(t *testing.T) {
	t.Parallel()

	trie, err := NewRawTrie(DefaultLimits())
	if err != nil {
		t.Fatalf("NewRawTrie() error = %v", err)
	}
	trie, err = trie.Update(context.Background(), []byte("a"), []byte("1"))
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	trie.snapshot.limits.MaxEncodingNodes = 0
	if _, err := trie.Rebuild(context.Background()); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("Rebuild(encoding limit) error = %v", err)
	}

	trie.snapshot.limits = DefaultLimits()
	trie.snapshot.hash[0] ^= 0xff
	if _, err := trie.Rebuild(context.Background()); !errors.Is(err, ErrRootMismatch) {
		t.Fatalf("Rebuild(root mismatch) error = %v", err)
	}
}

func TestInternalTraversalFailureContracts(t *testing.T) {
	t.Parallel()

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	state := traversalState{
		ctx: canceled, maxDepth: 2, nodesLeft: 2,
		budget: &workBudget{hashesLeft: 2},
	}
	if _, _, err := getNode(nil, nil, 0, &state); !errors.Is(err, ErrCanceled) {
		t.Fatalf("getNode(canceled) error = %v", err)
	}
	state = traversalState{
		ctx: context.Background(), maxDepth: 2, nodesLeft: 2,
		budget: &workBudget{hashesLeft: 2},
	}
	if _, _, err := getNode(struct{}{}, nil, 0, &state); !errors.Is(err, ErrMalformedNode) {
		t.Fatalf("getNode(unsupported) error = %v", err)
	}
	if _, err := insertNode(
		&extensionNode{path: []byte{1}, child: struct{}{}},
		[]byte{1}, []byte{1}, 0, &state,
	); !errors.Is(err, ErrMalformedNode) {
		t.Fatalf("insertNode(invalid extension) error = %v", err)
	}
	state.nodesLeft = 1
	var children [16]node
	children[0] = &leafNode{value: []byte{1}}
	if _, err := insertNode(
		&branchNode{children: children}, []byte{0, 1}, []byte{1}, 0, &state,
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("insertNode(branch traversal bound) error = %v", err)
	}
	state.nodesLeft = 2
	if _, err := insertNode(struct{}{}, nil, []byte{1}, 0, &state); !errors.Is(err, ErrMalformedNode) {
		t.Fatalf("insertNode(unsupported) error = %v", err)
	}
	if _, err := insertLeaf(
		&leafNode{path: []byte{0}, value: nil}, []byte{1}, []byte{1},
	); !errors.Is(err, ErrMalformedNode) {
		t.Fatalf("insertLeaf(invalid old value) error = %v", err)
	}
	if _, err := insertLeaf(
		&leafNode{path: []byte{0}, value: []byte{1}}, []byte{1}, nil,
	); !errors.Is(err, ErrMalformedNode) {
		t.Fatalf("insertLeaf(invalid new value) error = %v", err)
	}

	state.nodesLeft = 2
	if _, _, err := deleteNode(
		&extensionNode{path: []byte{1}, child: struct{}{}},
		[]byte{1}, 0, &state,
	); !errors.Is(err, ErrMalformedNode) {
		t.Fatalf("deleteNode(invalid extension) error = %v", err)
	}
	state.nodesLeft = 1
	if _, _, err := deleteNode(
		&branchNode{children: children}, []byte{0}, 0, &state,
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("deleteNode(branch traversal bound) error = %v", err)
	}
	state.nodesLeft = 2
	if _, _, err := deleteNode(struct{}{}, nil, 0, &state); !errors.Is(err, ErrMalformedNode) {
		t.Fatalf("deleteNode(unsupported) error = %v", err)
	}
	if compacted, err := compactBranch([16]node{}, nil); err != nil || compacted != nil {
		t.Fatalf("compactBranch(empty) = (%#v, %v)", compacted, err)
	}
}

func TestInternalProofValidationAndGenerationBounds(t *testing.T) {
	t.Parallel()

	invalid := DefaultLimits()
	invalid.MaxProofNodes = 0
	if _, err := ProofFromNodes(nil, invalid); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("ProofFromNodes(invalid limits) error = %v", err)
	}
	if err := verifyClaim(
		context.Background(), EmptyRoot(), nil, nil,
		false, false, Proof{}, invalid,
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("verifyClaim(invalid limits) error = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := verifyClaim(
		canceled, EmptyRoot(), nil, nil,
		false, false, Proof{}, DefaultLimits(),
	); !errors.Is(err, ErrCanceled) {
		t.Fatalf("verifyClaim(canceled) error = %v", err)
	}
	limits := DefaultLimits()
	if err := verifyClaim(
		context.Background(), EmptyRoot(),
		make([]byte, limits.MaxKeyBytes+1), nil,
		false, false, Proof{}, limits,
	); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("verifyClaim(oversized key) error = %v", err)
	}
	if err := verifyClaim(
		context.Background(), EmptyRoot(), nil, nil,
		true, false, Proof{}, limits,
	); !errors.Is(err, ErrInvalidValue) {
		t.Fatalf("verifyClaim(empty expected value) error = %v", err)
	}
	oversizedProof := Proof{nodes: make([][]byte, limits.MaxProofNodes+1)}
	if err := verifyClaim(
		context.Background(), EmptyRoot(), nil, nil,
		false, false, oversizedProof, limits,
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("verifyClaim(proof node limit) error = %v", err)
	}
	byteLimited := limits
	byteLimited.MaxProofBytes = 1
	if err := validateProofLimits(
		Proof{nodes: [][]byte{{1, 2}}}, byteLimited,
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("validateProofLimits(byte limit) error = %v", err)
	}
	if err := verifyClaim(
		context.Background(), EmptyRoot(), nil, nil,
		false, false, Proof{nodes: [][]byte{{0x80}}}, limits,
	); !errors.Is(err, ErrMalformedProof) {
		t.Fatalf("verifyClaim(empty root surplus) error = %v", err)
	}
	if err := verifyClaim(
		context.Background(), EmptyRoot(), nil, []byte{1},
		true, false, Proof{}, limits,
	); !errors.Is(err, ErrFailedProof) {
		t.Fatalf("verifyClaim(empty root membership) error = %v", err)
	}
	var nonempty Root
	nonempty[0] = 1
	if err := verifyClaim(
		context.Background(), nonempty, nil, nil,
		false, false, Proof{}, limits,
	); !errors.Is(err, ErrIncompleteProof) {
		t.Fatalf("verifyClaim(missing proof) error = %v", err)
	}

	trie, err := NewSecureTrie(DefaultLimits())
	if err != nil {
		t.Fatalf("NewSecureTrie() error = %v", err)
	}
	trie, err = trie.Update(context.Background(), []byte("key"), []byte("value"))
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	trie.snapshot.limits.MaxHashOperations = 0
	if _, err := trie.Prove(
		context.Background(), []byte("key"),
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("Prove(hash limit) error = %v", err)
	}
	trie.snapshot.limits = DefaultLimits()
	trie.snapshot.limits.MaxTraversalNodes = 0
	if _, err := trie.Prove(
		context.Background(), []byte("key"),
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("Prove(traversal limit) error = %v", err)
	}
	trie.snapshot.limits = DefaultLimits()
	trie.snapshot.limits.MaxEncodingNodes = 0
	if _, err := trie.Prove(
		context.Background(), []byte("key"),
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("Prove(encoding limit) error = %v", err)
	}
	trie.snapshot.limits = DefaultLimits()
	trie.snapshot.limits.MaxProofNodes = 0
	if _, err := trie.Prove(
		context.Background(), []byte("key"),
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("Prove(node limit) error = %v", err)
	}
	trie.snapshot.limits = DefaultLimits()
	trie.snapshot.limits.MaxProofBytes = 1
	if _, err := trie.Prove(
		context.Background(), []byte("key"),
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("Prove(byte limit) error = %v", err)
	}
	trie.snapshot.limits = DefaultLimits()
	trie.snapshot.root = struct{}{}
	if _, err := trie.Prove(
		context.Background(), []byte("key"),
	); !errors.Is(err, ErrMalformedNode) {
		t.Fatalf("Prove(unsupported node) error = %v", err)
	}
}

func TestInternalProofTraversalFailureContracts(t *testing.T) {
	t.Parallel()

	leafProof, leafRoot := proofForInternalNode(
		t, &leafNode{path: []byte{0, 1}, value: []byte{1}},
	)
	ctx := &nthErrorContext{at: 2}
	if err := verifyClaim(
		ctx, leafRoot, []byte{0x01}, []byte{1},
		true, false, leafProof, DefaultLimits(),
	); !errors.Is(err, ErrCanceled) {
		t.Fatalf("verifyClaim(loop cancellation) error = %v", err)
	}
	var branchChildren [16]node
	branchChildren[0] = &leafNode{
		path: nil, value: make([]byte, RootBytes),
	}
	branch := &branchNode{children: branchChildren, value: []byte{9}}
	branchProof, branchRoot := proofForInternalNode(t, branch)
	if err := verifyClaim(
		context.Background(), branchRoot, nil, []byte{9},
		true, false, branchProof, DefaultLimits(),
	); err != nil {
		t.Fatalf("verifyClaim(branch value) error = %v", err)
	}
	if err := verifyClaim(
		context.Background(), branchRoot, []byte{0x10}, nil,
		false, false, branchProof, DefaultLimits(),
	); err != nil {
		t.Fatalf("verifyClaim(branch nil child) error = %v", err)
	}
	boundedChildren := [16]node{}
	boundedChildren[0] = &leafNode{path: nil, value: []byte{1}}
	boundedChildren[1] = &leafNode{path: nil, value: []byte{2}}
	boundedProof, boundedRoot := proofForInternalNode(
		t, &branchNode{children: boundedChildren},
	)
	limited := DefaultLimits()
	limited.MaxTraversalNodes = 1
	if err := verifyClaim(
		context.Background(), boundedRoot, []byte{0x00}, []byte{1},
		true, false, boundedProof, limited,
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("verifyClaim(traversal limit) error = %v", err)
	}

	extensionChildren := [16]node{}
	extensionChildren[1] = &leafNode{path: nil, value: []byte{1}}
	extensionChildren[2] = &leafNode{path: nil, value: []byte{2}}
	extension := &extensionNode{
		path:  []byte{0},
		child: &branchNode{children: extensionChildren},
	}
	extensionProof, extensionRoot := proofForInternalNode(t, extension)
	if err := verifyClaim(
		context.Background(), extensionRoot, []byte{0xf0}, nil,
		false, false, extensionProof, DefaultLimits(),
	); err != nil {
		t.Fatalf("verifyClaim(extension mismatch) error = %v", err)
	}

	childLeaf := &leafNode{path: []byte{2}, value: make([]byte, RootBytes)}
	childEncoded, _, err := encodeNode(childLeaf)
	if err != nil {
		t.Fatalf("encode child leaf: %v", err)
	}
	childHash := keccakRoot(childEncoded)
	malformedExtension := &extensionNode{
		path: []byte{1}, child: hashNode(childHash),
	}
	rootEncoded, _, err := encodeNode(malformedExtension)
	if err != nil {
		t.Fatalf("encode malformed extension: %v", err)
	}
	malformedProof := Proof{nodes: [][]byte{rootEncoded, childEncoded}}
	if err := verifyClaim(
		context.Background(), keccakRoot(rootEncoded), []byte{0x12}, nil,
		false, false, malformedProof, DefaultLimits(),
	); !errors.Is(err, ErrMalformedProof) {
		t.Fatalf("verifyClaim(extension compact child) error = %v", err)
	}
	missingChild := Proof{nodes: [][]byte{rootEncoded}}
	if err := verifyClaim(
		context.Background(), keccakRoot(rootEncoded), []byte{0x12}, nil,
		false, false, missingChild, DefaultLimits(),
	); !errors.Is(err, ErrIncompleteProof) {
		t.Fatalf("verifyClaim(extension missing child) error = %v", err)
	}
	mismatchedChild := append([]byte(nil), childEncoded...)
	mismatchedChild[len(mismatchedChild)-1] ^= 0xff
	if err := verifyClaim(
		context.Background(),
		keccakRoot(rootEncoded),
		[]byte{0x12},
		nil,
		false,
		false,
		Proof{nodes: [][]byte{rootEncoded, mismatchedChild}},
		DefaultLimits(),
	); !errors.Is(err, ErrMalformedProof) {
		t.Fatalf("verifyClaim(extension mismatched child) error = %v", err)
	}

	hashLimited := DefaultLimits()
	hashLimited.MaxHashOperations = 1
	if err := verifyClaim(
		context.Background(),
		keccakRoot(rootEncoded),
		[]byte{0x12},
		nil,
		false,
		false,
		malformedProof,
		hashLimited,
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("verifyClaim(hash limit) error = %v", err)
	}
}

func TestInternalProofNodeCanonicalityFailures(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	index := 0
	invalidEncoding := []byte{0x80}
	if _, err := proofNode(
		Proof{nodes: [][]byte{invalidEncoding}},
		&index,
		keccakRoot(invalidEncoding),
		true,
		&workBudget{hashesLeft: 1},
	); !errors.Is(err, ErrMalformedProof) {
		t.Fatalf("proofNode(null) error = %v", err)
	}
	index = 0
	malformed := []byte{0x81}
	if _, err := proofNode(
		Proof{nodes: [][]byte{malformed}},
		&index,
		keccakRoot(malformed),
		true,
		&workBudget{hashesLeft: 1},
	); !errors.Is(err, ErrMalformedProof) {
		t.Fatalf("proofNode(malformed) error = %v", err)
	}
	if err := validateProofLimits(
		Proof{nodes: make([][]byte, limits.MaxProofNodes+1)}, limits,
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("validateProofLimits(node limit) error = %v", err)
	}
}

func TestInternalLoadedProofAndResolveFailures(t *testing.T) {
	t.Parallel()

	trie, err := NewRawTrie(DefaultLimits())
	if err != nil {
		t.Fatalf("NewRawTrie() error = %v", err)
	}
	trie, err = trie.Update(context.Background(), []byte("key"), []byte("value"))
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	encoded, _, err := encodeNode(trie.snapshot.root)
	if err != nil {
		t.Fatalf("encodeNode() error = %v", err)
	}
	root := keccakRoot(encoded)
	reader := nodeReaderFunc(func(_ context.Context, hash Root) ([]byte, error) {
		if hash != root {
			return nil, ErrMissingNode
		}
		return append([]byte(nil), encoded...), nil
	})
	loaded := &trieSnapshot{
		root: hashNode(root), hash: root, base: root,
		limits: DefaultLimits(), reader: reader,
	}
	if _, err := proveSnapshot(
		context.Background(), loaded, []byte("key"), false,
	); err != nil {
		t.Fatalf("proveSnapshot(loaded) error = %v", err)
	}
	loaded.reader = nodeReaderFunc(func(context.Context, Root) ([]byte, error) {
		return nil, ErrMissingNode
	})
	if _, err := proveSnapshot(
		context.Background(), loaded, []byte("key"), false,
	); !errors.Is(err, ErrMissingNode) {
		t.Fatalf("proveSnapshot(missing root) error = %v", err)
	}

	state := traversalState{
		ctx: context.Background(), maxDepth: 2, nodesLeft: 2,
		readsLeft: 1, budget: &workBudget{hashesLeft: 1},
	}
	if _, err := state.resolve(root); !errors.Is(err, ErrMalformedNode) {
		t.Fatalf("resolve(no reader) error = %v", err)
	}
	state.reader = reader
	state.readsLeft = 0
	if _, err := state.resolve(root); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("resolve(read limit) error = %v", err)
	}
	state.readsLeft = 1
	state.budget.hashesLeft = 0
	if _, err := state.resolve(root); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("resolve(hash limit) error = %v", err)
	}

	nullEncoding := []byte{0x80}
	nullRoot := keccakRoot(nullEncoding)
	state.reader = nodeReaderFunc(func(context.Context, Root) ([]byte, error) {
		return nullEncoding, nil
	})
	state.readsLeft = 1
	state.budget.hashesLeft = 1
	if _, err := state.resolve(nullRoot); !errors.Is(err, ErrCorruptNode) {
		t.Fatalf("resolve(null node) error = %v", err)
	}
	malformedEncoding := []byte{0x81}
	malformedRoot := keccakRoot(malformedEncoding)
	state.reader = nodeReaderFunc(func(context.Context, Root) ([]byte, error) {
		return malformedEncoding, nil
	})
	state.readsLeft = 1
	state.budget.hashesLeft = 1
	if _, err := state.resolve(malformedRoot); !errors.Is(err, ErrCorruptNode) {
		t.Fatalf("resolve(malformed node) error = %v", err)
	}

	state.reader = nodeReaderFunc(func(context.Context, Root) ([]byte, error) {
		return nil, ErrMissingNode
	})
	state.readsLeft = 1
	state.budget.hashesLeft = 1
	if _, err := state.extensionChild(
		hashNode(root),
	); !errors.Is(err, ErrMissingNode) {
		t.Fatalf("extensionChild(resolve failure) error = %v", err)
	}
	compactEncoding, _, err := encodeNode(
		&leafNode{path: nil, value: make([]byte, RootBytes)},
	)
	if err != nil {
		t.Fatalf("encode compact child: %v", err)
	}
	compactRoot := keccakRoot(compactEncoding)
	state.reader = nodeReaderFunc(func(context.Context, Root) ([]byte, error) {
		return compactEncoding, nil
	})
	state.readsLeft = 1
	state.budget.hashesLeft = 1
	if _, err := state.extensionChild(
		hashNode(compactRoot),
	); !errors.Is(err, ErrCorruptNode) {
		t.Fatalf("extensionChild(compact child) error = %v", err)
	}
}

func TestInternalProofGenerationTerminalPaths(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	var branchChildren [16]node
	branchChildren[0] = &leafNode{path: nil, value: []byte{1}}
	branchChildren[1] = &leafNode{path: nil, value: []byte{2}}
	branchSnapshot := &trieSnapshot{
		root: &branchNode{children: branchChildren, value: []byte{9}},
		hash: EmptyRoot(), base: EmptyRoot(), limits: limits,
	}
	if _, err := proveSnapshot(
		context.Background(), branchSnapshot, nil, false,
	); err != nil {
		t.Fatalf("proveSnapshot(branch value) error = %v", err)
	}
	extensionSnapshot := &trieSnapshot{
		root: &extensionNode{
			path:  []byte{0},
			child: &branchNode{children: branchChildren},
		},
		hash: EmptyRoot(), base: EmptyRoot(), limits: limits,
	}
	if _, err := proveSnapshot(
		context.Background(), extensionSnapshot, []byte{0xf0}, false,
	); err != nil {
		t.Fatalf("proveSnapshot(extension mismatch) error = %v", err)
	}
}

func TestInternalCommitFinishAndHashNodeFailures(t *testing.T) {
	t.Parallel()

	store := &captureNodeStore{}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := commitSnapshot(
		canceled,
		&trieSnapshot{limits: DefaultLimits()},
		store,
	); !errors.Is(err, ErrCanceled) {
		t.Fatalf("commitSnapshot(canceled) error = %v", err)
	}
	root := EmptyRoot()
	bound := &trieSnapshot{
		hash: root, base: root, limits: DefaultLimits(), reader: store,
	}
	if got, err := commitSnapshot(
		context.Background(), bound, store,
	); err != nil || got != bound {
		t.Fatalf("commitSnapshot(no-op) = (%p, %v), want %p", got, err, bound)
	}

	ctx := &nthErrorContext{at: 3}
	if _, err := finishSnapshot(
		ctx,
		&leafNode{path: nil, value: []byte{1}},
		DefaultLimits(),
		EmptyRoot(),
		nil,
		&workBudget{hashesLeft: 2},
	); !errors.Is(err, ErrCanceled) {
		t.Fatalf("finishSnapshot(post-encode cancellation) error = %v", err)
	}
	if _, err := finishSnapshot(
		canceled,
		&leafNode{path: nil, value: []byte{1}},
		DefaultLimits(),
		EmptyRoot(),
		nil,
		&workBudget{hashesLeft: 2},
	); !errors.Is(err, ErrCanceled) {
		t.Fatalf("finishSnapshot(initial cancellation) error = %v", err)
	}

	state := traversalState{
		ctx: context.Background(), maxDepth: 2, nodesLeft: 2,
		readsLeft: 1, budget: &workBudget{hashesLeft: 1},
	}
	if _, err := insertNode(
		hashNode(root), nil, []byte{1}, 0, &state,
	); !errors.Is(err, ErrMalformedNode) {
		t.Fatalf("insertNode(unresolved hash) error = %v", err)
	}
	state.nodesLeft = 2
	if _, _, err := deleteNode(
		hashNode(root), nil, 0, &state,
	); !errors.Is(err, ErrMalformedNode) {
		t.Fatalf("deleteNode(unresolved hash) error = %v", err)
	}
	state.nodesLeft = 2
	if _, _, err := deleteNode(
		&branchNode{}, nil, 0, &state,
	); err != nil {
		t.Fatalf("deleteNode(empty branch value) error = %v", err)
	}

	invalidSnapshot := &trieSnapshot{
		root: struct{}{}, hash: EmptyRoot(), base: EmptyRoot(),
		limits: DefaultLimits(),
	}
	if _, err := updateSnapshot(
		context.Background(), invalidSnapshot, nil, []byte{1}, false,
	); !errors.Is(err, ErrMalformedNode) {
		t.Fatalf("updateSnapshot(invalid root) error = %v", err)
	}
	if _, err := deleteSnapshot(
		context.Background(), invalidSnapshot, nil, false,
	); !errors.Is(err, ErrMalformedNode) {
		t.Fatalf("deleteSnapshot(invalid root) error = %v", err)
	}

	secure, err := NewSecureTrie(DefaultLimits())
	if err != nil {
		t.Fatalf("NewSecureTrie() error = %v", err)
	}
	secure, err = secure.Update(context.Background(), []byte("a"), []byte("1"))
	if err != nil {
		t.Fatalf("secure Update(a) error = %v", err)
	}
	secure, err = secure.Update(context.Background(), []byte("b"), []byte("2"))
	if err != nil {
		t.Fatalf("secure Update(b) error = %v", err)
	}
	secure.snapshot.limits.MaxRebuildNodes = 1
	if _, err := secure.Rebuild(context.Background()); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("SecureTrie Rebuild(limit) error = %v", err)
	}
}

func TestInternalInsertExtensionFailureContracts(t *testing.T) {
	t.Parallel()

	state := traversalState{
		ctx: context.Background(), maxDepth: 2, nodesLeft: 1,
		budget: &workBudget{hashesLeft: 2},
	}
	current := &extensionNode{
		path:  []byte{1},
		child: &branchNode{},
	}
	if _, err := insertExtension(
		current, []byte{1, 2}, []byte{1}, 0, &state,
	); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("insertExtension(recursion limit) error = %v", err)
	}

	invalidNested := &extensionNode{
		path: []byte{1, 2},
		child: &extensionNode{
			path: []byte{3},
			child: &leafNode{
				path: nil, value: []byte{1},
			},
		},
	}
	state.nodesLeft = 4
	if _, err := insertExtension(
		invalidNested, []byte{4}, []byte{1}, 0, &state,
	); !errors.Is(err, ErrMalformedNode) {
		t.Fatalf("insertExtension(invalid old compact path) error = %v", err)
	}
	state.nodesLeft = 4
	if _, err := insertExtension(
		&extensionNode{
			path:  []byte{1},
			child: &branchNode{},
		},
		[]byte{2},
		nil,
		0,
		&state,
	); !errors.Is(err, ErrMalformedNode) {
		t.Fatalf("insertExtension(invalid new leaf) error = %v", err)
	}
}

type nodeReaderFunc func(context.Context, Root) ([]byte, error)

func (reader nodeReaderFunc) GetNode(ctx context.Context, root Root) ([]byte, error) {
	return reader(ctx, root)
}

type captureNodeStore struct{}

func (*captureNodeStore) GetNode(context.Context, Root) ([]byte, error) {
	return nil, ErrMissingNode
}

func (*captureNodeStore) CommitTrie(context.Context, StoreCommit) error {
	return nil
}

func proofForInternalNode(t *testing.T, current node) (Proof, Root) {
	t.Helper()
	encoded, _, err := encodeNode(current)
	if err != nil {
		t.Fatalf("encodeNode() error = %v", err)
	}
	return Proof{nodes: [][]byte{encoded}}, keccakRoot(encoded)
}

type nthErrorContext struct {
	calls int
	at    int
}

func (ctx *nthErrorContext) Deadline() (time.Time, bool) {
	return time.Time{}, false
}

func (ctx *nthErrorContext) Done() <-chan struct{} {
	return nil
}

func (ctx *nthErrorContext) Err() error {
	ctx.calls++
	if ctx.calls >= ctx.at {
		return context.Canceled
	}
	return nil
}

func (ctx *nthErrorContext) Value(any) any {
	return nil
}
