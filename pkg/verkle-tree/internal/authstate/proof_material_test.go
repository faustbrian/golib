package authstate

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/faustbrian/golib/pkg/verkle-tree/internal/backend"
	"github.com/faustbrian/golib/pkg/verkle-tree/internal/committedtree"
)

func TestSnapshotProofMaterialDerivesCanonicalClaimsAndPaths(t *testing.T) {
	t.Parallel()

	present := testKey(0, 0)
	present[1] = 1
	absentSuffix := present
	absentSuffix[31] = 200
	existing := testKey(1, 1)
	existing[1] = 1
	different := existing
	different[1] = 2
	missing := testKey(2, 2)
	snapshot := newTestSnapshot(t, []Entry{
		{Key: present, Value: testValue(7)},
		{Key: existing, Value: testValue(8)},
	})

	material, err := snapshot.ProofMaterial(
		context.Background(),
		[]Key{missing, absentSuffix, different, present},
		testProofMaterialLimits(),
	)
	if err != nil {
		t.Fatalf("assemble proof material: %v", err)
	}
	claims, err := material.Claims()
	if err != nil {
		t.Fatalf("material claims: %v", err)
	}
	for _, expected := range []struct {
		key     Key
		kind    ClaimKind
		value   Value
		present bool
	}{
		{key: present, kind: ClaimMembership, value: testValue(7), present: true},
		{key: absentSuffix, kind: ClaimAbsence},
		{key: different, kind: ClaimAbsence},
		{key: missing, kind: ClaimAbsence},
	} {
		claim, found, lookupErr := claims.Lookup(expected.key)
		if lookupErr != nil || !found {
			t.Fatalf("claim %x found = %t, error %v", expected.key, found, lookupErr)
		}
		kind, kindErr := claim.Kind()
		value, presentValue, valueErr := claim.Value()
		if kindErr != nil || valueErr != nil || kind != expected.kind ||
			value != expected.value || presentValue != expected.present {
			t.Fatalf(
				"claim %x = %d/%x/%t, errors %v/%v",
				expected.key,
				kind,
				value,
				presentValue,
				kindErr,
				valueErr,
			)
		}
	}

	stemPaths, err := material.StemPaths(context.Background())
	if err != nil {
		t.Fatalf("material stem paths: %v", err)
	}
	if len(stemPaths) != 3 {
		t.Fatalf("stem path count = %d, want 3", len(stemPaths))
	}
	wantKinds := []StemPathKind{
		StemPathPresent,
		StemPathDifferent,
		StemPathMissing,
	}
	for index := range stemPaths {
		kind, kindErr := stemPaths[index].Kind()
		if kindErr != nil || kind != wantKinds[index] {
			t.Fatalf("stem path %d kind = %d, error %v", index, kind, kindErr)
		}
	}
	encountered, found, err := stemPaths[1].ExistingStem()
	if err != nil || !found || encountered != stemFromKey(existing) {
		t.Fatalf("different stem = %x/%t, error %v", encountered, found, err)
	}

	commitments, err := material.PathCommitments(context.Background())
	if err != nil {
		t.Fatalf("material path commitments: %v", err)
	}
	wantPaths := [][]byte{{0}, {0, 2}, {0, 3}, {1}}
	if len(commitments) != len(wantPaths) {
		t.Fatalf("path commitment count = %d, want %d", len(commitments), len(wantPaths))
	}
	for index := range commitments {
		path, pathErr := commitments[index].Path()
		commitment, commitmentErr := commitments[index].Commitment()
		identity, identityErr := commitment.IsIdentity()
		if pathErr != nil || commitmentErr != nil || identityErr != nil ||
			!bytes.Equal(path, wantPaths[index]) || identity != (index == 2) {
			t.Fatalf(
				"path %d = %x identity %t, errors %v/%v/%v",
				index,
				path,
				identity,
				pathErr,
				commitmentErr,
				identityErr,
			)
		}
	}
	root, err := material.Root()
	if err != nil {
		t.Fatalf("material root: %v", err)
	}
	wantRoot, err := snapshot.RootContainer(context.Background())
	if err != nil {
		t.Fatalf("snapshot root: %v", err)
	}
	gotRootBytes, err := root.Bytes()
	if err != nil {
		t.Fatalf("material root bytes: %v", err)
	}
	wantRootBytes, err := wantRoot.Bytes()
	if err != nil {
		t.Fatalf("snapshot root bytes: %v", err)
	}
	if gotRootBytes != wantRootBytes {
		t.Fatalf("material root = %x, want %x", gotRootBytes, wantRootBytes)
	}
	if _, err := NewTreeProof(
		context.Background(),
		root,
		claims,
		stemPaths,
		commitments,
		testRawOpeningProof(t),
		testTreeProofLimits(),
	); err != nil {
		t.Fatalf("material is not structurally complete for tree proof: %v", err)
	}
}

