package backend

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync/atomic"

	multiproof "github.com/crate-crypto/go-ipa"
	"github.com/crate-crypto/go-ipa/bandersnatch/fr"
	"github.com/crate-crypto/go-ipa/banderwagon"
	"github.com/crate-crypto/go-ipa/common"
	"github.com/crate-crypto/go-ipa/ipa"
)

const (
	maxAggregateOpeningQueries   = uint32(65_536)
	maxAggregateQueuedOperations = uint32(65_536)
	aggregateSetupWorkingBytes   = uint64(1 << 30)
	aggregateQueryWorkingBytes   = uint64(VectorWidth*scalarSize) * 8
	aggregateFixedMSMTerms       = uint64(2 * VectorWidth * 8)
	aggregateTranscriptLabel     = "verkle"
	aggregateBindingLabel        = "verkletree-proof-statement-v0"
	pinnedIPAAuxiliaryGenerator  = "\x4a\x2c\x74\x86\xfd\x92\x48\x82" +
		"\xbf\x02\xc6\x90\x8d\xe3\x95\x12" +
		"\x28\x43\xe3\xe0\x52\x64\xd7\x99" +
		"\x1e\x18\xe7\x98\x5d\xad\x51\xe9"
)

var (
	errInvalidAggregateOpeningEngine  = errors.New("invalid aggregate opening engine")
	errInvalidAggregateOpeningContext = errors.New("invalid aggregate opening context")
	errInvalidAggregateOpeningLimits  = errors.New("invalid aggregate opening limits")
	errInvalidAggregateOpeningQuery   = errors.New("invalid aggregate opening query")
	errAggregateOpeningCancelled      = errors.New("aggregate opening operation cancelled")
	errAggregateOpeningResource       = errors.New("aggregate opening resource limit exceeded")
	errAggregateOpeningGeneration     = errors.New("aggregate opening generation failed")
	errAggregateOpeningVerification   = errors.New("aggregate opening verification failed")
)

// AggregateOpeningLimits bounds fixed-profile setup and aggregate-opening
// operations. Every field except MaxQueuedOperations must be positive, and no
// field denotes an unbounded resource. A zero queue limit rejects concurrent
// waiters. The pinned backend may use exactly runtime.NumCPU workers; an
// operation is rejected before setup when MaxWorkers is smaller.
type AggregateOpeningLimits struct {
	MaxGeneratorDerivations uint32
	MaxPrecomputedPoints    uint32
	MaxQueries              uint32
	MaxScalarDecodes        uint64
	MaxMSMTerms             uint64
	MaxTemporaryBytes       uint64
	MaxWorkers              uint32
	MaxQueuedOperations     uint32
}

func (limits AggregateOpeningLimits) validate() error {
	if limits.MaxGeneratorDerivations == 0 ||
		limits.MaxPrecomputedPoints == 0 ||
		limits.MaxQueries == 0 ||
		limits.MaxQueries > maxAggregateOpeningQueries ||
		limits.MaxScalarDecodes == 0 ||
		limits.MaxMSMTerms == 0 ||
		limits.MaxTemporaryBytes == 0 ||
		limits.MaxWorkers == 0 ||
		limits.MaxQueuedOperations > maxAggregateQueuedOperations {
		return errInvalidAggregateOpeningLimits
	}

	return nil
}

// AggregateOpeningResource identifies one bounded setup or proof resource.
type AggregateOpeningResource uint8

const (
	// AggregateOpeningResourceGeneratorDerivations counts fixed generators.
	AggregateOpeningResourceGeneratorDerivations AggregateOpeningResource = iota + 1

	// AggregateOpeningResourcePrecomputedPoints counts fixed-base tables.
	AggregateOpeningResourcePrecomputedPoints

	// AggregateOpeningResourceQueries counts transcript openings.
	AggregateOpeningResourceQueries

	// AggregateOpeningResourceScalarDecodes counts canonical field decodings.
	AggregateOpeningResourceScalarDecodes

	// AggregateOpeningResourceMSMTerms conservatively counts backend terms.
	AggregateOpeningResourceMSMTerms

	// AggregateOpeningResourceTemporaryBytes counts conservative scratch.
	AggregateOpeningResourceTemporaryBytes

	// AggregateOpeningResourceWorkers counts dependency-owned workers.
	AggregateOpeningResourceWorkers

	// AggregateOpeningResourceQueuedOperations counts calls waiting to enter
	// the dependency's uncancellable proof boundary.
	AggregateOpeningResourceQueuedOperations
)

