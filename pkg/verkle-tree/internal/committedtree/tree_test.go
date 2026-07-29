package committedtree

import (
	"context"
	"encoding/hex"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/verkle-tree/internal/backend"
)

func TestBuildMatchesPinnedIndependentTreeRoot(t *testing.T) {
	t.Parallel()

	entries := []Entry{
		{Key: testKey(0x00, 0x00), Value: testValue(0x11)},
		{Key: testKey(0x00, 0x01), Value: testValue(0x22)},
		{Key: testKey(0x01, 0xff), Value: testValue(0x33)},
		{Key: testKey(0x01, 0x7f), Value: testValue(0x44)},
	}
	tree, err := Build(
		context.Background(),
		entries,
		testLimits(),
		testCommitmentLimits(),
	)
	if err != nil {
		t.Fatalf("build committed tree: %v", err)
	}
	root, err := tree.Root()
	if err != nil {
		t.Fatalf("get root: %v", err)
	}
	got, err := root.Bytes()
	if err != nil {
		t.Fatalf("encode root: %v", err)
	}
	want, err := hex.DecodeString(
		"45c94c43252d82b4ee001e956c39b519bb38349dfc3576a11f3ea2f8a4525135",
	)
	if err != nil {
		t.Fatalf("decode expected root: %v", err)
	}
	if string(got[:]) != string(want) {
		t.Fatalf("root = %x, want pinned independent root %x", got, want)
	}
}

func TestBuilderReusesImmutableCommitmentEngineConcurrently(t *testing.T) {
	t.Parallel()

	builder, err := NewBuilder(
		context.Background(),
		testLimits(),
		testCommitmentLimits(),
	)
	if err != nil {
		t.Fatalf("new builder: %v", err)
	}
	entries := []Entry{{Key: testKey(1, 2), Value: testValue(3)}}
	want, err := builder.Build(context.Background(), entries)
	if err != nil {
		t.Fatalf("build expected tree: %v", err)
	}

	const workers = 8
	errorsByWorker := make(chan error, workers)
	var group sync.WaitGroup
	group.Add(workers)
	for range workers {
		go func() {
			defer group.Done()
			got, buildErr := builder.Build(context.Background(), entries)
			if buildErr != nil {
				errorsByWorker <- buildErr
				return
			}
			wantRoot, rootErr := want.Root()
			if rootErr != nil {
				errorsByWorker <- rootErr
				return
			}
			gotRoot, rootErr := got.Root()
			if rootErr != nil {
				errorsByWorker <- rootErr
				return
			}
			wantBytes, encodeErr := wantRoot.Bytes()
			if encodeErr != nil {
				errorsByWorker <- encodeErr
				return
			}
			gotBytes, encodeErr := gotRoot.Bytes()
			if encodeErr != nil {
				errorsByWorker <- encodeErr
				return
			}
			if gotBytes != wantBytes {
				errorsByWorker <- errors.New("concurrent root differs")
			}
		}()
	}
	group.Wait()
	close(errorsByWorker)
	for workerErr := range errorsByWorker {
		t.Error(workerErr)
	}
}

func TestBuildEmptyTreeRetainsIdentityRoot(t *testing.T) {
	t.Parallel()

	tree, err := Build(
		context.Background(),
		nil,
		testLimits(),
		testCommitmentLimits(),
	)
	if err != nil {
		t.Fatalf("build empty tree: %v", err)
	}
	count, err := tree.NodeCount()
	if err != nil {
		t.Fatalf("count empty tree nodes: %v", err)
	}
	if count != 1 {
		t.Fatalf("empty tree node count = %d, want root only", count)
	}
	root, err := tree.Root()
	if err != nil {
		t.Fatalf("get empty root: %v", err)
	}
	empty, err := root.IsIdentity()
	if err != nil {
		t.Fatalf("classify empty root: %v", err)
	}
	if !empty {
		t.Fatal("empty tree root is not the internal identity")
	}
}