func TestSnapshotProofMaterialDerivesEmptyRootNonMembership(t *testing.T) {
	t.Parallel()

	first := testKey(0x20, 0x01)
	second := testKey(0x10, 0x02)
	secondSuffix := second
	secondSuffix[31] = 0x80
	material, err := newTestSnapshot(t, nil).ProofMaterial(
		context.Background(),
		[]Key{first, secondSuffix, second},
		testProofMaterialLimits(),
	)
	if err != nil {
		t.Fatalf("empty-root proof material: %v", err)
	}
	root, err := material.Root()
	if err != nil {
		t.Fatalf("material root: %v", err)
	}
	empty, err := root.IsEmpty()
	if err != nil || !empty {
		t.Fatalf("material root empty = %t, error %v", empty, err)
	}
	claims, err := material.Claims()
	if err != nil {
		t.Fatalf("material claims: %v", err)
	}
	for _, key := range []Key{second, secondSuffix, first} {
		claim, found, lookupErr := claims.Lookup(key)
		if lookupErr != nil || !found {
			t.Fatalf("claim %x found = %t, error %v", key, found, lookupErr)
		}
		kind, kindErr := claim.Kind()
		if kindErr != nil || kind != ClaimAbsence {
			t.Fatalf("claim %x kind = %d, error %v", key, kind, kindErr)
		}
	}
	paths, err := material.StemPaths(context.Background())
	if err != nil {
		t.Fatalf("material stem paths: %v", err)
	}
	if len(paths) != 2 {
		t.Fatalf("stem path count = %d, want 2", len(paths))
	}
	for index, want := range []Stem{stemFromKey(second), stemFromKey(first)} {
		stem, stemErr := paths[index].Stem()
		depth, depthErr := paths[index].Depth()
		kind, kindErr := paths[index].Kind()
		if stemErr != nil || depthErr != nil || kindErr != nil ||
			stem != want || depth != 1 || kind != StemPathMissing {
			t.Fatalf(
				"path %d = %x/%d/%d, errors %v/%v/%v",
				index, stem, depth, kind, stemErr, depthErr, kindErr,
			)
		}
	}
	commitments, err := material.PathCommitments(context.Background())
	if err != nil || len(commitments) != 0 {
		t.Fatalf("material commitments = %#v, error %v", commitments, err)
	}
}

func testProofMaterialLimits() ProofMaterialLimits {
	return ProofMaterialLimits{
		MaxKeys:            64,
		MaxStemPaths:       64,
		MaxNodeReads:       2_048,
		MaxPathCommitments: 2_048,
		MaxPathBytes:       32_768,
		MaxTemporaryBytes:  1 << 20,
	}
}

