package verkletree

import (
	"context"
	"errors"
	"fmt"

	"github.com/faustbrian/golib/pkg/verkle-tree/internal/authstate"
	"github.com/faustbrian/golib/pkg/verkle-tree/internal/backend"
	"github.com/faustbrian/golib/pkg/verkle-tree/internal/committedtree"
)

const maxPublicProofQueries = uint32(65_536)

// OpeningLimits bounds setup and fixed-profile aggregate-opening work.
type OpeningLimits struct {
	MaxGeneratorDerivations uint32
	MaxPrecomputedPoints    uint32
	MaxQueries              uint32
	MaxScalarDecodes        uint64
	MaxMSMTerms             uint64
	MaxTemporaryBytes       uint64
	MaxWorkers              uint32
}

// ProofMaterialLimits bounds snapshot proof-material assembly.
type ProofMaterialLimits struct {
	MaxKeys            uint32
	MaxStemPaths       uint32
	MaxNodeReads       uint64
	MaxPathCommitments uint32
	MaxPathBytes       uint64
	MaxTemporaryBytes  uint64
}

// ProverQueryLimits bounds complete-vector query extraction.
type ProverQueryLimits struct {
	MaxKeys           uint32
	MaxQueries        uint32
	MaxNodeReads      uint64
	MaxTemporaryBytes uint64
}

// VerifierQueryLimits bounds independent public-evaluation reconstruction.
type VerifierQueryLimits struct {
	MaxQueries        uint32
	MaxTemporaryBytes uint64
}

// ProofContainerLimits bounds immutable proof construction.
type ProofContainerLimits struct {
	MaxClaims          uint32
	MaxStemPaths       uint32
	MaxPathCommitments uint32
	MaxPathDerivations uint32
	MaxPathBytes       uint64
	MaxTemporaryBytes  uint64
}

// ProofGenerationLimits binds every proof-generation stage budget.
type ProofGenerationLimits struct {
	Material        ProofMaterialLimits
	ProverQueries   ProverQueryLimits
	VerifierQueries VerifierQueryLimits
	Proof           ProofContainerLimits
}

// ProofVerificationLimits bounds independent verifier reconstruction.
type ProofVerificationLimits struct {
	VerifierQueries VerifierQueryLimits
}

// ProofEncodingLimits bounds canonical proof serialization.
type ProofEncodingLimits struct {
	MaxProofBytes     uint64
	MaxTemporaryBytes uint64
}

// ProofDecodingLimits bounds hostile canonical proof decoding. Point and
// scalar decode limits may be zero to reject before cryptographic decoding.
type ProofDecodingLimits struct {
	MaxProofBytes      uint64
	MaxClaims          uint32
	MaxStemPaths       uint32
	MaxPathCommitments uint32
	MaxPathDerivations uint32
	MaxPathBytes       uint64
	MaxPointDecodes    uint32
	MaxScalarDecodes   uint32
	MaxTemporaryBytes  uint64
}

func (limits OpeningLimits) validate() error {
	if limits.MaxGeneratorDerivations == 0 ||
		limits.MaxPrecomputedPoints == 0 ||
		limits.MaxQueries == 0 ||
		limits.MaxQueries > maxPublicProofQueries ||
		limits.MaxScalarDecodes == 0 ||
		limits.MaxMSMTerms == 0 ||
		limits.MaxTemporaryBytes == 0 ||
		limits.MaxWorkers == 0 {
		return ErrInvalidLimits
	}

	return nil
}

func (limits ProofGenerationLimits) validate() error {
	if limits.Material.MaxKeys == 0 ||
		limits.Material.MaxStemPaths == 0 ||
		limits.Material.MaxNodeReads == 0 ||
		limits.Material.MaxPathCommitments == 0 ||
		limits.Material.MaxPathBytes == 0 ||
		limits.Material.MaxTemporaryBytes == 0 ||
		limits.ProverQueries.MaxKeys == 0 ||
		limits.ProverQueries.MaxQueries == 0 ||
		limits.ProverQueries.MaxQueries > maxPublicProofQueries ||
		limits.ProverQueries.MaxNodeReads == 0 ||
		limits.ProverQueries.MaxTemporaryBytes == 0 ||
		limits.VerifierQueries.validate() != nil ||
		limits.Proof.MaxClaims == 0 ||
		limits.Proof.MaxStemPaths == 0 ||
		limits.Proof.MaxPathCommitments == 0 ||
		limits.Proof.MaxPathDerivations == 0 ||
		limits.Proof.MaxPathBytes == 0 ||
		limits.Proof.MaxTemporaryBytes == 0 {
		return ErrInvalidLimits
	}

	return nil
}

