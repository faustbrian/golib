package committedtree

import (
	"context"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/verkle-tree/internal/backend"
	"github.com/faustbrian/golib/pkg/verkle-tree/internal/leafvector"
)

func TestAggregateProverQueriesAreCanonicalAndComplete(t *testing.T) {
	t.Parallel()

	tree, err := Build(
		context.Background(),
		[]Entry{
			{Key: testKey(0x00, 0x00), Value: testValue(0x11)},
			{Key: testKey(0x00, 0x01), Value: testValue(0x22)},
			{Key: testKey(0x01, 0xff), Value: testValue(0x33)},
			{Key: testKey(0x01, 0x7f), Value: testValue(0x44)},
		},
		testLimits(),
		testCommitmentLimits(),
	)
	if err != nil {
		t.Fatalf("build tree: %v", err)
	}
	queries, err := tree.AggregateProverQueries(
		context.Background(),
		[]Key{
			testKey(0x01, 0xff),
			testKey(0x00, 0x02),
			testKey(0x02, 0x00),
			testKey(0x00, 0x00),
		},
		testAggregateProverQueryLimits(),
	)
	if err != nil {
		t.Fatalf("derive aggregate queries: %v", err)
	}

	type expectedQuery struct {
		path  []byte
		index uint8
	}
	want := []expectedQuery{
		{nil, 0}, {nil, 1}, {nil, 2},
		{[]byte{0}, 0}, {[]byte{0}, 1}, {[]byte{0}, 2},
		{[]byte{0, 2}, 0}, {[]byte{0, 2}, 1},
		{[]byte{0, 2}, 4}, {[]byte{0, 2}, 5},
		{[]byte{1}, 0}, {[]byte{1}, 1}, {[]byte{1}, 3},
		{[]byte{1, 3}, 254}, {[]byte{1, 3}, 255},
	}
	if len(queries) != len(want) {
		t.Fatalf("query count = %d, want %d", len(queries), len(want))
	}
	for index := range want {
		query := queries[index]
		if int(query.Length) != len(want[index].path) ||
			string(query.Path[:query.Length]) != string(want[index].path) ||
			query.Opening.Index != want[index].index {
			t.Fatalf(
				"query %d = (%x, %d), want (%x, %d)",
				index,
				query.Path[:query.Length],
				query.Opening.Index,
				want[index].path,
				want[index].index,
			)
		}
		expectedZero := query.Length == 0 && query.Opening.Index == 2 ||
			query.Length == 1 && query.Path[0] == 0 && query.Opening.Index == 1 ||
			query.Length == 2 && query.Path[0] == 0 &&
				(query.Opening.Index == 4 || query.Opening.Index == 5)
		if query.Opening.Vector[query.Opening.Index] == ([32]byte{}) && !expectedZero {
			// The missing root child and absent suffix are the expected zero
			// evaluations in this corpus; every other query opens a present value.
			t.Fatalf("query %d unexpectedly opens zero", index)
		}
	}
	rootVector := aggregateProverQueryVector(&queries[0])
	if aggregateProverQueryVector(&queries[1]) != rootVector ||
		aggregateProverQueryVector(&queries[2]) != rootVector {
		t.Fatal("openings for one committed vector do not share immutable storage")
	}
	if aggregateProverQueryVector(&queries[3]) == rootVector {
		t.Fatal("openings for distinct committed vectors share storage")
	}
	if aggregateProverQueryVector(&queries[6]) !=
		aggregateProverQueryVector(&queries[8]) {
		t.Fatal("separate keys in one stem half do not share vector storage")
	}
}

func aggregateProverQueryVector(query *AggregateProverQuery) *backend.Vector {
	return query.Opening.Vector
}

