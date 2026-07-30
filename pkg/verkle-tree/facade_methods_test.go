package verkletree

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/verkle-tree/internal/authstate"
)

type cancellingContext struct {
	remaining int
}

func (ctx *cancellingContext) Deadline() (time.Time, bool) {
	return time.Time{}, false
}

func (ctx *cancellingContext) Done() <-chan struct{} {
	return nil
}

func (ctx *cancellingContext) Err() error {
	if ctx.remaining == 0 {
		return context.Canceled
	}
	ctx.remaining--

	return nil
}

func (ctx *cancellingContext) Value(any) any {
	return nil
}

func TestFacadeRootRejectsHostileAndZeroValues(t *testing.T) {
	t.Parallel()

	var nilContext context.Context
	limits := RootDecodingLimits{MaxRootBytes: RootSize, MaxPointDecodes: 1}
	if _, err := DecodeRoot(nilContext, nil, limits); !errors.Is(err, ErrInvalidContext) {
		t.Fatalf("nil context = %v", err)
	}
	if _, err := DecodeRoot(context.Background(), nil, RootDecodingLimits{}); !errors.Is(err, ErrInvalidLimits) {
		t.Fatalf("zero limits = %v", err)
	}
	if _, err := DecodeRoot(context.Background(), make([]byte, RootSize+1), limits); !errors.Is(err, ErrResourceExhausted) {
		t.Fatalf("oversized root = %v", err)
	}
	if _, err := DecodeRoot(context.Background(), make([]byte, RootSize), limits); !errors.Is(err, ErrInvalidRoot) {
		t.Fatalf("malformed root = %v", err)
	}
	var zero Root
	if _, err := zero.Bytes(); !errors.Is(err, ErrInvalidRoot) {
		t.Fatalf("zero bytes = %v", err)
	}
	if _, err := zero.Profile(); !errors.Is(err, ErrInvalidRoot) {
		t.Fatalf("zero profile = %v", err)
	}
	if _, err := zero.IsEmpty(); !errors.Is(err, ErrInvalidRoot) {
		t.Fatalf("zero empty = %v", err)
	}

	empty, err := NewSnapshot(
		context.Background(),
		ExperimentalBandersnatchIPA256V0(),
		nil,
		testFacadeSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("empty snapshot: %v", err)
	}
	emptyRoot, err := empty.Root()
	if err != nil {
		t.Fatalf("empty root: %v", err)
	}
	isEmpty, err := emptyRoot.IsEmpty()
	if err != nil || !isEmpty {
		t.Fatalf("empty root status = %t, %v", isEmpty, err)
	}
	encoded, err := emptyRoot.Bytes()
	if err != nil {
		t.Fatalf("empty root bytes: %v", err)
	}
	encoded[4] = 0xff
	if _, err := DecodeRoot(context.Background(), encoded[:], limits); !errors.Is(err, ErrUnsupportedProfile) {
		t.Fatalf("wrong profile = %v", err)
	}
	encoded, _ = emptyRoot.Bytes()
	encoded[9] = 2
	if _, err := DecodeRoot(
		context.Background(),
		encoded[:],
		RootDecodingLimits{MaxRootBytes: RootSize},
	); !errors.Is(err, ErrResourceExhausted) {
		t.Fatalf("point budget = %v", err)
	}
}