func TestBuildCanonicalizesAndOwnsEntries(t *testing.T) {
	t.Parallel()

	entries := []Entry{
		{Key: testKey(0x01, 0xff), Value: testValue(0x33)},
		{Key: testKey(0x00, 0x01), Value: testValue(0x22)},
		{Key: testKey(0x00, 0x00), Value: testValue(0x11)},
	}
	ordered := slices.Clone(entries)
	slices.Reverse(ordered)
	first, err := Build(
		context.Background(),
		entries,
		testLimits(),
		testCommitmentLimits(),
	)
	if err != nil {
		t.Fatalf("build first tree: %v", err)
	}
	second, err := Build(
		context.Background(),
		ordered,
		testLimits(),
		testCommitmentLimits(),
	)
	if err != nil {
		t.Fatalf("build reordered tree: %v", err)
	}
	assertSameRoot(t, first, second)

	rootBefore, err := first.Root()
	if err != nil {
		t.Fatalf("get owned root: %v", err)
	}
	beforeBytes, err := rootBefore.Bytes()
	if err != nil {
		t.Fatalf("encode owned root before mutation: %v", err)
	}
	entries[0].Key = Key{}
	entries[0].Value = Value{}
	rootAfter, err := first.Root()
	if err != nil {
		t.Fatalf("get owned root after caller mutation: %v", err)
	}
	afterBytes, err := rootAfter.Bytes()
	if err != nil {
		t.Fatalf("encode owned root after mutation: %v", err)
	}
	if beforeBytes != afterBytes {
		t.Fatal("caller entry mutation changed immutable root")
	}
}

func TestBuildDistinguishesPresentZeroFromAbsence(t *testing.T) {
	t.Parallel()

	empty, err := Build(context.Background(), nil, testLimits(), testCommitmentLimits())
	if err != nil {
		t.Fatalf("build empty tree: %v", err)
	}
	present, err := Build(
		context.Background(),
		[]Entry{{Key: Key{}, Value: Value{}}},
		testLimits(),
		testCommitmentLimits(),
	)
	if err != nil {
		t.Fatalf("build present-zero tree: %v", err)
	}
	emptyRoot, err := empty.Root()
	if err != nil {
		t.Fatalf("get empty root: %v", err)
	}
	presentRoot, err := present.Root()
	if err != nil {
		t.Fatalf("get present root: %v", err)
	}
	missing, err := emptyRoot.IsIdentity()
	if err != nil || !missing {
		t.Fatalf("empty root identity = %t, error %v", missing, err)
	}
	zeroPresent, err := presentRoot.IsIdentity()
	if err != nil {
		t.Fatalf("classify present-zero root: %v", err)
	}
	if zeroPresent {
		t.Fatal("present-zero entry produced empty root")
	}
}

func TestBuildRetainsCanonicalCollisionTopology(t *testing.T) {
	t.Parallel()

	var left Key
	var right Key
	left[30] = 0x01
	right[30] = 0x02
	tree, err := Build(
		context.Background(),
		[]Entry{
			{Key: right, Value: testValue(0x22)},
			{Key: left, Value: testValue(0x11)},
		},
		testLimits(),
		testCommitmentLimits(),
	)
	if err != nil {
		t.Fatalf("build maximum-depth collision: %v", err)
	}
	count, err := tree.NodeCount()
	if err != nil {
		t.Fatalf("count collision nodes: %v", err)
	}
	if count != 33 {
		t.Fatalf("collision node count = %d, want 33", count)
	}
	edgeCount, err := tree.EdgeCount()
	if err != nil {
		t.Fatalf("count collision edges: %v", err)
	}
	if edgeCount != 32 {
		t.Fatalf("collision edge count = %d, want 32", edgeCount)
	}
	stemDepths := make(map[uint8]int)
	internalDepths := make(map[uint8]int)
	for _, retained := range tree.nodes {
		switch retained.kind {
		case nodeStem:
			stemDepths[retained.depth]++
		case nodeInternal:
			internalDepths[retained.depth]++
		}
	}
	if stemDepths[31] != 2 || len(stemDepths) != 1 {
		t.Fatalf("stem depths = %v, want two at 31", stemDepths)
	}
	if len(internalDepths) != 31 {
		t.Fatalf("internal depth count = %d, want 31", len(internalDepths))
	}
	for depth := uint8(0); depth <= 30; depth++ {
		if internalDepths[depth] != 1 {
			t.Fatalf("internal nodes at depth %d = %d, want 1", depth, internalDepths[depth])
		}
	}
	for nodeIndex, retained := range tree.nodes {
		if retained.kind == nodeStem {
			if retained.edgeCount != 0 {
				t.Fatalf("stem node %d retains %d edges", nodeIndex, retained.edgeCount)
			}
			continue
		}
		first := int(retained.firstEdge)
		end := first + int(retained.edgeCount)
		if first < 0 || end > len(tree.edges) || first == end {
			t.Fatalf("internal node %d edge range = [%d,%d)", nodeIndex, first, end)
		}
		for edgeIndex := first; edgeIndex < end; edgeIndex++ {
			if int(tree.edges[edgeIndex].child) >= len(tree.nodes) {
				t.Fatalf("edge %d child = %d", edgeIndex, tree.edges[edgeIndex].child)
			}
			if edgeIndex > first && tree.edges[edgeIndex-1].index >= tree.edges[edgeIndex].index {
				t.Fatalf("node %d edges are not ordered", nodeIndex)
			}
		}
	}
}

