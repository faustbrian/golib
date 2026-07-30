package committedtree

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

func TestProofPathExtractsCanonicalMembershipAndAbsenceMaterial(
	t *testing.T,
) {
	t.Parallel()

	left := testKey(7, 1)
	left[1] = 1
	right := testKey(7, 128)
	right[1] = 2
	isolated := testKey(9, 1)
	isolated[1] = 4
	tree, err := Build(
		context.Background(),
		[]Entry{
			{Key: left, Value: testValue(1)},
			{Key: right, Value: testValue(2)},
			{Key: isolated, Value: testValue(3)},
		},
		testLimits(),
		testCommitmentLimits(),
	)
	if err != nil {
		t.Fatalf("build proof tree: %v", err)
	}

	tests := map[string]struct {
		key          Key
		kind         ProofPathKind
		depth        uint8
		existingStem [31]byte
		paths        [][]byte
		identityAt   int
	}{
		"present c1": {
			key:        left,
			kind:       ProofPathPresent,
			depth:      2,
			paths:      [][]byte{{7}, {7, 1}, {7, 1, 2}},
			identityAt: -1,
		},
		"absent suffix in present stem": {
			key: func() Key {
				key := left
				key[31] = 42
				return key
			}(),
			kind:       ProofPathPresent,
			depth:      2,
			paths:      [][]byte{{7}, {7, 1}, {7, 1, 2}},
			identityAt: -1,
		},
		"present c2": {
			key:        right,
			kind:       ProofPathPresent,
			depth:      2,
			paths:      [][]byte{{7}, {7, 2}, {7, 2, 3}},
			identityAt: -1,
		},
		"absent suffix in empty half": {
			key: func() Key {
				key := isolated
				key[31] = 200
				return key
			}(),
			kind:       ProofPathPresent,
			depth:      1,
			paths:      [][]byte{{9}, {9, 3}},
			identityAt: 1,
		},
		"missing child": {
			key:        testKey(8, 1),
			kind:       ProofPathMissing,
			depth:      1,
			identityAt: -1,
		},
		"missing child after final edge": {
			key:        testKey(255, 1),
			kind:       ProofPathMissing,
			depth:      1,
			identityAt: -1,
		},
		"different stem": {
			key: func() Key {
				key := isolated
				key[1] = 5
				return key
			}(),
			kind:         ProofPathDifferent,
			depth:        1,
			existingStem: [31]byte(isolated[:31]),
			paths:        [][]byte{{9}},
			identityAt:   -1,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			result, pathErr := tree.ProofPath(
				context.Background(),
				test.key,
				testProofPathLimits(),
			)
			if pathErr != nil {
				t.Fatalf("extract proof path: %v", pathErr)
			}
			if result.Kind != test.kind ||
				result.Depth != test.depth ||
				result.ExistingStem != test.existingStem {
				t.Fatalf("proof path metadata = %#v", result)
			}
			if len(result.Commitments) != len(test.paths) {
				t.Fatalf(
					"commitment count = %d, want %d",
					len(result.Commitments),
					len(test.paths),
				)
			}
			for index := range test.paths {
				path := result.Commitments[index]
				if !bytes.Equal(
					path.Path[:path.Length],
					test.paths[index],
				) {
					t.Fatalf(
						"path %d = %x, want %x",
						index,
						path.Path[:path.Length],
						test.paths[index],
					)
				}
				identity, identityErr := path.Commitment.IsIdentity()
				if identityErr != nil || identity != (index == test.identityAt) {
					t.Fatalf(
						"commitment %d identity = %t, error %v",
						index,
						identity,
						identityErr,
					)
				}
			}
		})
	}
}

