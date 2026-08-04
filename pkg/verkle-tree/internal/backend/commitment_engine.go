package backend

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/crate-crypto/go-ipa/banderwagon"
	"github.com/crate-crypto/go-ipa/ipa"
)

const (
	// VectorWidth is the fixed vector width of the pre-v1 profile.
	VectorWidth = 256

	generatorWorkingBytes = uint64(256)
	commitWorkingBytes    = uint64(VectorWidth*scalarSize) +
		2*generatorWorkingBytes
	commitmentUpdateWorkingBytes = uint64(
		3*VectorWidth*scalarSize+2*VectorWidth,
	) + 2*generatorWorkingBytes
)

var (
	errInvalidCommitmentEngine  = errors.New("invalid commitment engine")
	errInvalidCommitmentContext = errors.New("invalid commitment context")
	errInvalidCommitmentLimits  = errors.New("invalid commitment limits")
	errCommitmentCancelled      = errors.New("commitment operation cancelled")
	errCommitmentResource       = errors.New("commitment resource limit exceeded")
	errGeneratorMismatch        = errors.New("commitment generator set mismatch")
	errInvalidCommitmentUpdate  = errors.New("invalid commitment update")
)

var pinnedGeneratorDigest = [sha256.Size]byte{
	0x1f, 0xca, 0xea, 0x10, 0xbf, 0x24, 0xf7, 0x50,
	0x20, 0x0e, 0x06, 0xfa, 0x47, 0x3c, 0x76, 0xff,
	0x04, 0x68, 0x00, 0x72, 0x91, 0xfa, 0x54, 0x8e,
	0x2d, 0x99, 0xf0, 0x9b, 0xa9, 0x25, 0x6f, 0xdb,
}

// Vector is one complete width-256 vector of canonical little-endian scalar
// encodings. Its array representation prevents caller-controlled vector sizes.
type Vector [VectorWidth][scalarSize]byte

// VectorUpdate replaces one authenticated canonical scalar at a fixed vector
// position. Callers must authenticate Old before applying the update.
type VectorUpdate struct {
	Index uint8
	Old   [scalarSize]byte
	New   [scalarSize]byte
}

// CommitmentLimits bounds setup and commitment work. Zero values are invalid
// and no field denotes an unbounded resource.
type CommitmentLimits struct {
	MaxGeneratorDerivations uint32
	MaxScalarDecodes        uint32
	MaxMSMTerms             uint32
	MaxTemporaryBytes       uint64
}

func (limits CommitmentLimits) validate() error {
	if limits.MaxGeneratorDerivations == 0 ||
		limits.MaxScalarDecodes == 0 ||
		limits.MaxMSMTerms == 0 ||
		limits.MaxTemporaryBytes == 0 {
		return errInvalidCommitmentLimits
	}

	return nil
}

// CommitmentResource identifies one bounded commitment resource.
type CommitmentResource uint8

const (
	// CommitmentResourceGeneratorDerivations counts fixed-profile generators.
	CommitmentResourceGeneratorDerivations CommitmentResource = iota + 1

	// CommitmentResourceScalarDecodes counts canonical scalar decodings.
	CommitmentResourceScalarDecodes

	// CommitmentResourceMSMTerms counts non-zero scalar multiplication terms.
	CommitmentResourceMSMTerms

	// CommitmentResourceTemporaryBytes counts deterministic scratch accounting.
	CommitmentResourceTemporaryBytes
)

// CommitmentResourceError reports a configured commitment bound and rejected
// value without disclosing vector contents.
type CommitmentResourceError struct {
	Resource CommitmentResource
	Limit    uint64
	Actual   uint64
}

// Error implements error.
func (err *CommitmentResourceError) Error() string {
	return fmt.Sprintf(
		"%v: resource %d has value %d, limit %d",
		errCommitmentResource,
		err.Resource,
		err.Actual,
		err.Limit,
	)
}

// Unwrap makes CommitmentResourceError match errCommitmentResource.
func (err *CommitmentResourceError) Unwrap() error {
	return errCommitmentResource
}

// CommitmentEngine owns the exact pre-v1 profile generator set. Once
// constructed, it is immutable and safe for concurrent commitment operations.
type CommitmentEngine struct {
	limits     CommitmentLimits
	generators [VectorWidth]banderwagon.Element
	valid      bool
}

// VectorCommitment is an opaque in-memory commitment. It may represent the
// internal identity, which has no accepted untrusted wire encoding yet.
type VectorCommitment struct {
	value commitment
	valid bool
}

// EmptyVectorCommitment returns the mathematical commitment to the all-zero
// vector. It is valid only as opaque in-memory state and has no accepted point
// encoding.
func EmptyVectorCommitment() VectorCommitment {
	var identity banderwagon.Element
	identity.SetIdentity()

	return VectorCommitment{
		value: commitment{element: identity},
		valid: true,
	}
}