func (limits VerifierQueryLimits) validate() error {
	if limits.MaxQueries == 0 ||
		limits.MaxQueries > maxPublicProofQueries ||
		limits.MaxTemporaryBytes == 0 {
		return ErrInvalidLimits
	}

	return nil
}

func (limits ProofVerificationLimits) validate() error {
	return limits.VerifierQueries.validate()
}

func (limits ProofEncodingLimits) validate() error {
	if limits.MaxProofBytes == 0 || limits.MaxTemporaryBytes == 0 {
		return ErrInvalidLimits
	}

	return nil
}

func (limits ProofDecodingLimits) validate() error {
	if limits.MaxProofBytes == 0 ||
		limits.MaxClaims == 0 ||
		limits.MaxStemPaths == 0 ||
		limits.MaxPathCommitments == 0 ||
		limits.MaxPathDerivations == 0 ||
		limits.MaxPathBytes == 0 ||
		limits.MaxTemporaryBytes == 0 {
		return ErrInvalidLimits
	}

	return nil
}

// ClaimKind distinguishes membership from absence.
type ClaimKind uint8

const (
	// ClaimMembership states that one exact value is present.
	ClaimMembership ClaimKind = iota + 1

	// ClaimAbsence states that one exact key is absent.
	ClaimAbsence
)

// Claim is one immutable proof assertion. Its zero value is invalid.
type Claim struct {
	kind  ClaimKind
	key   Key
	value Value
}

// Kind returns whether the claim asserts membership or absence.
func (claim Claim) Kind() (ClaimKind, error) {
	if claim.kind != ClaimMembership && claim.kind != ClaimAbsence {
		return 0, ErrInvalidProof
	}

	return claim.kind, nil
}

// Key returns the exact fixed-length key.
func (claim Claim) Key() (Key, error) {
	if _, err := claim.Kind(); err != nil {
		return Key{}, err
	}

	return claim.key, nil
}

// Value returns the membership value and whether it is present. An absence
// claim returns false so an all-zero membership remains unambiguous.
func (claim Claim) Value() (Value, bool, error) {
	kind, err := claim.Kind()
	if err != nil {
		return Value{}, false, err
	}
	if kind == ClaimAbsence {
		return Value{}, false, nil
	}

	return claim.value, true, nil
}

// Proof is one immutable canonical aggregate tree proof. Its zero value
// rejects use.
type Proof struct {
	value authstate.TreeProof
	valid bool
}

// ProofEngine owns immutable fixed-profile aggregate-opening settings and is
// safe for concurrent proof generation and verification.
type ProofEngine struct {
	value *authstate.ProofEngine
	valid bool
}

// NewProofEngine explicitly initializes the fixed profile's proof backend.
func NewProofEngine(
	ctx context.Context,
	profile Profile,
	limits OpeningLimits,
) (ProofEngine, error) {
	if err := checkPublicContext(ctx); err != nil {
		return ProofEngine{}, err
	}
	if err := profile.Validate(); err != nil {
		return ProofEngine{}, ErrUnsupportedProfile
	}
	if err := limits.validate(); err != nil {
		return ProofEngine{}, err
	}
	value, err := authstate.NewProofEngine(ctx, backend.AggregateOpeningLimits{
		MaxGeneratorDerivations: limits.MaxGeneratorDerivations,
		MaxPrecomputedPoints:    limits.MaxPrecomputedPoints,
		MaxQueries:              limits.MaxQueries,
		MaxScalarDecodes:        limits.MaxScalarDecodes,
		MaxMSMTerms:             limits.MaxMSMTerms,
		MaxTemporaryBytes:       limits.MaxTemporaryBytes,
		MaxWorkers:              limits.MaxWorkers,
	})
	if err != nil {
		return ProofEngine{}, translateProofEngineError(err)
	}

	return ProofEngine{value: value, valid: true}, nil
}

func translateProofEngineError(err error) error {
	if resourceErr := translateResourceError(err); resourceErr != nil {
		return resourceErr
	}
	if errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf(
			"initialize proof engine: %w: %w",
			ErrCancelled,
			err,
		)
	}

	return fmt.Errorf("initialize proof engine: %w", ErrCryptographic)
}

