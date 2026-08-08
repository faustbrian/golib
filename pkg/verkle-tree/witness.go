package verkletree

import (
	"context"
	"errors"
	"fmt"

	"github.com/faustbrian/golib/pkg/verkle-tree/internal/authstate"
	"github.com/faustbrian/golib/pkg/verkle-tree/internal/backend"
)

const (
	maxPublicWitnessUpdates         = uint32(65_536)
	publicWitnessUpdateWorkingBytes = uint64(384)
)

// WitnessLimits bounds immutable stateless-witness construction.
type WitnessLimits struct {
	// MaxUpdates bounds operations in the canonical witness batch.
	MaxUpdates uint32
	// MaxTemporaryBytes bounds conservatively accounted construction scratch.
	MaxTemporaryBytes uint64
}

func (limits WitnessLimits) validate() error {
	if limits.MaxUpdates == 0 ||
		limits.MaxUpdates > maxPublicWitnessUpdates ||
		limits.MaxTemporaryBytes == 0 {
		return ErrInvalidLimits
	}

	return nil
}

// WitnessEncodingLimits bounds canonical stateless-witness serialization.
type WitnessEncodingLimits struct {
	// MaxWitnessBytes bounds the complete canonical witness output.
	MaxWitnessBytes uint64
	// MaxProofBytes bounds the embedded canonical proof bytes.
	MaxProofBytes uint64
	// MaxTemporaryBytes bounds conservatively accounted encoding scratch.
	MaxTemporaryBytes uint64
}

func (limits WitnessEncodingLimits) validate() error {
	if limits.MaxWitnessBytes == 0 ||
		limits.MaxProofBytes == 0 ||
		limits.MaxTemporaryBytes == 0 {
		return ErrInvalidLimits
	}

	return nil
}

// WitnessDecodingLimits bounds hostile canonical witness decoding.
// MaxPostRootPointDecodes must equal one. Proof limits apply independently to
// the embedded pre-state proof.
type WitnessDecodingLimits struct {
	// MaxWitnessBytes bounds the complete untrusted witness container.
	MaxWitnessBytes uint64
	// MaxUpdates bounds decoded Set and Delete operations.
	MaxUpdates uint32
	// MaxPostRootPointDecodes must be one for the embedded post-state root.
	MaxPostRootPointDecodes uint32
	// MaxTemporaryBytes bounds conservatively accounted decoding scratch.
	MaxTemporaryBytes uint64
	// Proof independently bounds the embedded pre-state proof decoder.
	Proof ProofDecodingLimits
}

func (limits WitnessDecodingLimits) validate() error {
	if limits.MaxWitnessBytes == 0 ||
		limits.MaxUpdates == 0 ||
		limits.MaxUpdates > maxPublicWitnessUpdates ||
		limits.MaxPostRootPointDecodes != 1 ||
		limits.MaxTemporaryBytes == 0 ||
		limits.Proof.validate() != nil {
		return ErrInvalidLimits
	}

	return nil
}

// StatelessUpdateLimits bounds verified post-state root calculation.
type StatelessUpdateLimits struct {
	// MaxUpdates bounds authenticated Set and Delete operations.
	MaxUpdates uint32
	// MaxCommitmentUpdates bounds bottom-up authenticated vector changes.
	MaxCommitmentUpdates uint32
	// MaxFieldMappings bounds commitment-to-field operations.
	MaxFieldMappings uint32
	// MaxPathLookups bounds reads from authenticated witness paths.
	MaxPathLookups uint32
	// MaxTemporaryBytes bounds conservatively accounted update scratch.
	MaxTemporaryBytes uint64
}

func (limits StatelessUpdateLimits) validate() error {
	if limits.MaxUpdates == 0 ||
		limits.MaxUpdates > maxPublicWitnessUpdates ||
		limits.MaxCommitmentUpdates == 0 ||
		limits.MaxFieldMappings == 0 ||
		limits.MaxPathLookups == 0 ||
		limits.MaxTemporaryBytes == 0 {
		return ErrInvalidLimits
	}

	return nil
}