// AggregateOpeningResourceError reports a rejected resource bound without
// disclosing vectors, commitments, evaluations, or proofs.
type AggregateOpeningResourceError struct {
	Resource AggregateOpeningResource
	Limit    uint64
	Actual   uint64
}

// Error implements error.
func (err *AggregateOpeningResourceError) Error() string {
	return fmt.Sprintf(
		"%v: resource %d has value %d, limit %d",
		errAggregateOpeningResource,
		err.Resource,
		err.Actual,
		err.Limit,
	)
}

// Unwrap makes AggregateOpeningResourceError match the resource sentinel.
func (err *AggregateOpeningResourceError) Unwrap() error {
	return errAggregateOpeningResource
}

// AggregateProverQuery binds one caller-owned committed vector to one
// in-domain opening. The engine consumes Vector synchronously without retaining
// it; callers must keep it immutable until the operation returns. The engine
// rejects a vector whose commitment does not match Commitment.
type AggregateProverQuery struct {
	Commitment VectorCommitment
	Vector     *Vector
	Index      uint8
}

// AggregateVerifierQuery binds one commitment, canonical scalar evaluation,
// and in-domain index. The scalar is little-endian and must be canonical.
type AggregateVerifierQuery struct {
	Commitment VectorCommitment
	Value      [scalarSize]byte
	Index      uint8
}

// AggregateOpeningBinding binds one package-owned proof statement into the
// aggregate-opening transcript. Bound operations also add one fixed nonzero
// anchor opening so even an all-zero vector proof depends on this binding.
type AggregateOpeningBinding [32]byte

// AggregateOpeningEngine owns the fixed profile's IPA settings. Construction
// is explicit; the engine is immutable and safe for concurrent operations.
type AggregateOpeningEngine struct {
	backend           aggregateOpeningBackend
	limits            AggregateOpeningLimits
	bindingCommitment VectorCommitment
	gate              *aggregateOpeningGate
	valid             bool
}

type aggregateOpeningGate struct {
	active    chan struct{}
	queued    atomic.Uint32
	maxQueued uint32
}

type aggregateOpeningBackend interface {
	commit([]fr.Element) banderwagon.Element
	open(
		*AggregateOpeningBinding,
		[]*banderwagon.Element,
		[][]fr.Element,
		[]uint8,
	) (*multiproof.MultiProof, error)
	verify(
		*AggregateOpeningBinding,
		*multiproof.MultiProof,
		[]*banderwagon.Element,
		[]*fr.Element,
		[]uint8,
	) (bool, error)
}

type ipaAggregateOpeningBackend struct {
	config *ipa.IPAConfig
}

func (backend *ipaAggregateOpeningBackend) commit(
	polynomial []fr.Element,
) banderwagon.Element {
	return backend.config.Commit(polynomial)
}

func (backend *ipaAggregateOpeningBackend) open(
	binding *AggregateOpeningBinding,
	commitments []*banderwagon.Element,
	polynomials [][]fr.Element,
	indices []uint8,
) (*multiproof.MultiProof, error) {
	return multiproof.CreateMultiProof(
		newAggregateOpeningTranscript(binding),
		backend.config,
		commitments,
		polynomials,
		indices,
	)
}

func (backend *ipaAggregateOpeningBackend) verify(
	binding *AggregateOpeningBinding,
	proof *multiproof.MultiProof,
	commitments []*banderwagon.Element,
	values []*fr.Element,
	indices []uint8,
) (bool, error) {
	return multiproof.CheckMultiProof(
		newAggregateOpeningTranscript(binding),
		backend.config,
		proof,
		commitments,
		values,
		indices,
	)
}

// NewAggregateOpeningEngine explicitly initializes the pinned IPA settings
// after preflighting setup resources and the dependency's worker count.
func NewAggregateOpeningEngine(
	ctx context.Context,
	limits AggregateOpeningLimits,
) (*AggregateOpeningEngine, error) {
	return newAggregateOpeningEngine(ctx, limits, ipa.NewIPASettings)
}