// NewCommitmentEngine explicitly derives the fixed generator set and starts no
// engine-owned goroutines. The pinned dependency's own initialization remains
// a documented production blocker.
func NewCommitmentEngine(
	ctx context.Context,
	limits CommitmentLimits,
) (*CommitmentEngine, error) {
	if err := checkCommitmentContext(ctx); err != nil {
		return nil, err
	}
	if err := limits.validate(); err != nil {
		return nil, err
	}
	if err := checkCommitmentResource(
		CommitmentResourceGeneratorDerivations,
		uint64(limits.MaxGeneratorDerivations),
		VectorWidth,
	); err != nil {
		return nil, err
	}
	if err := checkCommitmentResource(
		CommitmentResourceTemporaryBytes,
		limits.MaxTemporaryBytes,
		VectorWidth*generatorWorkingBytes,
	); err != nil {
		return nil, err
	}

	derived := ipa.GenerateRandomPoints(VectorWidth)

	return newCommitmentEngineFromGenerators(ctx, limits, derived)
}

// UpdateCapacity returns the maximum number of vector positions that one
// sparse commitment update can process under the engine's fixed limits. A
// zero or invalid engine reports zero so callers can fall back to full commits.
func (engine *CommitmentEngine) UpdateCapacity() uint16 {
	if engine == nil || !engine.valid || engine.limits.validate() != nil {
		return 0
	}
	capacity := min(
		uint32(VectorWidth),
		engine.limits.MaxScalarDecodes/2,
		engine.limits.MaxMSMTerms,
	)

	return uint16(capacity)
}

func newCommitmentEngineFromGenerators(
	ctx context.Context,
	limits CommitmentLimits,
	derived []banderwagon.Element,
) (*CommitmentEngine, error) {
	if err := checkCommitmentContext(ctx); err != nil {
		return nil, err
	}
	if err := validateGeneratorSet(derived); err != nil {
		return nil, err
	}

	engine := &CommitmentEngine{limits: limits, valid: true}
	copy(engine.generators[:], derived)

	return engine, nil
}

func validateGeneratorSet(generators []banderwagon.Element) error {
	if len(generators) != VectorWidth {
		return errGeneratorMismatch
	}
	digest := sha256.New()
	for index := range generators {
		encoded := encodeCommitment(commitment{element: generators[index]})
		_, _ = digest.Write(encoded[:])
	}
	if [sha256.Size]byte(digest.Sum(nil)) != pinnedGeneratorDigest {
		return errGeneratorMismatch
	}

	return nil
}

// Commit commits to a complete fixed-width vector.
func (engine *CommitmentEngine) Commit(
	ctx context.Context,
	vector Vector,
) (VectorCommitment, error) {
	if engine == nil || !engine.valid || engine.limits.validate() != nil {
		return VectorCommitment{}, errInvalidCommitmentEngine
	}
	if err := checkCommitmentContext(ctx); err != nil {
		return VectorCommitment{}, err
	}
	if err := checkCommitmentResource(
		CommitmentResourceTemporaryBytes,
		engine.limits.MaxTemporaryBytes,
		commitWorkingBytes,
	); err != nil {
		return VectorCommitment{}, err
	}

	terms := uint64(0)
	for index := range vector {
		if err := checkCommitmentContext(ctx); err != nil {
			return VectorCommitment{}, err
		}
		if vector[index] != ([scalarSize]byte{}) {
			terms++
		}
	}
	if err := checkCommitmentResource(
		CommitmentResourceScalarDecodes,
		uint64(engine.limits.MaxScalarDecodes),
		terms,
	); err != nil {
		return VectorCommitment{}, err
	}
	if err := checkCommitmentResource(
		CommitmentResourceMSMTerms,
		uint64(engine.limits.MaxMSMTerms),
		terms,
	); err != nil {
		return VectorCommitment{}, err
	}

	var result banderwagon.Element
	result.SetIdentity()
	for index := range vector {
		if vector[index] == ([scalarSize]byte{}) {
			continue
		}
		if err := checkCommitmentContext(ctx); err != nil {
			return VectorCommitment{}, err
		}
		decoded, err := decodeScalar(vector[index][:])
		if err != nil {
			return VectorCommitment{}, err
		}

		var term banderwagon.Element
		term.ScalarMul(&engine.generators[index], &decoded.element)
		var sum banderwagon.Element
		sum.Add(&result, &term)
		result = sum
	}
	if err := checkCommitmentContext(ctx); err != nil {
		return VectorCommitment{}, err
	}

	return VectorCommitment{
		value: commitment{element: result},
		valid: true,
	}, nil
}

