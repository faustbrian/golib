package backend

import (
	"context"
	"errors"
	"fmt"
	"runtime"

	multiproof "github.com/crate-crypto/go-ipa"
	"github.com/crate-crypto/go-ipa/bandersnatch/fr"
	"github.com/crate-crypto/go-ipa/banderwagon"
	"github.com/crate-crypto/go-ipa/common"
	"github.com/crate-crypto/go-ipa/ipa"
)

const (
	maxAggregateOpeningQueries = uint32(65_536)
	aggregateSetupWorkingBytes = uint64(1 << 30)
	aggregateQueryWorkingBytes = uint64(VectorWidth*scalarSize) * 8
	aggregateFixedMSMTerms     = uint64(2 * VectorWidth * 8)
	aggregateTranscriptLabel   = "verkle"
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
// operations. Every field must be positive and no field denotes an unbounded
// resource. The pinned backend may use exactly runtime.NumCPU workers; an
// operation is rejected before setup when MaxWorkers is smaller.
type AggregateOpeningLimits struct {
	MaxGeneratorDerivations uint32
	MaxPrecomputedPoints    uint32
	MaxQueries              uint32
	MaxScalarDecodes        uint64
	MaxMSMTerms             uint64
	MaxTemporaryBytes       uint64
	MaxWorkers              uint32
}

func (limits AggregateOpeningLimits) validate() error {
	if limits.MaxGeneratorDerivations == 0 ||
		limits.MaxPrecomputedPoints == 0 ||
		limits.MaxQueries == 0 ||
		limits.MaxQueries > maxAggregateOpeningQueries ||
		limits.MaxScalarDecodes == 0 ||
		limits.MaxMSMTerms == 0 ||
		limits.MaxTemporaryBytes == 0 ||
		limits.MaxWorkers == 0 {
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

// AggregateProverQuery binds one committed vector to one in-domain opening.
// The engine rejects a vector whose commitment does not match Commitment.
type AggregateProverQuery struct {
	Commitment VectorCommitment
	Vector     Vector
	Index      uint8
}

// AggregateVerifierQuery binds one commitment, canonical scalar evaluation,
// and in-domain index. The scalar is little-endian and must be canonical.
type AggregateVerifierQuery struct {
	Commitment VectorCommitment
	Value      [scalarSize]byte
	Index      uint8
}

// AggregateOpeningEngine owns the fixed profile's IPA settings. Construction
// is explicit; the engine is immutable and safe for concurrent operations.
type AggregateOpeningEngine struct {
	backend aggregateOpeningBackend
	limits  AggregateOpeningLimits
	valid   bool
}

type aggregateOpeningBackend interface {
	commit([]fr.Element) banderwagon.Element
	open(
		[]*banderwagon.Element,
		[][]fr.Element,
		[]uint8,
	) (*multiproof.MultiProof, error)
	verify(
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
	commitments []*banderwagon.Element,
	polynomials [][]fr.Element,
	indices []uint8,
) (*multiproof.MultiProof, error) {
	return multiproof.CreateMultiProof(
		common.NewTranscript(aggregateTranscriptLabel),
		backend.config,
		commitments,
		polynomials,
		indices,
	)
}

func (backend *ipaAggregateOpeningBackend) verify(
	proof *multiproof.MultiProof,
	commitments []*banderwagon.Element,
	values []*fr.Element,
	indices []uint8,
) (bool, error) {
	return multiproof.CheckMultiProof(
		common.NewTranscript(aggregateTranscriptLabel),
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
	if config == nil || validateGeneratorSet(config.SRS) != nil {
		return nil, errGeneratorMismatch
	}

	return &AggregateOpeningEngine{
		backend: &ipaAggregateOpeningBackend{config: config},
		limits:  limits,
		valid:   true,
	}, nil
}

// Open creates one canonical aggregate proof in exact caller query order.
func (engine *AggregateOpeningEngine) Open(
	ctx context.Context,
	queries []AggregateProverQuery,
) (OpeningProof, error) {
	if err := engine.validate(); err != nil {
		return OpeningProof{}, err
	}
	if err := checkAggregateOpeningContext(ctx); err != nil {
		return OpeningProof{}, err
	}
	if err := engine.preflight(uint64(len(queries)), VectorWidth); err != nil {
		return OpeningProof{}, err
	}
	if len(queries) == 0 {
		return OpeningProof{}, fmt.Errorf("%w: empty query set", errInvalidAggregateOpeningQuery)
	}

	commitments := make([]banderwagon.Element, len(queries))
	commitmentPointers := make([]*banderwagon.Element, len(queries))
	polynomials := make([][]fr.Element, len(queries))
	indices := make([]uint8, len(queries))
	seen := make(map[aggregateOpeningQueryIdentity]struct{}, len(queries))
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

		polynomial := make([]fr.Element, VectorWidth)
		for scalarIndex := range query.Vector {
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
		commitments[queryIndex] = computed
		commitmentPointers[queryIndex] = &commitments[queryIndex]
		polynomials[queryIndex] = polynomial
		indices[queryIndex] = query.Index
	}

	generated, err := engine.backend.open(
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
	if err := engine.validate(); err != nil {
		return err
	}
	if err := checkAggregateOpeningContext(ctx); err != nil {
		return err
	}
	if err := engine.preflight(uint64(len(queries)), 1); err != nil {
		return err
	}
	if len(queries) == 0 || !proof.valid {
		return fmt.Errorf("%w: empty or invalid input", errInvalidAggregateOpeningQuery)
	}

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

func (engine *AggregateOpeningEngine) validate() error {
	if engine == nil || !engine.valid || engine.backend == nil || engine.limits.validate() != nil {
		return errInvalidAggregateOpeningEngine
	}

	return nil
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