func TestFacadeSnapshotRejectsEveryInvalidOwnershipState(t *testing.T) {
	t.Parallel()

	var nilContext context.Context
	if _, err := NewSnapshot(
		&cancellingContext{remaining: 1},
		ExperimentalBandersnatchIPA256V0(),
		[]Entry{{}},
		testFacadeSnapshotLimits(),
	); !errors.Is(err, ErrCancelled) {
		t.Fatalf("entry-copy cancellation = %v", err)
	}
	var zero Snapshot
	if _, err := zero.Root(); !errors.Is(err, ErrInvalidSnapshot) {
		t.Fatalf("zero root = %v", err)
	}
	if _, _, err := zero.Apply(context.Background(), nil); !errors.Is(err, ErrInvalidSnapshot) {
		t.Fatalf("zero apply = %v", err)
	}
	forged := Snapshot{valid: true}
	if _, _, err := forged.Get(context.Background(), Key{}); !errors.Is(err, ErrCryptographic) {
		t.Fatalf("forged get = %v", err)
	}
	if _, err := forged.Root(); !errors.Is(err, ErrCryptographic) {
		t.Fatalf("forged root = %v", err)
	}
	if _, _, err := forged.Apply(
		context.Background(),
		[]Update{Set(Key{}, Value{})},
	); !errors.Is(err, ErrCryptographic) {
		t.Fatalf("forged apply = %v", err)
	}
	snapshot, err := NewSnapshot(
		context.Background(),
		ExperimentalBandersnatchIPA256V0(),
		nil,
		testFacadeSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("new snapshot: %v", err)
	}
	if _, _, err := snapshot.Get(nilContext, Key{}); !errors.Is(err, ErrInvalidContext) {
		t.Fatalf("nil get context = %v", err)
	}
	if _, _, err := snapshot.Apply(nilContext, nil); !errors.Is(err, ErrInvalidContext) {
		t.Fatalf("nil apply context = %v", err)
	}
	if _, _, err := snapshot.Apply(
		&cancellingContext{remaining: 1},
		[]Update{Set(Key{}, Value{})},
	); !errors.Is(err, ErrCancelled) {
		t.Fatalf("update-copy cancellation = %v", err)
	}
	invalidDelete := Delete(Key{})
	invalidDelete.value[0] = 1
	if _, _, err := snapshot.Apply(
		context.Background(),
		[]Update{invalidDelete},
	); !errors.Is(err, ErrInvalidUpdate) {
		t.Fatalf("invalid delete = %v", err)
	}
	if _, _, err := snapshot.Apply(
		context.Background(),
		[]Update{{}},
	); !errors.Is(err, ErrInvalidUpdate) {
		t.Fatalf("zero update = %v", err)
	}
	var transition Transition
	if _, err := transition.PreRoot(); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("zero pre-root = %v", err)
	}
	if _, err := transition.PostRoot(); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("zero post-root = %v", err)
	}
	forgedTransition := Transition{valid: true}
	if _, err := forgedTransition.PreRoot(); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("forged pre-root = %v", err)
	}
	if _, err := forgedTransition.PostRoot(); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("forged post-root = %v", err)
	}
}

