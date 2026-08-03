package authstate

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/faustbrian/golib/pkg/verkle-tree/internal/backend"
	"github.com/faustbrian/golib/pkg/verkle-tree/internal/committedtree"
	internalprofile "github.com/faustbrian/golib/pkg/verkle-tree/internal/profile"
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

func TestProofEngineProvesCompleteTopologyDeletionTransition(t *testing.T) {
	t.Parallel()

	deleted := testKey(0x29, 0x01)
	snapshot := newTestSnapshot(t, []Entry{
		{Key: deleted, Value: testValue(1)},
	})
	engine := newTestProofEngine(t)
	proof, err := engine.ProveUpdates(
		context.Background(),
		snapshot,
		[]Update{Delete(deleted)},
		topologyProofGenerationLimits(),
	)
	if err != nil {
		t.Fatalf("prove topology deletion transition: %v", err)
	}
	if len(proof.claims.claims) != backend.VectorWidth {
		t.Fatalf(
			"topology deletion claims = %d, want %d",
			len(proof.claims.claims), backend.VectorWidth,
		)
	}
	updater, err := NewStatelessUpdater(
		context.Background(),
		testAuthstateAggregateOpeningLimits(),
		testCommitmentLimits(),
	)
	if err != nil {
		t.Fatalf("new stateless updater: %v", err)
	}
	got, err := updater.Apply(
		context.Background(), proof, []Update{Delete(deleted)},
		topologyProofVerificationLimits(), topologyStatelessUpdateLimits(),
	)
	if err != nil {
		t.Fatalf("apply generated topology deletion proof: %v", err)
	}
	wantSnapshot, _, err := snapshot.Apply(
		context.Background(), []Update{Delete(deleted)},
	)
	if err != nil {
		t.Fatalf("apply stateful topology deletion: %v", err)
	}
	want, err := wantSnapshot.RootContainer(context.Background())
	if err != nil {
		t.Fatalf("stateful topology deletion root: %v", err)
	}
	assertSameBackendRoot(t, got, want)
}

func TestProofEngineDerivesCanonicalUpdateProofKeys(t *testing.T) {
	t.Parallel()

	first := testKey(0x2a, 0x01)
	retained := testKey(0x2a, 0x02)
	absent := testKey(0x2b, 0x01)
	snapshot := newTestSnapshot(t, []Entry{
		{Key: first, Value: testValue(1)},
		{Key: retained, Value: testValue(2)},
	})
	engine := newTestProofEngine(t)

	proof, err := engine.ProveUpdates(
		context.Background(), snapshot, []Update{Delete(first)},
		testProofGenerationLimits(),
	)
	if err != nil {
		t.Fatalf("prove retained-stem deletion: %v", err)
	}
	if got, want := len(proof.claims.claims), 2; got != want {
		t.Fatalf("retained-stem claims = %d, want %d", got, want)
	}
	if proof.claims.claims[0].key != first || proof.claims.claims[1].key != retained {
		t.Fatal("retained-stem proof keys are not canonical")
	}

	proof, err = engine.ProveUpdates(
		context.Background(), snapshot, []Update{Delete(absent)},
		testProofGenerationLimits(),
	)
	if err != nil {
		t.Fatalf("prove absent deletion: %v", err)
	}
	if got, want := len(proof.claims.claims), 1; got != want ||
		proof.claims.claims[0].key != absent {
		t.Fatalf("absent-delete claims = %d, want one exact key", got)
	}

	setValue := testValue(3)
	forward, err := engine.ProveUpdates(
		context.Background(), snapshot,
		[]Update{Delete(first), Set(retained, setValue)},
		testProofGenerationLimits(),
	)
	if err != nil {
		t.Fatalf("prove mixed same-stem updates: %v", err)
	}
	reverse, err := engine.ProveUpdates(
		context.Background(), snapshot,
		[]Update{Set(retained, setValue), Delete(first)},
		testProofGenerationLimits(),
	)
	if err != nil {
		t.Fatalf("prove reordered mixed same-stem updates: %v", err)
	}
	forwardBytes, err := forward.Bytes(context.Background(), testTreeProofEncodingLimits())
	if err != nil {
		t.Fatalf("encode mixed update proof: %v", err)
	}
	reverseBytes, err := reverse.Bytes(context.Background(), testTreeProofEncodingLimits())
	if err != nil {
		t.Fatalf("encode reordered mixed update proof: %v", err)
	}
	if !bytes.Equal(forwardBytes, reverseBytes) {
		t.Fatal("reordered updates produced different canonical proofs")
	}
}

