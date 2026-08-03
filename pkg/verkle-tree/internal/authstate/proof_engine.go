package authstate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"

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

// ProveUpdates creates the canonical proof key set required to authenticate a
// complete update transition. Deletions add one retained member when topology
// stays stable or complete suffix and internal-node probes when a stem empties.
func (engine *ProofEngine) ProveUpdates(
	ctx context.Context,
	snapshot Snapshot,
	updates []Update,
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
	if len(updates) == 0 {
		return TreeProof{}, errInvalidStatelessUpdate
	}
	maxKeys := min(limits.Material.MaxKeys, limits.ProverQueries.MaxKeys)
	if err := checkProofMaterialResource(
		ProofMaterialResourceKeys,
		uint64(maxKeys),
		uint64(len(updates)),
	); err != nil {
		return TreeProof{}, err
	}
	if err := checkProofMaterialResource(
		ProofMaterialResourceTemporaryBytes,
		limits.Material.MaxTemporaryBytes,
		uint64(len(updates))*2*proofMaterialKeyWorkingBytes,
	); err != nil {
		return TreeProof{}, err
	}
	unique := make(map[Key]struct{}, min(len(updates), int(maxKeys)))
	ordered := append([]Update(nil), updates...)
	if err := sortUpdates(ctx, ordered); err != nil {
		return TreeProof{}, err
	}
	for index := range ordered {
		if err := checkTreeProofContext(ctx); err != nil {
			return TreeProof{}, err
		}
		if err := ordered[index].validate(); err != nil {
			return TreeProof{}, errInvalidStatelessUpdate
		}
		if index > 0 && ordered[index-1].key == ordered[index].key {
			return TreeProof{}, errDuplicateKey
		}
		unique[ordered[index].key] = struct{}{}
	}
	for start := 0; start < len(ordered); {
		if err := checkTreeProofContext(ctx); err != nil {
			return TreeProof{}, err
		}
		stem := Stem(ordered[start].key[:31])
		end := start + 1
		setStem := ordered[start].kind == UpdateSet
		deletedMembership := ordered[start].kind == UpdateDelete &&
			statelessSnapshotContains(snapshot.entries, ordered[start].key)
		for end < len(ordered) && Stem(ordered[end].key[:31]) == stem {
			setStem = setStem || ordered[end].kind == UpdateSet
			deletedMembership = deletedMembership ||
				(ordered[end].kind == UpdateDelete &&
					statelessSnapshotContains(snapshot.entries, ordered[end].key))
			end++
		}
		if !deletedMembership || setStem {
			start = end

			continue
		}
		retained, found, err := statelessRetainedSnapshotKey(
			ctx, snapshot.entries, ordered[start:end], stem,
		)
		if err != nil {
			return TreeProof{}, err
		}
		if found {
			if err := addStatelessProofKey(
				unique, retained, maxKeys,
				limits.Material.MaxTemporaryBytes,
			); err != nil {
				return TreeProof{}, err
			}
			start = end

			continue
		}
		for suffix := range backend.VectorWidth {
			if err := checkTreeProofContext(ctx); err != nil {
				return TreeProof{}, err
			}
			var key Key
			copy(key[:31], stem[:])
			key[31] = byte(suffix)
			if err := addStatelessProofKey(
				unique, key, maxKeys,
				limits.Material.MaxTemporaryBytes,
			); err != nil {
				return TreeProof{}, err
			}
		}
		depth, _, err := statelessSnapshotStemDepth(
			ctx, snapshot.entries, stem,
		)
		if err != nil {
			return TreeProof{}, err
		}
		for parentDepth := uint8(1); parentDepth < depth; parentDepth++ {
			parent := makeStatelessPath(stem[:parentDepth])
			for child := range backend.VectorWidth {
				if err := checkTreeProofContext(ctx); err != nil {
					return TreeProof{}, err
				}
				if err := addStatelessProofKey(
					unique,
					statelessTopologyProbe(parent, byte(child)),
					maxKeys,
					limits.Material.MaxTemporaryBytes,
				); err != nil {
					return TreeProof{}, err
				}
			}
		}
		start = end
	}
	keys := make([]Key, 0, len(unique))
	for key := range unique {
		if err := checkTreeProofContext(ctx); err != nil {
			return TreeProof{}, err
		}
		keys = append(keys, key)
	}

	return engine.Prove(ctx, snapshot, keys, limits)
}

func addStatelessProofKey(
	keys map[Key]struct{},
	key Key,
	limit uint32,
	temporaryLimit uint64,
) error {
	if _, exists := keys[key]; exists {
		return nil
	}
	if err := checkProofMaterialResource(
		ProofMaterialResourceKeys,
		uint64(limit),
		uint64(len(keys)+1),
	); err != nil {
		return err
	}
	if err := checkProofMaterialResource(
		ProofMaterialResourceTemporaryBytes,
		temporaryLimit,
		uint64(len(keys)+1)*2*proofMaterialKeyWorkingBytes,
	); err != nil {
		return err
	}
	keys[key] = struct{}{}

	return nil
}

func statelessSnapshotContains(entries []Entry, key Key) bool {
	_, found := findEntry(entries, key)

	return found
}

func statelessRetainedSnapshotKey(
	ctx context.Context,
	entries []Entry,
	updates []Update,
	stem Stem,
) (Key, bool, error) {
	entryIndex := sort.Search(len(entries), func(index int) bool {
		return bytes.Compare(entries[index].Key[:31], stem[:]) >= 0
	})
	updateIndex := 0
	for entryIndex < len(entries) && Stem(entries[entryIndex].Key[:31]) == stem {
		if err := checkTreeProofContext(ctx); err != nil {
			return Key{}, false, err
		}
		for updateIndex < len(updates) &&
			bytes.Compare(updates[updateIndex].key[:], entries[entryIndex].Key[:]) < 0 {
			if err := checkTreeProofContext(ctx); err != nil {
				return Key{}, false, err
			}
			updateIndex++
		}
		if updateIndex == len(updates) ||
			updates[updateIndex].key != entries[entryIndex].Key ||
			updates[updateIndex].kind != UpdateDelete {
			return entries[entryIndex].Key, true, nil
		}
		entryIndex++
	}

	return Key{}, false, nil
}

func statelessSnapshotStemDepth(
	ctx context.Context,
	entries []Entry,
	stem Stem,
) (uint8, bool, error) {
	if err := checkTreeProofContext(ctx); err != nil {
		return 0, false, err
	}
	start := sort.Search(len(entries), func(index int) bool {
		return bytes.Compare(entries[index].Key[:31], stem[:]) >= 0
	})
	if err := checkTreeProofContext(ctx); err != nil {
		return 0, false, err
	}
	if start == len(entries) || Stem(entries[start].Key[:31]) != stem {
		return 0, false, nil
	}
	end := start + 1
	for end < len(entries) && Stem(entries[end].Key[:31]) == stem {
		if err := checkTreeProofContext(ctx); err != nil {
			return 0, false, err
		}
		end++
	}
	depth := uint8(1)
	for _, index := range []int{start - 1, end} {
		if index < 0 || index >= len(entries) {
			continue
		}
		other := Stem(entries[index].Key[:31])
		shared := uint8(0)
		for shared < uint8(len(stem)) && stem[shared] == other[shared] {
			if err := checkTreeProofContext(ctx); err != nil {
				return 0, false, err
			}
			shared++
		}
		candidate := shared + 1
		depth = max(depth, candidate)
	}

	return depth, true, nil
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
