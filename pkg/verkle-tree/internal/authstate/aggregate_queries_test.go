package authstate

import (
	"context"
	"errors"
	"runtime"
	"testing"

	"github.com/faustbrian/golib/pkg/verkle-tree/internal/backend"
	"github.com/faustbrian/golib/pkg/verkle-tree/internal/committedtree"
)

func TestProofMaterialReconstructsVerifiableAggregateQueries(t *testing.T) {
	t.Parallel()

	keys := []Key{
		testKey(0x01, 0xff),
		testKey(0x00, 0x02),
		testKey(0x00, 0x80),
		testKey(0x02, 0x00),
		testKey(0x00, 0x00),
	}
	snapshot := newTestSnapshot(t, []Entry{
		{Key: testKey(0x00, 0x00), Value: testValue(0x11)},
		{Key: testKey(0x00, 0x01), Value: testValue(0x22)},
		{Key: testKey(0x00, 0x80), Value: testValue(0x55)},
		{Key: testKey(0x01, 0xff), Value: testValue(0x33)},
		{Key: testKey(0x01, 0x7f), Value: testValue(0x44)},
	})
	material, err := snapshot.ProofMaterial(
		context.Background(),
		keys,
		testProofMaterialLimits(),
	)
	if err != nil {
		t.Fatalf("proof material: %v", err)
	}
	proverRecords, err := snapshot.tree.AggregateProverQueries(
		context.Background(),
		keys,
		testCommittedAggregateQueryLimits(),
	)
	if err != nil {
		t.Fatalf("prover queries: %v", err)
	}
	verifierRecords, err := material.AggregateVerifierQueries(
		context.Background(),
		testAggregateVerifierQueryLimits(),
	)
	if err != nil {
		t.Fatalf("verifier queries: %v", err)
	}
	if len(proverRecords) != len(verifierRecords) {
		t.Fatalf("query counts = %d/%d", len(proverRecords), len(verifierRecords))
	}
	proverQueries := make([]backend.AggregateProverQuery, len(proverRecords))
	verifierQueries := make([]backend.AggregateVerifierQuery, len(verifierRecords))
	for index := range proverRecords {
		prover := proverRecords[index]
		verifier := verifierRecords[index]
		if prover.Length != verifier.Length ||
			prover.Path != verifier.Path ||
			prover.Opening.Index != verifier.Opening.Index ||
			prover.Opening.Vector[prover.Opening.Index] != verifier.Opening.Value {
			t.Fatalf("query %d prover/verifier mismatch", index)
		}
		proverQueries[index] = prover.Opening
		verifierQueries[index] = verifier.Opening
	}

	engine, err := backend.NewAggregateOpeningEngine(
		context.Background(),
		testAuthstateAggregateOpeningLimits(),
	)
	if err != nil {
		t.Fatalf("new opening engine: %v", err)
	}
	proof, err := engine.Open(context.Background(), proverQueries)
	if err != nil {
		t.Fatalf("open proof: %v", err)
	}
	if err := engine.Verify(context.Background(), proof, verifierQueries); err != nil {
		t.Fatalf("verify proof: %v", err)
	}
	wrong := append([]backend.AggregateVerifierQuery(nil), verifierQueries...)
	wrong[0].Value[0]++
	if err := engine.Verify(context.Background(), proof, wrong); err == nil {
		t.Fatal("accepted proof under changed reconstructed evaluation")
	}
}