// Witness is one immutable canonical stateless update witness. Its zero value
// rejects use. Construction and decoding do not verify its proof or claimed
// post-state root; StatelessEngine.Apply performs both checks.
type Witness struct {
	value authstate.StatelessWitness
	valid bool
}

// StatelessEngine owns fixed-profile proof and commitment backends. It is safe
// for concurrent witness verification.
type StatelessEngine struct {
	value *authstate.StatelessUpdater
	valid bool
}

// StatelessResult binds one successfully verified pre-state root to the exact
// independently derived post-state root. Its zero value rejects use.
type StatelessResult struct {
	preRoot  Root
	postRoot Root
	valid    bool
}

// NewWitness validates, canonicalizes, and defensively owns one non-empty
// update batch with its complete pre-state proof and claimed post-state root.
func NewWitness(
	ctx context.Context,
	proof Proof,
	updates []Update,
	postRoot Root,
	limits WitnessLimits,
) (Witness, error) {
	if err := checkPublicContext(ctx); err != nil {
		return Witness{}, err
	}
	if !proof.valid {
		return Witness{}, ErrInvalidWitness
	}
	if _, err := postRoot.Profile(); err != nil {
		return Witness{}, ErrInvalidWitness
	}
	if err := limits.validate(); err != nil {
		return Witness{}, err
	}
	if len(updates) == 0 {
		return Witness{}, ErrInvalidWitness
	}
	if uint64(len(updates)) > uint64(limits.MaxUpdates) {
		return Witness{}, newPublicResourceError(
			ResourceBatchUpdates,
			uint64(limits.MaxUpdates),
			uint64(len(updates)),
		)
	}
	temporaryBytes := uint64(len(updates)) * publicWitnessUpdateWorkingBytes
	if temporaryBytes > limits.MaxTemporaryBytes {
		return Witness{}, newPublicResourceError(
			ResourceTemporaryBytes,
			limits.MaxTemporaryBytes,
			temporaryBytes,
		)
	}
	owned, err := toInternalWitnessUpdates(ctx, updates)
	if err != nil {
		return Witness{}, err
	}
	value, err := authstate.NewStatelessWitness(
		ctx,
		proof.value,
		owned,
		postRoot.value,
		authstate.StatelessWitnessLimits{
			MaxUpdates: limits.MaxUpdates, MaxTemporaryBytes: limits.MaxTemporaryBytes,
		},
	)
	if err != nil {
		return Witness{}, translateWitnessError("construct witness", err)
	}

	return Witness{value: value, valid: true}, nil
}

// DecodeWitness strictly decodes and defensively owns one canonical witness.
// Cryptographic verification remains a separate explicit operation.
func DecodeWitness(
	ctx context.Context,
	encoded []byte,
	limits WitnessDecodingLimits,
) (Witness, error) {
	if err := checkPublicContext(ctx); err != nil {
		return Witness{}, err
	}
	if err := limits.validate(); err != nil {
		return Witness{}, err
	}
	value, err := authstate.DecodeStatelessWitness(
		ctx,
		encoded,
		authstate.StatelessWitnessDecodingLimits{
			MaxWitnessBytes:         limits.MaxWitnessBytes,
			MaxUpdates:              limits.MaxUpdates,
			MaxPostRootPointDecodes: limits.MaxPostRootPointDecodes,
			MaxTemporaryBytes:       limits.MaxTemporaryBytes,
			Proof:                   toInternalProofDecodingLimits(limits.Proof),
		},
	)
	if err != nil {
		return Witness{}, translateWitnessError("decode witness", err)
	}

	return Witness{value: value, valid: true}, nil
}

