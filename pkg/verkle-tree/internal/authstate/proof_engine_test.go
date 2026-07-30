package authstate

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"

	verkletree "github.com/faustbrian/golib/pkg/verkle-tree"
	"github.com/faustbrian/golib/pkg/verkle-tree/internal/backend"
	"github.com/faustbrian/golib/pkg/verkle-tree/internal/committedtree"
)

func TestProofEngineGeneratesDeterministicVerifiableTreeProof(t *testing.T) {
	t.Parallel()

	snapshot := newTestSnapshot(t, []Entry{
		{Key: testKey(0x00, 0x00), Value: testValue(0x11)},
		{Key: testKey(0x00, 0x01), Value: testValue(0x22)},
		{Key: testKey(0x01, 0xff), Value: testValue(0x33)},
	})
	keys := []Key{
		testKey(0x01, 0xff),
		testKey(0x00, 0x82),
		testKey(0x02, 0x00),
		testKey(0x00, 0x00),
	}
	engine := newTestProofEngine(t)
	proof, err := engine.Prove(
		context.Background(),
		snapshot,
		keys,
		testProofGenerationLimits(),
	)
	if err != nil {
		t.Fatalf("generate proof: %v", err)
	}
	if err := engine.Verify(
		context.Background(),
		proof,
		testProofVerificationLimits(),
	); err != nil {
		t.Fatalf("verify proof: %v", err)
	}

	reversed := append([]Key(nil), keys...)
	for left, right := 0, len(reversed)-1; left < right; left, right = left+1, right-1 {
		reversed[left], reversed[right] = reversed[right], reversed[left]
	}
	repeated, err := engine.Prove(
		context.Background(),
		snapshot,
		reversed,
		testProofGenerationLimits(),
	)
	if err != nil {
		t.Fatalf("generate reordered proof: %v", err)
	}
	encoded, err := proof.Bytes(context.Background(), testTreeProofEncodingLimits())
	if err != nil {
		t.Fatalf("encode proof: %v", err)
	}
	repeatedBytes, err := repeated.Bytes(
		context.Background(),
		testTreeProofEncodingLimits(),
	)
	if err != nil {
		t.Fatalf("encode reordered proof: %v", err)
	}
	if !bytes.Equal(encoded, repeatedBytes) {
		t.Fatal("proof bytes depend on caller key order")
	}
	decoded, err := DecodeTreeProof(
		context.Background(),
		encoded,
		testTreeProofDecodingLimits(),
	)
	if err != nil {
		t.Fatalf("decode generated proof: %v", err)
	}
	if err := engine.Verify(
		context.Background(),
		decoded,
		testProofVerificationLimits(),
	); err != nil {
		t.Fatalf("verify decoded proof: %v", err)
	}
}

func TestProofEngineRejectsTamperedProofs(t *testing.T) {
	t.Parallel()

	key := testKey(0x00, 0x00)
	snapshot := newTestSnapshot(t, []Entry{{Key: key, Value: testValue(0x11)}})
	engine := newTestProofEngine(t)
	proof, err := engine.Prove(
		context.Background(),
		snapshot,
		[]Key{key},
		testProofGenerationLimits(),
	)
	if err != nil {
		t.Fatalf("generate proof: %v", err)
	}

	changedClaims := append([]Claim(nil), proof.claims.claims...)
	changedClaims[0].value[0]++
	claimSet, err := NewClaimSet(
		context.Background(),
		verkletree.ExperimentalBandersnatchIPA256V0(),
		changedClaims,
		testClaimLimits(),
	)
	if err != nil {
		t.Fatalf("changed claims: %v", err)
	}
	tampered, err := NewTreeProof(
		context.Background(),
		proof.root,
		claimSet,
		proof.stemPaths,
		proof.commitments,
		proof.opening,
		testTreeProofLimits(),
	)
	if err != nil {
		t.Fatalf("construct tampered proof: %v", err)
	}
	if err := engine.Verify(
		context.Background(),
		tampered,
		testProofVerificationLimits(),
	); !errors.Is(err, errProofVerification) {
		t.Fatalf("tampered claim error = %v", err)
	}

	other := newTestSnapshot(t, []Entry{{Key: key, Value: testValue(0x99)}})
	otherRoot, err := other.RootContainer(context.Background())
	if err != nil {
		t.Fatalf("other root: %v", err)
	}
	tampered, err = NewTreeProof(
		context.Background(),
		otherRoot,
		proof.claims,
		proof.stemPaths,
		proof.commitments,
		proof.opening,
		testTreeProofLimits(),
	)
	if err != nil {
		t.Fatalf("construct root-tampered proof: %v", err)
	}
	if err := engine.Verify(
		context.Background(),
		tampered,
		testProofVerificationLimits(),
	); !errors.Is(err, errProofVerification) {
		t.Fatalf("tampered root error = %v", err)
	}
}