func TestProofPathEnforcesLimitsAndCallerOwnership(t *testing.T) {
	t.Parallel()

	key := testKey(7, 1)
	key[1] = 1
	other := testKey(7, 2)
	other[1] = 2
	tree, err := Build(
		context.Background(),
		[]Entry{
			{Key: key, Value: testValue(1)},
			{Key: other, Value: testValue(2)},
		},
		testLimits(),
		testCommitmentLimits(),
	)
	if err != nil {
		t.Fatalf("build proof tree: %v", err)
	}

	tests := map[string]struct {
		change   func(*ProofPathLimits)
		resource ProofPathResource
		limit    uint64
		actual   uint64
	}{
		"node reads": {
			change: func(limits *ProofPathLimits) {
				limits.MaxNodeReads = 2
			},
			resource: ProofPathResourceNodeReads,
			limit:    2,
			actual:   3,
		},
		"commitments": {
			change: func(limits *ProofPathLimits) {
				limits.MaxCommitments = 2
			},
			resource: ProofPathResourceCommitments,
			limit:    2,
			actual:   3,
		},
		"path bytes": {
			change: func(limits *ProofPathLimits) {
				limits.MaxPathBytes = 5
			},
			resource: ProofPathResourcePathBytes,
			limit:    5,
			actual:   6,
		},
		"temporary bytes": {
			change: func(limits *ProofPathLimits) {
				limits.MaxTemporaryBytes = 511
			},
			resource: ProofPathResourceTemporaryBytes,
			limit:    511,
			actual:   512,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			limits := testProofPathLimits()
			test.change(&limits)
			_, pathErr := tree.ProofPath(
				context.Background(),
				key,
				limits,
			)
			if !errors.Is(pathErr, errProofPathResource) {
				t.Fatalf("resource error = %v", pathErr)
			}
			var resourceErr *ProofPathResourceError
			if !errors.As(pathErr, &resourceErr) ||
				resourceErr.Resource != test.resource ||
				resourceErr.Limit != test.limit ||
				resourceErr.Actual != test.actual ||
				resourceErr.Unwrap() != errProofPathResource ||
				resourceErr.Error() == "" {
				t.Fatalf("resource detail = %#v", resourceErr)
			}
		})
	}

	first, err := tree.ProofPath(
		context.Background(),
		key,
		testProofPathLimits(),
	)
	if err != nil {
		t.Fatalf("extract first path: %v", err)
	}
	first.Commitments[0] = ProofPathCommitment{}
	second, err := tree.ProofPath(
		context.Background(),
		key,
		testProofPathLimits(),
	)
	if err != nil {
		t.Fatalf("extract second path: %v", err)
	}
	if second.Commitments[0].Length == 0 {
		t.Fatal("caller mutation changed immutable tree proof material")
	}
}