// Bytes returns caller-owned canonical witness bytes.
func (witness Witness) Bytes(
	ctx context.Context,
	limits WitnessEncodingLimits,
) ([]byte, error) {
	if !witness.valid {
		return nil, ErrInvalidWitness
	}
	if err := checkPublicContext(ctx); err != nil {
		return nil, err
	}
	if err := limits.validate(); err != nil {
		return nil, err
	}
	encoded, err := witness.value.Bytes(
		ctx,
		authstate.StatelessWitnessEncodingLimits{
			MaxWitnessBytes:   limits.MaxWitnessBytes,
			MaxProofBytes:     limits.MaxProofBytes,
			MaxTemporaryBytes: limits.MaxTemporaryBytes,
		},
	)
	if err != nil {
		return nil, translateWitnessError("encode witness", err)
	}

	return encoded, nil
}

// Proof returns the complete unverified pre-state proof.
func (witness Witness) Proof() (Proof, error) {
	if !witness.valid {
		return Proof{}, ErrInvalidWitness
	}
	value, err := witness.value.Proof()
	if err != nil {
		return Proof{}, ErrInvalidWitness
	}

	return Proof{value: value, valid: true}, nil
}

// Updates returns a caller-owned copy in canonical key order.
func (witness Witness) Updates(ctx context.Context) ([]Update, error) {
	if !witness.valid {
		return nil, ErrInvalidWitness
	}
	if err := checkPublicContext(ctx); err != nil {
		return nil, err
	}
	values, err := witness.value.Updates(ctx)
	if err != nil {
		return nil, translateWitnessError("copy witness updates", err)
	}
	return toPublicWitnessUpdates(ctx, values)
}

func toPublicWitnessUpdates(
	ctx context.Context,
	values []authstate.Update,
) ([]Update, error) {
	updates := make([]Update, len(values))
	for index := range values {
		if err := checkPublicContext(ctx); err != nil {
			return nil, err
		}
		value, present, err := values[index].Value()
		if err != nil {
			return nil, ErrInvalidWitness
		}
		key, _ := values[index].Key()
		if present {
			updates[index] = Set(Key(key), Value(value))
		} else {
			updates[index] = Delete(Key(key))
		}
	}

	return updates, nil
}

// PostRoot returns the exact post-state root claimed by the witness.
func (witness Witness) PostRoot() (Root, error) {
	if !witness.valid {
		return Root{}, ErrInvalidWitness
	}
	value, err := witness.value.PostRoot()
	if err != nil {
		return Root{}, ErrInvalidWitness
	}

	return Root{value: value}, nil
}

// NewStatelessEngine explicitly initializes the selected profile's proof and
// commitment backends.
func NewStatelessEngine(
	ctx context.Context,
	profile Profile,
	openingLimits OpeningLimits,
	commitmentLimits CommitmentLimits,
) (StatelessEngine, error) {
	if err := checkPublicContext(ctx); err != nil {
		return StatelessEngine{}, err
	}
	if err := profile.Validate(); err != nil {
		return StatelessEngine{}, ErrUnsupportedProfile
	}
	if err := openingLimits.validate(); err != nil {
		return StatelessEngine{}, err
	}
	if err := validateCommitmentLimits(commitmentLimits); err != nil {
		return StatelessEngine{}, err
	}
	value, err := authstate.NewStatelessUpdater(
		ctx,
		backend.AggregateOpeningLimits{
			MaxGeneratorDerivations: openingLimits.MaxGeneratorDerivations,
			MaxPrecomputedPoints:    openingLimits.MaxPrecomputedPoints,
			MaxQueries:              openingLimits.MaxQueries,
			MaxScalarDecodes:        openingLimits.MaxScalarDecodes,
			MaxMSMTerms:             openingLimits.MaxMSMTerms,
			MaxTemporaryBytes:       openingLimits.MaxTemporaryBytes,
			MaxWorkers:              openingLimits.MaxWorkers,
			MaxQueuedOperations:     openingLimits.MaxQueuedOperations,
		},
		toInternalCommitmentLimits(commitmentLimits),
	)
	if err != nil {
		return StatelessEngine{}, translateWitnessError("initialize stateless engine", err)
	}

	return StatelessEngine{value: value, valid: true}, nil
}