func TestProofEngineRejectsInvalidInputsBeforeOpeningWork(t *testing.T) {
	t.Parallel()

	var missingContext context.Context
	if _, err := NewProofEngine(
		missingContext,
		testAuthstateAggregateOpeningLimits(),
	); !errors.Is(err, errInvalidTreeProofContext) {
		t.Fatalf("nil constructor context error = %v", err)
	}
	if _, err := NewProofEngine(
		context.Background(),
		backend.AggregateOpeningLimits{},
	); err == nil {
		t.Fatal("invalid opening limits were accepted")
	}

	engine := newTestProofEngine(t)
	snapshot := newTestSnapshot(t, []Entry{{Key: testKey(0, 0), Value: testValue(1)}})
	var nilEngine *ProofEngine
	if _, err := nilEngine.Prove(
		context.Background(),
		snapshot,
		[]Key{testKey(0, 0)},
		testProofGenerationLimits(),
	); !errors.Is(err, errInvalidProofEngine) {
		t.Fatalf("nil prover error = %v", err)
	}
	if _, err := engine.Prove(
		missingContext,
		snapshot,
		[]Key{testKey(0, 0)},
		testProofGenerationLimits(),
	); !errors.Is(err, errInvalidTreeProofContext) {
		t.Fatalf("nil prove context error = %v", err)
	}
	if _, err := engine.Prove(
		context.Background(),
		snapshot,
		[]Key{testKey(0, 0)},
		ProofGenerationLimits{},
	); !errors.Is(err, errInvalidProofGenerationLimits) {
		t.Fatalf("invalid generation limits error = %v", err)
	}
	invalidGenerationLimits := map[string]func(*ProofGenerationLimits){
		"material": func(limits *ProofGenerationLimits) {
			limits.Material = ProofMaterialLimits{}
		},
		"prover queries": func(limits *ProofGenerationLimits) {
			limits.ProverQueries = committedtree.AggregateProverQueryLimits{}
		},
		"verifier queries": func(limits *ProofGenerationLimits) {
			limits.VerifierQueries = AggregateVerifierQueryLimits{}
		},
		"tree proof": func(limits *ProofGenerationLimits) {
			limits.TreeProof = TreeProofLimits{}
		},
	}
	for name, invalidate := range invalidGenerationLimits {
		t.Run(name, func(t *testing.T) {
			limits := testProofGenerationLimits()
			invalidate(&limits)
			if _, err := engine.Prove(
				context.Background(),
				snapshot,
				[]Key{testKey(0, 0)},
				limits,
			); !errors.Is(err, errInvalidProofGenerationLimits) {
				t.Fatalf("invalid generation limits error = %v", err)
			}
		})
	}
	if _, err := engine.Prove(
		context.Background(),
		Snapshot{},
		[]Key{testKey(0, 0)},
		testProofGenerationLimits(),
	); !errors.Is(err, errInvalidSnapshot) {
		t.Fatalf("invalid snapshot error = %v", err)
	}

	if err := nilEngine.Verify(
		context.Background(),
		TreeProof{},
		testProofVerificationLimits(),
	); !errors.Is(err, errInvalidProofEngine) {
		t.Fatalf("nil verifier error = %v", err)
	}
	if err := engine.Verify(
		missingContext,
		TreeProof{},
		testProofVerificationLimits(),
	); !errors.Is(err, errInvalidTreeProofContext) {
		t.Fatalf("nil verify context error = %v", err)
	}
	if err := engine.Verify(
		context.Background(),
		TreeProof{},
		ProofVerificationLimits{},
	); !errors.Is(err, errInvalidProofVerificationLimits) {
		t.Fatalf("invalid verification limits error = %v", err)
	}
	if err := engine.Verify(
		context.Background(),
		TreeProof{},
		testProofVerificationLimits(),
	); !errors.Is(err, errInvalidTreeProof) {
		t.Fatalf("invalid proof error = %v", err)
	}
}