func TestSnapshotProofMaterialRejectsInvalidInputs(t *testing.T) {
	t.Parallel()

	snapshot := newTestSnapshot(t, []Entry{{Key: testKey(0, 0), Value: testValue(1)}})
	limits := testProofMaterialLimits()
	var nilContext context.Context
	if _, err := snapshot.ProofMaterial(nilContext, []Key{testKey(0, 0)}, limits); !errors.Is(err, errInvalidContext) {
		t.Fatalf("nil context error = %v", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := snapshot.ProofMaterial(cancelled, []Key{testKey(0, 0)}, limits); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled context error = %v", err)
	}
	if _, err := (Snapshot{}).ProofMaterial(context.Background(), []Key{testKey(0, 0)}, limits); !errors.Is(err, errInvalidSnapshot) {
		t.Fatalf("zero snapshot error = %v", err)
	}
	if _, err := snapshot.ProofMaterial(context.Background(), nil, limits); !errors.Is(err, errInvalidProofMaterial) {
		t.Fatalf("empty key set error = %v", err)
	}
	invalidLimits := limits
	invalidLimits.MaxKeys = 0
	if _, err := snapshot.ProofMaterial(context.Background(), []Key{testKey(0, 0)}, invalidLimits); !errors.Is(err, errInvalidProofMaterialLimits) {
		t.Fatalf("invalid limits error = %v", err)
	}
	emptyKeys := []Key{testKey(0, 0), testKey(1, 0)}
	emptyLimits := limits
	emptyLimits.MaxKeys = 1
	_, err := newTestSnapshot(t, nil).ProofMaterial(
		context.Background(), emptyKeys, emptyLimits,
	)
	assertProofMaterialResourceError(
		t, err, ProofMaterialResourceKeys, 1, 2,
	)
	corrupt := snapshot
	corrupt.tree = committedtree.Tree{}
	if _, err := corrupt.ProofMaterial(context.Background(), []Key{testKey(0, 0)}, limits); err == nil {
		t.Fatal("corrupt committed tree was accepted")
	}
	duplicate := testKey(1, 1)
	if _, err := snapshot.ProofMaterial(context.Background(), []Key{duplicate, duplicate}, limits); !errors.Is(err, errDuplicateClaimKey) {
		t.Fatalf("duplicate key error = %v", err)
	}
}

func TestProofMaterialLimitsRejectZeroAndExcessiveValues(t *testing.T) {
	t.Parallel()

	valid := testProofMaterialLimits()
	tests := map[string]ProofMaterialLimits{
		"keys zero":    func() ProofMaterialLimits { value := valid; value.MaxKeys = 0; return value }(),
		"keys maximum": func() ProofMaterialLimits { value := valid; value.MaxKeys = maxClaimCount + 1; return value }(),
		"stems zero":   func() ProofMaterialLimits { value := valid; value.MaxStemPaths = 0; return value }(),
		"stems maximum": func() ProofMaterialLimits {
			value := valid
			value.MaxStemPaths = maxTreeProofStemPaths + 1
			return value
		}(),
		"reads zero": func() ProofMaterialLimits { value := valid; value.MaxNodeReads = 0; return value }(),
		"reads maximum": func() ProofMaterialLimits {
			value := valid
			value.MaxNodeReads = uint64(maxTreeProofPathDerivations) + 1
			return value
		}(),
		"commitments zero": func() ProofMaterialLimits { value := valid; value.MaxPathCommitments = 0; return value }(),
		"commitments maximum": func() ProofMaterialLimits {
			value := valid
			value.MaxPathCommitments = maxTreeProofPathCommitments + 1
			return value
		}(),
		"path bytes zero": func() ProofMaterialLimits { value := valid; value.MaxPathBytes = 0; return value }(),
		"path bytes maximum": func() ProofMaterialLimits {
			value := valid
			value.MaxPathBytes = uint64(maxTreeProofPathCommitments)*maxProofPathLength + 1
			return value
		}(),
		"temporary zero": func() ProofMaterialLimits { value := valid; value.MaxTemporaryBytes = 0; return value }(),
	}
	for name, limits := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := limits.validate(); !errors.Is(err, errInvalidProofMaterialLimits) {
				t.Fatalf("limits error = %v", err)
			}
		})
	}
	if err := valid.validate(); err != nil {
		t.Fatalf("valid limits: %v", err)
	}
	exact := []ProofMaterialLimits{
		func() ProofMaterialLimits { value := valid; value.MaxKeys = maxClaimCount; return value }(),
		func() ProofMaterialLimits { value := valid; value.MaxStemPaths = maxTreeProofStemPaths; return value }(),
		func() ProofMaterialLimits {
			value := valid
			value.MaxNodeReads = uint64(maxTreeProofPathDerivations)
			return value
		}(),
		func() ProofMaterialLimits {
			value := valid
			value.MaxPathCommitments = maxTreeProofPathCommitments
			return value
		}(),
		func() ProofMaterialLimits {
			value := valid
			value.MaxPathBytes = uint64(maxTreeProofPathCommitments) * maxProofPathLength
			return value
		}(),
	}
	for index := range exact {
		if err := exact[index].validate(); err != nil {
			t.Fatalf("exact maximum %d: %v", index, err)
		}
	}
}

func TestSnapshotProofMaterialEnforcesAggregateResources(t *testing.T) {
	t.Parallel()

	present := testKey(0, 0)
	snapshot := newTestSnapshot(t, []Entry{{Key: present, Value: testValue(1)}})
	tests := map[string]struct {
		keys     []Key
		resource ProofMaterialResource
		limits   ProofMaterialLimits
		limit    uint64
		actual   uint64
	}{
		"keys": {
			keys: []Key{present, testKey(1, 0)}, resource: ProofMaterialResourceKeys,
			limits: func() ProofMaterialLimits { value := testProofMaterialLimits(); value.MaxKeys = 1; return value }(),
			limit:  1, actual: 2,
		},
		"stem paths": {
			keys: []Key{testKey(1, 0), testKey(2, 0)}, resource: ProofMaterialResourceStemPaths,
			limits: func() ProofMaterialLimits { value := testProofMaterialLimits(); value.MaxStemPaths = 1; return value }(),
			limit:  1, actual: 2,
		},
		"node reads": {
			keys: []Key{present}, resource: ProofMaterialResourceNodeReads,
			limits: func() ProofMaterialLimits { value := testProofMaterialLimits(); value.MaxNodeReads = 1; return value }(),
			limit:  1, actual: 2,
		},
		"path commitments": {
			keys: []Key{present}, resource: ProofMaterialResourcePathCommitments,
			limits: func() ProofMaterialLimits {
				value := testProofMaterialLimits()
				value.MaxPathCommitments = 1
				return value
			}(),
			limit: 1, actual: 2,
		},
		"path bytes": {
			keys: []Key{present}, resource: ProofMaterialResourcePathBytes,
			limits: func() ProofMaterialLimits { value := testProofMaterialLimits(); value.MaxPathBytes = 2; return value }(),
			limit:  2, actual: 3,
		},
		"temporary bytes": {
			keys: []Key{present}, resource: ProofMaterialResourceTemporaryBytes,
			limits: func() ProofMaterialLimits {
				value := testProofMaterialLimits()
				value.MaxPathCommitments = 2
				value.MaxTemporaryBytes = 1_055
				return value
			}(),
			limit: 1_055, actual: 1_056,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := snapshot.ProofMaterial(context.Background(), test.keys, test.limits)
			assertProofMaterialResourceError(t, err, test.resource, test.limit, test.actual)
		})
	}
}