func TestAggregateProverQueriesRejectInvalidInputsAndResources(t *testing.T) {
	t.Parallel()

	validLimits := testAggregateProverQueryLimits()
	if err := validLimits.Validate(); err != nil {
		t.Fatalf("validate limits: %v", err)
	}
	if err := (AggregateProverQueryLimits{}).Validate(); !errors.Is(err, errInvalidAggregateProverQueryLimits) {
		t.Fatalf("invalid limits error = %v", err)
	}
	for name, mutate := range map[string]func(*AggregateProverQueryLimits){
		"keys zero":     func(limits *AggregateProverQueryLimits) { limits.MaxKeys = 0 },
		"keys overflow": func(limits *AggregateProverQueryLimits) { limits.MaxKeys = maxSupportedCount + 1 },
		"queries zero":  func(limits *AggregateProverQueryLimits) { limits.MaxQueries = 0 },
		"queries overflow": func(limits *AggregateProverQueryLimits) {
			limits.MaxQueries = maxAggregateProverQueries + 1
		},
		"node reads zero": func(limits *AggregateProverQueryLimits) { limits.MaxNodeReads = 0 },
		"temporary zero":  func(limits *AggregateProverQueryLimits) { limits.MaxTemporaryBytes = 0 },
	} {
		limits := validLimits
		mutate(&limits)
		if err := limits.Validate(); !errors.Is(err, errInvalidAggregateProverQueryLimits) {
			t.Fatalf("%s error = %v", name, err)
		}
	}
	boundary := validLimits
	boundary.MaxKeys = maxSupportedCount
	boundary.MaxQueries = maxAggregateProverQueries
	if err := boundary.Validate(); err != nil {
		t.Fatalf("maximum limits: %v", err)
	}
	if _, err := (Tree{}).AggregateProverQueries(
		context.Background(),
		[]Key{testKey(0, 0)},
		validLimits,
	); !errors.Is(err, errInvalidTree) {
		t.Fatalf("invalid tree error = %v", err)
	}
	tree := testAggregateQueryTree(t)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	invalidFlag := tree
	invalidFlag.valid = false
	if _, err := invalidFlag.AggregateProverQueries(
		cancelled,
		[]Key{testKey(0, 0)},
		validLimits,
	); !errors.Is(err, errInvalidTree) {
		t.Fatalf("invalid flag precedence error = %v", err)
	}
	emptyNodes := tree
	emptyNodes.nodes = nil
	if _, err := emptyNodes.AggregateProverQueries(
		cancelled,
		[]Key{testKey(0, 0)},
		validLimits,
	); !errors.Is(err, errInvalidTree) {
		t.Fatalf("empty nodes precedence error = %v", err)
	}
	var missingContext context.Context
	corruptRoot := cloneAggregateQueryTree(tree)
	corruptRoot.root = uint32(len(corruptRoot.nodes))
	if _, err := corruptRoot.AggregateProverQueries(
		cancelled,
		[]Key{testKey(0, 0)},
		validLimits,
	); !errors.Is(err, errInvalidTree) {
		t.Fatalf("invalid root error = %v", err)
	}
	if _, err := tree.AggregateProverQueries(missingContext, []Key{testKey(0, 0)}, validLimits); !errors.Is(err, errInvalidContext) {
		t.Fatalf("nil context error = %v", err)
	}
	if _, err := tree.AggregateProverQueries(
		context.Background(),
		[]Key{testKey(0, 0)},
		AggregateProverQueryLimits{},
	); !errors.Is(err, errInvalidAggregateProverQueryLimits) {
		t.Fatalf("invalid query limits error = %v", err)
	}
	if _, err := tree.AggregateProverQueries(context.Background(), nil, validLimits); !errors.Is(err, errInvalidTree) {
		t.Fatalf("empty keys error = %v", err)
	}

	keys := []Key{testKey(0, 0), testKey(1, 0)}
	keyLimit := validLimits
	keyLimit.MaxKeys = 1
	_, err := tree.AggregateProverQueries(context.Background(), keys, keyLimit)
	assertAggregateProverQueryResourceError(
		t,
		err,
		AggregateProverQueryResourceKeys,
		1,
		2,
	)
	temporary := validLimits
	temporary.MaxTemporaryBytes = 1
	_, err = tree.AggregateProverQueries(context.Background(), []Key{keys[0]}, temporary)
	assertAggregateProverQueryResourceError(
		t,
		err,
		AggregateProverQueryResourceTemporaryBytes,
		1,
		128,
	)
	temporary.MaxTemporaryBytes = 128
	_, err = tree.AggregateProverQueries(context.Background(), []Key{keys[0]}, temporary)
	assertAggregateProverQueryResourceError(
		t,
		err,
		AggregateProverQueryResourceTemporaryBytes,
		128,
		304_832,
	)
	duplicate := []Key{keys[0], keys[0]}
	if _, err := tree.AggregateProverQueries(
		context.Background(),
		duplicate,
		validLimits,
	); !errors.Is(err, errDuplicateKey) {
		t.Fatalf("duplicate key error = %v", err)
	}
	queryLimit := validLimits
	queryLimit.MaxQueries = 1
	_, err = tree.AggregateProverQueries(context.Background(), []Key{keys[0]}, queryLimit)
	assertAggregateProverQueryResourceError(
		t,
		err,
		AggregateProverQueryResourceQueries,
		1,
		2,
	)
	nodeLimit := validLimits
	nodeLimit.MaxNodeReads = 1
	_, err = tree.AggregateProverQueries(context.Background(), []Key{keys[0]}, nodeLimit)
	assertAggregateProverQueryResourceError(
		t,
		err,
		AggregateProverQueryResourceNodeReads,
		1,
		2,
	)
	if err := checkAggregateProverQueryResource(
		AggregateProverQueryResourceKeys,
		1,
		1,
	); err != nil {
		t.Fatalf("exact resource limit: %v", err)
	}
}