func TestFacadeProofRejectsEveryInvalidOwnershipState(t *testing.T) {
	t.Parallel()

	var nilContext context.Context
	if err := (ProofGenerationLimits{}).validate(); !errors.Is(err, ErrInvalidLimits) {
		t.Fatalf("generation limits = %v", err)
	}
	if err := (VerifierQueryLimits{}).validate(); !errors.Is(err, ErrInvalidLimits) {
		t.Fatalf("verifier limits = %v", err)
	}
	if err := (ProofEncodingLimits{}).validate(); !errors.Is(err, ErrInvalidLimits) {
		t.Fatalf("encoding limits = %v", err)
	}
	if err := (ProofDecodingLimits{}).validate(); !errors.Is(err, ErrInvalidLimits) {
		t.Fatalf("decoding limits = %v", err)
	}
	if _, err := NewProofEngine(
		nilContext,
		ExperimentalBandersnatchIPA256V0(),
		testFacadeOpeningLimits(),
	); !errors.Is(err, ErrInvalidContext) {
		t.Fatalf("nil engine context = %v", err)
	}
	resourceLimits := testFacadeOpeningLimits()
	resourceLimits.MaxGeneratorDerivations = 1
	if _, err := NewProofEngine(
		context.Background(),
		ExperimentalBandersnatchIPA256V0(),
		resourceLimits,
	); !errors.Is(err, ErrResourceExhausted) {
		t.Fatalf("engine resource = %v", err)
	}
	engine, snapshot, proof := testFacadeProof(t)
	if _, err := engine.Prove(
		context.Background(),
		Snapshot{},
		nil,
		testFacadeProofGenerationLimits(),
	); !errors.Is(err, ErrInvalidSnapshot) {
		t.Fatalf("invalid snapshot = %v", err)
	}
	if _, err := engine.Prove(
		nilContext,
		snapshot,
		nil,
		testFacadeProofGenerationLimits(),
	); !errors.Is(err, ErrInvalidContext) {
		t.Fatalf("nil prove context = %v", err)
	}
	if _, err := engine.Prove(
		context.Background(),
		snapshot,
		nil,
		ProofGenerationLimits{},
	); !errors.Is(err, ErrInvalidLimits) {
		t.Fatalf("prove limits = %v", err)
	}
	if _, err := engine.Prove(
		&cancellingContext{remaining: 1},
		snapshot,
		[]Key{{}},
		testFacadeProofGenerationLimits(),
	); !errors.Is(err, ErrCancelled) {
		t.Fatalf("prove copy cancellation = %v", err)
	}
	keyLimits := testFacadeProofGenerationLimits()
	keyLimits.Material.MaxKeys = 1
	if _, err := engine.Prove(
		context.Background(),
		snapshot,
		[]Key{{}, {1}},
		keyLimits,
	); !errors.Is(err, ErrResourceExhausted) {
		t.Fatalf("proof key resource = %v", err)
	}
	if _, err := engine.Prove(
		context.Background(),
		snapshot,
		[]Key{{}, {}},
		testFacadeProofGenerationLimits(),
	); !errors.Is(err, ErrDuplicateKey) {
		t.Fatalf("duplicate proof keys = %v", err)
	}
	var zeroEngine ProofEngine
	if err := zeroEngine.Verify(
		context.Background(),
		proof,
		testFacadeProofVerificationLimits(),
	); !errors.Is(err, ErrInvalidProofEngine) {
		t.Fatalf("zero verify engine = %v", err)
	}
	if err := engine.Verify(nilContext, proof, testFacadeProofVerificationLimits()); !errors.Is(err, ErrInvalidContext) {
		t.Fatalf("nil verify context = %v", err)
	}
	if err := engine.Verify(context.Background(), proof, ProofVerificationLimits{}); !errors.Is(err, ErrInvalidLimits) {
		t.Fatalf("verify limits = %v", err)
	}
	if _, err := DecodeProof(nilContext, nil, testFacadeProofDecodingLimits()); !errors.Is(err, ErrInvalidContext) {
		t.Fatalf("nil decode context = %v", err)
	}
	if _, err := DecodeProof(context.Background(), nil, ProofDecodingLimits{}); !errors.Is(err, ErrInvalidLimits) {
		t.Fatalf("decode limits = %v", err)
	}
	if _, err := DecodeProof(context.Background(), nil, testFacadeProofDecodingLimits()); !errors.Is(err, ErrInvalidProof) {
		t.Fatalf("malformed proof = %v", err)
	}
	var zeroProof Proof
	if _, err := zeroProof.Bytes(context.Background(), testFacadeProofEncodingLimits()); !errors.Is(err, ErrInvalidProof) {
		t.Fatalf("zero proof bytes = %v", err)
	}
	if _, err := proof.Bytes(nilContext, testFacadeProofEncodingLimits()); !errors.Is(err, ErrInvalidContext) {
		t.Fatalf("nil proof bytes context = %v", err)
	}
	if _, err := proof.Bytes(context.Background(), ProofEncodingLimits{}); !errors.Is(err, ErrInvalidLimits) {
		t.Fatalf("proof byte limits = %v", err)
	}
	if _, err := zeroProof.Claims(context.Background()); !errors.Is(err, ErrInvalidProof) {
		t.Fatalf("zero claims = %v", err)
	}
	if _, err := proof.Claims(nilContext); !errors.Is(err, ErrInvalidContext) {
		t.Fatalf("nil claims context = %v", err)
	}
	if _, err := proof.Claims(&cancellingContext{remaining: 2}); !errors.Is(err, ErrCancelled) {
		t.Fatalf("claim-copy cancellation = %v", err)
	}
	if _, err := proof.Claims(&cancellingContext{remaining: 3}); !errors.Is(err, ErrCancelled) {
		t.Fatalf("public claim cancellation = %v", err)
	}
	if _, err := toPublicClaim(authstate.Claim{}); !errors.Is(err, ErrInvalidProof) {
		t.Fatalf("invalid internal claim = %v", err)
	}
	if _, err := toPublicClaims(
		context.Background(),
		[]authstate.Claim{{}},
	); !errors.Is(err, ErrInvalidProof) {
		t.Fatalf("invalid internal claims = %v", err)
	}
	if _, err := zeroProof.Root(); !errors.Is(err, ErrInvalidProof) {
		t.Fatalf("zero proof root = %v", err)
	}
	proofRoot, err := proof.Root()
	if err != nil {
		t.Fatalf("proof root: %v", err)
	}
	snapshotRoot, _ := snapshot.Root()
	if proofBytes, proofErr := proofRoot.Bytes(); proofErr != nil {
		t.Fatalf("proof root bytes: %v", proofErr)
	} else if snapshotBytes, snapshotErr := snapshotRoot.Bytes(); snapshotErr != nil || proofBytes != snapshotBytes {
		t.Fatalf("snapshot root mismatch: %v", snapshotErr)
	}
	forged := Proof{valid: true}
	if _, err := forged.Bytes(context.Background(), testFacadeProofEncodingLimits()); !errors.Is(err, ErrInvalidProof) {
		t.Fatalf("forged proof bytes = %v", err)
	}
	if _, err := forged.Claims(context.Background()); !errors.Is(err, ErrInvalidProof) {
		t.Fatalf("forged claims = %v", err)
	}
	if _, err := forged.Root(); !errors.Is(err, ErrInvalidProof) {
		t.Fatalf("forged root = %v", err)
	}
	forgedEngine := ProofEngine{value: &authstate.ProofEngine{}, valid: true}
	if _, err := forgedEngine.Prove(
		context.Background(),
		snapshot,
		[]Key{{}},
		testFacadeProofGenerationLimits(),
	); !errors.Is(err, ErrInvalidProof) {
		t.Fatalf("forged engine prove = %v", err)
	}
	if err := forgedEngine.Verify(
		context.Background(),
		proof,
		testFacadeProofVerificationLimits(),
	); !errors.Is(err, ErrVerification) {
		t.Fatalf("forged engine verify = %v", err)
	}
	for _, partial := range []ProofEngine{
		{valid: true},
		{value: &authstate.ProofEngine{}},
	} {
		if _, err := partial.Prove(
			context.Background(),
			snapshot,
			[]Key{{}},
			testFacadeProofGenerationLimits(),
		); !errors.Is(err, ErrInvalidProofEngine) {
			t.Fatalf("partial engine prove = %v", err)
		}
		if err := partial.Verify(
			context.Background(),
			proof,
			testFacadeProofVerificationLimits(),
		); !errors.Is(err, ErrInvalidProofEngine) {
			t.Fatalf("partial engine verify = %v", err)
		}
	}
	exactKeyLimits := testFacadeProofGenerationLimits()
	exactKeyLimits.Material.MaxKeys = 1
	exactKeyLimits.ProverQueries.MaxKeys = 1
	if _, err := engine.Prove(
		context.Background(),
		snapshot,
		[]Key{{}},
		exactKeyLimits,
	); err != nil {
		t.Fatalf("exact proof key limit: %v", err)
	}
}