func TestProofEnginePropagatesBoundedStageFailures(t *testing.T) {
	t.Parallel()

	key := testKey(0, 0)
	snapshot := newTestSnapshot(t, []Entry{{Key: key, Value: testValue(1)}})
	engine := newTestProofEngine(t)
	if _, err := engine.Prove(
		context.Background(),
		snapshot,
		nil,
		testProofGenerationLimits(),
	); !errors.Is(err, errInvalidProofMaterial) {
		t.Fatalf("material-stage error = %v", err)
	}

	proverLimits := testProofGenerationLimits()
	proverLimits.ProverQueries.MaxQueries = 1
	if _, err := engine.Prove(
		context.Background(),
		snapshot,
		[]Key{key},
		proverLimits,
	); err == nil {
		t.Fatal("prover-query resource exhaustion was accepted")
	}
	verifierLimits := testProofGenerationLimits()
	verifierLimits.VerifierQueries.MaxQueries = 1
	if _, err := engine.Prove(
		context.Background(),
		snapshot,
		[]Key{key},
		verifierLimits,
	); !errors.Is(err, errAggregateVerifierQueryResource) {
		t.Fatalf("verifier-query resource error = %v", err)
	}

	corrupt := snapshot
	corrupt.entries = append([]Entry(nil), snapshot.entries...)
	corrupt.entries[0].Value[0]++
	if _, err := engine.Prove(
		context.Background(),
		corrupt,
		[]Key{key},
		testProofGenerationLimits(),
	); !errors.Is(err, errProofGeneration) {
		t.Fatalf("query divergence error = %v", err)
	}

	openingLimits := testAuthstateAggregateOpeningLimits()
	openingLimits.MaxQueries = 1
	boundedEngine, err := NewProofEngine(context.Background(), openingLimits)
	if err != nil {
		t.Fatalf("new bounded proof engine: %v", err)
	}
	if _, err := boundedEngine.Prove(
		context.Background(),
		snapshot,
		[]Key{key},
		testProofGenerationLimits(),
	); !errors.Is(err, errProofGeneration) {
		t.Fatalf("opening-stage resource error = %v", err)
	}
}

func TestProofEngineVerificationPreservesMaterialAndCancellationFailures(t *testing.T) {
	t.Parallel()

	key := testKey(0, 0)
	snapshot := newTestSnapshot(t, []Entry{{Key: key, Value: testValue(1)}})
	engine := newTestProofEngine(t)
	proof, err := engine.Prove(
		context.Background(),
		snapshot,
		[]Key{key},
		testProofGenerationLimits(),
	)
	if err != nil {
		t.Fatalf("generate proof: %v", err)
	}
	corrupt := proof
	corrupt.commitments = corrupt.commitments[:len(corrupt.commitments)-1]
	if err := engine.Verify(
		context.Background(),
		corrupt,
		testProofVerificationLimits(),
	); !errors.Is(err, errInvalidProofMaterial) {
		t.Fatalf("incomplete material error = %v", err)
	}

	observed := false
	for successfulChecks := 1; successfulChecks < 160; successfulChecks++ {
		err := engine.Verify(
			&stepContext{successfulChecks: successfulChecks},
			proof,
			testProofVerificationLimits(),
		)
		if err == nil {
			break
		}
		observed = true
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("verification cancellation after %d checks = %v", successfulChecks, err)
		}
	}
	if !observed {
		t.Fatal("no verifier cancellation boundary was exercised")
	}
}

func TestProofEngineSupportsConcurrentImmutableUse(t *testing.T) {
	t.Parallel()

	key := testKey(0, 0)
	snapshot := newTestSnapshot(t, []Entry{{Key: key, Value: testValue(1)}})
	engine := newTestProofEngine(t)
	proof, err := engine.Prove(
		context.Background(),
		snapshot,
		[]Key{key},
		testProofGenerationLimits(),
	)
	if err != nil {
		t.Fatalf("generate proof: %v", err)
	}
	wantBytes, err := proof.Bytes(
		context.Background(),
		testTreeProofEncodingLimits(),
	)
	if err != nil {
		t.Fatalf("encode reference proof: %v", err)
	}

	const workers = 8
	var group sync.WaitGroup
	errorsByWorker := make(chan error, workers*2)
	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			generated, proveErr := engine.Prove(
				context.Background(),
				snapshot,
				[]Key{key},
				testProofGenerationLimits(),
			)
			if proveErr != nil {
				errorsByWorker <- proveErr
				return
			}
			generatedBytes, encodeErr := generated.Bytes(
				context.Background(),
				testTreeProofEncodingLimits(),
			)
			if encodeErr != nil {
				errorsByWorker <- encodeErr
				return
			}
			if !bytes.Equal(generatedBytes, wantBytes) {
				errorsByWorker <- errors.New("concurrent proof bytes differ")
				return
			}
			if verifyErr := engine.Verify(
				context.Background(),
				generated,
				testProofVerificationLimits(),
			); verifyErr != nil {
				errorsByWorker <- verifyErr
			}
		}()
	}
	group.Wait()
	close(errorsByWorker)
	for workerErr := range errorsByWorker {
		t.Errorf("concurrent proof operation: %v", workerErr)
	}
	if err := engine.Verify(
		context.Background(),
		proof,
		testProofVerificationLimits(),
	); err != nil {
		t.Fatalf("original proof after concurrent use: %v", err)
	}
}