func TestAggregateProverQueriesBoundScratchByDistinctStems(t *testing.T) {
	t.Parallel()

	const keyCount = 64
	entries := make([]Entry, keyCount)
	keys := make([]Key, keyCount)
	for index := range entries {
		keys[index] = testKey(0, byte(index))
		entries[index] = Entry{Key: keys[index], Value: testValue(byte(index + 1))}
	}
	buildLimits := testLimits()
	buildLimits.MaxEntries = keyCount
	tree, err := Build(
		context.Background(),
		entries,
		buildLimits,
		testCommitmentLimits(),
	)
	if err != nil {
		t.Fatalf("build shared-stem tree: %v", err)
	}

	const queryCapacity = uint64(31 + 2 + 1 + 2*keyCount)
	temporaryBytes := uint64(keyCount)*2*aggregateQueryKeyWorkingBytes +
		queryCapacity*(aggregateQueryResultWorkingBytes()+16)
	limits := testAggregateProverQueryLimits()
	limits.MaxTemporaryBytes = temporaryBytes
	queries, err := tree.AggregateProverQueries(context.Background(), keys, limits)
	if err != nil {
		t.Fatalf("derive shared-stem queries: %v", err)
	}
	if len(queries) >= int(queryCapacity) {
		t.Fatalf("query count = %d, capacity bound %d", len(queries), queryCapacity)
	}
	if cap(queries) != int(queryCapacity) {
		t.Fatalf("query capacity = %d, want %d", cap(queries), queryCapacity)
	}

	limits.MaxTemporaryBytes--
	_, err = tree.AggregateProverQueries(context.Background(), keys, limits)
	assertAggregateProverQueryResourceError(
		t,
		err,
		AggregateProverQueryResourceTemporaryBytes,
		temporaryBytes-1,
		temporaryBytes,
	)
}

func TestGrowAggregateProverQueriesCapsAllocatedCapacity(t *testing.T) {
	t.Parallel()

	queries := make([]AggregateProverQuery, 2)
	same, err := growAggregateProverQueries(queries, 5, cap(queries))
	if err != nil {
		t.Fatalf("retain exact-capacity queries: %v", err)
	}
	if &same[0] != &queries[0] {
		t.Fatal("exact-capacity growth replaced query storage")
	}
	grown, err := growAggregateProverQueries(queries, 5, 3)
	if err != nil {
		t.Fatalf("grow queries: %v", err)
	}
	if len(grown) != len(queries) || cap(grown) != 4 {
		t.Fatalf("grown query shape = %d/%d, want 2/4", len(grown), cap(grown))
	}
	grown = grown[:cap(grown)]
	clamped, err := growAggregateProverQueries(grown, 5, 5)
	if err != nil {
		t.Fatalf("clamp query growth: %v", err)
	}
	if len(clamped) != len(grown) || cap(clamped) != 5 {
		t.Fatalf("clamped query shape = %d/%d, want 4/5", len(clamped), cap(clamped))
	}
	if _, err := growAggregateProverQueries(clamped[:cap(clamped)], 5, 6); !errors.Is(err, errInvalidTree) {
		t.Fatalf("growth beyond bound error = %v", err)
	}
}