func newAggregateOpeningEngine(
	ctx context.Context,
	limits AggregateOpeningLimits,
	newSettings func() (*ipa.IPAConfig, error),
) (*AggregateOpeningEngine, error) {
	if err := checkAggregateOpeningContext(ctx); err != nil {
		return nil, err
	}
	if err := limits.validate(); err != nil {
		return nil, err
	}
	for _, check := range [...]struct {
		resource AggregateOpeningResource
		limit    uint64
		actual   uint64
	}{
		{AggregateOpeningResourceGeneratorDerivations, uint64(limits.MaxGeneratorDerivations), VectorWidth},
		{AggregateOpeningResourcePrecomputedPoints, uint64(limits.MaxPrecomputedPoints), VectorWidth},
		{AggregateOpeningResourceTemporaryBytes, limits.MaxTemporaryBytes, aggregateSetupWorkingBytes},
		{AggregateOpeningResourceWorkers, uint64(limits.MaxWorkers), uint64(runtime.NumCPU())},
	} {
		if err := checkAggregateOpeningResource(check.resource, check.limit, check.actual); err != nil {
			return nil, err
		}
	}

	if newSettings == nil {
		return nil, errInvalidAggregateOpeningEngine
	}
	config, err := newSettings()
	if err != nil {
		return nil, fmt.Errorf("%w: settings: %v", errInvalidAggregateOpeningEngine, err)
	}
	if err := checkAggregateOpeningContext(ctx); err != nil {
		return nil, err
	}
	if config == nil ||
		validateGeneratorSet(config.SRS) != nil ||
		!validIPAAuxiliaryGenerator(config.Q) {
		return nil, errGeneratorMismatch
	}

	return &AggregateOpeningEngine{
		backend: &ipaAggregateOpeningBackend{config: config},
		limits:  limits,
		bindingCommitment: VectorCommitment{
			value: commitment{element: config.SRS[0]},
			valid: true,
		},
		gate: &aggregateOpeningGate{
			active:    make(chan struct{}, 1),
			maxQueued: limits.MaxQueuedOperations,
		},
		valid: true,
	}, nil
}

func validIPAAuxiliaryGenerator(generator banderwagon.Element) bool {
	encoded := generator.Bytes()

	return string(encoded[:]) == pinnedIPAAuxiliaryGenerator
}

// Open creates one canonical aggregate proof in exact caller query order.
func (engine *AggregateOpeningEngine) Open(
	ctx context.Context,
	queries []AggregateProverQuery,
) (OpeningProof, error) {
	return engine.open(ctx, nil, aggregateProverQueryReferences(queries))
}

// OpenBound creates one canonical aggregate proof bound to an exact
// package-owned statement digest.
func (engine *AggregateOpeningEngine) OpenBound(
	ctx context.Context,
	binding AggregateOpeningBinding,
	queries []AggregateProverQuery,
) (OpeningProof, error) {
	return engine.open(ctx, &binding, aggregateProverQueryReferences(queries))
}

// OpenBoundReferences creates one canonical bound proof from caller-owned
// queries without copying their vectors. The engine consumes the references
// synchronously and does not retain them; callers must keep every query
// immutable until the call returns.
func (engine *AggregateOpeningEngine) OpenBoundReferences(
	ctx context.Context,
	binding AggregateOpeningBinding,
	queries []*AggregateProverQuery,
) (OpeningProof, error) {
	return engine.open(ctx, &binding, queries)
}