func TestProofMaterialExtractionRejectsAlreadyExhaustedAccounting(t *testing.T) {
	t.Parallel()

	snapshot := newTestSnapshot(t, []Entry{{Key: testKey(0, 0), Value: testValue(1)}})
	limits := testProofMaterialLimits()
	limits.MaxNodeReads = 1
	nodeReads := uint64(2)
	pathBytes := uint64(0)
	commitments := make([]PathCommitment, 0)
	_, err := snapshot.extractProofPath(
		context.Background(),
		testKey(0, 0),
		limits,
		&nodeReads,
		&pathBytes,
		&commitments,
	)
	assertProofMaterialResourceError(
		t,
		err,
		ProofMaterialResourceNodeReads,
		1,
		3,
	)

	nodeReads = 0
	limits = testProofMaterialLimits()
	limits.MaxPathCommitments = 1
	commitments = make([]PathCommitment, 1)
	_, err = snapshot.extractProofPath(context.Background(), testKey(0, 0), limits, &nodeReads, &pathBytes, &commitments)
	assertProofMaterialResourceError(t, err, ProofMaterialResourcePathCommitments, 1, 2)

	nodeReads = 0
	pathBytes = 1
	commitments = nil
	limits = testProofMaterialLimits()
	limits.MaxPathBytes = 1
	_, err = snapshot.extractProofPath(context.Background(), testKey(0, 0), limits, &nodeReads, &pathBytes, &commitments)
	assertProofMaterialResourceError(t, err, ProofMaterialResourcePathBytes, 1, 2)
}

func TestProofMaterialExtractionAccountsSuccessfulPaths(t *testing.T) {
	t.Parallel()

	present := testKey(0, 0)
	snapshot := newTestSnapshot(t, []Entry{{Key: present, Value: testValue(1)}})
	nodeReads := uint64(0)
	pathBytes := uint64(0)
	commitments := make([]PathCommitment, 0)
	path, err := snapshot.extractProofPath(
		context.Background(),
		present,
		testProofMaterialLimits(),
		&nodeReads,
		&pathBytes,
		&commitments,
	)
	if err != nil {
		t.Fatalf("present path: %v", err)
	}
	if path.Kind != committedtree.ProofPathPresent || nodeReads != 2 || pathBytes != 3 || len(commitments) != 2 {
		t.Fatalf("present accounting = kind %d, reads %d, bytes %d, commitments %d", path.Kind, nodeReads, pathBytes, len(commitments))
	}

	nodeReads = 1
	pathBytes = 1
	commitments = []PathCommitment{mustPathCommitment(t, []byte{9}, testProofCommitment(t))}
	path, err = snapshot.extractProofPath(
		context.Background(),
		present,
		testProofMaterialLimits(),
		&nodeReads,
		&pathBytes,
		&commitments,
	)
	if err != nil {
		t.Fatalf("cumulative present path: %v", err)
	}
	if path.Kind != committedtree.ProofPathPresent || nodeReads != 3 || pathBytes != 4 || len(commitments) != 3 {
		t.Fatalf("cumulative accounting = kind %d, reads %d, bytes %d, commitments %d", path.Kind, nodeReads, pathBytes, len(commitments))
	}

	nodeReads = 0
	pathBytes = 0
	commitments = nil
	path, err = snapshot.extractProofPath(
		context.Background(),
		testKey(1, 0),
		testProofMaterialLimits(),
		&nodeReads,
		&pathBytes,
		&commitments,
	)
	if err != nil {
		t.Fatalf("missing path: %v", err)
	}
	if path.Kind != committedtree.ProofPathMissing || nodeReads != 1 || pathBytes != 0 || len(commitments) != 0 {
		t.Fatalf("missing accounting = kind %d, reads %d, bytes %d, commitments %d", path.Kind, nodeReads, pathBytes, len(commitments))
	}
}