func TestAggregateVerifierQueriesTraverseEveryInternalDepth(t *testing.T) {
	t.Parallel()

	left := testKey(1, 0)
	left[1], left[2] = 2, 3
	right := testKey(1, 0)
	right[1], right[2] = 2, 4
	snapshot := newTestSnapshot(t, []Entry{
		{Key: left, Value: testValue(1)},
		{Key: right, Value: testValue(2)},
	})
	material, err := snapshot.ProofMaterial(
		context.Background(),
		[]Key{left},
		testProofMaterialLimits(),
	)
	if err != nil {
		t.Fatalf("proof material: %v", err)
	}
	queries, err := material.AggregateVerifierQueries(
		context.Background(),
		testAggregateVerifierQueryLimits(),
	)
	if err != nil {
		t.Fatalf("verifier queries: %v", err)
	}

	want := []struct {
		path  []byte
		index uint8
	}{
		{nil, 1},
		{[]byte{1}, 2},
		{[]byte{1, 2}, 3},
	}
	for _, expected := range want {
		found := false
		for _, query := range queries {
			if string(query.Path[:query.Length]) == string(expected.path) &&
				query.Opening.Index == expected.index {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing internal opening (%x, %d)", expected.path, expected.index)
		}
	}
}

func TestAggregateQueriesConsolidateSharedOpenings(t *testing.T) {
	t.Parallel()

	keys := []Key{
		testKey(0x00, 0x82),
		testKey(0x01, 0x82),
	}
	snapshot := newTestSnapshot(t, []Entry{
		{Key: testKey(0x00, 0x00), Value: testValue(0x11)},
		{Key: testKey(0x01, 0x00), Value: testValue(0x22)},
	})
	material, err := snapshot.ProofMaterial(
		context.Background(),
		keys,
		testProofMaterialLimits(),
	)
	if err != nil {
		t.Fatalf("proof material: %v", err)
	}
	proverRecords, err := snapshot.tree.AggregateProverQueries(
		context.Background(),
		keys,
		testCommittedAggregateQueryLimits(),
	)
	if err != nil {
		t.Fatalf("prover queries: %v", err)
	}
	verifierRecords, err := material.AggregateVerifierQueries(
		context.Background(),
		testAggregateVerifierQueryLimits(),
	)
	if err != nil {
		t.Fatalf("verifier queries: %v", err)
	}
	if len(proverRecords) != len(verifierRecords) {
		t.Fatalf("query counts = %d/%d", len(proverRecords), len(verifierRecords))
	}

	proverQueries := make([]backend.AggregateProverQuery, len(proverRecords))
	verifierQueries := make([]backend.AggregateVerifierQuery, len(verifierRecords))
	for index := range proverRecords {
		proverQueries[index] = proverRecords[index].Opening
		verifierQueries[index] = verifierRecords[index].Opening
	}
	engine, err := backend.NewAggregateOpeningEngine(
		context.Background(),
		testAuthstateAggregateOpeningLimits(),
	)
	if err != nil {
		t.Fatalf("new opening engine: %v", err)
	}
	proof, err := engine.Open(context.Background(), proverQueries)
	if err != nil {
		t.Fatalf("open consolidated proof: %v", err)
	}
	if err := engine.Verify(context.Background(), proof, verifierQueries); err != nil {
		t.Fatalf("verify consolidated proof: %v", err)
	}
}

func TestAggregateVerifierQueriesRejectSurplusCommitment(t *testing.T) {
	t.Parallel()

	key := testKey(0x00, 0x00)
	snapshot := newTestSnapshot(t, []Entry{{Key: key, Value: testValue(0x11)}})
	material, err := snapshot.ProofMaterial(
		context.Background(),
		[]Key{key},
		testProofMaterialLimits(),
	)
	if err != nil {
		t.Fatalf("proof material: %v", err)
	}
	material.commitments = append(
		material.commitments,
		mustPathCommitment(t, []byte{0x09}, testProofCommitment(t)),
	)

	if _, err := material.AggregateVerifierQueries(
		context.Background(),
		testAggregateVerifierQueryLimits(),
	); !errors.Is(err, errInvalidProofMaterial) {
		t.Fatalf("surplus commitment error = %v", err)
	}
}

func TestAggregateVerifierQueriesOpenDifferentStem(t *testing.T) {
	t.Parallel()

	stored := testKey(0, 0)
	stored[1] = 1
	queried := testKey(0, 0)
	queried[1] = 2
	snapshot := newTestSnapshot(t, []Entry{{Key: stored, Value: testValue(1)}})
	material, err := snapshot.ProofMaterial(
		context.Background(),
		[]Key{queried},
		testProofMaterialLimits(),
	)
	if err != nil {
		t.Fatalf("proof material: %v", err)
	}
	queries, err := material.AggregateVerifierQueries(
		context.Background(),
		testAggregateVerifierQueryLimits(),
	)
	if err != nil {
		t.Fatalf("different-stem queries: %v", err)
	}
	if len(queries) != 3 || queries[1].Opening.Value != extensionMarkerScalar() {
		t.Fatalf("different-stem query set = %#v", queries)
	}
}

func TestAggregateVerifierQueriesPreserveCancellation(t *testing.T) {
	t.Parallel()

	key := testKey(0x00, 0x00)
	material, err := newTestSnapshot(t, []Entry{{Key: key, Value: testValue(1)}}).ProofMaterial(
		context.Background(),
		[]Key{key},
		testProofMaterialLimits(),
	)
	if err != nil {
		t.Fatalf("proof material: %v", err)
	}
	observed := false
	for successfulChecks := 1; successfulChecks < 80; successfulChecks++ {
		_, err := material.AggregateVerifierQueries(
			&stepContext{successfulChecks: successfulChecks},
			testAggregateVerifierQueryLimits(),
		)
		if err == nil {
			break
		}
		observed = true
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation after %d checks = %v", successfulChecks, err)
		}
	}
	if !observed {
		t.Fatal("no cancellation boundary was exercised")
	}
}