func TestAggregateProverQueriesPreserveCancellation(t *testing.T) {
	t.Parallel()

	tree := testAggregateQueryTree(t)
	keys := []Key{testKey(1, 0), testKey(0, 0)}
	observed := false
	for cancelAt := 1; cancelAt < 1_000; cancelAt++ {
		_, err := tree.AggregateProverQueries(
			&cancelContext{cancelAt: cancelAt},
			keys,
			testAggregateProverQueryLimits(),
		)
		if err == nil {
			break
		}
		observed = true
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation at %d = %v", cancelAt, err)
		}
	}
	if !observed {
		t.Fatal("no cancellation boundary was exercised")
	}
}

func TestAggregateProverQueriesTraverseInternalAndDifferentStems(t *testing.T) {
	t.Parallel()

	left := testKey(0, 0)
	left[1] = 1
	right := testKey(0, 0)
	right[1] = 2
	tree, err := Build(
		context.Background(),
		[]Entry{
			{Key: left, Value: testValue(1)},
			{Key: right, Value: testValue(2)},
		},
		testLimits(),
		testCommitmentLimits(),
	)
	if err != nil {
		t.Fatalf("build collision tree: %v", err)
	}
	if _, err := tree.AggregateProverQueries(
		context.Background(),
		[]Key{left},
		testAggregateProverQueryLimits(),
	); err != nil {
		t.Fatalf("internal traversal: %v", err)
	}
	different := left
	different[2] = 9
	if _, err := tree.AggregateProverQueries(
		context.Background(),
		[]Key{different},
		testAggregateProverQueryLimits(),
	); err != nil {
		t.Fatalf("different stem traversal: %v", err)
	}

	deepLeft := testKey(0, 0)
	deepRight := testKey(0, 1)
	deepRight[30] = 1
	deepTree, err := Build(
		context.Background(),
		[]Entry{
			{Key: deepLeft, Value: testValue(1)},
			{Key: deepRight, Value: testValue(2)},
		},
		testLimits(),
		testCommitmentLimits(),
	)
	if err != nil {
		t.Fatalf("build depth-31 tree: %v", err)
	}
	if _, err := deepTree.AggregateProverQueries(
		context.Background(),
		[]Key{deepLeft, deepRight},
		testAggregateProverQueryLimits(),
	); err != nil {
		t.Fatalf("depth-31 stem traversal: %v", err)
	}
}

func TestAggregateQueryCollectorRejectsCorruptTrees(t *testing.T) {
	t.Parallel()

	base := testAggregateQueryTree(t)
	root := base.root
	firstChild := base.edges[base.nodes[root].firstEdge].child
	tests := []struct {
		name   string
		mutate func(*Tree)
	}{
		{
			name: "current node out of range",
			mutate: func(tree *Tree) {
				tree.root = uint32(len(tree.nodes))
			},
		},
		{
			name: "root kind",
			mutate: func(tree *Tree) {
				tree.nodes[root].kind = nodeStem
			},
		},
		{
			name: "root depth",
			mutate: func(tree *Tree) {
				tree.nodes[root].depth = 31
			},
		},
		{
			name: "edge range",
			mutate: func(tree *Tree) {
				tree.nodes[root].firstEdge = uint32(len(tree.edges))
				tree.nodes[root].edgeCount = 1
			},
		},
		{
			name: "edge order",
			mutate: func(tree *Tree) {
				first := tree.nodes[root].firstEdge
				tree.edges[first+1].index = tree.edges[first].index
			},
		},
		{
			name: "edge child",
			mutate: func(tree *Tree) {
				tree.edges[tree.nodes[root].firstEdge].child = uint32(len(tree.nodes))
			},
		},
		{
			name: "child depth",
			mutate: func(tree *Tree) {
				tree.nodes[firstChild].depth++
			},
		},
		{
			name: "child commitment",
			mutate: func(tree *Tree) {
				tree.nodes[firstChild].commitment = backend.VectorCommitment{}
			},
		},
		{
			name: "child kind",
			mutate: func(tree *Tree) {
				tree.nodes[firstChild].kind = 0
			},
		},
		{
			name: "empty stem entries",
			mutate: func(tree *Tree) {
				tree.nodes[firstChild].entryCount = 0
			},
		},
		{
			name: "stem entry range",
			mutate: func(tree *Tree) {
				tree.nodes[firstChild].entryStart = uint32(len(tree.entries))
				tree.nodes[firstChild].entryCount = 1
			},
		},
		{
			name: "stem entry mismatch",
			mutate: func(tree *Tree) {
				tree.entries[tree.nodes[firstChild].entryStart].Key[1]++
			},
		},
		{
			name: "c1 commitment",
			mutate: func(tree *Tree) {
				tree.nodes[firstChild].c1 = backend.VectorCommitment{}
			},
		},
		{
			name: "c2 commitment",
			mutate: func(tree *Tree) {
				tree.nodes[firstChild].c2 = backend.VectorCommitment{}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tree := cloneAggregateQueryTree(base)
			test.mutate(&tree)
			if _, err := tree.AggregateProverQueries(
				context.Background(),
				[]Key{testKey(0, 0)},
				testAggregateProverQueryLimits(),
			); !errors.Is(err, errInvalidTree) &&
				!errors.Is(err, errInvalidAggregateProverQuery) {
				t.Fatalf("corrupt tree error = %v", err)
			}
		})
	}
}