func TestSnapshotProofMaterialAccountsSecondSuffixHalf(t *testing.T) {
	t.Parallel()

	first := testKey(0, 0)
	first[1] = 1
	secondLow := first
	secondLow[31] = 1
	high := first
	high[31] = 200
	snapshot := newTestSnapshot(t, []Entry{{Key: first, Value: testValue(1)}})
	limits := testProofMaterialLimits()
	limits.MaxPathCommitments = 3
	_, err := snapshot.ProofMaterial(context.Background(), []Key{high, secondLow, first}, limits)
	assertProofMaterialResourceError(t, err, ProofMaterialResourcePathCommitments, 3, 4)
}

func TestSnapshotProofMaterialSupportsMaximumStemDepth(t *testing.T) {
	t.Parallel()

	left := testKey(0, 0)
	right := testKey(0, 1)
	for index := 0; index < 30; index++ {
		left[index] = 9
		right[index] = 9
	}
	left[30] = 1
	right[30] = 2
	snapshot := newTestSnapshot(t, []Entry{
		{Key: left, Value: testValue(1)},
		{Key: right, Value: testValue(2)},
	})
	material, err := snapshot.ProofMaterial(context.Background(), []Key{left}, testProofMaterialLimits())
	if err != nil {
		t.Fatalf("proof material: %v", err)
	}
	paths, err := material.StemPaths(context.Background())
	if err != nil {
		t.Fatalf("stem paths: %v", err)
	}
	depth, err := paths[0].Depth()
	if err != nil || depth != 31 {
		t.Fatalf("stem depth = %d, error %v", depth, err)
	}
	commitments, err := material.PathCommitments(context.Background())
	if err != nil {
		t.Fatalf("path commitments: %v", err)
	}
	if len(commitments) != 32 {
		t.Fatalf("path commitment count = %d, want 32", len(commitments))
	}
}

func TestSnapshotProofMaterialAvoidsRedundantStemExtractions(t *testing.T) {
	t.Parallel()

	presentHigh := testKey(0, 128)
	snapshot := newTestSnapshot(t, []Entry{{Key: presentHigh, Value: testValue(1)}})
	limits := testProofMaterialLimits()
	limits.MaxNodeReads = 2
	material, err := snapshot.ProofMaterial(
		context.Background(),
		[]Key{presentHigh, testKey(0, 200)},
		limits,
	)
	if err != nil {
		t.Fatalf("high-half material: %v", err)
	}
	root, claims, paths, commitments := mustProofMaterialParts(t, material)
	if _, err := NewTreeProof(
		context.Background(),
		root,
		claims,
		paths,
		commitments,
		testRawOpeningProof(t),
		testTreeProofLimits(),
	); err != nil {
		t.Fatalf("high-half material completeness: %v", err)
	}

	missingLimits := testProofMaterialLimits()
	missingLimits.MaxNodeReads = 1
	if _, err := snapshot.ProofMaterial(
		context.Background(),
		[]Key{testKey(1, 0), testKey(1, 200)},
		missingLimits,
	); err != nil {
		t.Fatalf("missing-stem material: %v", err)
	}
}

func TestSnapshotProofMaterialExtractsSuffixBoundaryHalf(t *testing.T) {
	t.Parallel()

	low := testKey(0, 0)
	boundary := testKey(0, 128)
	snapshot := newTestSnapshot(t, []Entry{{Key: low, Value: testValue(1)}})
	material, err := snapshot.ProofMaterial(
		context.Background(),
		[]Key{boundary, low},
		testProofMaterialLimits(),
	)
	if err != nil {
		t.Fatalf("proof material: %v", err)
	}
	root, claims, paths, commitments := mustProofMaterialParts(t, material)
	if _, err := NewTreeProof(
		context.Background(),
		root,
		claims,
		paths,
		commitments,
		testRawOpeningProof(t),
		testTreeProofLimits(),
	); err != nil {
		t.Fatalf("boundary-half material completeness: %v", err)
	}
}

func TestSnapshotProofMaterialCancellationCheckpoints(t *testing.T) {
	t.Parallel()

	first := testKey(0, 0)
	first[1] = 1
	secondLow := first
	secondLow[31] = 1
	high := first
	high[31] = 200
	snapshot := newTestSnapshot(t, []Entry{
		{Key: first, Value: testValue(1)},
		{Key: testKey(1, 0), Value: testValue(2)},
	})
	keys := []Key{testKey(3, 0), high, testKey(2, 0), secondLow, first, testKey(1, 0)}
	for successful := 0; successful <= 300; successful++ {
		_, err := snapshot.ProofMaterial(&stepContext{successfulChecks: successful}, keys, testProofMaterialLimits())
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation after %d checks = %v", successful, err)
		}
	}
}