func TestFacadeLimitValidationChecksEveryFieldAndBoundary(t *testing.T) {
	t.Parallel()

	openingMutations := []func(*OpeningLimits){
		func(value *OpeningLimits) { value.MaxGeneratorDerivations = 0 },
		func(value *OpeningLimits) { value.MaxPrecomputedPoints = 0 },
		func(value *OpeningLimits) { value.MaxQueries = 0 },
		func(value *OpeningLimits) { value.MaxQueries = maxPublicProofQueries + 1 },
		func(value *OpeningLimits) { value.MaxScalarDecodes = 0 },
		func(value *OpeningLimits) { value.MaxMSMTerms = 0 },
		func(value *OpeningLimits) { value.MaxTemporaryBytes = 0 },
		func(value *OpeningLimits) { value.MaxWorkers = 0 },
	}
	for index, mutate := range openingMutations {
		value := testFacadeOpeningLimits()
		mutate(&value)
		if err := value.validate(); !errors.Is(err, ErrInvalidLimits) {
			t.Fatalf("opening mutation %d = %v", index, err)
		}
	}
	opening := testFacadeOpeningLimits()
	opening.MaxQueries = maxPublicProofQueries
	if err := opening.validate(); err != nil {
		t.Fatalf("exact opening query limit: %v", err)
	}

	generationMutations := []func(*ProofGenerationLimits){
		func(value *ProofGenerationLimits) { value.Material.MaxKeys = 0 },
		func(value *ProofGenerationLimits) { value.Material.MaxStemPaths = 0 },
		func(value *ProofGenerationLimits) { value.Material.MaxNodeReads = 0 },
		func(value *ProofGenerationLimits) { value.Material.MaxPathCommitments = 0 },
		func(value *ProofGenerationLimits) { value.Material.MaxPathBytes = 0 },
		func(value *ProofGenerationLimits) { value.Material.MaxTemporaryBytes = 0 },
		func(value *ProofGenerationLimits) { value.ProverQueries.MaxKeys = 0 },
		func(value *ProofGenerationLimits) { value.ProverQueries.MaxQueries = 0 },
		func(value *ProofGenerationLimits) { value.ProverQueries.MaxQueries = maxPublicProofQueries + 1 },
		func(value *ProofGenerationLimits) { value.ProverQueries.MaxNodeReads = 0 },
		func(value *ProofGenerationLimits) { value.ProverQueries.MaxTemporaryBytes = 0 },
		func(value *ProofGenerationLimits) { value.VerifierQueries.MaxQueries = 0 },
		func(value *ProofGenerationLimits) { value.VerifierQueries.MaxTemporaryBytes = 0 },
		func(value *ProofGenerationLimits) { value.Proof.MaxClaims = 0 },
		func(value *ProofGenerationLimits) { value.Proof.MaxStemPaths = 0 },
		func(value *ProofGenerationLimits) { value.Proof.MaxPathCommitments = 0 },
		func(value *ProofGenerationLimits) { value.Proof.MaxPathDerivations = 0 },
		func(value *ProofGenerationLimits) { value.Proof.MaxPathBytes = 0 },
		func(value *ProofGenerationLimits) { value.Proof.MaxTemporaryBytes = 0 },
	}
	for index, mutate := range generationMutations {
		value := testFacadeProofGenerationLimits()
		mutate(&value)
		if err := value.validate(); !errors.Is(err, ErrInvalidLimits) {
			t.Fatalf("generation mutation %d = %v", index, err)
		}
	}
	generation := testFacadeProofGenerationLimits()
	generation.ProverQueries.MaxQueries = maxPublicProofQueries
	generation.VerifierQueries.MaxQueries = maxPublicProofQueries
	if err := generation.validate(); err != nil {
		t.Fatalf("exact generation query limit: %v", err)
	}

	for index, mutate := range []func(*VerifierQueryLimits){
		func(value *VerifierQueryLimits) { value.MaxQueries = 0 },
		func(value *VerifierQueryLimits) { value.MaxQueries = maxPublicProofQueries + 1 },
		func(value *VerifierQueryLimits) { value.MaxTemporaryBytes = 0 },
	} {
		value := VerifierQueryLimits{MaxQueries: 1, MaxTemporaryBytes: 1}
		mutate(&value)
		if err := value.validate(); !errors.Is(err, ErrInvalidLimits) {
			t.Fatalf("verifier mutation %d = %v", index, err)
		}
	}
	if err := (VerifierQueryLimits{
		MaxQueries:        maxPublicProofQueries,
		MaxTemporaryBytes: 1,
	}).validate(); err != nil {
		t.Fatalf("exact verifier query limit: %v", err)
	}

	for index, mutate := range []func(*ProofEncodingLimits){
		func(value *ProofEncodingLimits) { value.MaxProofBytes = 0 },
		func(value *ProofEncodingLimits) { value.MaxTemporaryBytes = 0 },
	} {
		value := testFacadeProofEncodingLimits()
		mutate(&value)
		if err := value.validate(); !errors.Is(err, ErrInvalidLimits) {
			t.Fatalf("encoding mutation %d = %v", index, err)
		}
	}

	decodingMutations := []func(*ProofDecodingLimits){
		func(value *ProofDecodingLimits) { value.MaxProofBytes = 0 },
		func(value *ProofDecodingLimits) { value.MaxClaims = 0 },
		func(value *ProofDecodingLimits) { value.MaxStemPaths = 0 },
		func(value *ProofDecodingLimits) { value.MaxPathCommitments = 0 },
		func(value *ProofDecodingLimits) { value.MaxPathDerivations = 0 },
		func(value *ProofDecodingLimits) { value.MaxPathBytes = 0 },
		func(value *ProofDecodingLimits) { value.MaxTemporaryBytes = 0 },
	}
	for index, mutate := range decodingMutations {
		value := testFacadeProofDecodingLimits()
		mutate(&value)
		if err := value.validate(); !errors.Is(err, ErrInvalidLimits) {
			t.Fatalf("decoding mutation %d = %v", index, err)
		}
	}

	snapshotMutations := []func(*SnapshotLimits){
		func(value *SnapshotLimits) { value.State.MaxEntries = 0 },
		func(value *SnapshotLimits) { value.State.MaxEntries = maxPublicCount + 1 },
		func(value *SnapshotLimits) { value.State.MaxBatchUpdates = 0 },
		func(value *SnapshotLimits) { value.State.MaxBatchUpdates = maxPublicCount + 1 },
		func(value *SnapshotLimits) { value.State.MaxTemporaryBytes = 0 },
		func(value *SnapshotLimits) { value.Tree.MaxEntries = 0 },
		func(value *SnapshotLimits) { value.Tree.MaxEntries = maxPublicCount + 1 },
		func(value *SnapshotLimits) { value.Tree.MaxStems = 0 },
		func(value *SnapshotLimits) { value.Tree.MaxStems = maxPublicCount + 1 },
		func(value *SnapshotLimits) { value.Tree.MaxNodes = 0 },
		func(value *SnapshotLimits) { value.Tree.MaxNodes = maxPublicCount + 1 },
		func(value *SnapshotLimits) { value.Tree.MaxEdges = 0 },
		func(value *SnapshotLimits) { value.Tree.MaxEdges = maxPublicCount + 1 },
		func(value *SnapshotLimits) { value.Tree.MaxCommitments = 0 },
		func(value *SnapshotLimits) { value.Tree.MaxCommitments = maxPublicCount + 1 },
		func(value *SnapshotLimits) { value.Tree.MaxFieldMappings = 0 },
		func(value *SnapshotLimits) { value.Tree.MaxCommitmentTerms = 0 },
		func(value *SnapshotLimits) { value.Tree.MaxTemporaryBytes = 0 },
		func(value *SnapshotLimits) { value.Commitment.MaxGeneratorDerivations = 0 },
		func(value *SnapshotLimits) { value.Commitment.MaxScalarDecodes = 0 },
		func(value *SnapshotLimits) { value.Commitment.MaxMSMTerms = 0 },
		func(value *SnapshotLimits) { value.Commitment.MaxTemporaryBytes = 0 },
	}
	for index, mutate := range snapshotMutations {
		value := testFacadeSnapshotLimits()
		mutate(&value)
		if err := value.validate(); !errors.Is(err, ErrInvalidLimits) {
			t.Fatalf("snapshot mutation %d = %v", index, err)
		}
	}
	snapshot := testFacadeSnapshotLimits()
	snapshot.State.MaxEntries = maxPublicCount
	snapshot.State.MaxBatchUpdates = maxPublicCount
	snapshot.Tree.MaxEntries = maxPublicCount
	snapshot.Tree.MaxStems = maxPublicCount
	snapshot.Tree.MaxNodes = maxPublicCount
	snapshot.Tree.MaxEdges = maxPublicCount
	snapshot.Tree.MaxCommitments = maxPublicCount
	if err := snapshot.validate(); err != nil {
		t.Fatalf("exact snapshot count limit: %v", err)
	}
}