func (engine *AggregateOpeningEngine) open(
	ctx context.Context,
	binding *AggregateOpeningBinding,
	queries []*AggregateProverQuery,
) (OpeningProof, error) {
	if err := engine.validate(); err != nil {
		return OpeningProof{}, err
	}
	if err := checkAggregateOpeningContext(ctx); err != nil {
		return OpeningProof{}, err
	}
	if len(queries) == 0 {
		return OpeningProof{}, fmt.Errorf("%w: empty query set", errInvalidAggregateOpeningQuery)
	}
	queryCount := uint64(len(queries))
	if err := engine.preflight(queryCount, VectorWidth); err != nil {
		return OpeningProof{}, err
	}
	for queryIndex := range queries {
		if queries[queryIndex] == nil || queries[queryIndex].Vector == nil {
			return OpeningProof{}, fmt.Errorf(
				"%w: query %d",
				errInvalidAggregateOpeningQuery,
				queryIndex,
			)
		}
	}
	needsBindingAnchor := binding != nil &&
		!engine.hasBindingProverQuery(queries)
	if needsBindingAnchor {
		queryCount++
		if err := engine.preflight(queryCount, VectorWidth); err != nil {
			return OpeningProof{}, err
		}
		anchor := engine.bindingProverQuery()
		queries = append([]*AggregateProverQuery{&anchor}, queries...)
	}
	release, err := engine.gate.acquire(ctx)
	if err != nil {
		return OpeningProof{}, err
	}
	defer release()

	// go-ipa batch-normalizes commitment pointers in place, so every query
	// retains distinct commitment storage even when polynomial preparation is shared.
	commitments := make([]banderwagon.Element, len(queries))
	commitmentPointers := make([]*banderwagon.Element, len(queries))
	polynomials := make([][]fr.Element, len(queries))
	indices := make([]uint8, len(queries))
	seen := make(map[aggregateOpeningQueryIdentity]struct{}, len(queries))
	preparedCapacity := aggregateOpeningPreparationCapacity(len(queries))
	preparedIndexes := make(map[[commitmentSize]byte]int, preparedCapacity)
	preparedVectors := make(
		[]preparedAggregateProverVector,
		0,
		preparedCapacity,
	)
	for queryIndex := range queries {
		if err := checkAggregateOpeningContext(ctx); err != nil {
			return OpeningProof{}, err
		}
		query := queries[queryIndex]
		if !query.Commitment.valid {
			return OpeningProof{}, fmt.Errorf("%w: commitment %d", errInvalidAggregateOpeningQuery, queryIndex)
		}
		identity := aggregateOpeningIdentity(query.Commitment, query.Index)
		if _, exists := seen[identity]; exists {
			return OpeningProof{}, fmt.Errorf("%w: duplicate query", errInvalidAggregateOpeningQuery)
		}
		seen[identity] = struct{}{}

		if preparedIndex, exists := preparedIndexes[identity.commitment]; exists {
			existing := &preparedVectors[preparedIndex]
			if *existing.vector != *query.Vector {
				return OpeningProof{}, fmt.Errorf(
					"%w: commitment vector mismatch",
					errInvalidAggregateOpeningQuery,
				)
			}
			commitments[queryIndex] = existing.commitment
			commitmentPointers[queryIndex] = &commitments[queryIndex]
			polynomials[queryIndex] = existing.polynomial
			indices[queryIndex] = query.Index

			continue
		}

		polynomial := make([]fr.Element, VectorWidth)
		for scalarIndex := range *query.Vector {
			if err := checkAggregateOpeningContext(ctx); err != nil {
				return OpeningProof{}, err
			}
			decoded, err := decodeScalar(query.Vector[scalarIndex][:])
			if err != nil {
				return OpeningProof{}, fmt.Errorf(
					"%w: query %d scalar %d",
					errInvalidAggregateOpeningQuery,
					queryIndex,
					scalarIndex,
				)
			}
			polynomial[scalarIndex] = decoded.element
		}
		computed := engine.backend.commit(polynomial)
		if !computed.Equal(&query.Commitment.value.element) {
			return OpeningProof{}, fmt.Errorf("%w: commitment mismatch", errInvalidAggregateOpeningQuery)
		}
		preparedIndex := len(preparedVectors)
		preparedVectors = append(preparedVectors, preparedAggregateProverVector{
			vector:     query.Vector,
			commitment: computed,
			polynomial: polynomial,
		})
		preparedIndexes[identity.commitment] = preparedIndex
		commitments[queryIndex] = computed
		commitmentPointers[queryIndex] = &commitments[queryIndex]
		polynomials[queryIndex] = polynomial
		indices[queryIndex] = query.Index
	}

	generated, err := engine.backend.open(
		binding,
		commitmentPointers,
		polynomials,
		indices,
	)
	if err != nil {
		return OpeningProof{}, fmt.Errorf("%w: %v", errAggregateOpeningGeneration, err)
	}
	if err := checkAggregateOpeningContext(ctx); err != nil {
		return OpeningProof{}, err
	}
	encoded, err := encodeNativeAggregateOpeningProof(generated)
	if err != nil {
		return OpeningProof{}, err
	}

	return DecodeOpeningProof(
		ctx,
		encoded[:],
		OpeningProofLimits{
			MaxProofBytes:    OpeningProofSize,
			MaxPointDecodes:  OpeningProofPointDecodes,
			MaxScalarDecodes: OpeningProofScalarDecodes,
		},
	)
}