func TestProofMaterialIsDeterministicOwnedAndConcurrent(t *testing.T) {
	t.Parallel()

	first := testKey(0, 1)
	second := testKey(1, 2)
	snapshot := newTestSnapshot(t, []Entry{
		{Key: first, Value: testValue(1)},
		{Key: second, Value: testValue(2)},
	})
	left, err := snapshot.ProofMaterial(context.Background(), []Key{second, first}, testProofMaterialLimits())
	if err != nil {
		t.Fatalf("left material: %v", err)
	}
	right, err := snapshot.ProofMaterial(context.Background(), []Key{first, second}, testProofMaterialLimits())
	if err != nil {
		t.Fatalf("right material: %v", err)
	}
	leftPaths, err := left.StemPaths(context.Background())
	if err != nil {
		t.Fatalf("left stem paths: %v", err)
	}
	rightPaths, err := right.StemPaths(context.Background())
	if err != nil {
		t.Fatalf("right stem paths: %v", err)
	}
	if !slices.Equal(leftPaths, rightPaths) {
		t.Fatalf("stem paths differ: %#v / %#v", leftPaths, rightPaths)
	}
	leftCommitments, err := left.PathCommitments(context.Background())
	if err != nil {
		t.Fatalf("left path commitments: %v", err)
	}
	rightCommitments, err := right.PathCommitments(context.Background())
	if err != nil {
		t.Fatalf("right path commitments: %v", err)
	}
	if !slices.Equal(leftCommitments, rightCommitments) {
		t.Fatalf("commitments differ: %#v / %#v", leftCommitments, rightCommitments)
	}
	leftPaths[0] = StemPath{}
	leftCommitments[0] = PathCommitment{}
	ownedPaths, err := left.StemPaths(context.Background())
	if err != nil {
		t.Fatalf("owned stem paths: %v", err)
	}
	ownedCommitments, err := left.PathCommitments(context.Background())
	if err != nil {
		t.Fatalf("owned path commitments: %v", err)
	}
	if ownedPaths[0] == (StemPath{}) || ownedCommitments[0] == (PathCommitment{}) {
		t.Fatal("returned slices alias retained proof material")
	}

	var wait sync.WaitGroup
	errs := make(chan error, 16*4)
	for range 16 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if _, readErr := left.Root(); readErr != nil {
				errs <- readErr
			}
			if _, readErr := left.Claims(); readErr != nil {
				errs <- readErr
			}
			if _, readErr := left.StemPaths(context.Background()); readErr != nil {
				errs <- readErr
			}
			if _, readErr := left.PathCommitments(context.Background()); readErr != nil {
				errs <- readErr
			}
		}()
	}
	wait.Wait()
	close(errs)
	for readErr := range errs {
		t.Fatalf("concurrent read: %v", readErr)
	}
}