func TestProofEngineUpdateProofRejectsInvalidAndBoundedInputs(t *testing.T) {
	t.Parallel()

	key := testKey(0x2c, 0x01)
	snapshot := newTestSnapshot(t, []Entry{{Key: key, Value: testValue(1)}})
	engine := newTestProofEngine(t)
	updates := []Update{Delete(key)}
	var nilEngine *ProofEngine
	var nilContext context.Context

	for name, operation := range map[string]func() error{
		"engine": func() error {
			_, err := nilEngine.ProveUpdates(
				context.Background(), snapshot, updates, topologyProofGenerationLimits(),
			)
			return err
		},
		"context": func() error {
			_, err := engine.ProveUpdates(
				nilContext, snapshot, updates, topologyProofGenerationLimits(),
			)
			return err
		},
		"limits": func() error {
			_, err := engine.ProveUpdates(
				context.Background(), snapshot, updates, ProofGenerationLimits{},
			)
			return err
		},
		"snapshot": func() error {
			_, err := engine.ProveUpdates(
				context.Background(), Snapshot{}, updates, topologyProofGenerationLimits(),
			)
			return err
		},
		"empty updates": func() error {
			_, err := engine.ProveUpdates(
				context.Background(), snapshot, nil, topologyProofGenerationLimits(),
			)
			return err
		},
		"invalid update": func() error {
			_, err := engine.ProveUpdates(
				context.Background(), snapshot, []Update{{}}, topologyProofGenerationLimits(),
			)
			return err
		},
		"duplicate update": func() error {
			_, err := engine.ProveUpdates(
				context.Background(), snapshot, []Update{Delete(key), Delete(key)},
				topologyProofGenerationLimits(),
			)
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := operation(); err == nil {
				t.Fatal("invalid update proof input was accepted")
			}
		})
	}

	keyLimits := topologyProofGenerationLimits()
	keyLimits.Material.MaxKeys = 1
	keyLimits.ProverQueries.MaxKeys = 1
	if _, err := engine.ProveUpdates(
		context.Background(), snapshot,
		[]Update{Delete(key), Delete(testKey(0x2d, 0x01))}, keyLimits,
	); !errors.Is(err, errProofMaterialResource) {
		t.Fatalf("initial key resource error = %v", err)
	}

	temporaryLimits := topologyProofGenerationLimits()
	temporaryLimits.Material.MaxTemporaryBytes = 2*proofMaterialKeyWorkingBytes - 1
	if _, err := engine.ProveUpdates(
		context.Background(), snapshot, updates, temporaryLimits,
	); !errors.Is(err, errProofMaterialResource) {
		t.Fatalf("initial temporary resource error = %v", err)
	}

	suffixLimits := topologyProofGenerationLimits()
	suffixLimits.Material.MaxKeys = backend.VectorWidth - 1
	suffixLimits.ProverQueries.MaxKeys = backend.VectorWidth - 1
	if _, err := engine.ProveUpdates(
		context.Background(), snapshot, updates, suffixLimits,
	); !errors.Is(err, errProofMaterialResource) {
		t.Fatalf("topology suffix resource error = %v", err)
	}

	pathLimits := topologyProofGenerationLimits()
	pathLimits.Material.MaxNodeReads = 1
	_, err := engine.ProveUpdates(
		context.Background(), snapshot, updates, pathLimits,
	)
	if !errors.Is(err, errProofMaterialResource) {
		t.Fatalf("topology path resource error = %v", err)
	}
	corruptSnapshot := snapshot
	corruptSnapshot.entries = append([]Entry(nil), snapshot.entries...)
	corruptKey := testKey(0x3c, 0x02)
	corruptSnapshot.entries[0].Key = corruptKey
	if _, err := engine.ProveUpdates(
		context.Background(), corruptSnapshot, []Update{Delete(corruptKey)},
		topologyProofGenerationLimits(),
	); !errors.Is(err, errInvalidProofMaterial) {
		t.Fatalf("non-present topology path error = %v", err)
	}

	collision := key
	collision[1]++
	deepSnapshot := newTestSnapshot(t, []Entry{
		{Key: key, Value: testValue(1)},
		{Key: collision, Value: testValue(2)},
	})
	parentLimits := topologyProofGenerationLimits()
	parentLimits.Material.MaxKeys = backend.VectorWidth
	parentLimits.ProverQueries.MaxKeys = backend.VectorWidth
	if _, err := engine.ProveUpdates(
		context.Background(), deepSnapshot, updates, parentLimits,
	); !errors.Is(err, errProofMaterialResource) {
		t.Fatalf("topology parent resource error = %v", err)
	}
}