func testFacadeProof(t testing.TB) (ProofEngine, Snapshot, Proof) {
	t.Helper()

	snapshot, err := NewSnapshot(
		context.Background(),
		ExperimentalBandersnatchIPA256V0(),
		[]Entry{{Key: Key{}, Value: Value{1}}},
		testFacadeSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("new snapshot: %v", err)
	}
	engine, err := NewProofEngine(
		context.Background(),
		ExperimentalBandersnatchIPA256V0(),
		testFacadeOpeningLimits(),
	)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	proof, err := engine.Prove(
		context.Background(),
		snapshot,
		[]Key{{}},
		testFacadeProofGenerationLimits(),
	)
	if err != nil {
		t.Fatalf("prove: %v", err)
	}

	return engine, snapshot, proof
}

func testFacadeSnapshotLimits() SnapshotLimits {
	return SnapshotLimits{
		State: StateLimits{MaxEntries: 8, MaxBatchUpdates: 8, MaxTemporaryBytes: 1 << 20},
		Tree: TreeLimits{
			MaxEntries: 8, MaxStems: 8, MaxNodes: 32, MaxEdges: 32,
			MaxCommitments: 64, MaxFieldMappings: 64,
			MaxCommitmentTerms: 1 << 14, MaxTemporaryBytes: 8 << 20,
		},
		Commitment: CommitmentLimits{
			MaxGeneratorDerivations: 256, MaxScalarDecodes: 256,
			MaxMSMTerms: 256, MaxTemporaryBytes: 1 << 20,
		},
	}
}