// Verify checks every supplied opening against one canonical aggregate proof.
// It returns a typed verification error for a well-formed invalid proof.
func (engine *AggregateOpeningEngine) Verify(
	ctx context.Context,
	proof OpeningProof,
	queries []AggregateVerifierQuery,
) error {
	return engine.verify(ctx, nil, proof, queries)
}

// VerifyBound verifies every opening under the exact package-owned statement
// digest supplied during generation.
func (engine *AggregateOpeningEngine) VerifyBound(
	ctx context.Context,
	binding AggregateOpeningBinding,
	proof OpeningProof,
	queries []AggregateVerifierQuery,
) error {
	return engine.verify(ctx, &binding, proof, queries)
}

func (engine *AggregateOpeningEngine) verify(
	ctx context.Context,
	binding *AggregateOpeningBinding,
	proof OpeningProof,
	queries []AggregateVerifierQuery,
) error {
	if err := engine.validate(); err != nil {
		return err
	}
	if err := checkAggregateOpeningContext(ctx); err != nil {
		return err
	}
	if len(queries) == 0 || !proof.valid {
		return fmt.Errorf("%w: empty or invalid input", errInvalidAggregateOpeningQuery)
	}
	queryCount := uint64(len(queries))
	if err := engine.preflight(queryCount, 1); err != nil {
		return err
	}
	needsBindingAnchor := binding != nil &&
		!engine.hasBindingVerifierQuery(queries)
	if needsBindingAnchor {
		queryCount++
		if err := engine.preflight(queryCount, 1); err != nil {
			return err
		}
		queries = append(
			[]AggregateVerifierQuery{engine.bindingVerifierQuery()},
			queries...,
		)
	}
	release, err := engine.gate.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()

	nativeProof, err := nativeAggregateOpeningProof(proof)
	if err != nil {
		return err
	}
	commitments := make([]banderwagon.Element, len(queries))
	commitmentPointers := make([]*banderwagon.Element, len(queries))
	values := make([]fr.Element, len(queries))
	valuePointers := make([]*fr.Element, len(queries))
	indices := make([]uint8, len(queries))
	seen := make(map[aggregateOpeningQueryIdentity]struct{}, len(queries))
	for index := range queries {
		if err := checkAggregateOpeningContext(ctx); err != nil {
			return err
		}
		query := queries[index]
		if !query.Commitment.valid {
			return fmt.Errorf("%w: commitment %d", errInvalidAggregateOpeningQuery, index)
		}
		identity := aggregateOpeningIdentity(query.Commitment, query.Index)
		if _, exists := seen[identity]; exists {
			return fmt.Errorf("%w: duplicate query", errInvalidAggregateOpeningQuery)
		}
		seen[identity] = struct{}{}
		decoded, err := decodeScalar(query.Value[:])
		if err != nil {
			return fmt.Errorf("%w: value %d", errInvalidAggregateOpeningQuery, index)
		}
		commitments[index] = query.Commitment.value.element
		commitmentPointers[index] = &commitments[index]
		values[index] = decoded.element
		valuePointers[index] = &values[index]
		indices[index] = query.Index
	}

	ok, err := engine.backend.verify(
		binding,
		&nativeProof,
		commitmentPointers,
		valuePointers,
		indices,
	)
	if err != nil {
		return fmt.Errorf("%w: backend", errAggregateOpeningVerification)
	}
	if err := checkAggregateOpeningContext(ctx); err != nil {
		return err
	}
	if !ok {
		return errAggregateOpeningVerification
	}

	return nil
}