func TestProofMaterialRejectsInvalidReceiversAndCopyContexts(t *testing.T) {
	t.Parallel()

	var zero ProofMaterial
	if _, err := zero.Root(); !errors.Is(err, errInvalidProofMaterial) {
		t.Fatalf("zero root error = %v", err)
	}
	if _, err := zero.Claims(); !errors.Is(err, errInvalidProofMaterial) {
		t.Fatalf("zero claims error = %v", err)
	}
	if _, err := zero.StemPaths(context.Background()); !errors.Is(err, errInvalidProofMaterial) {
		t.Fatalf("zero stems error = %v", err)
	}
	if _, err := zero.PathCommitments(context.Background()); !errors.Is(err, errInvalidProofMaterial) {
		t.Fatalf("zero commitments error = %v", err)
	}

	snapshot := newTestSnapshot(t, []Entry{{Key: testKey(0, 0), Value: testValue(1)}})
	material, err := snapshot.ProofMaterial(context.Background(), []Key{testKey(0, 0)}, testProofMaterialLimits())
	if err != nil {
		t.Fatalf("proof material: %v", err)
	}
	var nilContext context.Context
	if _, err := material.StemPaths(nilContext); !errors.Is(err, errInvalidTreeProofContext) {
		t.Fatalf("nil stem context error = %v", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := material.PathCommitments(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled commitment context error = %v", err)
	}

	corrupt := material
	corrupt.valid = false
	if _, err := corrupt.Root(); !errors.Is(err, errInvalidProofMaterial) {
		t.Fatalf("corrupt material error = %v", err)
	}
	corrupt = material
	corrupt.root = backendEmptyRoot(t)
	if _, err := corrupt.Root(); !errors.Is(err, errInvalidProofMaterial) {
		t.Fatalf("empty-root material error = %v", err)
	}
	corrupt = material
	corrupt.root = backend.Root{}
	if _, err := corrupt.Claims(); !errors.Is(err, errInvalidProofMaterial) {
		t.Fatalf("invalid-root material error = %v", err)
	}
}

func TestProofMaterialHelpers(t *testing.T) {
	t.Parallel()

	if err := checkProofMaterialResource(ProofMaterialResourceKeys, 1, 1); err != nil {
		t.Fatalf("exact resource limit: %v", err)
	}
	for kind, expected := range map[committedtree.ProofPathKind]StemPathKind{
		committedtree.ProofPathPresent:   StemPathPresent,
		committedtree.ProofPathMissing:   StemPathMissing,
		committedtree.ProofPathDifferent: StemPathDifferent,
	} {
		stem := stemFromKey(testKey(0, 0))
		existing := stemFromKey(testKey(0, 0))
		existing[1] = 1
		path := stemPathFromCommitted(stem, committedtree.ProofPath{Kind: kind, Depth: 1, ExistingStem: existing})
		got, err := path.Kind()
		if err != nil {
			t.Fatalf("kind %d: %v", kind, err)
		}
		if got != expected {
			t.Fatalf("kind %d mapped to %d", kind, got)
		}
	}
	if path := stemPathFromCommitted(Stem{}, committedtree.ProofPath{}); path != (StemPath{}) {
		t.Fatalf("invalid kind path = %#v", path)
	}

	validCommitment := testProofCommitment(t)
	var rawPath [32]byte
	rawPath[0] = 1
	converted, pathBytes := convertProofPathCommitments([]committedtree.ProofPathCommitment{{
		Path: rawPath, Length: 1, Commitment: validCommitment,
	}})
	if len(converted) != 1 || pathBytes != 1 {
		t.Fatalf("converted commitments = %d/%d", len(converted), pathBytes)
	}
	invalidConverted, _ := convertProofPathCommitments([]committedtree.ProofPathCommitment{{Path: rawPath, Length: 1}})
	if len(invalidConverted) != 1 || invalidConverted[0] != (PathCommitment{}) {
		t.Fatalf("invalid converted commitment = %#v", invalidConverted)
	}
	overlongConverted, overlongBytes := convertProofPathCommitments([]committedtree.ProofPathCommitment{{Length: 33}})
	if len(overlongConverted) != 1 || overlongConverted[0] != (PathCommitment{}) || overlongBytes != 33 {
		t.Fatalf("overlong converted commitment = %#v / %d", overlongConverted, overlongBytes)
	}
	afterOverlong, _ := convertProofPathCommitments([]committedtree.ProofPathCommitment{
		{Length: 33},
		{Path: rawPath, Length: 1, Commitment: validCommitment},
	})
	if len(afterOverlong) != 2 || afterOverlong[1] == (PathCommitment{}) {
		t.Fatalf("valid commitment after overlong entry = %#v", afterOverlong)
	}
	afterInvalid, _ := convertProofPathCommitments([]committedtree.ProofPathCommitment{
		{Path: rawPath, Length: 1},
		{Path: rawPath, Length: 1, Commitment: validCommitment},
	})
	if len(afterInvalid) != 2 || afterInvalid[1] == (PathCommitment{}) {
		t.Fatalf("valid commitment after invalid entry = %#v", afterInvalid)
	}

	first := mustPathCommitment(t, []byte{1}, validCommitment)
	duplicate := []PathCommitment{first, first}
	unique, err := deduplicatePathCommitments(context.Background(), duplicate)
	if err != nil || len(unique) != 1 {
		t.Fatalf("deduplicated commitments = %d, error %v", len(unique), err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := deduplicatePathCommitments(cancelled, []PathCommitment{first}); !errors.Is(err, context.Canceled) {
		t.Fatalf("deduplication cancellation error = %v", err)
	}
	otherSnapshot := newTestSnapshot(t, []Entry{{Key: testKey(9, 0), Value: testValue(9)}})
	otherCommitment, err := otherSnapshot.Root()
	if err != nil {
		t.Fatalf("other commitment: %v", err)
	}
	conflicting := mustPathCommitment(t, []byte{1}, otherCommitment)
	if _, err := deduplicatePathCommitments(context.Background(), []PathCommitment{first, conflicting}); !errors.Is(err, errInvalidProofMaterial) {
		t.Fatalf("conflicting duplicate error = %v", err)
	}
	invalid := PathCommitment{path: first.path, length: first.length, valid: true}
	if _, err := deduplicatePathCommitments(context.Background(), []PathCommitment{invalid, first}); !errors.Is(err, errInvalidProofMaterial) {
		t.Fatalf("invalid left duplicate error = %v", err)
	}
	if _, err := deduplicatePathCommitments(context.Background(), []PathCommitment{first, invalid}); !errors.Is(err, errInvalidProofMaterial) {
		t.Fatalf("invalid right duplicate error = %v", err)
	}

	sentinel := errors.New("sentinel")
	if got := translateProofPathResourceError(sentinel, testProofMaterialLimits(), 0, 0, 0); !errors.Is(got, sentinel) {
		t.Fatalf("non-resource translation = %v", got)
	}
	temporaryErr := &committedtree.ProofPathResourceError{Resource: committedtree.ProofPathResourceTemporaryBytes, Limit: 1, Actual: 2}
	if got := translateProofPathResourceError(temporaryErr, testProofMaterialLimits(), 0, 0, 0); !errors.Is(got, errInvalidProofMaterial) {
		t.Fatalf("temporary-resource translation = %v", got)
	}
	if got := saturatingProofMaterialAdd(^uint64(0), 1); got != ^uint64(0) {
		t.Fatalf("saturating addition = %d", got)
	}
	if got := saturatingProofMaterialAdd(1, 2); got != 3 {
		t.Fatalf("ordinary addition = %d", got)
	}
	if got := saturatingProofMaterialAdd(^uint64(0)-1, 1); got != ^uint64(0) {
		t.Fatalf("exact maximum addition = %d", got)
	}

	validMaterial, err := newTestSnapshot(t, []Entry{{Key: testKey(0, 0), Value: testValue(1)}}).ProofMaterial(
		context.Background(),
		[]Key{testKey(0, 0)},
		testProofMaterialLimits(),
	)
	if err != nil {
		t.Fatalf("valid material: %v", err)
	}
	if _, err := newProofMaterial(
		validMaterial.root,
		validMaterial.claims,
		[]StemPath{{}},
		validMaterial.commitments,
	); !errors.Is(err, errInvalidProofMaterial) {
		t.Fatalf("invalid stem material error = %v", err)
	}
	if _, err := newProofMaterial(
		validMaterial.root,
		validMaterial.claims,
		validMaterial.stemPaths,
		[]PathCommitment{{}},
	); !errors.Is(err, errInvalidProofMaterial) {
		t.Fatalf("invalid commitment material error = %v", err)
	}
	if _, err := newProofMaterial(
		backend.Root{},
		validMaterial.claims,
		validMaterial.stemPaths,
		validMaterial.commitments,
	); !errors.Is(err, errInvalidProofMaterial) {
		t.Fatalf("invalid root material error = %v", err)
	}
}

func backendEmptyRoot(t testing.TB) backend.Root {
	t.Helper()

	root, err := newTestSnapshot(t, nil).RootContainer(context.Background())
	if err != nil {
		t.Fatalf("empty root: %v", err)
	}

	return root
}

func mustProofMaterialParts(
	t testing.TB,
	material ProofMaterial,
) (backend.Root, ClaimSet, []StemPath, []PathCommitment) {
	t.Helper()

	root, err := material.Root()
	if err != nil {
		t.Fatalf("material root: %v", err)
	}
	claims, err := material.Claims()
	if err != nil {
		t.Fatalf("material claims: %v", err)
	}
	paths, err := material.StemPaths(context.Background())
	if err != nil {
		t.Fatalf("material stem paths: %v", err)
	}
	commitments, err := material.PathCommitments(context.Background())
	if err != nil {
		t.Fatalf("material path commitments: %v", err)
	}

	return root, claims, paths, commitments
}

func assertProofMaterialResourceError(
	t testing.TB,
	err error,
	resource ProofMaterialResource,
	limit uint64,
	actual uint64,
) {
	t.Helper()

	var resourceErr *ProofMaterialResourceError
	if !errors.As(err, &resourceErr) {
		t.Fatalf("error = %v, want ProofMaterialResourceError", err)
	}
	if resourceErr.Resource != resource || resourceErr.Limit != limit || resourceErr.Actual != actual {
		t.Fatalf("resource error = (%d, %d, %d), want (%d, %d, %d)", resourceErr.Resource, resourceErr.Limit, resourceErr.Actual, resource, limit, actual)
	}
	if !errors.Is(err, errProofMaterialResource) || resourceErr.Error() == "" || resourceErr.Unwrap() != errProofMaterialResource {
		t.Fatalf("resource error contract = %v", err)
	}
}