func TestAggregateVerifierQueriesRejectInvalidInputsAndResources(t *testing.T) {
	t.Parallel()

	if _, err := (ProofMaterial{}).AggregateVerifierQueries(
		context.Background(),
		testAggregateVerifierQueryLimits(),
	); !errors.Is(err, errInvalidProofMaterial) {
		t.Fatalf("invalid material error = %v", err)
	}
	key := testKey(0, 0)
	material, err := newTestSnapshot(t, []Entry{{Key: key, Value: testValue(1)}}).ProofMaterial(
		context.Background(),
		[]Key{key},
		testProofMaterialLimits(),
	)
	if err != nil {
		t.Fatalf("proof material: %v", err)
	}
	var missingContext context.Context
	if _, err := material.AggregateVerifierQueries(
		missingContext,
		testAggregateVerifierQueryLimits(),
	); !errors.Is(err, errInvalidTreeProofContext) {
		t.Fatalf("nil context error = %v", err)
	}
	if _, err := material.AggregateVerifierQueries(
		context.Background(),
		AggregateVerifierQueryLimits{},
	); !errors.Is(err, errInvalidAggregateVerifierQueryLimits) {
		t.Fatalf("invalid limits error = %v", err)
	}
	validLimits := testAggregateVerifierQueryLimits()
	invalidLimits := map[string]AggregateVerifierQueryLimits{
		"queries zero": {
			MaxTemporaryBytes: validLimits.MaxTemporaryBytes,
		},
		"queries above maximum": {
			MaxQueries:        maxAggregateVerifierQueries + 1,
			MaxTemporaryBytes: validLimits.MaxTemporaryBytes,
		},
		"temporary bytes zero": {
			MaxQueries: validLimits.MaxQueries,
		},
	}
	for name, limits := range invalidLimits {
		t.Run(name, func(t *testing.T) {
			if _, err := material.AggregateVerifierQueries(
				context.Background(),
				limits,
			); !errors.Is(err, errInvalidAggregateVerifierQueryLimits) {
				t.Fatalf("invalid limits error = %v", err)
			}
		})
	}
	maximum := validLimits
	maximum.MaxQueries = maxAggregateVerifierQueries
	if _, err := material.AggregateVerifierQueries(context.Background(), maximum); err != nil {
		t.Fatalf("maximum query limit: %v", err)
	}

	temporary := testAggregateVerifierQueryLimits()
	temporary.MaxTemporaryBytes = 1
	_, err = material.AggregateVerifierQueries(context.Background(), temporary)
	assertAggregateVerifierResourceError(
		t,
		err,
		AggregateVerifierQueryResourceTemporaryBytes,
		1,
		aggregateVerifierQueriesPerClaim*2*aggregateVerifierQueryWorkingByte,
	)
	queries := testAggregateVerifierQueryLimits()
	queries.MaxQueries = 1
	_, err = material.AggregateVerifierQueries(context.Background(), queries)
	assertAggregateVerifierResourceError(
		t,
		err,
		AggregateVerifierQueryResourceQueries,
		1,
		2,
	)

	corrupt := material
	corrupt.stemPaths[0].stem[0]++
	if _, err := corrupt.AggregateVerifierQueries(
		context.Background(),
		testAggregateVerifierQueryLimits(),
	); !errors.Is(err, errInvalidProofMaterial) {
		t.Fatalf("invalid topology error = %v", err)
	}
}