func TestStatelessProofKeyHelpersEnforceBoundsAndCancellation(t *testing.T) {
	t.Parallel()

	key := testKey(0x2e, 0x02)
	keys := map[Key]struct{}{key: {}}
	if err := addStatelessProofKey(keys, key, 1, 1); err != nil {
		t.Fatalf("duplicate proof key: %v", err)
	}
	if err := addStatelessProofKey(
		keys, testKey(0x2e, 0x03), 1, 1<<20,
	); !errors.Is(err, errProofMaterialResource) {
		t.Fatalf("proof-key count error = %v", err)
	}
	if err := addStatelessProofKey(
		map[Key]struct{}{}, key, 1, 2*proofMaterialKeyWorkingBytes-1,
	); !errors.Is(err, errProofMaterialResource) {
		t.Fatalf("proof-key temporary error = %v", err)
	}

	stem := Stem(key[:31])
	entries := []Entry{
		{Key: testKey(0x2e, 0x01), Value: testValue(1)},
		{Key: key, Value: testValue(2)},
	}
	if _, _, err := statelessRetainedSnapshotKey(
		&stepContext{}, entries, []Update{Delete(key)}, stem,
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("retained-key cancellation error = %v", err)
	}
	if _, _, err := statelessRetainedSnapshotKey(
		&stepContext{successfulChecks: 1}, entries,
		[]Update{Delete(testKey(0x2e, 0x00))}, stem,
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("retained-key update-advance cancellation error = %v", err)
	}
	retained, found, err := statelessRetainedSnapshotKey(
		context.Background(), entries,
		[]Update{Delete(testKey(0x2e, 0x00)), Delete(testKey(0x2e, 0x01))},
		stem,
	)
	if err != nil || !found || retained != key {
		t.Fatalf("retained key = %x, found %v, error %v", retained, found, err)
	}
}