// Prove binds the unordered distinct key set to one immutable snapshot.
func (engine ProofEngine) Prove(
	ctx context.Context,
	snapshot Snapshot,
	keys []Key,
	limits ProofGenerationLimits,
) (Proof, error) {
	if !engine.valid || engine.value == nil {
		return Proof{}, ErrInvalidProofEngine
	}
	if !snapshot.valid {
		return Proof{}, ErrInvalidSnapshot
	}
	if err := checkPublicContext(ctx); err != nil {
		return Proof{}, err
	}
	if err := limits.validate(); err != nil {
		return Proof{}, err
	}
	maxKeys := min(limits.Material.MaxKeys, limits.ProverQueries.MaxKeys)
	if uint64(len(keys)) > uint64(maxKeys) {
		return Proof{}, newPublicResourceError(
			ResourceKeys,
			uint64(maxKeys),
			uint64(len(keys)),
		)
	}
	owned := make([]authstate.Key, len(keys))
	for index := range keys {
		if err := checkPublicContext(ctx); err != nil {
			return Proof{}, err
		}
		owned[index] = authstate.Key(keys[index])
	}
	value, err := engine.value.Prove(
		ctx,
		snapshot.value,
		owned,
		toInternalProofGenerationLimits(limits),
	)
	if err != nil {
		return Proof{}, translateProofError("generate proof", err, false)
	}

	return Proof{value: value, valid: true}, nil
}

// Verify independently reconstructs and verifies every proof opening.
func (engine ProofEngine) Verify(
	ctx context.Context,
	proof Proof,
	limits ProofVerificationLimits,
) error {
	if !engine.valid || engine.value == nil {
		return ErrInvalidProofEngine
	}
	if !proof.valid {
		return ErrInvalidProof
	}
	if err := checkPublicContext(ctx); err != nil {
		return err
	}
	if err := limits.validate(); err != nil {
		return err
	}
	err := engine.value.Verify(ctx, proof.value, authstate.ProofVerificationLimits{
		VerifierQueries: authstate.AggregateVerifierQueryLimits{
			MaxQueries:        limits.VerifierQueries.MaxQueries,
			MaxTemporaryBytes: limits.VerifierQueries.MaxTemporaryBytes,
		},
	})
	if err != nil {
		return translateProofError("verify proof", err, true)
	}

	return nil
}

// DecodeProof validates and defensively owns one canonical proof encoding.
// Cryptographic verification remains a separate explicit operation.
func DecodeProof(
	ctx context.Context,
	encoded []byte,
	limits ProofDecodingLimits,
) (Proof, error) {
	if err := checkPublicContext(ctx); err != nil {
		return Proof{}, err
	}
	if err := limits.validate(); err != nil {
		return Proof{}, err
	}
	value, err := authstate.DecodeTreeProof(
		ctx,
		encoded,
		authstate.TreeProofDecodingLimits{
			MaxProofBytes:      limits.MaxProofBytes,
			MaxClaims:          limits.MaxClaims,
			MaxStemPaths:       limits.MaxStemPaths,
			MaxPathCommitments: limits.MaxPathCommitments,
			MaxPathDerivations: limits.MaxPathDerivations,
			MaxPathBytes:       limits.MaxPathBytes,
			MaxPointDecodes:    limits.MaxPointDecodes,
			MaxScalarDecodes:   limits.MaxScalarDecodes,
			MaxTemporaryBytes:  limits.MaxTemporaryBytes,
		},
	)
	if err != nil {
		return Proof{}, translateProofError("decode proof", err, false)
	}

	return Proof{value: value, valid: true}, nil
}

// Bytes returns caller-owned canonical proof bytes.
func (proof Proof) Bytes(
	ctx context.Context,
	limits ProofEncodingLimits,
) ([]byte, error) {
	if !proof.valid {
		return nil, ErrInvalidProof
	}
	if err := checkPublicContext(ctx); err != nil {
		return nil, err
	}
	if err := limits.validate(); err != nil {
		return nil, err
	}
	encoded, err := proof.value.Bytes(
		ctx,
		authstate.TreeProofEncodingLimits{
			MaxProofBytes:     limits.MaxProofBytes,
			MaxTemporaryBytes: limits.MaxTemporaryBytes,
		},
	)
	if err != nil {
		return nil, translateProofError("encode proof", err, false)
	}

	return encoded, nil
}