func TestProofPathRejectsInvalidUseAndHonorsCancellation(t *testing.T) {
	t.Parallel()

	var zero Tree
	if _, err := zero.ProofPath(
		context.Background(),
		Key{},
		testProofPathLimits(),
	); !errors.Is(err, errInvalidTree) {
		t.Fatalf("zero tree error = %v", err)
	}

	tree, err := Build(
		context.Background(),
		nil,
		testLimits(),
		testCommitmentLimits(),
	)
	if err != nil {
		t.Fatalf("build empty tree: %v", err)
	}
	var nilContext context.Context
	if _, err := tree.ProofPath(
		nilContext,
		Key{},
		testProofPathLimits(),
	); !errors.Is(err, errInvalidContext) {
		t.Fatalf("nil context error = %v", err)
	}
	if _, err := tree.ProofPath(
		context.Background(),
		Key{},
		ProofPathLimits{},
	); !errors.Is(err, errInvalidProofPathLimits) {
		t.Fatalf("invalid limits error = %v", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := tree.ProofPath(
		cancelled,
		Key{},
		testProofPathLimits(),
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled path error = %v", err)
	}
	result, err := tree.ProofPath(
		context.Background(),
		Key{},
		ProofPathLimits{
			MaxNodeReads:      1,
			MaxCommitments:    5,
			MaxPathBytes:      1,
			MaxTemporaryBytes: 2 * proofPathWorkingBytes,
		},
	)
	if err != nil ||
		result.Kind != ProofPathMissing ||
		result.Depth != 1 ||
		len(result.Commitments) != 0 {
		t.Fatalf("empty-tree proof path = %#v, error %v", result, err)
	}
	if cap(result.Commitments) != 2 {
		t.Fatalf(
			"empty-tree result capacity = %d, want 2",
			cap(result.Commitments),
		)
	}

	for cancelAt := 1; cancelAt <= 12; cancelAt++ {
		key := testKey(1, 2)
		single, buildErr := Build(
			context.Background(),
			[]Entry{{Key: key, Value: testValue(1)}},
			testLimits(),
			testCommitmentLimits(),
		)
		if buildErr != nil {
			t.Fatalf("build cancellation tree: %v", buildErr)
		}
		_, pathErr := single.ProofPath(
			&cancelContext{cancelAt: cancelAt},
			key,
			testProofPathLimits(),
		)
		if pathErr != nil && !errors.Is(pathErr, context.Canceled) {
			t.Fatalf("cancel at %d error = %v", cancelAt, pathErr)
		}
	}
}

func TestProofPathLimitsAndCorruptTreesFailClosed(t *testing.T) {
	t.Parallel()

	limits := testProofPathLimits()
	for name, change := range map[string]func(*ProofPathLimits){
		"zero node reads": func(value *ProofPathLimits) {
			value.MaxNodeReads = 0
		},
		"excessive node reads": func(value *ProofPathLimits) {
			value.MaxNodeReads = maxProofPathCommitments + 1
		},
		"zero commitments": func(value *ProofPathLimits) {
			value.MaxCommitments = 0
		},
		"excessive commitments": func(value *ProofPathLimits) {
			value.MaxCommitments = maxProofPathCommitments + 1
		},
		"zero path bytes": func(value *ProofPathLimits) {
			value.MaxPathBytes = 0
		},
		"excessive path bytes": func(value *ProofPathLimits) {
			value.MaxPathBytes = maxProofPathBytes + 1
		},
		"zero temporary bytes": func(value *ProofPathLimits) {
			value.MaxTemporaryBytes = 0
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			candidate := limits
			change(&candidate)
			if !errors.Is(
				candidate.validate(),
				errInvalidProofPathLimits,
			) {
				t.Fatalf("limits %#v were accepted", candidate)
			}
		})
	}
	if err := limits.validate(); err != nil {
		t.Fatalf("valid limits: %v", err)
	}
	if err := checkProofPathResource(
		ProofPathResourceNodeReads,
		1,
		1,
	); err != nil {
		t.Fatalf("exact resource limit: %v", err)
	}
	reads := uint64(0)
	if _, err := (Tree{nodes: []node{{}}}).readProofPathNode(
		context.Background(),
		1,
		limits,
		&reads,
	); !errors.Is(err, errInvalidTree) {
		t.Fatalf("invalid direct node read error = %v", err)
	}

	valid := validVectorCommitment(t)
	treeValidity := []Tree{
		{nodes: []node{{kind: nodeInternal}}, root: 0, valid: false},
		{nodes: nil, root: 0, valid: true},
		{nodes: []node{{kind: nodeInternal}}, root: 1, valid: true},
	}
	for index := range treeValidity {
		if _, err := treeValidity[index].ProofPath(
			context.Background(),
			Key{},
			testProofPathLimits(),
		); !errors.Is(err, errInvalidTree) {
			t.Fatalf("invalid tree header %d error = %v", index, err)
		}
	}
	var nilContext context.Context
	if _, err := treeValidity[2].ProofPath(
		nilContext,
		Key{},
		ProofPathLimits{},
	); !errors.Is(err, errInvalidTree) {
		t.Fatalf("root-boundary precedence error = %v", err)
	}
	childBoundary := Tree{
		nodes: []node{{kind: nodeInternal, edgeCount: 1}},
		edges: []edge{{child: 1}},
		root:  0,
		valid: true,
	}
	childLimits := testProofPathLimits()
	childLimits.MaxNodeReads = 1
	if _, err := childBoundary.ProofPath(
		context.Background(),
		Key{},
		childLimits,
	); !errors.Is(err, errInvalidTree) {
		t.Fatalf("child-boundary precedence error = %v", err)
	}
	corrupt := []Tree{
		{nodes: []node{{kind: nodeStem}}, root: 0, valid: true},
		{nodes: []node{{kind: nodeInternal, depth: 31}}, root: 0, valid: true},
		{
			nodes: []node{{kind: nodeInternal, firstEdge: 1, edgeCount: 1}},
			root:  0,
			valid: true,
		},
		{
			nodes: []node{{kind: nodeInternal, edgeCount: 1}},
			edges: []edge{{child: 1}},
			root:  0,
			valid: true,
		},
		{
			nodes: []node{
				{kind: nodeInternal, edgeCount: 1},
				{kind: nodeStem, depth: 2, commitment: valid},
			},
			edges: []edge{{child: 1}},
			root:  0,
			valid: true,
		},
		{
			nodes: []node{
				{kind: nodeInternal, edgeCount: 1},
				{kind: 99, depth: 1, commitment: valid},
			},
			edges: []edge{{child: 1}},
			root:  0,
			valid: true,
		},
		{
			nodes: []node{
				{kind: nodeInternal, edgeCount: 1},
				{kind: nodeStem, depth: 1},
			},
			edges: []edge{{child: 1}},
			root:  0,
			valid: true,
		},
	}
	for index := range corrupt {
		if _, err := corrupt[index].ProofPath(
			context.Background(),
			Key{},
			testProofPathLimits(),
		); !errors.Is(err, errInvalidTree) {
			t.Fatalf("corrupt tree %d error = %v", index, err)
		}
	}
}

func TestProofPathAcceptsMaximumDepth(t *testing.T) {
	t.Parallel()

	var left Key
	var right Key
	left[30] = 1
	right[30] = 2
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
		t.Fatalf("build maximum-depth tree: %v", err)
	}
	result, err := tree.ProofPath(
		context.Background(),
		left,
		testProofPathLimits(),
	)
	if err != nil {
		t.Fatalf("extract maximum-depth path: %v", err)
	}
	if result.Kind != ProofPathPresent ||
		result.Depth != 31 ||
		len(result.Commitments) != 32 {
		t.Fatalf("maximum-depth proof path = %#v", result)
	}
}

func testProofPathLimits() ProofPathLimits {
	return ProofPathLimits{
		MaxNodeReads:      32,
		MaxCommitments:    32,
		MaxPathBytes:      528,
		MaxTemporaryBytes: 32 * 256,
	}
}