func TestAggregateQueryHelpersRejectInvalidState(t *testing.T) {
	t.Parallel()

	tree := testAggregateQueryTree(t)
	collector := newAggregateQueryTestCollector(&tree)
	root := tree.nodes[tree.root]
	path := aggregateQueryPath{}
	outOfRange := cloneAggregateQueryTree(tree)
	outOfRangeCollector := newAggregateQueryTestCollector(&outOfRange)
	outOfRangeCollector.tree.root = uint32(len(outOfRange.nodes))
	if err := outOfRangeCollector.collect(testKey(0, 0)); !errors.Is(err, errInvalidTree) {
		t.Fatalf("collector current-node error = %v", err)
	}
	collector.vectorByID[path] = aggregateQueryVector{nodeIndex: tree.root + 1}
	if _, err := collector.internalVector(path, tree.root, root); !errors.Is(err, errInvalidTree) {
		t.Fatalf("cached vector mismatch error = %v", err)
	}

	stemIndex := tree.edges[root.firstEdge].child
	stem := tree.nodes[stemIndex]
	stem.depth = 0
	stemCollector := newAggregateQueryTestCollector(&tree)
	if _, _, _, err := stemCollector.stemVectors(
		aggregateQueryPath{},
		stemIndex,
		stem,
	); err != nil {
		t.Fatalf("direct stem vectors: %v", err)
	}
	stemPath := aggregateQueryPath{}
	c1Path := aggregateStemHalfPath(stemPath, leafvector.C1HashIndex)
	c2Path := aggregateStemHalfPath(stemPath, leafvector.C2HashIndex)
	cachedStem := stemCollector.vectorByID[stemPath]
	cachedC1 := stemCollector.vectorByID[c1Path]
	cachedC2 := stemCollector.vectorByID[c2Path]
	cacheCorruptions := map[string]func(map[aggregateQueryPath]aggregateQueryVector){
		"stem node": func(cache map[aggregateQueryPath]aggregateQueryVector) {
			corrupt := cache[stemPath]
			corrupt.nodeIndex++
			cache[stemPath] = corrupt
		},
		"c1 node": func(cache map[aggregateQueryPath]aggregateQueryVector) {
			corrupt := cache[c1Path]
			corrupt.nodeIndex++
			cache[c1Path] = corrupt
		},
		"missing c2": func(cache map[aggregateQueryPath]aggregateQueryVector) {
			delete(cache, c2Path)
		},
	}
	for name, corrupt := range cacheCorruptions {
		cachedCollector := newAggregateQueryTestCollector(&tree)
		cachedCollector.vectorByID[stemPath] = cachedStem
		cachedCollector.vectorByID[c1Path] = cachedC1
		cachedCollector.vectorByID[c2Path] = cachedC2
		corrupt(cachedCollector.vectorByID)
		if _, _, _, err := cachedCollector.stemVectors(
			stemPath,
			stemIndex,
			stem,
		); !errors.Is(err, errInvalidTree) {
			t.Fatalf("%s cached stem vectors error = %v", name, err)
		}
	}
	if err := collector.collectStem(testKey(0, 0), stemIndex, stem); !errors.Is(err, errInvalidTree) {
		t.Fatalf("invalid stem depth error = %v", err)
	}

	badRange := root
	badRange.firstEdge = uint32(len(tree.edges))
	badRange.edgeCount = 1
	if _, _, err := collector.findChild(badRange, 0); !errors.Is(err, errInvalidTree) {
		t.Fatalf("find child range error = %v", err)
	}
	badChildTree := cloneAggregateQueryTree(tree)
	badChildTree.edges[root.firstEdge].child = uint32(len(tree.nodes))
	badChildCollector := newAggregateQueryTestCollector(&badChildTree)
	if _, _, err := badChildCollector.findChild(root, 0); !errors.Is(err, errInvalidTree) {
		t.Fatalf("find child target error = %v", err)
	}
	badRangeTree := cloneAggregateQueryTree(tree)
	badRangeTree.nodes[badRangeTree.root] = badRange
	badRangeCollector := newAggregateQueryTestCollector(&badRangeTree)
	badRangeCollector.vectorByID[path] = aggregateQueryVector{
		commitment: root.commitment,
		vector:     new(backend.Vector),
		nodeIndex:  badRangeTree.root,
	}
	if err := badRangeCollector.collect(testKey(0, 0)); !errors.Is(err, errInvalidTree) {
		t.Fatalf("collector find-child error = %v", err)
	}
	if child, found, err := collector.findChild(root, 0xff); err != nil || found || child != 0 {
		t.Fatalf("absent child = %d/%t, error = %v", child, found, err)
	}
	laterOnly := root
	laterOnly.firstEdge++
	laterOnly.edgeCount = 1
	if child, found, err := collector.findChild(laterOnly, 0); err != nil || found || child != 0 {
		t.Fatalf("early absent child = %d/%t, error = %v", child, found, err)
	}

	exhausted := newAggregateQueryTestCollector(&tree)
	exhausted.nodeReads = exhausted.limits.MaxNodeReads
	if _, err := exhausted.internalVector(path, tree.root, root); err == nil {
		t.Fatal("initial internal-vector node budget was accepted")
	}
	if _, _, _, err := exhausted.stemVectors(
		aggregateQueryPath{path: [32]byte{0}, length: 1},
		stemIndex,
		tree.nodes[stemIndex],
	); err == nil {
		t.Fatal("initial stem-vector node budget was accepted")
	}

	if err := collector.append(
		path,
		aggregateQueryVector{commitment: root.commitment},
		9,
	); !errors.Is(err, errInvalidTree) {
		t.Fatalf("missing query vector error = %v", err)
	}
	vector := aggregateQueryVector{
		commitment: root.commitment,
		vector:     new(backend.Vector),
	}
	if err := collector.append(path, vector, 9); err != nil {
		t.Fatalf("append first query: %v", err)
	}
	bounded := newAggregateQueryTestCollector(&tree)
	bounded.queryCapacity = 0
	if err := bounded.append(path, vector, 9); !errors.Is(err, errInvalidTree) {
		t.Fatalf("query growth beyond derived bound error = %v", err)
	}
	differentCommitment := vector
	differentCommitment.commitment = tree.nodes[stemIndex].commitment
	if err := collector.append(path, differentCommitment, 9); !errors.Is(err, errInvalidTree) {
		t.Fatalf("conflicting path commitment error = %v", err)
	}
	invalidIncoming := vector
	invalidIncoming.commitment = backend.VectorCommitment{}
	if err := collector.append(path, invalidIncoming, 9); !errors.Is(err, errInvalidTree) {
		t.Fatalf("invalid incoming commitment error = %v", err)
	}
	invalidFirst := newAggregateQueryTestCollector(&tree)
	if err := invalidFirst.append(path, invalidIncoming, 9); !errors.Is(err, errInvalidTree) {
		t.Fatalf("initial invalid commitment error = %v", err)
	}
	corruptExisting := newAggregateQueryTestCollector(&tree)
	corruptExisting.queries = append(corruptExisting.queries, AggregateProverQuery{
		Opening: backend.AggregateProverQuery{Commitment: backend.VectorCommitment{}},
	})
	corruptExisting.queryByID[aggregateQueryIdentity{path: path, index: 9}] = 0
	if err := corruptExisting.append(path, vector, 9); !errors.Is(err, errInvalidTree) {
		t.Fatalf("corrupt existing commitment error = %v", err)
	}
	conflictingVector := *vector.vector
	conflictingVector[0][0] = 1
	conflicting := vector
	conflicting.vector = &conflictingVector
	if err := collector.append(path, conflicting, 9); !errors.Is(err, errInvalidTree) {
		t.Fatalf("conflicting path query error = %v", err)
	}

	var missingContext context.Context
	if err := sortAggregateProverQueries(missingContext, nil); !errors.Is(err, errInvalidContext) {
		t.Fatalf("singleton sort context error = %v", err)
	}
	validVector := new(backend.Vector)
	valid := AggregateProverQuery{
		Opening: backend.AggregateProverQuery{
			Commitment: root.commitment,
			Vector:     validVector,
			Index:      7,
		},
	}
	if _, err := consolidateAggregateProverQueries(
		missingContext,
		[]AggregateProverQuery{valid},
	); !errors.Is(err, errInvalidContext) {
		t.Fatalf("consolidation context error = %v", err)
	}
	invalid := valid
	invalid.Opening.Commitment = backend.VectorCommitment{}
	if _, err := consolidateAggregateProverQueries(
		context.Background(),
		[]AggregateProverQuery{invalid},
	); !errors.Is(err, errInvalidAggregateProverQuery) {
		t.Fatalf("invalid consolidation commitment error = %v", err)
	}
	missingVector := valid
	missingVector.Opening.Vector = nil
	if _, err := consolidateAggregateProverQueries(
		context.Background(),
		[]AggregateProverQuery{missingVector},
	); !errors.Is(err, errInvalidAggregateProverQuery) {
		t.Fatalf("missing consolidation vector error = %v", err)
	}
	conflictVector := *valid.Opening.Vector
	conflict := valid
	conflict.Opening.Vector = &conflictVector
	conflict.Opening.Vector[0][0] = 1
	if _, err := consolidateAggregateProverQueries(
		context.Background(),
		[]AggregateProverQuery{valid, conflict},
	); !errors.Is(err, errInvalidAggregateProverQuery) {
		t.Fatalf("conflicting consolidation error = %v", err)
	}
	if got, err := consolidateAggregateProverQueries(
		context.Background(),
		[]AggregateProverQuery{valid, valid},
	); err != nil || len(got) != 1 {
		t.Fatalf("consolidated duplicate count = %d, error = %v", len(got), err)
	}
	unique := valid
	unique.Opening.Index++
	if got, err := consolidateAggregateProverQueries(
		context.Background(),
		[]AggregateProverQuery{valid, valid, unique},
	); err != nil || len(got) != 2 || got[1].Opening.Index != unique.Opening.Index {
		t.Fatalf("duplicate followed by unique = %#v, error = %v", got, err)
	}
}

