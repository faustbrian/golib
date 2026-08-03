package backend

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"runtime"
	"sync"
	"testing"
	"time"

	multiproof "github.com/crate-crypto/go-ipa"
	"github.com/crate-crypto/go-ipa/bandersnatch/fr"
	"github.com/crate-crypto/go-ipa/banderwagon"
	"github.com/crate-crypto/go-ipa/ipa"
)

var (
	testAggregateOpeningEngineOnce sync.Once
	testAggregateOpeningEngine     *AggregateOpeningEngine
	testAggregateOpeningEngineErr  error
)

func TestAggregateOpeningEngineMatchesPinnedRustProof(t *testing.T) {
	t.Parallel()

	_, fixture := readMultiProofFixture(t)
	engine, err := NewAggregateOpeningEngine(
		context.Background(),
		testAggregateOpeningLimits(),
	)
	if err != nil {
		t.Fatalf("new aggregate opening engine: %v", err)
	}
	proverQueries, verifierQueries := aggregateOpeningCorpus()
	commitAggregateOpeningCorpus(t, proverQueries, verifierQueries)
	proof, err := engine.Open(context.Background(), proverQueries)
	if err != nil {
		t.Fatalf("open aggregate proof: %v", err)
	}
	encoded, err := proof.Bytes()
	if err != nil {
		t.Fatalf("encode aggregate proof: %v", err)
	}
	if !bytes.Equal(encoded[:], fixture) {
		t.Fatal("aggregate proof differs from pinned Rust proof")
	}
	if err := engine.Verify(context.Background(), proof, verifierQueries); err != nil {
		t.Fatalf("verify aggregate proof: %v", err)
	}

	wrong := append([]AggregateVerifierQuery(nil), verifierQueries...)
	wrong[0].Value[0]++
	if err := engine.Verify(context.Background(), proof, wrong); !errors.Is(err, errAggregateOpeningVerification) {
		t.Fatalf("wrong-value error = %v, want %v", err, errAggregateOpeningVerification)
	}
}