// NewStatelessEngineFromProofEngine initializes a stateless verifier while
// reusing one already initialized immutable proof backend. Proof generation
// and stateless verification share that engine's bounded dependency-operation
// gate when used concurrently.
func NewStatelessEngineFromProofEngine(
	ctx context.Context,
	proofEngine ProofEngine,
	commitmentLimits CommitmentLimits,
) (StatelessEngine, error) {
	if err := checkPublicContext(ctx); err != nil {
		return StatelessEngine{}, err
	}
	if !proofEngine.valid || proofEngine.value == nil {
		return StatelessEngine{}, ErrInvalidProofEngine
	}
	if err := proofEngine.profile.Validate(); err != nil {
		return StatelessEngine{}, ErrUnsupportedProfile
	}
	if err := validateCommitmentLimits(commitmentLimits); err != nil {
		return StatelessEngine{}, err
	}
	value, err := authstate.NewStatelessUpdaterFromProofEngine(
		ctx,
		proofEngine.value,
		toInternalCommitmentLimits(commitmentLimits),
	)
	if err != nil {
		return StatelessEngine{}, translateWitnessError(
			"initialize stateless engine from proof engine",
			err,
		)
	}

	return StatelessEngine{value: value, valid: true}, nil
}

// Apply cryptographically verifies the complete witness, applies its bounded
// canonical update batch, and requires the independently derived post-state
// root to equal the root bound by the witness.
func (engine StatelessEngine) Apply(
	ctx context.Context,
	witness Witness,
	verificationLimits ProofVerificationLimits,
	limits StatelessUpdateLimits,
) (StatelessResult, error) {
	if !engine.valid || engine.value == nil {
		return StatelessResult{}, ErrInvalidStatelessEngine
	}
	if !witness.valid {
		return StatelessResult{}, ErrInvalidWitness
	}
	if err := checkPublicContext(ctx); err != nil {
		return StatelessResult{}, err
	}
	if err := verificationLimits.validate(); err != nil {
		return StatelessResult{}, err
	}
	if err := limits.validate(); err != nil {
		return StatelessResult{}, err
	}
	postRoot, err := engine.value.ApplyWitness(
		ctx,
		witness.value,
		authstate.ProofVerificationLimits{
			VerifierQueries: authstate.AggregateVerifierQueryLimits{
				MaxQueries:        verificationLimits.VerifierQueries.MaxQueries,
				MaxTemporaryBytes: verificationLimits.VerifierQueries.MaxTemporaryBytes,
			},
		},
		authstate.StatelessUpdateLimits{
			MaxUpdates:           limits.MaxUpdates,
			MaxCommitmentUpdates: limits.MaxCommitmentUpdates,
			MaxFieldMappings:     limits.MaxFieldMappings,
			MaxPathLookups:       limits.MaxPathLookups,
			MaxTemporaryBytes:    limits.MaxTemporaryBytes,
		},
	)
	if err != nil {
		return StatelessResult{}, translateWitnessError("apply witness", err)
	}
	proof, _ := witness.value.Proof()
	preRoot, _ := proof.Root()

	return StatelessResult{
		preRoot:  Root{value: preRoot},
		postRoot: Root{value: postRoot},
		valid:    true,
	}, nil
}

// PreRoot returns the exact cryptographically verified pre-state root.
func (result StatelessResult) PreRoot() (Root, error) {
	if !result.valid {
		return Root{}, ErrInvalidStatelessResult
	}

	return result.preRoot, nil
}

// PostRoot returns the independently derived and witness-matched post-state
// root.
func (result StatelessResult) PostRoot() (Root, error) {
	if !result.valid {
		return Root{}, ErrInvalidStatelessResult
	}

	return result.postRoot, nil
}