func TestAggregateQuerySortHelpers(t *testing.T) {
	t.Parallel()

	keys := []Key{testKey(2, 0), testKey(0, 0), testKey(1, 0), testKey(1, 0)}
	if err := sortAggregateQueryKeys(context.Background(), keys); err != nil {
		t.Fatalf("sort keys: %v", err)
	}
	for index, first := range []byte{0, 1, 1, 2} {
		if keys[index][0] != first {
			t.Fatalf("key %d first byte = %d, want %d", index, keys[index][0], first)
		}
	}
	pair := []Key{testKey(1, 0), testKey(0, 0)}
	if err := sortAggregateQueryKeys(context.Background(), pair); err != nil {
		t.Fatalf("sort key pair: %v", err)
	}
	if pair[0][0] != 0 || pair[1][0] != 1 {
		t.Fatalf("sorted key pair = (%d, %d)", pair[0][0], pair[1][0])
	}

	queries := []AggregateProverQuery{
		{Path: [32]byte{1}, Length: 1, Opening: backend.AggregateProverQuery{Index: 2}},
		{Path: [32]byte{0}, Length: 1, Opening: backend.AggregateProverQuery{Index: 3}},
		{Path: [32]byte{1}, Length: 1, Opening: backend.AggregateProverQuery{Index: 1}},
		{Path: [32]byte{0}, Length: 1, Opening: backend.AggregateProverQuery{Index: 2}},
	}
	if err := sortAggregateProverQueries(context.Background(), queries); err != nil {
		t.Fatalf("sort queries: %v", err)
	}
	want := [][2]uint8{{0, 2}, {0, 3}, {1, 1}, {1, 2}}
	for index := range want {
		if queries[index].Path[0] != want[index][0] ||
			queries[index].Opening.Index != want[index][1] {
			t.Fatalf(
				"query %d = (%d, %d), want (%d, %d)",
				index,
				queries[index].Path[0],
				queries[index].Opening.Index,
				want[index][0],
				want[index][1],
			)
		}
	}
	queryPair := []AggregateProverQuery{queries[3], queries[0]}
	if err := sortAggregateProverQueries(context.Background(), queryPair); err != nil {
		t.Fatalf("sort query pair: %v", err)
	}
	if queryPair[0].Path[0] != 0 || queryPair[1].Path[0] != 1 {
		t.Fatalf("sorted query pair = (%d, %d)", queryPair[0].Path[0], queryPair[1].Path[0])
	}
	stableLeftVector := &backend.Vector{{1}}
	stableLeft := AggregateProverQuery{
		Path:   [32]byte{1},
		Length: 1,
		Opening: backend.AggregateProverQuery{
			Index:  2,
			Vector: stableLeftVector,
		},
	}
	stableRight := stableLeft
	stableRightVector := *stableLeft.Opening.Vector
	stableRight.Opening.Vector = &stableRightVector
	stableRight.Opening.Vector[0][0] = 2
	stable := []AggregateProverQuery{stableLeft, stableRight}
	if err := sortAggregateProverQueries(context.Background(), stable); err != nil {
		t.Fatalf("stable query sort: %v", err)
	}
	if stable[0].Opening.Vector[0][0] != 1 || stable[1].Opening.Vector[0][0] != 2 {
		t.Fatalf(
			"stable query markers = (%d, %d)",
			stable[0].Opening.Vector[0][0],
			stable[1].Opening.Vector[0][0],
		)
	}
}