func (engine *AggregateOpeningEngine) bindingProverQuery() AggregateProverQuery {
	var vector Vector
	vector[0][0] = 1

	return AggregateProverQuery{
		Commitment: engine.bindingCommitment,
		Vector:     &vector,
		Index:      0,
	}
}

func (engine *AggregateOpeningEngine) bindingVerifierQuery() AggregateVerifierQuery {
	var value [scalarSize]byte
	value[0] = 1

	return AggregateVerifierQuery{
		Commitment: engine.bindingCommitment,
		Value:      value,
		Index:      0,
	}
}

func (engine *AggregateOpeningEngine) hasBindingProverQuery(
	queries []*AggregateProverQuery,
) bool {
	anchor := engine.bindingProverQuery()
	anchorIdentity := aggregateOpeningIdentity(anchor.Commitment, anchor.Index)
	for index := range queries {
		query := queries[index]
		if !query.Commitment.valid {
			continue
		}
		if aggregateOpeningIdentity(query.Commitment, query.Index) == anchorIdentity &&
			*query.Vector == *anchor.Vector {
			return true
		}
	}

	return false
}

func aggregateProverQueryReferences(
	queries []AggregateProverQuery,
) []*AggregateProverQuery {
	references := make([]*AggregateProverQuery, len(queries))
	for index := range queries {
		references[index] = &queries[index]
	}

	return references
}

func (engine *AggregateOpeningEngine) hasBindingVerifierQuery(
	queries []AggregateVerifierQuery,
) bool {
	anchor := engine.bindingVerifierQuery()
	anchorIdentity := aggregateOpeningIdentity(anchor.Commitment, anchor.Index)
	for index := range queries {
		query := queries[index]
		if !query.Commitment.valid {
			continue
		}
		if aggregateOpeningIdentity(query.Commitment, query.Index) == anchorIdentity &&
			query.Value == anchor.Value {
			return true
		}
	}

	return false
}

func newAggregateOpeningTranscript(
	binding *AggregateOpeningBinding,
) *common.Transcript {
	transcript := common.NewTranscript(aggregateTranscriptLabel)
	if binding != nil {
		transcript.AppendMessage(binding[:], []byte(aggregateBindingLabel))
	}

	return transcript
}

func (engine *AggregateOpeningEngine) validate() error {
	if engine == nil ||
		!engine.valid ||
		engine.backend == nil ||
		engine.gate == nil ||
		engine.gate.active == nil ||
		cap(engine.gate.active) != 1 ||
		engine.gate.maxQueued != engine.limits.MaxQueuedOperations ||
		engine.limits.validate() != nil {
		return errInvalidAggregateOpeningEngine
	}

	return nil
}

func (gate *aggregateOpeningGate) acquire(ctx context.Context) (func(), error) {
	select {
	case gate.active <- struct{}{}:
		return gate.finishAcquire(ctx)
	default:
	}

	for {
		queued := gate.queued.Load()
		if queued >= gate.maxQueued {
			return nil, &AggregateOpeningResourceError{
				Resource: AggregateOpeningResourceQueuedOperations,
				Limit:    uint64(gate.maxQueued),
				Actual:   uint64(queued) + 1,
			}
		}
		if gate.queued.CompareAndSwap(queued, queued+1) {
			break
		}
	}
	defer gate.queued.Add(^uint32(0))

	select {
	case gate.active <- struct{}{}:
		return gate.finishAcquire(ctx)
	case <-ctx.Done():
		return nil, checkAggregateOpeningContext(ctx)
	}
}

func (gate *aggregateOpeningGate) finishAcquire(ctx context.Context) (func(), error) {
	if err := checkAggregateOpeningContext(ctx); err != nil {
		<-gate.active

		return nil, err
	}

	return func() { <-gate.active }, nil
}

func encodeNativeAggregateOpeningProof(
	proof *multiproof.MultiProof,
) ([OpeningProofSize]byte, error) {
	if proof == nil ||
		len(proof.IPA.L) != 8 ||
		len(proof.IPA.R) != 8 {
		return [OpeningProofSize]byte{}, fmt.Errorf(
			"%w: backend proof shape",
			errAggregateOpeningGeneration,
		)
	}

	var encoded [OpeningProofSize]byte
	points := make([]banderwagon.Element, 0, openingProofPointCount)
	points = append(points, proof.D)
	points = append(points, proof.IPA.L...)
	points = append(points, proof.IPA.R...)
	for index := range points {
		point := points[index].Bytes()
		copy(encoded[index*commitmentSize:], point[:])
	}
	final := proof.IPA.A_scalar.BytesLE()
	copy(encoded[OpeningProofSize-scalarSize:], final[:])

	return encoded, nil
}