func testFacadeOpeningLimits() OpeningLimits {
	return OpeningLimits{
		MaxGeneratorDerivations: 256, MaxPrecomputedPoints: 256,
		MaxQueries: 128, MaxScalarDecodes: 1 << 16,
		MaxMSMTerms: 1 << 20, MaxTemporaryBytes: 1 << 30,
		MaxWorkers: uint32(runtime.NumCPU()),
	}
}

func testFacadeProofGenerationLimits() ProofGenerationLimits {
	return ProofGenerationLimits{
		Material: ProofMaterialLimits{
			MaxKeys: 8, MaxStemPaths: 8, MaxNodeReads: 128,
			MaxPathCommitments: 128, MaxPathBytes: 4096, MaxTemporaryBytes: 1 << 20,
		},
		ProverQueries: ProverQueryLimits{
			MaxKeys: 8, MaxQueries: 128, MaxNodeReads: 128, MaxTemporaryBytes: 8 << 20,
		},
		VerifierQueries: VerifierQueryLimits{MaxQueries: 128, MaxTemporaryBytes: 8 << 20},
		Proof: ProofContainerLimits{
			MaxClaims: 8, MaxStemPaths: 8, MaxPathCommitments: 128,
			MaxPathDerivations: 128, MaxPathBytes: 4096, MaxTemporaryBytes: 1 << 20,
		},
	}
}

func testFacadeProofVerificationLimits() ProofVerificationLimits {
	return ProofVerificationLimits{
		VerifierQueries: VerifierQueryLimits{MaxQueries: 128, MaxTemporaryBytes: 8 << 20},
	}
}

func testFacadeProofEncodingLimits() ProofEncodingLimits {
	return ProofEncodingLimits{MaxProofBytes: 64 << 10, MaxTemporaryBytes: 64 << 10}
}

func testFacadeProofDecodingLimits() ProofDecodingLimits {
	return ProofDecodingLimits{
		MaxProofBytes: 64 << 10, MaxClaims: 8, MaxStemPaths: 8,
		MaxPathCommitments: 128, MaxPathDerivations: 128, MaxPathBytes: 4096,
		MaxPointDecodes: 128, MaxScalarDecodes: 1, MaxTemporaryBytes: 1 << 20,
	}
}