func toInternalWitnessUpdates(ctx context.Context, updates []Update) ([]authstate.Update, error) {
	owned := make([]authstate.Update, len(updates))
	for index := range updates {
		if err := checkPublicContext(ctx); err != nil {
			return nil, err
		}
		kind, err := updates[index].Kind()
		if err != nil {
			return nil, err
		}
		if kind == UpdateDelete {
			owned[index] = authstate.Delete(authstate.Key(updates[index].key))

			continue
		}
		owned[index] = authstate.Set(
			authstate.Key(updates[index].key), authstate.Value(updates[index].value),
		)
	}

	return owned, nil
}

func toInternalProofDecodingLimits(limits ProofDecodingLimits) authstate.TreeProofDecodingLimits {
	return authstate.TreeProofDecodingLimits{
		MaxProofBytes:      limits.MaxProofBytes,
		MaxClaims:          limits.MaxClaims,
		MaxStemPaths:       limits.MaxStemPaths,
		MaxPathCommitments: limits.MaxPathCommitments,
		MaxPathDerivations: limits.MaxPathDerivations,
		MaxPathBytes:       limits.MaxPathBytes,
		MaxPointDecodes:    limits.MaxPointDecodes,
		MaxScalarDecodes:   limits.MaxScalarDecodes,
		MaxTemporaryBytes:  limits.MaxTemporaryBytes,
	}
}

func validateCommitmentLimits(limits CommitmentLimits) error {
	if limits.MaxGeneratorDerivations == 0 ||
		limits.MaxScalarDecodes == 0 ||
		limits.MaxMSMTerms == 0 ||
		limits.MaxTemporaryBytes == 0 {
		return ErrInvalidLimits
	}

	return nil
}

func toInternalCommitmentLimits(limits CommitmentLimits) backend.CommitmentLimits {
	return backend.CommitmentLimits{
		MaxGeneratorDerivations: limits.MaxGeneratorDerivations,
		MaxScalarDecodes:        limits.MaxScalarDecodes,
		MaxMSMTerms:             limits.MaxMSMTerms,
		MaxTemporaryBytes:       limits.MaxTemporaryBytes,
	}
}

func translateWitnessError(operation string, err error) error {
	if resourceErr := translateResourceError(err); resourceErr != nil {
		return resourceErr
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%s: %w: %w", operation, ErrCancelled, err)
	}
	if authstate.IsInvalidStatelessWitnessLimitsError(err) ||
		authstate.IsInvalidProofLimitsError(err) {
		return fmt.Errorf("%s: %w", operation, ErrInvalidLimits)
	}
	if errors.Is(err, ErrUnsupportedProfile) {
		return fmt.Errorf("%s: %w", operation, ErrUnsupportedProfile)
	}
	if authstate.IsDuplicateKeyError(err) {
		return fmt.Errorf("%s: %w", operation, ErrDuplicateKey)
	}
	if authstate.IsInvalidStatelessUpdaterError(err) {
		return fmt.Errorf("%s: %w", operation, ErrInvalidStatelessEngine)
	}
	if authstate.IsIncompleteStatelessWitnessError(err) {
		return fmt.Errorf("%s: %w", operation, ErrIncompleteWitness)
	}
	if authstate.IsUnsupportedStatelessUpdateError(err) {
		return fmt.Errorf("%s: %w", operation, ErrUnsupportedUpdate)
	}
	if authstate.IsStatelessPostRootMismatchError(err) {
		return fmt.Errorf("%s: %w", operation, ErrPostStateMismatch)
	}
	if authstate.IsProofVerificationError(err) {
		return fmt.Errorf("%s: %w", operation, ErrVerification)
	}
	if authstate.IsInvalidStatelessWitnessError(err) ||
		authstate.IsInvalidStatelessWitnessEncodingError(err) ||
		authstate.IsInvalidProofError(err) ||
		authstate.IsInvalidProofEncodingError(err) {
		return fmt.Errorf("%s: %w", operation, ErrInvalidWitness)
	}
	if authstate.IsInvalidStatelessUpdateError(err) {
		return fmt.Errorf("%s: %w", operation, ErrInvalidUpdate)
	}

	return fmt.Errorf("%s: %w", operation, ErrCryptographic)
}