func (engine *AggregateOpeningEngine) preflight(
	queryCount uint64,
	scalarDecodesPerQuery uint64,
) error {
	if err := checkAggregateOpeningResource(
		AggregateOpeningResourceQueries,
		uint64(engine.limits.MaxQueries),
		queryCount,
	); err != nil {
		return err
	}

	// QueryCount is now bounded by maxAggregateOpeningQueries and the only
	// internal per-query factors are one or VectorWidth, so the derived
	// accounting below cannot overflow.
	scalarDecodes := queryCount * scalarDecodesPerQuery
	msmTerms := queryCount*VectorWidth + aggregateFixedMSMTerms
	temporaryBytes := queryCount * aggregateQueryWorkingBytes
	for _, check := range [...]struct {
		resource AggregateOpeningResource
		limit    uint64
		actual   uint64
	}{
		{AggregateOpeningResourceScalarDecodes, engine.limits.MaxScalarDecodes, scalarDecodes},
		{AggregateOpeningResourceMSMTerms, engine.limits.MaxMSMTerms, msmTerms},
		{AggregateOpeningResourceTemporaryBytes, engine.limits.MaxTemporaryBytes, temporaryBytes},
		{AggregateOpeningResourceWorkers, uint64(engine.limits.MaxWorkers), uint64(runtime.NumCPU())},
	} {
		if err := checkAggregateOpeningResource(check.resource, check.limit, check.actual); err != nil {
			return err
		}
	}

	return nil
}

type aggregateOpeningQueryIdentity struct {
	commitment [commitmentSize]byte
	index      uint8
}

type preparedAggregateProverVector struct {
	vector     *Vector
	commitment banderwagon.Element
	polynomial []fr.Element
}

func aggregateOpeningPreparationCapacity(queryCount int) int {
	// A complete fixed-width vector accounts for 256 queries. Larger proofs
	// may contain more committed vectors, but letting the slices and map grow
	// as needed avoids reserving one preparation entry for every hostile query.
	return min(queryCount, VectorWidth)
}

func aggregateOpeningIdentity(
	commitment VectorCommitment,
	index uint8,
) aggregateOpeningQueryIdentity {
	return aggregateOpeningQueryIdentity{
		commitment: encodeCommitment(commitment.value),
		index:      index,
	}
}

func nativeAggregateOpeningProof(proof OpeningProof) (multiproof.MultiProof, error) {
	if !proof.valid {
		return multiproof.MultiProof{}, errInvalidOpeningProof
	}
	points := make([]banderwagon.Element, openingProofPointCount)
	for index := range points {
		start := index * commitmentSize
		decoded, err := decodeOpeningProofPoint(proof.encoded[start : start+commitmentSize])
		if err != nil {
			return multiproof.MultiProof{}, errInvalidOpeningProof
		}
		points[index] = decoded.element
	}
	final, err := decodeScalar(proof.encoded[OpeningProofSize-scalarSize:])
	if err != nil {
		return multiproof.MultiProof{}, errInvalidOpeningProof
	}

	return multiproof.MultiProof{
		D: points[0],
		IPA: ipa.IPAProof{
			L:        append([]banderwagon.Element(nil), points[1:9]...),
			R:        append([]banderwagon.Element(nil), points[9:17]...),
			A_scalar: final.element,
		},
	}, nil
}

func checkAggregateOpeningContext(ctx context.Context) error {
	if ctx == nil {
		return errInvalidAggregateOpeningContext
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(errAggregateOpeningCancelled, err)
	}

	return nil
}

func checkAggregateOpeningResource(
	resource AggregateOpeningResource,
	limit uint64,
	actual uint64,
) error {
	if actual <= limit {
		return nil
	}

	return &AggregateOpeningResourceError{
		Resource: resource,
		Limit:    limit,
		Actual:   actual,
	}
}