func TestMatchAggregateQueriesRejectsDivergence(t *testing.T) {
	t.Parallel()

	commitment := testProofCommitment(t)
	var vector backend.Vector
	vector[7][0] = 1
	prover := []committedtree.AggregateProverQuery{{
		Length: 1,
		Path:   [32]byte{9},
		Opening: backend.AggregateProverQuery{
			Commitment: commitment,
			Vector:     vector,
			Index:      7,
		},
	}}
	verifier := []AggregateVerifierQuery{{
		Length: 1,
		Path:   [32]byte{9},
		Opening: backend.AggregateVerifierQuery{
			Commitment: commitment,
			Value:      vector[7],
			Index:      7,
		},
	}}
	if got, err := matchAggregateQueries(context.Background(), prover, verifier); err != nil || len(got) != 1 {
		t.Fatalf("matching queries = %d, error = %v", len(got), err)
	}
	if _, err := matchAggregateQueries(context.Background(), prover, nil); !errors.Is(err, errProofGeneration) {
		t.Fatalf("count mismatch error = %v", err)
	}
	if _, err := matchAggregateQueries(missingContext(), prover, verifier); !errors.Is(err, errInvalidTreeProofContext) {
		t.Fatalf("context error = %v", err)
	}

	mutations := []func([]committedtree.AggregateProverQuery, []AggregateVerifierQuery){
		func(prover []committedtree.AggregateProverQuery, _ []AggregateVerifierQuery) {
			prover[0].Opening.Commitment = backend.VectorCommitment{}
		},
		func(_ []committedtree.AggregateProverQuery, verifier []AggregateVerifierQuery) {
			verifier[0].Opening.Commitment = backend.VectorCommitment{}
		},
		func(prover []committedtree.AggregateProverQuery, _ []AggregateVerifierQuery) {
			prover[0].Path[0]++
		},
		func(prover []committedtree.AggregateProverQuery, _ []AggregateVerifierQuery) {
			prover[0].Length++
		},
		func(prover []committedtree.AggregateProverQuery, _ []AggregateVerifierQuery) {
			prover[0].Opening.Index++
		},
		func(prover []committedtree.AggregateProverQuery, _ []AggregateVerifierQuery) {
			prover[0].Opening.Vector[7][0]++
		},
	}
	for index, mutate := range mutations {
		changedProver := append([]committedtree.AggregateProverQuery(nil), prover...)
		changedVerifier := append([]AggregateVerifierQuery(nil), verifier...)
		mutate(changedProver, changedVerifier)
		if _, err := matchAggregateQueries(
			context.Background(),
			changedProver,
			changedVerifier,
		); !errors.Is(err, errProofGeneration) {
			t.Fatalf("mutation %d error = %v", index, err)
		}
	}
}

func missingContext() context.Context {
	return nil
}

func newTestProofEngine(t testing.TB) *ProofEngine {
	t.Helper()

	engine, err := NewProofEngine(
		context.Background(),
		testAuthstateAggregateOpeningLimits(),
	)
	if err != nil {
		t.Fatalf("new proof engine: %v", err)
	}

	return engine
}

func testProofGenerationLimits() ProofGenerationLimits {
	return ProofGenerationLimits{
		Material:        testProofMaterialLimits(),
		ProverQueries:   testCommittedAggregateQueryLimits(),
		VerifierQueries: testAggregateVerifierQueryLimits(),
		TreeProof:       testTreeProofLimits(),
	}
}

func testProofVerificationLimits() ProofVerificationLimits {
	return ProofVerificationLimits{
		VerifierQueries: testAggregateVerifierQueryLimits(),
	}
}
