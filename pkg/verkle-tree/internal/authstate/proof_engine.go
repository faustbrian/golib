package authstate

import (
	"context"
	"errors"
	"fmt"

	"github.com/faustbrian/golib/pkg/verkle-tree/internal/backend"
	"github.com/faustbrian/golib/pkg/verkle-tree/internal/committedtree"
)

var (
	errInvalidProofEngine             = errors.New("invalid tree-proof engine")
	errInvalidProofGenerationLimits   = errors.New("invalid tree-proof generation limits")
	errInvalidProofVerificationLimits = errors.New("invalid tree-proof verification limits")
	errProofGeneration                = errors.New("tree-proof generation failed")
	errProofVerification              = errors.New("tree-proof verification failed")
)

// ProofGenerationLimits binds every bounded stage of deterministic proof
// generation. No stage inherits an implicit or unbounded limit.
type ProofGenerationLimits struct {
	Material        ProofMaterialLimits
	ProverQueries   committedtree.AggregateProverQueryLimits
	VerifierQueries AggregateVerifierQueryLimits
	TreeProof       TreeProofLimits
}

func (limits ProofGenerationLimits) validate() error {
	if limits.Material.validate() != nil ||
		limits.ProverQueries.Validate() != nil ||
		limits.VerifierQueries.validate() != nil ||
		limits.TreeProof.validate() != nil {
		return errInvalidProofGenerationLimits
	}

	return nil
}

// ProofVerificationLimits bounds verifier-side transcript reconstruction.
type ProofVerificationLimits struct {
	VerifierQueries AggregateVerifierQueryLimits
}

func (limits ProofVerificationLimits) validate() error {
	if limits.VerifierQueries.validate() != nil {
		return errInvalidProofVerificationLimits
	}

	return nil
}

// ProofEngine owns the immutable fixed-profile aggregate-opening settings. It
// is safe for concurrent proof generation and verification.
type ProofEngine struct {
	opening *backend.AggregateOpeningEngine
	valid   bool
}

// NewProofEngine explicitly initializes the fixed-profile opening backend.
func NewProofEngine(
	ctx context.Context,
	limits backend.AggregateOpeningLimits,
) (*ProofEngine, error) {
	if err := checkTreeProofContext(ctx); err != nil {
		return nil, err
	}
	opening, err := backend.NewAggregateOpeningEngine(ctx, limits)
	if err != nil {
		return nil, err
	}

	return &ProofEngine{opening: opening, valid: true}, nil
}

// Prove creates a canonical aggregate tree proof bound to one immutable
// snapshot and one unordered set of distinct keys.
func (engine *ProofEngine) Prove(
	ctx context.Context,
	snapshot Snapshot,
	keys []Key,
	limits ProofGenerationLimits,
) (TreeProof, error) {
	if err := engine.validate(); err != nil {
		return TreeProof{}, err
	}
	if err := checkTreeProofContext(ctx); err != nil {
		return TreeProof{}, err
	}
	if err := limits.validate(); err != nil {
		return TreeProof{}, err
	}
	if err := snapshot.validate(); err != nil {
		return TreeProof{}, err
	}

	material, err := snapshot.ProofMaterial(ctx, keys, limits.Material)
	if err != nil {
		return TreeProof{}, err
	}
	proverRecords, err := snapshot.tree.AggregateProverQueries(
		ctx,
		keys,
		limits.ProverQueries,
	)
	if err != nil {
		return TreeProof{}, err
	}
	verifierRecords, err := material.AggregateVerifierQueries(
		ctx,
		limits.VerifierQueries,
	)
	if err != nil {
		return TreeProof{}, err
	}
	proverQueries, err := matchAggregateQueries(
		ctx,
		proverRecords,
		verifierRecords,
	)
	if err != nil {
		return TreeProof{}, err
	}
	opening, err := engine.opening.Open(ctx, proverQueries)
	if err != nil {
		return TreeProof{}, fmt.Errorf("%w: %w", errProofGeneration, err)
	}

	return NewTreeProof(
		ctx,
		material.root,
		material.claims,
		material.stemPaths,
		material.commitments,
		opening,
		limits.TreeProof,
	)
}

// Verify reconstructs the complete expected opening set from an immutable
// proof and verifies its aggregate opening independently from mutable state.
func (engine *ProofEngine) Verify(
	ctx context.Context,
	proof TreeProof,
	limits ProofVerificationLimits,
) error {
	if err := engine.validate(); err != nil {
		return err
	}
	if err := checkTreeProofContext(ctx); err != nil {
		return err
	}
	if err := limits.validate(); err != nil {
		return err
	}
	if err := proof.validate(); err != nil {
		return err
	}
	material := ProofMaterial{
		root:        proof.root,
		claims:      proof.claims,
		stemPaths:   proof.stemPaths,
		commitments: proof.commitments,
		valid:       true,
	}
	queries, err := material.AggregateVerifierQueries(
		ctx,
		limits.VerifierQueries,
	)
	if err != nil {
		return err
	}
	openings := make([]backend.AggregateVerifierQuery, len(queries))
	for index := range queries {
		if err := checkTreeProofContext(ctx); err != nil {
			return err
		}
		openings[index] = queries[index].Opening
	}
	if err := engine.opening.Verify(ctx, proof.opening, openings); err != nil {
		return fmt.Errorf("%w: %w", errProofVerification, err)
	}

	return nil
}

func (engine *ProofEngine) validate() error {
	if engine == nil || !engine.valid || engine.opening == nil {
		return errInvalidProofEngine
	}

	return nil
}

func matchAggregateQueries(
	ctx context.Context,
	prover []committedtree.AggregateProverQuery,
	verifier []AggregateVerifierQuery,
) ([]backend.AggregateProverQuery, error) {
	if len(prover) != len(verifier) {
		return nil, errProofGeneration
	}
	openings := make([]backend.AggregateProverQuery, len(prover))
	for index := range prover {
		if err := checkTreeProofContext(ctx); err != nil {
			return nil, err
		}
		proverKey, err := prover[index].Opening.Commitment.DeduplicationKey()
		if err != nil {
			return nil, errProofGeneration
		}
		verifierKey, err := verifier[index].Opening.Commitment.DeduplicationKey()
		if err != nil ||
			prover[index].Path != verifier[index].Path ||
			prover[index].Length != verifier[index].Length ||
			prover[index].Opening.Index != verifier[index].Opening.Index ||
			proverKey != verifierKey ||
			prover[index].Opening.Vector[prover[index].Opening.Index] !=
				verifier[index].Opening.Value {
			return nil, errProofGeneration
		}
		openings[index] = prover[index].Opening
	}

	return openings, nil
}

// IsProofVerificationError reports a failed cryptographic verification.
func IsProofVerificationError(err error) bool {
	return errors.Is(err, errProofVerification)
}