// Claims returns a caller-owned canonical copy of every bound claim.
func (proof Proof) Claims(ctx context.Context) ([]Claim, error) {
	if !proof.valid {
		return nil, ErrInvalidProof
	}
	if err := checkPublicContext(ctx); err != nil {
		return nil, err
	}
	set, err := proof.value.Claims()
	if err != nil {
		return nil, ErrInvalidProof
	}
	internalClaims, err := set.Claims(ctx)
	if err != nil {
		return nil, translateProofError("copy proof claims", err, false)
	}
	return toPublicClaims(ctx, internalClaims)
}

func toPublicClaims(
	ctx context.Context,
	internalClaims []authstate.Claim,
) ([]Claim, error) {
	claims := make([]Claim, len(internalClaims))
	for index := range internalClaims {
		if err := checkPublicContext(ctx); err != nil {
			return nil, err
		}
		claim, err := toPublicClaim(internalClaims[index])
		if err != nil {
			return nil, err
		}
		claims[index] = claim
	}

	return claims, nil
}

func toPublicClaim(value authstate.Claim) (Claim, error) {
	kind, kindErr := value.Kind()
	if kindErr != nil {
		return Claim{}, ErrInvalidProof
	}
	key, _ := value.Key()
	claimValue, _, _ := value.Value()
	publicKind := ClaimAbsence
	if kind == authstate.ClaimMembership {
		publicKind = ClaimMembership
	}

	return Claim{
		kind:  publicKind,
		key:   Key(key),
		value: Value(claimValue),
	}, nil
}

// Root returns the exact root authenticated by the proof.
func (proof Proof) Root() (Root, error) {
	if !proof.valid {
		return Root{}, ErrInvalidProof
	}
	value, err := proof.value.Root()
	if err != nil {
		return Root{}, ErrInvalidProof
	}

	return Root{value: value}, nil
}

func toInternalProofGenerationLimits(
	limits ProofGenerationLimits,
) authstate.ProofGenerationLimits {
	return authstate.ProofGenerationLimits{
		Material: authstate.ProofMaterialLimits{
			MaxKeys:            limits.Material.MaxKeys,
			MaxStemPaths:       limits.Material.MaxStemPaths,
			MaxNodeReads:       limits.Material.MaxNodeReads,
			MaxPathCommitments: limits.Material.MaxPathCommitments,
			MaxPathBytes:       limits.Material.MaxPathBytes,
			MaxTemporaryBytes:  limits.Material.MaxTemporaryBytes,
		},
		ProverQueries: committedtree.AggregateProverQueryLimits{
			MaxKeys:           limits.ProverQueries.MaxKeys,
			MaxQueries:        limits.ProverQueries.MaxQueries,
			MaxNodeReads:      limits.ProverQueries.MaxNodeReads,
			MaxTemporaryBytes: limits.ProverQueries.MaxTemporaryBytes,
		},
		VerifierQueries: authstate.AggregateVerifierQueryLimits{
			MaxQueries:        limits.VerifierQueries.MaxQueries,
			MaxTemporaryBytes: limits.VerifierQueries.MaxTemporaryBytes,
		},
		TreeProof: authstate.TreeProofLimits{
			MaxClaims:          limits.Proof.MaxClaims,
			MaxStemPaths:       limits.Proof.MaxStemPaths,
			MaxPathCommitments: limits.Proof.MaxPathCommitments,
			MaxPathDerivations: limits.Proof.MaxPathDerivations,
			MaxPathBytes:       limits.Proof.MaxPathBytes,
			MaxTemporaryBytes:  limits.Proof.MaxTemporaryBytes,
		},
	}
}

func translateProofError(operation string, err error, verification bool) error {
	if resourceErr := translateResourceError(err); resourceErr != nil {
		return resourceErr
	}
	if errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%s: %w: %w", operation, ErrCancelled, err)
	}
	if authstate.IsInvalidProofLimitsError(err) {
		return fmt.Errorf("%s: %w", operation, ErrInvalidLimits)
	}
	if authstate.IsDuplicateKeyError(err) {
		return fmt.Errorf("%s: %w", operation, ErrDuplicateKey)
	}
	if verification || authstate.IsProofVerificationError(err) {
		return fmt.Errorf("%s: %w", operation, ErrVerification)
	}
	if authstate.IsInvalidProofError(err) {
		return fmt.Errorf("%s: %w", operation, ErrInvalidProof)
	}
	if authstate.IsInvalidProofEncodingError(err) {
		return fmt.Errorf("%s: %w", operation, ErrInvalidProof)
	}

	return fmt.Errorf("%s: %w", operation, ErrInvalidProof)
}