func TestAggregateOpeningEngineRejectsInvalidSetup(t *testing.T) {
	t.Parallel()

	valid := testAggregateOpeningLimits()
	invalid := []AggregateOpeningLimits{
		func() AggregateOpeningLimits { value := valid; value.MaxGeneratorDerivations = 0; return value }(),
		func() AggregateOpeningLimits { value := valid; value.MaxPrecomputedPoints = 0; return value }(),
		func() AggregateOpeningLimits { value := valid; value.MaxQueries = 0; return value }(),
		func() AggregateOpeningLimits {
			value := valid
			value.MaxQueries = maxAggregateOpeningQueries + 1
			return value
		}(),
		func() AggregateOpeningLimits { value := valid; value.MaxScalarDecodes = 0; return value }(),
		func() AggregateOpeningLimits { value := valid; value.MaxMSMTerms = 0; return value }(),
		func() AggregateOpeningLimits { value := valid; value.MaxTemporaryBytes = 0; return value }(),
		func() AggregateOpeningLimits { value := valid; value.MaxWorkers = 0; return value }(),
	}
	for index, limits := range invalid {
		if _, err := NewAggregateOpeningEngine(context.Background(), limits); !errors.Is(err, errInvalidAggregateOpeningLimits) {
			t.Fatalf("invalid limits %d error = %v", index, err)
		}
	}
	boundary := valid
	boundary.MaxQueries = maxAggregateOpeningQueries
	if err := boundary.validate(); err != nil {
		t.Fatalf("maximum query limit: %v", err)
	}
	var missingContext context.Context
	if _, err := NewAggregateOpeningEngine(missingContext, valid); !errors.Is(err, errInvalidAggregateOpeningContext) {
		t.Fatalf("nil context error = %v", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewAggregateOpeningEngine(cancelled, valid); !errors.Is(err, errAggregateOpeningCancelled) || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled context error = %v", err)
	}
	if _, err := newAggregateOpeningEngine(context.Background(), valid, nil); !errors.Is(err, errInvalidAggregateOpeningEngine) {
		t.Fatalf("nil settings error = %v", err)
	}
	sentinel := errors.New("settings failed")
	if _, err := newAggregateOpeningEngine(context.Background(), valid, func() (*ipa.IPAConfig, error) {
		return nil, sentinel
	}); !errors.Is(err, errInvalidAggregateOpeningEngine) {
		t.Fatalf("settings failure error = %v", err)
	}
	if _, err := newAggregateOpeningEngine(context.Background(), valid, func() (*ipa.IPAConfig, error) {
		return &ipa.IPAConfig{SRS: make([]banderwagon.Element, VectorWidth-1)}, nil
	}); !errors.Is(err, errGeneratorMismatch) {
		t.Fatalf("generator mismatch error = %v", err)
	}
	config, err := ipa.NewIPASettings()
	if err != nil {
		t.Fatalf("new settings for cancellation: %v", err)
	}
	duringSetup, cancelDuringSetup := context.WithCancel(context.Background())
	if _, err := newAggregateOpeningEngine(duringSetup, valid, func() (*ipa.IPAConfig, error) {
		cancelDuringSetup()
		return config, nil
	}); !errors.Is(err, errAggregateOpeningCancelled) {
		t.Fatalf("post-settings cancellation error = %v", err)
	}
}

func TestAggregateOpeningEnginePreflightsSetupResources(t *testing.T) {
	t.Parallel()

	valid := testAggregateOpeningLimits()
	tests := []struct {
		limits   AggregateOpeningLimits
		resource AggregateOpeningResource
		actual   uint64
	}{
		{func() AggregateOpeningLimits {
			value := valid
			value.MaxGeneratorDerivations = VectorWidth - 1
			return value
		}(), AggregateOpeningResourceGeneratorDerivations, VectorWidth},
		{func() AggregateOpeningLimits {
			value := valid
			value.MaxPrecomputedPoints = VectorWidth - 1
			return value
		}(), AggregateOpeningResourcePrecomputedPoints, VectorWidth},
		{func() AggregateOpeningLimits {
			value := valid
			value.MaxTemporaryBytes = aggregateSetupWorkingBytes - 1
			return value
		}(), AggregateOpeningResourceTemporaryBytes, aggregateSetupWorkingBytes},
	}
	if runtime.NumCPU() > 1 {
		tests = append(tests, struct {
			limits   AggregateOpeningLimits
			resource AggregateOpeningResource
			actual   uint64
		}{func() AggregateOpeningLimits { value := valid; value.MaxWorkers--; return value }(), AggregateOpeningResourceWorkers, uint64(runtime.NumCPU())})
	}
	for index, test := range tests {
		called := false
		_, err := newAggregateOpeningEngine(context.Background(), test.limits, func() (*ipa.IPAConfig, error) {
			called = true
			return nil, errors.New("must not run")
		})
		assertAggregateOpeningResourceError(t, err, test.resource, test.actual)
		if called {
			t.Fatalf("resource case %d initialized settings", index)
		}
	}
}

func TestAggregateOpeningOperationsRejectInvalidInputsAndResources(t *testing.T) {
	t.Parallel()

	engine := newTestAggregateOpeningEngine(t)
	prover, verifier := aggregateOpeningCorpus()
	commitAggregateOpeningCorpus(t, prover, verifier)
	proof, err := engine.Open(context.Background(), prover)
	if err != nil {
		t.Fatalf("open proof: %v", err)
	}

	if _, err := (*AggregateOpeningEngine)(nil).Open(context.Background(), prover); !errors.Is(err, errInvalidAggregateOpeningEngine) {
		t.Fatalf("nil engine open error = %v", err)
	}
	if err := (*AggregateOpeningEngine)(nil).Verify(context.Background(), proof, verifier); !errors.Is(err, errInvalidAggregateOpeningEngine) {
		t.Fatalf("nil engine verify error = %v", err)
	}
	corrupt := *engine
	corrupt.valid = false
	if _, err := corrupt.Open(context.Background(), prover); !errors.Is(err, errInvalidAggregateOpeningEngine) {
		t.Fatalf("corrupt engine open error = %v", err)
	}
	corrupt = *engine
	corrupt.backend = nil
	if err := corrupt.Verify(context.Background(), proof, verifier); !errors.Is(err, errInvalidAggregateOpeningEngine) {
		t.Fatalf("corrupt engine verify error = %v", err)
	}
	corrupt = *engine
	corrupt.limits.MaxQueries = 0
	if _, err := corrupt.Open(context.Background(), prover); !errors.Is(err, errInvalidAggregateOpeningEngine) {
		t.Fatalf("corrupt limits error = %v", err)
	}
	var missingContext context.Context
	if _, err := engine.Open(missingContext, prover); !errors.Is(err, errInvalidAggregateOpeningContext) {
		t.Fatalf("nil open context error = %v", err)
	}
	if err := engine.Verify(missingContext, proof, verifier); !errors.Is(err, errInvalidAggregateOpeningContext) {
		t.Fatalf("nil verify context error = %v", err)
	}

	for _, test := range []struct {
		name         string
		mutate       func(*AggregateOpeningLimits)
		resource     AggregateOpeningResource
		openActual   uint64
		verifyActual uint64
	}{
		{"queries", func(value *AggregateOpeningLimits) { value.MaxQueries = 1 }, AggregateOpeningResourceQueries, 3, 3},
		{"scalars", func(value *AggregateOpeningLimits) { value.MaxScalarDecodes = 1 }, AggregateOpeningResourceScalarDecodes, 768, 3},
		{"msm", func(value *AggregateOpeningLimits) { value.MaxMSMTerms = 4_096 }, AggregateOpeningResourceMSMTerms, 4_864, 4_864},
		{"temporary", func(value *AggregateOpeningLimits) { value.MaxTemporaryBytes = 65_536 }, AggregateOpeningResourceTemporaryBytes, 196_608, 196_608},
	} {
		t.Run(test.name, func(t *testing.T) {
			limited := *engine
			test.mutate(&limited.limits)
			_, openErr := limited.Open(context.Background(), prover)
			assertAggregateOpeningResourceError(t, openErr, test.resource, test.openActual)
			verifyErr := limited.Verify(context.Background(), proof, verifier)
			assertAggregateOpeningResourceError(t, verifyErr, test.resource, test.verifyActual)
		})
	}
	if runtime.NumCPU() > 1 {
		limited := *engine
		limited.limits.MaxWorkers--
		_, err := limited.Open(context.Background(), prover)
		assertAggregateOpeningResourceError(t, err, AggregateOpeningResourceWorkers, uint64(runtime.NumCPU()))
	}
	if _, err := engine.Open(context.Background(), nil); !errors.Is(err, errInvalidAggregateOpeningQuery) {
		t.Fatalf("empty open error = %v", err)
	}
	if err := engine.Verify(context.Background(), proof, nil); !errors.Is(err, errInvalidAggregateOpeningQuery) {
		t.Fatalf("empty verify error = %v", err)
	}
	if err := engine.Verify(context.Background(), OpeningProof{}, verifier); !errors.Is(err, errInvalidAggregateOpeningQuery) {
		t.Fatalf("invalid proof error = %v", err)
	}
	for _, successful := range []int{1, 2} {
		ctx := &aggregateOpeningStepContext{successful: successful}
		if _, err := engine.Open(ctx, prover); !errors.Is(err, errAggregateOpeningCancelled) {
			t.Fatalf("open cancellation after %d checks = %v", successful, err)
		}
	}
	if err := engine.Verify(
		&aggregateOpeningStepContext{successful: 1},
		proof,
		verifier,
	); !errors.Is(err, errAggregateOpeningCancelled) {
		t.Fatalf("verify loop cancellation error = %v", err)
	}
	corruptProof := proof
	clear(corruptProof.encoded[:commitmentSize])
	if err := engine.Verify(context.Background(), corruptProof, verifier); !errors.Is(err, errAggregateOpeningVerification) {
		t.Fatalf("corrupt native proof error = %v", err)
	}
}

func TestAggregateOpeningSupportsOneAuthenticatedZeroOpening(t *testing.T) {
	t.Parallel()

	engine := newTestAggregateOpeningEngine(t)
	prover := []AggregateProverQuery{{Index: 1}}
	setVectorUint64(&prover[0].Vector, 0, 1)
	setVectorUint64(&prover[0].Vector, 2, 2)
	verifier := []AggregateVerifierQuery{{Index: 1}}
	commitAggregateOpeningCorpus(t, prover, verifier)
	polynomial := make([]fr.Element, VectorWidth)
	for index := range prover[0].Vector {
		decoded, decodeErr := decodeScalar(prover[0].Vector[index][:])
		if decodeErr != nil {
			t.Fatalf("decode polynomial scalar %d: %v", index, decodeErr)
		}
		polynomial[index] = decoded.element
	}
	commitment := engine.backend.commit(polynomial)
	native, err := engine.backend.open(
		[]*banderwagon.Element{&commitment}, [][]fr.Element{polynomial}, []uint8{1},
	)
	if err != nil {
		t.Fatalf("open native one authenticated zero: %v", err)
	}
	zero := fr.Zero()
	verified, err := engine.backend.verify(
		native, []*banderwagon.Element{&commitment}, []*fr.Element{&zero}, []uint8{1},
	)
	if err != nil || !verified {
		t.Fatalf("verify native one authenticated zero = %t, %v", verified, err)
	}

	proof, err := engine.Open(context.Background(), prover)
	if err != nil {
		t.Fatalf("open one authenticated zero: %v", err)
	}
	if err := engine.Verify(context.Background(), proof, verifier); err != nil {
		t.Fatalf("verify one authenticated zero: %v", err)
	}
	_, fixture := readZeroEvaluationMultiProofFixture(t)
	encoded, err := proof.Bytes()
	if err != nil {
		t.Fatalf("encode one authenticated zero: %v", err)
	}
	if !bytes.Equal(encoded[:], fixture) {
		t.Fatal("one authenticated zero proof differs from pinned Rust proof")
	}
}

func TestAggregateOpeningOperationsRejectMalformedQueries(t *testing.T) {
	t.Parallel()

	engine := newTestAggregateOpeningEngine(t)
	prover, verifier := aggregateOpeningCorpus()
	commitAggregateOpeningCorpus(t, prover, verifier)
	proof, err := engine.Open(context.Background(), prover)
	if err != nil {
		t.Fatalf("open proof: %v", err)
	}

	invalidCommitment := append([]AggregateProverQuery(nil), prover...)
	invalidCommitment[0].Commitment = VectorCommitment{}
	if _, err := engine.Open(context.Background(), invalidCommitment); !errors.Is(err, errInvalidAggregateOpeningQuery) {
		t.Fatalf("invalid prover commitment error = %v", err)
	}
	invalidVerifier := append([]AggregateVerifierQuery(nil), verifier...)
	invalidVerifier[0].Commitment = VectorCommitment{}
	if err := engine.Verify(context.Background(), proof, invalidVerifier); !errors.Is(err, errInvalidAggregateOpeningQuery) {
		t.Fatalf("invalid verifier commitment error = %v", err)
	}
	duplicateProver := append(append([]AggregateProverQuery(nil), prover...), prover[0])
	if _, err := engine.Open(context.Background(), duplicateProver); !errors.Is(err, errInvalidAggregateOpeningQuery) {
		t.Fatalf("duplicate prover error = %v", err)
	}
	duplicateVerifier := append(append([]AggregateVerifierQuery(nil), verifier...), verifier[0])
	if err := engine.Verify(context.Background(), proof, duplicateVerifier); !errors.Is(err, errInvalidAggregateOpeningQuery) {
		t.Fatalf("duplicate verifier error = %v", err)
	}
	nonCanonicalProver := append([]AggregateProverQuery(nil), prover...)
	nonCanonicalProver[0].Vector[0] = scalarModulusBytes(t)
	if _, err := engine.Open(context.Background(), nonCanonicalProver); !errors.Is(err, errInvalidAggregateOpeningQuery) {
		t.Fatalf("non-canonical prover scalar error = %v", err)
	}
	nonCanonicalVerifier := append([]AggregateVerifierQuery(nil), verifier...)
	nonCanonicalVerifier[0].Value = scalarModulusBytes(t)
	if err := engine.Verify(context.Background(), proof, nonCanonicalVerifier); !errors.Is(err, errInvalidAggregateOpeningQuery) {
		t.Fatalf("non-canonical verifier scalar error = %v", err)
	}
	mismatch := append([]AggregateProverQuery(nil), prover...)
	mismatch[0].Commitment = prover[1].Commitment
	if _, err := engine.Open(context.Background(), mismatch); !errors.Is(err, errInvalidAggregateOpeningQuery) {
		t.Fatalf("commitment mismatch error = %v", err)
	}
}

func TestAggregateOpeningOperationsPropagateBackendAndCancellationFailures(t *testing.T) {
	t.Parallel()

	real := newTestAggregateOpeningEngine(t)
	prover, verifier := aggregateOpeningCorpus()
	commitAggregateOpeningCorpus(t, prover, verifier)
	proof, err := real.Open(context.Background(), prover)
	if err != nil {
		t.Fatalf("open proof: %v", err)
	}
	native, err := nativeAggregateOpeningProof(proof)
	if err != nil {
		t.Fatalf("native proof: %v", err)
	}
	sentinel := errors.New("backend failed")

	failedOpen := *real
	failedOpen.backend = &fakeAggregateOpeningBackend{delegate: real.backend, openErr: sentinel}
	if _, err := failedOpen.Open(context.Background(), prover); !errors.Is(err, errAggregateOpeningGeneration) {
		t.Fatalf("backend open error = %v", err)
	}
	badShape := *real
	badShape.backend = &fakeAggregateOpeningBackend{delegate: real.backend, openProof: &multiproof.MultiProof{}}
	if _, err := badShape.Open(context.Background(), prover); !errors.Is(err, errAggregateOpeningGeneration) {
		t.Fatalf("backend shape error = %v", err)
	}
	shortLeft := native
	shortLeft.IPA.L = shortLeft.IPA.L[:7]
	badShape.backend = &fakeAggregateOpeningBackend{delegate: real.backend, openProof: &shortLeft}
	if _, err := badShape.Open(context.Background(), prover); !errors.Is(err, errAggregateOpeningGeneration) {
		t.Fatalf("short left proof error = %v", err)
	}
	shortRight := native
	shortRight.IPA.R = shortRight.IPA.R[:7]
	badShape.backend = &fakeAggregateOpeningBackend{delegate: real.backend, openProof: &shortRight}
	if _, err := badShape.Open(context.Background(), prover); !errors.Is(err, errAggregateOpeningGeneration) {
		t.Fatalf("short right proof error = %v", err)
	}
	failedVerify := *real
	failedVerify.backend = &fakeAggregateOpeningBackend{delegate: real.backend, verifyErr: sentinel}
	if err := failedVerify.Verify(context.Background(), proof, verifier); !errors.Is(err, errAggregateOpeningVerification) {
		t.Fatalf("backend verify error = %v", err)
	}
	rejected := *real
	rejected.backend = &fakeAggregateOpeningBackend{delegate: real.backend, verifySet: true}
	if err := rejected.Verify(context.Background(), proof, verifier); !errors.Is(err, errAggregateOpeningVerification) {
		t.Fatalf("backend rejection error = %v", err)
	}

	openContext, cancelOpen := context.WithCancel(context.Background())
	cancelledOpen := *real
	cancelledOpen.backend = &fakeAggregateOpeningBackend{
		delegate:  real.backend,
		openProof: &native,
		onOpen:    cancelOpen,
	}
	if _, err := cancelledOpen.Open(openContext, prover); !errors.Is(err, errAggregateOpeningCancelled) {
		t.Fatalf("post-open cancellation error = %v", err)
	}
	verifyContext, cancelVerify := context.WithCancel(context.Background())
	cancelledVerify := *real
	cancelledVerify.backend = &fakeAggregateOpeningBackend{
		delegate:  real.backend,
		verifyOK:  true,
		verifySet: true,
		onVerify:  cancelVerify,
	}
	if err := cancelledVerify.Verify(verifyContext, proof, verifier); !errors.Is(err, errAggregateOpeningCancelled) {
		t.Fatalf("post-verify cancellation error = %v", err)
	}
}

func TestAggregateOpeningProofNativeConversionRejectsCorruption(t *testing.T) {
	t.Parallel()

	if _, err := nativeAggregateOpeningProof(OpeningProof{}); !errors.Is(err, errInvalidOpeningProof) {
		t.Fatalf("invalid proof error = %v", err)
	}
	_, encoded := readMultiProofFixture(t)
	proof, err := DecodeOpeningProof(context.Background(), encoded, testOpeningProofLimits())
	if err != nil {
		t.Fatalf("decode proof: %v", err)
	}
	corruptPoint := proof
	for index := 0; index < commitmentSize; index++ {
		corruptPoint.encoded[index] = 0xff
	}
	if _, err := nativeAggregateOpeningProof(corruptPoint); !errors.Is(err, errInvalidOpeningProof) {
		t.Fatalf("corrupt point error = %v", err)
	}
	engine := newTestAggregateOpeningEngine(t)
	prover, verifier := aggregateOpeningCorpus()
	commitAggregateOpeningCorpus(t, prover, verifier)
	if err := engine.Verify(context.Background(), corruptPoint, verifier); !errors.Is(err, errInvalidOpeningProof) {
		t.Fatalf("verify corrupt point error = %v", err)
	}
	corruptScalar := proof
	modulus := scalarModulusBytes(t)
	copy(corruptScalar.encoded[OpeningProofSize-scalarSize:], modulus[:])
	if _, err := nativeAggregateOpeningProof(corruptScalar); !errors.Is(err, errInvalidOpeningProof) {
		t.Fatalf("corrupt scalar error = %v", err)
	}
}

type fakeAggregateOpeningBackend struct {
	delegate  aggregateOpeningBackend
	openProof *multiproof.MultiProof
	openErr   error
	verifyOK  bool
	verifySet bool
	verifyErr error
	onOpen    func()
	onVerify  func()
}

func (backend *fakeAggregateOpeningBackend) commit(polynomial []fr.Element) banderwagon.Element {
	return backend.delegate.commit(polynomial)
}

func (backend *fakeAggregateOpeningBackend) open(
	commitments []*banderwagon.Element,
	polynomials [][]fr.Element,
	indices []uint8,
) (*multiproof.MultiProof, error) {
	if backend.onOpen != nil {
		backend.onOpen()
	}
	if backend.openErr != nil || backend.openProof != nil {
		return backend.openProof, backend.openErr
	}

	return backend.delegate.open(commitments, polynomials, indices)
}

func (backend *fakeAggregateOpeningBackend) verify(
	proof *multiproof.MultiProof,
	commitments []*banderwagon.Element,
	values []*fr.Element,
	indices []uint8,
) (bool, error) {
	if backend.onVerify != nil {
		backend.onVerify()
	}
	if backend.verifyErr != nil || backend.verifySet {
		return backend.verifyOK, backend.verifyErr
	}

	return backend.delegate.verify(proof, commitments, values, indices)
}

type aggregateOpeningStepContext struct {
	checks     int
	successful int
}

func (ctx *aggregateOpeningStepContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (ctx *aggregateOpeningStepContext) Done() <-chan struct{}       { return nil }
func (ctx *aggregateOpeningStepContext) Value(any) any               { return nil }
func (ctx *aggregateOpeningStepContext) Err() error {
	ctx.checks++
	if ctx.checks > ctx.successful {
		return context.Canceled
	}

	return nil
}

func testAggregateOpeningLimits() AggregateOpeningLimits {
	return AggregateOpeningLimits{
		MaxGeneratorDerivations: VectorWidth,
		MaxPrecomputedPoints:    VectorWidth,
		MaxQueries:              32,
		MaxScalarDecodes:        32 * VectorWidth,
		MaxMSMTerms:             64 * VectorWidth,
		MaxTemporaryBytes:       1 << 30,
		MaxWorkers:              uint32(runtime.NumCPU()),
	}
}

func newTestAggregateOpeningEngine(t testing.TB) *AggregateOpeningEngine {
	t.Helper()

	testAggregateOpeningEngineOnce.Do(func() {
		testAggregateOpeningEngine, testAggregateOpeningEngineErr = NewAggregateOpeningEngine(
			context.Background(),
			testAggregateOpeningLimits(),
		)
	})
	if testAggregateOpeningEngineErr != nil {
		t.Fatalf("new shared aggregate opening engine: %v", testAggregateOpeningEngineErr)
	}

	return testAggregateOpeningEngine
}

func commitAggregateOpeningCorpus(
	t testing.TB,
	prover []AggregateProverQuery,
	verifier []AggregateVerifierQuery,
) {
	t.Helper()

	commitmentEngine, err := NewCommitmentEngine(
		context.Background(),
		testCommitmentLimits(),
	)
	if err != nil {
		t.Fatalf("new commitment engine: %v", err)
	}
	for index := range prover {
		commitment, commitErr := commitmentEngine.Commit(
			context.Background(),
			prover[index].Vector,
		)
		if commitErr != nil {
			t.Fatalf("commit query %d: %v", index, commitErr)
		}
		prover[index].Commitment = commitment
		verifier[index].Commitment = commitment
	}
}

func scalarModulusBytes(t testing.TB) [scalarSize]byte {
	t.Helper()

	decoded, err := hex.DecodeString(
		"e1e77628b506fd747104197400878fff007668020276ce0c525f67cad469fb1c",
	)
	if err != nil {
		t.Fatalf("decode scalar modulus: %v", err)
	}
	var result [scalarSize]byte
	copy(result[:], decoded)

	return result
}

func assertAggregateOpeningResourceError(
	t testing.TB,
	err error,
	resource AggregateOpeningResource,
	actual uint64,
) {
	t.Helper()

	var resourceErr *AggregateOpeningResourceError
	if !errors.As(err, &resourceErr) {
		t.Fatalf("error = %v, want AggregateOpeningResourceError", err)
	}
	if resourceErr.Resource != resource || resourceErr.Actual != actual {
		t.Fatalf(
			"resource error = (%d, %d), want (%d, %d)",
			resourceErr.Resource,
			resourceErr.Actual,
			resource,
			actual,
		)
	}
	if !errors.Is(err, errAggregateOpeningResource) ||
		resourceErr.Unwrap() != errAggregateOpeningResource ||
		resourceErr.Error() == "" {
		t.Fatalf("resource error does not preserve sentinel: %v", err)
	}
}

func aggregateOpeningCorpus() ([]AggregateProverQuery, []AggregateVerifierQuery) {
	prover := make([]AggregateProverQuery, 3)
	verifier := make([]AggregateVerifierQuery, 3)
	points := [...]uint8{3, 3, 200}
	for vectorIndex := range prover {
		for index := 0; index < VectorWidth; index++ {
			value := uint64(index + 1)
			switch vectorIndex {
			case 1:
				value *= value
			case 2:
				value = 3*uint64(index) + 7
			}
			setVectorUint64(&prover[vectorIndex].Vector, index, value)
		}
		prover[vectorIndex].Index = points[vectorIndex]
		verifier[vectorIndex].Index = points[vectorIndex]
		verifier[vectorIndex].Value = prover[vectorIndex].Vector[points[vectorIndex]]
	}

	return prover, verifier
}