// UpdateCommitment applies a canonical set of authenticated scalar changes to
// one opaque commitment. Input order does not affect the result. Duplicate
// positions and non-canonical scalars fail before group arithmetic.
func (engine *CommitmentEngine) UpdateCommitment(
	ctx context.Context,
	committed VectorCommitment,
	updates []VectorUpdate,
) (VectorCommitment, error) {
	if engine == nil || !engine.valid || engine.limits.validate() != nil {
		return VectorCommitment{}, errInvalidCommitmentEngine
	}
	if err := checkCommitmentContext(ctx); err != nil {
		return VectorCommitment{}, err
	}
	if !committed.valid {
		return VectorCommitment{}, errInvalidCommitment
	}
	if len(updates) > VectorWidth {
		return VectorCommitment{}, errInvalidCommitmentUpdate
	}
	if len(updates) == 0 {
		return committed, nil
	}
	if err := checkCommitmentResource(
		CommitmentResourceTemporaryBytes,
		engine.limits.MaxTemporaryBytes,
		commitmentUpdateWorkingBytes,
	); err != nil {
		return VectorCommitment{}, err
	}
	scalarDecodes := uint64(len(updates)) * 2
	if err := checkCommitmentResource(
		CommitmentResourceScalarDecodes,
		uint64(engine.limits.MaxScalarDecodes),
		scalarDecodes,
	); err != nil {
		return VectorCommitment{}, err
	}

	owned := make([]VectorUpdate, len(updates))
	var present [VectorWidth]bool
	for index := range updates {
		if err := checkCommitmentContext(ctx); err != nil {
			return VectorCommitment{}, err
		}
		position := updates[index].Index
		if present[position] {
			return VectorCommitment{}, errInvalidCommitmentUpdate
		}
		present[position] = true
		owned[index] = updates[index]
	}

	var deltas [VectorWidth]scalar
	terms := uint64(0)
	for index := range updates {
		if err := checkCommitmentContext(ctx); err != nil {
			return VectorCommitment{}, err
		}
		oldValue, err := decodeScalar(owned[index].Old[:])
		if err != nil {
			return VectorCommitment{}, err
		}
		newValue, err := decodeScalar(owned[index].New[:])
		if err != nil {
			return VectorCommitment{}, err
		}
		position := owned[index].Index
		deltas[position].element.Sub(&newValue.element, &oldValue.element)
		if !deltas[position].element.IsZero() {
			terms++
		}
	}
	if err := checkCommitmentResource(
		CommitmentResourceMSMTerms,
		uint64(engine.limits.MaxMSMTerms),
		terms,
	); err != nil {
		return VectorCommitment{}, err
	}

	result := committed.value.element
	for position := range deltas {
		if !present[position] || deltas[position].element.IsZero() {
			continue
		}
		if err := checkCommitmentContext(ctx); err != nil {
			return VectorCommitment{}, err
		}
		var term banderwagon.Element
		term.ScalarMul(&engine.generators[position], &deltas[position].element)
		var sum banderwagon.Element
		sum.Add(&result, &term)
		result = sum
	}
	if err := checkCommitmentContext(ctx); err != nil {
		return VectorCommitment{}, err
	}

	return VectorCommitment{
		value: commitment{element: result},
		valid: true,
	}, nil
}

// IsIdentity reports whether the commitment is the internal group identity.
func (value VectorCommitment) IsIdentity() (bool, error) {
	if !value.valid {
		return false, errInvalidCommitment
	}
	var identity banderwagon.Element
	identity.SetIdentity()

	return value.value.element.Equal(&identity), nil
}

// Bytes returns the canonical non-identity commitment encoding. Until the
// profile freezes an empty-root container, identity serialization fails.
func (value VectorCommitment) Bytes() ([commitmentSize]byte, error) {
	identity, err := value.IsIdentity()
	if err != nil {
		return [commitmentSize]byte{}, err
	}
	if identity {
		return [commitmentSize]byte{}, fmt.Errorf(
			"%w: identity",
			errInvalidCommitment,
		)
	}

	return encodeCommitment(value.value), nil
}

// ScalarBytes returns the canonical commitment-to-field image. The internal
// identity maps to scalar zero.
func (value VectorCommitment) ScalarBytes() ([scalarSize]byte, error) {
	identity, err := value.IsIdentity()
	if err != nil {
		return [scalarSize]byte{}, err
	}
	if identity {
		return [scalarSize]byte{}, nil
	}

	return encodeScalar(commitmentToScalar(value.value)), nil
}

// DeduplicationKey returns a stable comparable in-memory identity. The all-zero
// key is reserved for the mathematical identity and is not a wire encoding.
func (value VectorCommitment) DeduplicationKey() ([commitmentSize]byte, error) {
	identity, err := value.IsIdentity()
	if err != nil {
		return [commitmentSize]byte{}, err
	}
	if identity {
		return [commitmentSize]byte{}, nil
	}

	return encodeCommitment(value.value), nil
}

func checkCommitmentContext(ctx context.Context) error {
	if ctx == nil {
		return errInvalidCommitmentContext
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(errCommitmentCancelled, err)
	}

	return nil
}

func checkCommitmentResource(
	resource CommitmentResource,
	limit uint64,
	actual uint64,
) error {
	if actual <= limit {
		return nil
	}

	return &CommitmentResourceError{
		Resource: resource,
		Limit:    limit,
		Actual:   actual,
	}
}