func TestBuildRejectsDuplicateKeys(t *testing.T) {
	t.Parallel()

	entry := Entry{Key: testKey(1, 2), Value: testValue(3)}
	if _, err := Build(
		context.Background(),
		[]Entry{entry, entry},
		testLimits(),
		testCommitmentLimits(),
	); !errors.Is(err, errDuplicateKey) {
		t.Fatalf("duplicate error = %v, want %v", err, errDuplicateKey)
	}
}

func TestBuildRejectsInvalidLimits(t *testing.T) {
	t.Parallel()

	valid := testLimits()
	invalid := []Limits{{}}
	for _, clear := range []func(*Limits){
		func(limits *Limits) { limits.MaxEntries = 0 },
		func(limits *Limits) { limits.MaxStems = 0 },
		func(limits *Limits) { limits.MaxNodes = 0 },
		func(limits *Limits) { limits.MaxEdges = 0 },
		func(limits *Limits) { limits.MaxCommitments = 0 },
		func(limits *Limits) { limits.MaxFieldMappings = 0 },
		func(limits *Limits) { limits.MaxCommitmentTerms = 0 },
		func(limits *Limits) { limits.MaxTemporaryBytes = 0 },
	} {
		candidate := valid
		clear(&candidate)
		invalid = append(invalid, candidate)
	}
	tooLarge := valid
	tooLarge.MaxEntries = maxSupportedCount + 1
	invalid = append(invalid, tooLarge)
	tooLarge = valid
	tooLarge.MaxStems = maxSupportedCount + 1
	invalid = append(invalid, tooLarge)
	tooLarge = valid
	tooLarge.MaxNodes = maxSupportedCount + 1
	invalid = append(invalid, tooLarge)
	tooLarge = valid
	tooLarge.MaxEdges = maxSupportedCount + 1
	invalid = append(invalid, tooLarge)
	tooLarge = valid
	tooLarge.MaxCommitments = maxSupportedCount + 1
	invalid = append(invalid, tooLarge)

	for _, limits := range invalid {
		if _, err := Build(
			context.Background(),
			nil,
			limits,
			testCommitmentLimits(),
		); !errors.Is(err, errInvalidLimits) {
			t.Fatalf("invalid limits error = %v, want %v", err, errInvalidLimits)
		}
	}
}

func TestBuildRejectsExhaustedResourcesBeforeCommitmentWork(t *testing.T) {
	t.Parallel()

	entries := []Entry{
		{Key: testKey(0, 0), Value: testValue(1)},
		{Key: testKey(1, 0), Value: testValue(2)},
	}
	tests := []struct {
		name     string
		limits   Limits
		resource Resource
		limit    uint64
		actual   uint64
	}{
		{
			name:     "entries",
			limits:   Limits{MaxEntries: 1, MaxStems: 16, MaxNodes: 64, MaxEdges: 64, MaxCommitments: 64, MaxFieldMappings: 64, MaxCommitmentTerms: 1024, MaxTemporaryBytes: 1 << 20},
			resource: ResourceEntries,
			limit:    1,
			actual:   2,
		},
		{
			name:     "initial bytes",
			limits:   Limits{MaxEntries: 16, MaxStems: 16, MaxNodes: 64, MaxEdges: 64, MaxCommitments: 64, MaxFieldMappings: 64, MaxCommitmentTerms: 1024, MaxTemporaryBytes: 255},
			resource: ResourceTemporaryBytes,
			limit:    255,
			actual:   256,
		},
		{
			name:     "stems",
			limits:   Limits{MaxEntries: 16, MaxStems: 1, MaxNodes: 64, MaxEdges: 64, MaxCommitments: 64, MaxFieldMappings: 64, MaxCommitmentTerms: 1024, MaxTemporaryBytes: 1 << 20},
			resource: ResourceStems,
			limit:    1,
			actual:   2,
		},
		{
			name:     "nodes",
			limits:   Limits{MaxEntries: 16, MaxStems: 16, MaxNodes: 2, MaxEdges: 64, MaxCommitments: 64, MaxFieldMappings: 64, MaxCommitmentTerms: 1024, MaxTemporaryBytes: 1 << 20},
			resource: ResourceNodes,
			limit:    2,
			actual:   3,
		},
		{
			name:     "edges",
			limits:   Limits{MaxEntries: 16, MaxStems: 16, MaxNodes: 64, MaxEdges: 1, MaxCommitments: 64, MaxFieldMappings: 64, MaxCommitmentTerms: 1024, MaxTemporaryBytes: 1 << 20},
			resource: ResourceEdges,
			limit:    1,
			actual:   2,
		},
		{
			name:     "commitments",
			limits:   Limits{MaxEntries: 16, MaxStems: 16, MaxNodes: 64, MaxEdges: 64, MaxCommitments: 6, MaxFieldMappings: 64, MaxCommitmentTerms: 1024, MaxTemporaryBytes: 1 << 20},
			resource: ResourceCommitments,
			limit:    6,
			actual:   7,
		},
		{
			name:     "complete bytes",
			limits:   Limits{MaxEntries: 16, MaxStems: 16, MaxNodes: 64, MaxEdges: 64, MaxCommitments: 64, MaxFieldMappings: 64, MaxCommitmentTerms: 1024, MaxTemporaryBytes: 279_711},
			resource: ResourceTemporaryBytes,
			limit:    279_711,
			actual:   279_712,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Build(
				context.Background(),
				entries,
				test.limits,
				testCommitmentLimits(),
			)
			assertResourceError(t, err, test.resource, test.limit, test.actual)
		})
	}
}