func TestBuildPreparedRejectsFinalizedCountMismatch(t *testing.T) {
	t.Parallel()

	plan, err := prepareBuild(
		context.Background(),
		[]Entry{{Key: testKey(0, 0), Value: testValue(1)}},
		testLimits(),
	)
	if err != nil {
		t.Fatalf("prepare build: %v", err)
	}
	engine, err := backend.NewCommitmentEngine(context.Background(), testCommitmentLimits())
	if err != nil {
		t.Fatalf("new commitment engine: %v", err)
	}
	plan.nodeCount++
	if _, err := buildPrepared(context.Background(), plan, engine); !errors.Is(err, errInvalidTree) {
		t.Fatalf("finalized count error = %v", err)
	}
}

func newAggregateQueryTestCollector(tree *Tree) *aggregateQueryCollector {
	limits := testAggregateProverQueryLimits()

	return &aggregateQueryCollector{
		ctx:           context.Background(),
		tree:          tree,
		limits:        limits,
		queryCapacity: int(limits.MaxQueries),
		queryByID:     make(map[aggregateQueryIdentity]int),
		vectorByID:    make(map[aggregateQueryPath]aggregateQueryVector),
	}
}

func testAggregateQueryTree(t testing.TB) Tree {
	t.Helper()

	tree, err := Build(
		context.Background(),
		[]Entry{
			{Key: testKey(0, 0), Value: testValue(1)},
			{Key: testKey(1, 0), Value: testValue(2)},
		},
		testLimits(),
		testCommitmentLimits(),
	)
	if err != nil {
		t.Fatalf("build aggregate-query tree: %v", err)
	}

	return tree
}

func cloneAggregateQueryTree(tree Tree) Tree {
	tree.entries = append([]Entry(nil), tree.entries...)
	tree.nodes = append([]node(nil), tree.nodes...)
	tree.edges = append([]edge(nil), tree.edges...)

	return tree
}

func assertAggregateProverQueryResourceError(
	t testing.TB,
	err error,
	resource AggregateProverQueryResource,
	limit uint64,
	actual uint64,
) {
	t.Helper()

	var resourceErr *AggregateProverQueryResourceError
	if !errors.As(err, &resourceErr) ||
		resourceErr.Resource != resource ||
		resourceErr.Limit != limit ||
		resourceErr.Actual != actual ||
		!errors.Is(err, errAggregateProverQueryResource) ||
		resourceErr.Unwrap() != errAggregateProverQueryResource ||
		resourceErr.Error() == "" {
		t.Fatalf("resource error = %v", err)
	}
}

func testAggregateProverQueryLimits() AggregateProverQueryLimits {
	return AggregateProverQueryLimits{
		MaxKeys:           64,
		MaxQueries:        1024,
		MaxNodeReads:      1024,
		MaxTemporaryBytes: 16 << 20,
	}
}