func TestAggregateVerifierQueryHelpersRejectInvalidState(t *testing.T) {
	t.Parallel()

	commitment := testProofCommitment(t)
	path := aggregateVerifierPath{path: [32]byte{1}, length: 1}
	collector := aggregateVerifierCollector{
		ctx:         context.Background(),
		limits:      testAggregateVerifierQueryLimits(),
		commitments: map[aggregateVerifierPath]backend.VectorCommitment{},
		queryByID:   map[aggregateVerifierIdentity]int{},
	}
	if err := collector.appendValue(path, 1, [32]byte{}); !errors.Is(err, errInvalidProofMaterial) {
		t.Fatalf("missing path error = %v", err)
	}
	collector.commitments[path] = commitment
	if err := collector.appendValue(path, 1, [32]byte{1}); err != nil {
		t.Fatalf("append first value: %v", err)
	}
	if err := collector.appendValue(path, 1, [32]byte{2}); !errors.Is(err, errInvalidAggregateVerifierQuery) {
		t.Fatalf("conflicting duplicate error = %v", err)
	}

	invalidChild := path
	invalidChild.path[invalidChild.length] = 9
	invalidChild.length++
	collector.commitments[invalidChild] = backend.VectorCommitment{}
	if err := collector.appendChild(path, 9); !errors.Is(err, errInvalidProofMaterial) {
		t.Fatalf("invalid child commitment error = %v", err)
	}

	valid := AggregateVerifierQuery{
		Opening: backend.AggregateVerifierQuery{
			Commitment: commitment,
			Value:      [32]byte{1},
			Index:      7,
		},
	}
	var missingContext context.Context
	if _, err := consolidateAggregateVerifierQueries(
		missingContext,
		[]AggregateVerifierQuery{valid},
	); !errors.Is(err, errInvalidTreeProofContext) {
		t.Fatalf("consolidation context error = %v", err)
	}
	invalid := valid
	invalid.Opening.Commitment = backend.VectorCommitment{}
	if _, err := consolidateAggregateVerifierQueries(
		context.Background(),
		[]AggregateVerifierQuery{invalid},
	); !errors.Is(err, errInvalidAggregateVerifierQuery) {
		t.Fatalf("invalid consolidation commitment error = %v", err)
	}
	conflict := valid
	conflict.Opening.Value[0]++
	if _, err := consolidateAggregateVerifierQueries(
		context.Background(),
		[]AggregateVerifierQuery{valid, conflict},
	); !errors.Is(err, errInvalidAggregateVerifierQuery) {
		t.Fatalf("conflicting consolidation error = %v", err)
	}
	unique := valid
	unique.Opening.Index++
	retained, err := consolidateAggregateVerifierQueries(
		context.Background(),
		[]AggregateVerifierQuery{valid, valid, unique},
	)
	if err != nil {
		t.Fatalf("consolidate duplicate followed by unique query: %v", err)
	}
	if len(retained) != 2 || retained[1].Opening.Index != unique.Opening.Index {
		t.Fatalf("retained queries = %#v", retained)
	}
	if err := checkAggregateVerifierQueryResource(
		AggregateVerifierQueryResourceQueries,
		1,
		1,
	); err != nil {
		t.Fatalf("exact query resource: %v", err)
	}
}

func testCommittedAggregateQueryLimits() committedtree.AggregateProverQueryLimits {
	return committedtree.AggregateProverQueryLimits{
		MaxKeys:           64,
		MaxQueries:        1024,
		MaxNodeReads:      1024,
		MaxTemporaryBytes: 16 << 20,
	}
}

func testAggregateVerifierQueryLimits() AggregateVerifierQueryLimits {
	return AggregateVerifierQueryLimits{
		MaxQueries:        1024,
		MaxTemporaryBytes: 16 << 20,
	}
}

func testAuthstateAggregateOpeningLimits() backend.AggregateOpeningLimits {
	return backend.AggregateOpeningLimits{
		MaxGeneratorDerivations: backend.VectorWidth,
		MaxPrecomputedPoints:    backend.VectorWidth,
		MaxQueries:              1024,
		MaxScalarDecodes:        1024 * backend.VectorWidth,
		MaxMSMTerms:             2048 * backend.VectorWidth,
		MaxTemporaryBytes:       1 << 30,
		MaxWorkers:              uint32(runtime.NumCPU()),
	}
}

func assertAggregateVerifierResourceError(
	t testing.TB,
	err error,
	resource AggregateVerifierQueryResource,
	limit uint64,
	actual uint64,
) {
	t.Helper()

	var resourceErr *AggregateVerifierQueryResourceError
	if !errors.As(err, &resourceErr) ||
		resourceErr.Resource != resource ||
		resourceErr.Limit != limit ||
		resourceErr.Actual != actual ||
		!errors.Is(err, errAggregateVerifierQueryResource) ||
		resourceErr.Unwrap() != errAggregateVerifierQueryResource ||
		resourceErr.Error() == "" {
		t.Fatalf("resource error = %v", err)
	}
}