func TestStatelessSnapshotStemDepthUsesCanonicalNeighborCollisions(t *testing.T) {
	t.Parallel()

	singleton := testKey(0x50, 0x01)
	depth, found, err := statelessSnapshotStemDepth(
		context.Background(),
		[]Entry{{Key: singleton, Value: testValue(1)}},
		Stem(singleton[:31]),
	)
	if err != nil || !found || depth != 1 {
		t.Fatalf("singleton depth = %d, found %v, error %v", depth, found, err)
	}

	first := singleton
	first[1] = 0x10
	second := singleton
	second[1] = 0x20
	secondSuffix := second
	secondSuffix[31]++
	depth, found, err = statelessSnapshotStemDepth(
		context.Background(),
		[]Entry{
			{Key: first, Value: testValue(1)},
			{Key: second, Value: testValue(2)},
			{Key: secondSuffix, Value: testValue(3)},
		},
		Stem(second[:31]),
	)
	if err != nil || !found || depth != 2 {
		t.Fatalf("neighbor collision depth = %d, found %v, error %v", depth, found, err)
	}

	deepFirst := singleton
	deepSecond := singleton
	deepSecond[30] = 1
	depth, found, err = statelessSnapshotStemDepth(
		context.Background(),
		[]Entry{
			{Key: deepFirst, Value: testValue(1)},
			{Key: deepSecond, Value: testValue(2)},
		},
		Stem(deepFirst[:31]),
	)
	if err != nil || !found || depth != 31 {
		t.Fatalf("maximum collision depth = %d, found %v, error %v", depth, found, err)
	}

	missing := singleton
	missing[0]++
	if _, found, err := statelessSnapshotStemDepth(
		context.Background(),
		[]Entry{{Key: singleton, Value: testValue(1)}},
		Stem(missing[:31]),
	); err != nil || found {
		t.Fatalf("missing stem = found %v, error %v", found, err)
	}
	if _, _, err := statelessSnapshotStemDepth(
		&stepContext{},
		[]Entry{{Key: singleton, Value: testValue(1)}},
		Stem(singleton[:31]),
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("stem-depth cancellation error = %v", err)
	}
	secondSameStem := singleton
	secondSameStem[31]++
	if _, _, err := statelessSnapshotStemDepth(
		&stepContext{successfulChecks: 2},
		[]Entry{
			{Key: singleton, Value: testValue(1)},
			{Key: secondSameStem, Value: testValue(2)},
		},
		Stem(singleton[:31]),
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("stem-depth group cancellation error = %v", err)
	}
}

func TestProofEngineUpdateProofCancellationBoundaries(t *testing.T) {
	t.Parallel()

	absent := testKey(0x2f, 0x01)
	member := testKey(0x30, 0x01)
	retained := testKey(0x30, 0x02)
	snapshot := newTestSnapshot(t, []Entry{
		{Key: member, Value: testValue(1)},
		{Key: retained, Value: testValue(2)},
	})
	engine := newTestProofEngine(t)

	for successfulChecks := 0; successfulChecks <= 4; successfulChecks++ {
		_, err := engine.ProveUpdates(
			&stepContext{successfulChecks: successfulChecks},
			snapshot,
			[]Update{Delete(absent)},
			testProofGenerationLimits(),
		)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("absent deletion cancellation after %d checks = %v", successfulChecks, err)
		}
	}
	if _, err := engine.ProveUpdates(
		&stepContext{successfulChecks: 4}, snapshot,
		[]Update{Delete(member)}, testProofGenerationLimits(),
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("retained lookup cancellation error = %v", err)
	}

	retainedLimits := testProofGenerationLimits()
	retainedLimits.Material.MaxKeys = 1
	retainedLimits.ProverQueries.MaxKeys = 1
	if _, err := engine.ProveUpdates(
		context.Background(), snapshot, []Update{Delete(member)}, retainedLimits,
	); !errors.Is(err, errProofMaterialResource) {
		t.Fatalf("retained-key resource error = %v", err)
	}

	topologySnapshot := newTestSnapshot(t, []Entry{
		{Key: member, Value: testValue(1)},
	})
	if _, err := engine.ProveUpdates(
		&stepContext{successfulChecks: 4}, topologySnapshot,
		[]Update{Delete(member)}, topologyProofGenerationLimits(),
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("suffix disclosure cancellation error = %v", err)
	}

	collision := member
	collision[1]++
	deepSnapshot := newTestSnapshot(t, []Entry{
		{Key: member, Value: testValue(1)},
		{Key: collision, Value: testValue(2)},
	})
	for successfulChecks := 260; successfulChecks < 700; successfulChecks++ {
		_, err := engine.ProveUpdates(
			&stepContext{successfulChecks: successfulChecks},
			deepSnapshot,
			[]Update{Delete(member)},
			topologyProofGenerationLimits(),
		)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("parent disclosure cancellation after %d checks = %v", successfulChecks, err)
		}
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
		internalprofile.ExperimentalBandersnatchIPA256V0(),
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