func TestBuildRejectsInvalidCommitmentLimits(t *testing.T) {
	t.Parallel()

	if _, err := Build(
		context.Background(),
		nil,
		testLimits(),
		backend.CommitmentLimits{},
	); err == nil {
		t.Fatal("invalid commitment limits were accepted")
	}
}

func TestBuildRejectsNilAndCancelledContexts(t *testing.T) {
	t.Parallel()

	var missingContext context.Context
	if _, err := Build(
		missingContext,
		nil,
		testLimits(),
		testCommitmentLimits(),
	); !errors.Is(err, errInvalidContext) {
		t.Fatalf("nil context error = %v, want %v", err, errInvalidContext)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Build(
		cancelled,
		nil,
		testLimits(),
		testCommitmentLimits(),
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled context error = %v, want cancellation", err)
	}

	entries := []Entry{
		{Key: testKey(3, 0), Value: testValue(3)},
		{Key: testKey(2, 0), Value: testValue(2)},
		{Key: testKey(1, 0), Value: testValue(1)},
	}
	if err := sortEntries(&cancelContext{cancelAt: 3}, entries); !errors.Is(err, context.Canceled) {
		t.Fatalf("sort cancellation error = %v, want cancellation", err)
	}
}

func TestTreeZeroAndCorruptValuesRejectUse(t *testing.T) {
	t.Parallel()

	var zero Tree
	if _, err := zero.Root(); !errors.Is(err, errInvalidTree) {
		t.Fatalf("zero root error = %v, want %v", err, errInvalidTree)
	}
	if _, err := zero.NodeCount(); !errors.Is(err, errInvalidTree) {
		t.Fatalf("zero count error = %v, want %v", err, errInvalidTree)
	}
	if _, err := zero.EdgeCount(); !errors.Is(err, errInvalidTree) {
		t.Fatalf("zero edge count error = %v, want %v", err, errInvalidTree)
	}
	corrupt := Tree{nodes: []node{{}}, root: 1, valid: true}
	if _, err := corrupt.Root(); !errors.Is(err, errInvalidTree) {
		t.Fatalf("corrupt root error = %v, want %v", err, errInvalidTree)
	}
	if _, err := corrupt.NodeCount(); !errors.Is(err, errInvalidTree) {
		t.Fatalf("corrupt count error = %v, want %v", err, errInvalidTree)
	}
	if _, err := corrupt.EdgeCount(); !errors.Is(err, errInvalidTree) {
		t.Fatalf("corrupt edge count error = %v, want %v", err, errInvalidTree)
	}
	invalidFlag := Tree{nodes: []node{{}}, valid: false}
	if _, err := invalidFlag.Root(); !errors.Is(err, errInvalidTree) {
		t.Fatalf("invalid flag root error = %v, want %v", err, errInvalidTree)
	}
	if _, err := invalidFlag.NodeCount(); !errors.Is(err, errInvalidTree) {
		t.Fatalf("invalid flag count error = %v, want %v", err, errInvalidTree)
	}
	if _, err := invalidFlag.EdgeCount(); !errors.Is(err, errInvalidTree) {
		t.Fatalf("invalid flag edge count error = %v, want %v", err, errInvalidTree)
	}
	emptyArena := Tree{valid: true}
	if _, err := emptyArena.Root(); !errors.Is(err, errInvalidTree) {
		t.Fatalf("empty arena root error = %v, want %v", err, errInvalidTree)
	}
	if _, err := emptyArena.NodeCount(); !errors.Is(err, errInvalidTree) {
		t.Fatalf("empty arena count error = %v, want %v", err, errInvalidTree)
	}
	if _, err := emptyArena.EdgeCount(); !errors.Is(err, errInvalidTree) {
		t.Fatalf("empty arena edge count error = %v, want %v", err, errInvalidTree)
	}
}

func TestConstructionInvariantHelpersFailClosed(t *testing.T) {
	t.Parallel()

	if _, err := finalizeTree([]node{{}}, nil, 0, 2, 0); !errors.Is(err, errInvalidTree) {
		t.Fatalf("node-count mismatch error = %v, want %v", err, errInvalidTree)
	}
	if _, err := finalizeTree([]node{{}}, nil, 1, 1, 0); !errors.Is(err, errInvalidTree) {
		t.Fatalf("root-index mismatch error = %v, want %v", err, errInvalidTree)
	}
	if _, err := finalizeTree([]node{{}}, []edge{{}}, 0, 1, 0); !errors.Is(err, errInvalidTree) {
		t.Fatalf("edge-count mismatch error = %v, want %v", err, errInvalidTree)
	}
	duplicate := []stemGroup{{}, {}}
	counts := topologyCounts{stems: 2, internalNodes: 1}
	if err := countInternalNodes(
		context.Background(),
		duplicate,
		30,
		&counts,
	); !errors.Is(err, errDuplicateKey) {
		t.Fatalf("duplicate stem topology error = %v, want %v", err, errDuplicateKey)
	}
}

func assertSameRoot(t testing.TB, left Tree, right Tree) {
	t.Helper()

	leftRoot, err := left.Root()
	if err != nil {
		t.Fatalf("get left root: %v", err)
	}
	rightRoot, err := right.Root()
	if err != nil {
		t.Fatalf("get right root: %v", err)
	}
	leftBytes, err := leftRoot.Bytes()
	if err != nil {
		t.Fatalf("encode left root: %v", err)
	}
	rightBytes, err := rightRoot.Bytes()
	if err != nil {
		t.Fatalf("encode right root: %v", err)
	}
	if leftBytes != rightBytes {
		t.Fatalf("roots differ: %x / %x", leftBytes, rightBytes)
	}
}

func assertResourceError(
	t testing.TB,
	err error,
	resource Resource,
	limit uint64,
	actual uint64,
) {
	t.Helper()

	var resourceErr *ResourceError
	if !errors.As(err, &resourceErr) {
		t.Fatalf("error = %v, want ResourceError", err)
	}
	if resourceErr.Resource != resource ||
		resourceErr.Limit != limit ||
		resourceErr.Actual != actual {
		t.Fatalf(
			"resource error = (%d, %d, %d), want (%d, %d, %d)",
			resourceErr.Resource,
			resourceErr.Limit,
			resourceErr.Actual,
			resource,
			limit,
			actual,
		)
	}
	if !errors.Is(err, errResource) || resourceErr.Error() == "" {
		t.Fatalf("resource error does not preserve sentinel: %v", err)
	}
}

type cancelContext struct {
	calls    int
	cancelAt int
}

func (*cancelContext) Deadline() (time.Time, bool) {
	return time.Time{}, false
}

func (*cancelContext) Done() <-chan struct{} {
	return nil
}

func (ctx *cancelContext) Err() error {
	ctx.calls++
	if ctx.calls >= ctx.cancelAt {
		return context.Canceled
	}

	return nil
}

func (*cancelContext) Value(any) any {
	return nil
}

func testKey(first, suffix byte) Key {
	var key Key
	key[0] = first
	key[31] = suffix

	return key
}

func testValue(seed byte) Value {
	var value Value
	for index := range value {
		value[index] = seed + byte(index)
	}

	return value
}

func testLimits() Limits {
	return Limits{
		MaxEntries:         16,
		MaxStems:           16,
		MaxNodes:           64,
		MaxEdges:           64,
		MaxCommitments:     64,
		MaxFieldMappings:   64,
		MaxCommitmentTerms: 1024,
		MaxTemporaryBytes:  1 << 20,
	}
}

func testCommitmentLimits() backend.CommitmentLimits {
	return backend.CommitmentLimits{
		MaxGeneratorDerivations: backend.VectorWidth,
		MaxScalarDecodes:        backend.VectorWidth,
		MaxMSMTerms:             backend.VectorWidth,
		MaxTemporaryBytes:       1 << 20,
	}
}
