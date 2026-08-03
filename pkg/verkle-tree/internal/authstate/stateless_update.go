package authstate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/faustbrian/golib/pkg/verkle-tree/internal/backend"
	"github.com/faustbrian/golib/pkg/verkle-tree/internal/leafvector"
)

const (
	maxStatelessUpdates                   = uint32(65_536)
	statelessUpdateWorkingBytes           = uint64(256)
	statelessCommitmentPathWorkingBytes   = uint64(192)
	statelessStemPathWorkingBytes         = uint64(128)
	statelessPropagationLevelWorkingBytes = uint64(256)
	statelessInsertionVectorWorkingBytes  = uint64(maxProofPathLength * len(backend.Vector{}) * len(backend.Vector{}[0]))
)

var (
	errInvalidStatelessUpdater      = errors.New("invalid stateless updater")
	errInvalidStatelessUpdateLimits = errors.New("invalid stateless update limits")
	errInvalidStatelessUpdate       = errors.New("invalid stateless update")
	errIncompleteStatelessWitness   = errors.New("incomplete stateless update witness")
	errUnsupportedStatelessUpdate   = errors.New("unsupported stateless update")
	errStatelessUpdateResource      = errors.New("stateless update resource limit exceeded")
)

// StatelessUpdateLimits bounds authenticated post-state calculation. Every
// field must be positive and no field denotes an unbounded resource.
type StatelessUpdateLimits struct {
	MaxUpdates           uint32
	MaxCommitmentUpdates uint32
	MaxFieldMappings     uint32
	MaxPathLookups       uint32
	MaxTemporaryBytes    uint64
}

func (limits StatelessUpdateLimits) validate() error {
	if limits.MaxUpdates == 0 ||
		limits.MaxUpdates > maxStatelessUpdates ||
		limits.MaxCommitmentUpdates == 0 ||
		limits.MaxFieldMappings == 0 ||
		limits.MaxPathLookups == 0 ||
		limits.MaxTemporaryBytes == 0 {
		return errInvalidStatelessUpdateLimits
	}

	return nil
}

// StatelessUpdateResource identifies one bounded post-state calculation
// resource.
type StatelessUpdateResource uint8

const (
	// StatelessUpdateResourceUpdates counts distinct requested keys.
	StatelessUpdateResourceUpdates StatelessUpdateResource = iota + 1
	// StatelessUpdateResourceCommitmentUpdates counts vector commitments changed.
	StatelessUpdateResourceCommitmentUpdates
	// StatelessUpdateResourceFieldMappings counts commitment-to-field mappings.
	StatelessUpdateResourceFieldMappings
	// StatelessUpdateResourcePathLookups counts authenticated path reads.
	StatelessUpdateResourcePathLookups
	// StatelessUpdateResourceTemporaryBytes counts deterministic owned scratch.
	StatelessUpdateResourceTemporaryBytes
)

// StatelessUpdateResourceError reports one rejected resource without
// disclosing keys, values, commitments, or paths.
type StatelessUpdateResourceError struct {
	Resource StatelessUpdateResource
	Limit    uint64
	Actual   uint64
}

// Error implements error.
func (err *StatelessUpdateResourceError) Error() string {
	return fmt.Sprintf(
		"%v: resource %d has value %d, limit %d",
		errStatelessUpdateResource,
		err.Resource,
		err.Actual,
		err.Limit,
	)
}

// Unwrap makes StatelessUpdateResourceError match the resource sentinel.
func (err *StatelessUpdateResourceError) Unwrap() error {
	return errStatelessUpdateResource
}

// StatelessUpdater verifies an immutable proof and derives the resulting root
// for authenticated Set operations and for Delete operations that are absent
// or provably leave their stem non-empty. It is safe for concurrent use.
type StatelessUpdater struct {
	proof      *ProofEngine
	commitment *backend.CommitmentEngine
	valid      bool
}

// NewStatelessUpdater explicitly initializes the fixed-profile proof and
// commitment backends.
func NewStatelessUpdater(
	ctx context.Context,
	openingLimits backend.AggregateOpeningLimits,
	commitmentLimits backend.CommitmentLimits,
) (*StatelessUpdater, error) {
	if err := checkTreeProofContext(ctx); err != nil {
		return nil, err
	}
	proof, err := NewProofEngine(ctx, openingLimits)
	if err != nil {
		return nil, err
	}
	commitment, err := backend.NewCommitmentEngine(ctx, commitmentLimits)
	if err != nil {
		return nil, err
	}

	return &StatelessUpdater{proof: proof, commitment: commitment, valid: true}, nil
}

type statelessPath struct {
	path   [maxProofPathLength]byte
	length uint8
}

type statelessChangedCommitment struct {
	old backend.VectorCommitment
	new backend.VectorCommitment
}

type statelessInsertedStem struct {
	stem       Stem
	commitment backend.VectorCommitment
}

type statelessMissingInsertion struct {
	path  statelessPath
	stems []statelessInsertedStem
}

type statelessDifferentInsertion struct {
	path     statelessPath
	existing statelessInsertedStem
	stems    []statelessInsertedStem
}

type statelessUpdateBudget struct {
	limits            StatelessUpdateLimits
	commitmentUpdates uint64
	fieldMappings     uint64
	pathLookups       uint64
}

// Apply verifies proof before deriving a canonical post-state root. Update
// order does not affect the result. The proof must contain the exact old claim
// and terminal stem path for every update.
func (updater *StatelessUpdater) Apply(
	ctx context.Context,
	proof TreeProof,
	updates []Update,
	verificationLimits ProofVerificationLimits,
	limits StatelessUpdateLimits,
) (backend.Root, error) {
	if updater == nil || !updater.valid || updater.proof == nil || updater.commitment == nil {
		return backend.Root{}, errInvalidStatelessUpdater
	}
	if err := checkTreeProofContext(ctx); err != nil {
		return backend.Root{}, err
	}
	if err := limits.validate(); err != nil {
		return backend.Root{}, err
	}
	if err := proof.validate(); err != nil {
		return backend.Root{}, err
	}
	if !statelessProofCountsWithinLimits(proof) {
		return backend.Root{}, errInvalidTreeProof
	}
	if len(updates) == 0 {
		return backend.Root{}, errInvalidStatelessUpdate
	}
	if err := checkStatelessUpdateResource(
		StatelessUpdateResourceUpdates,
		uint64(limits.MaxUpdates),
		uint64(len(updates)),
	); err != nil {
		return backend.Root{}, err
	}
	temporaryBytes := statelessTemporaryBytes(proof, uint64(len(updates)))
	if err := checkStatelessUpdateResource(
		StatelessUpdateResourceTemporaryBytes,
		limits.MaxTemporaryBytes,
		temporaryBytes,
	); err != nil {
		return backend.Root{}, err
	}
	ordered := append([]Update(nil), updates...)
	if err := sortUpdates(ctx, ordered); err != nil {
		return backend.Root{}, err
	}
	for index := range ordered {
		if err := checkTreeProofContext(ctx); err != nil {
			return backend.Root{}, err
		}
		if err := ordered[index].validate(); err != nil {
			return backend.Root{}, errInvalidStatelessUpdate
		}
		if index > 0 && ordered[index-1].key == ordered[index].key {
			return backend.Root{}, errDuplicateKey
		}
	}
	if err := updater.proof.Verify(ctx, proof, verificationLimits); err != nil {
		return backend.Root{}, err
	}

	root, _ := proof.root.Commitment()
	commitments := make(map[statelessPath]backend.VectorCommitment)
	commitments[statelessPath{}] = root
	for index := range proof.commitments {
		path := statelessPath{
			path:   proof.commitments[index].path,
			length: proof.commitments[index].length,
		}
		commitments[path] = proof.commitments[index].commitment
	}
	paths := make(map[Stem]StemPath, len(proof.stemPaths))
	for index := range proof.stemPaths {
		paths[proof.stemPaths[index].stem] = proof.stemPaths[index]
	}
	budget := statelessUpdateBudget{limits: limits}
	changed, err := updater.updateStems(ctx, proof.claims, paths, commitments, ordered, &budget)
	if err != nil {
		return backend.Root{}, err
	}
	if len(changed) == 0 {
		return backend.NewRoot(ctx, proof.profile, root)
	}
	postRoot, err := updater.updateAncestors(ctx, commitments, changed, &budget)
	if err != nil {
		return backend.Root{}, err
	}

	return backend.NewRoot(ctx, proof.profile, postRoot)
}

func statelessTemporaryBytes(proof TreeProof, updateCount uint64) uint64 {
	insertionBytes := uint64(0)
	for index := range proof.stemPaths {
		if proof.stemPaths[index].kind == StemPathMissing ||
			proof.stemPaths[index].kind == StemPathDifferent {
			insertionBytes = statelessInsertionVectorWorkingBytes

			break
		}
	}

	return updateCount*statelessUpdateWorkingBytes +
		uint64(len(proof.commitments))*statelessCommitmentPathWorkingBytes +
		uint64(len(proof.stemPaths))*statelessStemPathWorkingBytes +
		updateCount*uint64(maxProofPathLength)*statelessPropagationLevelWorkingBytes +
		insertionBytes
}

func statelessProofCountsWithinLimits(proof TreeProof) bool {
	return uint64(len(proof.commitments)) <= uint64(maxTreeProofPathCommitments) &&
		uint64(len(proof.stemPaths)) <= uint64(maxTreeProofStemPaths)
}

func (updater *StatelessUpdater) updateStems(
	ctx context.Context,
	claims ClaimSet,
	paths map[Stem]StemPath,
	commitments map[statelessPath]backend.VectorCommitment,
	updates []Update,
	budget *statelessUpdateBudget,
) (map[statelessPath]statelessChangedCommitment, error) {
	changed := make(map[statelessPath]statelessChangedCommitment)
	missing := make([]statelessMissingInsertion, 0)
	missingByPath := make(map[statelessPath]int)
	different := make([]statelessDifferentInsertion, 0)
	differentByPath := make(map[statelessPath]int)
	for start := 0; start < len(updates); {
		if err := checkTreeProofContext(ctx); err != nil {
			return nil, err
		}
		stem := Stem(updates[start].key[:31])
		end := start + 1
		for end < len(updates) && Stem(updates[end].key[:31]) == stem {
			end++
		}
		path, exists := paths[stem]
		if err := budget.lookup(); err != nil {
			return nil, err
		}
		if !exists {
			return nil, errUnsupportedStatelessUpdate
		}
		stemPath := makeStatelessPath(stem[:path.depth])
		if path.kind == StemPathMissing {
			if !statelessUpdatesContainSet(updates[start:end]) {
				if err := validateStatelessAbsentDeletes(
					claims, updates[start:end], budget,
				); err != nil {
					return nil, err
				}
				start = end

				continue
			}
			newStem, err := updater.commitInsertedStem(
				ctx, claims, stem, updates[start:end], budget,
			)
			if err != nil {
				return nil, err
			}
			insertionIndex, found := missingByPath[stemPath]
			if !found {
				insertionIndex = len(missing)
				missingByPath[stemPath] = insertionIndex
				missing = append(missing, statelessMissingInsertion{path: stemPath})
			}
			missing[insertionIndex].stems = append(
				missing[insertionIndex].stems,
				statelessInsertedStem{stem: stem, commitment: newStem},
			)
			start = end

			continue
		}
		if path.kind == StemPathDifferent {
			if !statelessUpdatesContainSet(updates[start:end]) {
				if err := validateStatelessAbsentDeletes(
					claims, updates[start:end], budget,
				); err != nil {
					return nil, err
				}
				start = end

				continue
			}
			oldStem, oldFound := commitments[stemPath]
			if err := budget.lookup(); err != nil {
				return nil, err
			}
			if !oldFound {
				return nil, errIncompleteStatelessWitness
			}
			newStem, err := updater.commitInsertedStem(
				ctx, claims, stem, updates[start:end], budget,
			)
			if err != nil {
				return nil, err
			}
			insertionIndex, found := differentByPath[stemPath]
			if !found {
				insertionIndex = len(different)
				differentByPath[stemPath] = insertionIndex
				different = append(different, statelessDifferentInsertion{
					path: stemPath,
					existing: statelessInsertedStem{
						stem:       path.existing,
						commitment: oldStem,
					},
				})
			}
			different[insertionIndex].stems = append(
				different[insertionIndex].stems,
				statelessInsertedStem{stem: stem, commitment: newStem},
			)
			start = end

			continue
		}
		if path.kind != StemPathPresent {
			return nil, errUnsupportedStatelessUpdate
		}
		oldStem, exists := commitments[stemPath]
		if err := budget.lookup(); err != nil {
			return nil, err
		}
		if !exists {
			return nil, errIncompleteStatelessWitness
		}
		halfChanges := make(map[byte][]backend.VectorUpdate, 2)
		deletedMembership := false
		for index := start; index < end; index++ {
			claim, found, err := claims.Lookup(updates[index].key)
			if err != nil {
				return nil, err
			}
			if err := budget.lookup(); err != nil {
				return nil, err
			}
			if !found {
				return nil, errIncompleteStatelessWitness
			}
			oldOpening := leafvector.EncodeAbsent(updates[index].key[31])
			if claim.kind == ClaimMembership {
				oldOpening = leafvector.EncodePresent(claim.key[31], [32]byte(claim.value))
			}
			newOpening := leafvector.EncodePresent(updates[index].key[31], [32]byte(updates[index].value))
			if updates[index].kind == UpdateDelete {
				newOpening = leafvector.EncodeAbsent(updates[index].key[31])
				deletedMembership = deletedMembership || claim.kind == ClaimMembership
			}
			if oldOpening == newOpening {
				continue
			}
			half := byte(leafvector.C1HashIndex)
			if oldOpening.Half == leafvector.C2 {
				half = leafvector.C2HashIndex
			}
			halfChanges[half] = append(halfChanges[half],
				backend.VectorUpdate{Index: oldOpening.LowIndex, Old: [32]byte(oldOpening.Low), New: [32]byte(newOpening.Low)},
				backend.VectorUpdate{Index: oldOpening.HighIndex, Old: [32]byte(oldOpening.High), New: [32]byte(newOpening.High)},
			)
		}
		if len(halfChanges) == 0 {
			start = end

			continue
		}
		if deletedMembership {
			retained, err := statelessStemRetained(
				ctx, claims, updates[start:end], stem,
			)
			if err != nil {
				return nil, err
			}
			if !retained {
				return nil, errUnsupportedStatelessUpdate
			}
		}
		stemUpdates := make([]backend.VectorUpdate, 0, len(halfChanges))
		for _, half := range []byte{leafvector.C1HashIndex, leafvector.C2HashIndex} {
			vectorUpdates, exists := halfChanges[half]
			if !exists {
				continue
			}
			halfPath := stemPath
			halfPath.path[halfPath.length] = half
			halfPath.length++
			oldHalf, exists := commitments[halfPath]
			if err := budget.lookup(); err != nil {
				return nil, err
			}
			if !exists {
				return nil, errIncompleteStatelessWitness
			}
			if err := budget.commitmentUpdate(); err != nil {
				return nil, err
			}
			newHalf, err := updater.commitment.UpdateCommitment(ctx, oldHalf, vectorUpdates)
			if err != nil {
				return nil, err
			}
			oldScalar, err := budget.mapCommitment(oldHalf)
			if err != nil {
				return nil, err
			}
			newScalar, err := budget.mapCommitment(newHalf)
			if err != nil {
				return nil, err
			}
			stemUpdates = append(stemUpdates, backend.VectorUpdate{Index: half, Old: oldScalar, New: newScalar})
		}
		if err := budget.commitmentUpdate(); err != nil {
			return nil, err
		}
		newStem, err := updater.commitment.UpdateCommitment(ctx, oldStem, stemUpdates)
		if err != nil {
			return nil, err
		}
		changed[stemPath] = statelessChangedCommitment{old: oldStem, new: newStem}
		start = end
	}
	for index := range missing {
		if err := checkTreeProofContext(ctx); err != nil {
			return nil, err
		}
		inserted, err := updater.commitInsertedSubtree(
			ctx,
			missing[index].stems,
			missing[index].path.length,
			budget,
		)
		if err != nil {
			return nil, err
		}
		changed[missing[index].path] = statelessChangedCommitment{
			old: backend.EmptyVectorCommitment(),
			new: inserted,
		}
	}
	for index := range different {
		stems, err := mergeStatelessExistingStem(
			ctx, different[index].existing, different[index].stems,
		)
		if err != nil {
			return nil, err
		}
		inserted, err := updater.commitInsertedSubtree(
			ctx, stems, different[index].path.length, budget,
		)
		if err != nil {
			return nil, err
		}
		changed[different[index].path] = statelessChangedCommitment{
			old: different[index].existing.commitment,
			new: inserted,
		}
	}

	return changed, nil
}

func (updater *StatelessUpdater) commitInsertedStem(
	ctx context.Context,
	claims ClaimSet,
	stem Stem,
	updates []Update,
	budget *statelessUpdateBudget,
) (backend.VectorCommitment, error) {
	var halves [2]backend.Vector
	for index := range updates {
		if err := checkTreeProofContext(ctx); err != nil {
			return backend.VectorCommitment{}, err
		}
		claim, found, err := claims.Lookup(updates[index].key)
		if err != nil {
			return backend.VectorCommitment{}, err
		}
		if err := budget.lookup(); err != nil {
			return backend.VectorCommitment{}, err
		}
		if !found || claim.kind != ClaimAbsence {
			return backend.VectorCommitment{}, errIncompleteStatelessWitness
		}
		if updates[index].kind == UpdateDelete {
			continue
		}
		opening := leafvector.EncodePresent(
			updates[index].key[31],
			[32]byte(updates[index].value),
		)
		half := &halves[0]
		if opening.Half == leafvector.C2 {
			half = &halves[1]
		}
		half[opening.LowIndex] = [32]byte(opening.Low)
		half[opening.HighIndex] = [32]byte(opening.High)
	}

	var halfCommitments [2]backend.VectorCommitment
	for index := range halves {
		if err := budget.commitmentUpdate(); err != nil {
			return backend.VectorCommitment{}, err
		}
		committed, err := updater.commitment.Commit(ctx, halves[index])
		if err != nil {
			return backend.VectorCommitment{}, err
		}
		halfCommitments[index] = committed
	}
	c1Scalar, err := budget.mapCommitment(halfCommitments[0])
	if err != nil {
		return backend.VectorCommitment{}, err
	}
	c2Scalar, err := budget.mapCommitment(halfCommitments[1])
	if err != nil {
		return backend.VectorCommitment{}, err
	}

	var vector backend.Vector
	vector[leafvector.ExtensionMarkerIndex] = [32]byte(leafvector.EncodeExtensionMarker())
	vector[leafvector.StemIndex] = [32]byte(leafvector.EncodeStem(stem))
	vector[leafvector.C1HashIndex] = c1Scalar
	vector[leafvector.C2HashIndex] = c2Scalar
	if err := budget.commitmentUpdate(); err != nil {
		return backend.VectorCommitment{}, err
	}

	return updater.commitment.Commit(ctx, vector)
}

func statelessUpdatesContainSet(updates []Update) bool {
	for index := range updates {
		if updates[index].kind == UpdateSet {
			return true
		}
	}

	return false
}

func validateStatelessAbsentDeletes(
	claims ClaimSet,
	updates []Update,
	budget *statelessUpdateBudget,
) error {
	for index := range updates {
		claim, found, err := claims.Lookup(updates[index].key)
		if err != nil {
			return err
		}
		if err := budget.lookup(); err != nil {
			return err
		}
		if !found || claim.kind != ClaimAbsence {
			return errIncompleteStatelessWitness
		}
	}

	return nil
}

func statelessStemRetained(
	ctx context.Context,
	claims ClaimSet,
	updates []Update,
	stem Stem,
) (bool, error) {
	for index := range updates {
		if err := checkTreeProofContext(ctx); err != nil {
			return false, err
		}
		if updates[index].kind == UpdateSet {
			return true, nil
		}
	}
	claimIndex := sort.Search(len(claims.claims), func(index int) bool {
		return bytes.Compare(claims.claims[index].key[:31], stem[:]) >= 0
	})
	updateIndex := 0
	for claimIndex < len(claims.claims) {
		if err := checkTreeProofContext(ctx); err != nil {
			return false, err
		}
		claim := claims.claims[claimIndex]
		if Stem(claim.key[:31]) != stem {
			break
		}
		for updateIndex < len(updates) &&
			bytes.Compare(updates[updateIndex].key[:], claim.key[:]) < 0 {
			if err := checkTreeProofContext(ctx); err != nil {
				return false, err
			}
			updateIndex++
		}
		if claim.kind == ClaimMembership &&
			(updateIndex == len(updates) || updates[updateIndex].key != claim.key) {
			return true, nil
		}
		claimIndex++
	}

	return false, nil
}

func mergeStatelessExistingStem(
	ctx context.Context,
	existing statelessInsertedStem,
	inserted []statelessInsertedStem,
) ([]statelessInsertedStem, error) {
	result := make([]statelessInsertedStem, 0, len(inserted)+1)
	merged := false
	for index := range inserted {
		if err := checkTreeProofContext(ctx); err != nil {
			return nil, err
		}
		order := bytes.Compare(existing.stem[:], inserted[index].stem[:])
		if order == 0 {
			return nil, errInvalidStatelessUpdate
		}
		if !merged && order < 0 {
			result = append(result, existing)
			merged = true
		}
		result = append(result, inserted[index])
	}
	if !merged {
		result = append(result, existing)
	}

	return result, nil
}

func (updater *StatelessUpdater) commitInsertedSubtree(
	ctx context.Context,
	stems []statelessInsertedStem,
	depth uint8,
	budget *statelessUpdateBudget,
) (backend.VectorCommitment, error) {
	if err := checkTreeProofContext(ctx); err != nil {
		return backend.VectorCommitment{}, err
	}
	if len(stems) == 0 {
		return backend.VectorCommitment{}, errInvalidStatelessUpdate
	}
	if len(stems) == 1 {
		return stems[0].commitment, nil
	}
	if depth >= uint8(len(Stem{})) {
		return backend.VectorCommitment{}, errInvalidStatelessUpdate
	}

	var vector backend.Vector
	for start := 0; start < len(stems); {
		if err := checkTreeProofContext(ctx); err != nil {
			return backend.VectorCommitment{}, err
		}
		index := stems[start].stem[depth]
		end := start + 1
		for end < len(stems) && stems[end].stem[depth] == index {
			end++
		}
		child, err := updater.commitInsertedSubtree(
			ctx, stems[start:end], depth+1, budget,
		)
		if err != nil {
			return backend.VectorCommitment{}, err
		}
		mapped, err := budget.mapCommitment(child)
		if err != nil {
			return backend.VectorCommitment{}, err
		}
		vector[index] = mapped
		start = end
	}
	if err := budget.commitmentUpdate(); err != nil {
		return backend.VectorCommitment{}, err
	}

	return updater.commitment.Commit(ctx, vector)
}

func (updater *StatelessUpdater) updateAncestors(
	ctx context.Context,
	commitments map[statelessPath]backend.VectorCommitment,
	changed map[statelessPath]statelessChangedCommitment,
	budget *statelessUpdateBudget,
) (backend.VectorCommitment, error) {
	if len(changed) == 0 {
		return backend.VectorCommitment{}, errInvalidStatelessUpdate
	}
	for {
		maxDepth := uint8(0)
		for path := range changed {
			maxDepth = max(maxDepth, path.length)
		}
		if maxDepth == 0 {
			if root, exists := changed[statelessPath{}]; exists && len(changed) == 1 {
				return root.new, nil
			}

			return backend.VectorCommitment{}, errInvalidStatelessUpdate
		}
		parents := make(map[statelessPath][]backend.VectorUpdate)
		for path, value := range changed {
			if err := checkTreeProofContext(ctx); err != nil {
				return backend.VectorCommitment{}, err
			}
			if path.length == maxDepth {
				parent := path
				parent.length--
				index := path.path[parent.length]
				parent.path[parent.length] = 0
				oldScalar, err := budget.mapCommitment(value.old)
				if err != nil {
					return backend.VectorCommitment{}, err
				}
				newScalar, err := budget.mapCommitment(value.new)
				if err != nil {
					return backend.VectorCommitment{}, err
				}
				parents[parent] = append(parents[parent], backend.VectorUpdate{Index: index, Old: oldScalar, New: newScalar})
			}
		}
		next := make(map[statelessPath]statelessChangedCommitment)
		for path, value := range changed {
			if path.length != maxDepth {
				next[path] = value
			}
		}
		for parent, vectorUpdates := range parents {
			oldParent, exists := commitments[parent]
			if err := budget.lookup(); err != nil {
				return backend.VectorCommitment{}, err
			}
			if !exists {
				return backend.VectorCommitment{}, errIncompleteStatelessWitness
			}
			if err := budget.commitmentUpdate(); err != nil {
				return backend.VectorCommitment{}, err
			}
			newParent, err := updater.commitment.UpdateCommitment(ctx, oldParent, vectorUpdates)
			if err != nil {
				return backend.VectorCommitment{}, err
			}
			next[parent] = statelessChangedCommitment{old: oldParent, new: newParent}
		}
		changed = next
	}
}

func makeStatelessPath(path []byte) statelessPath {
	value := statelessPath{length: uint8(len(path))}
	copy(value.path[:], path)

	return value
}

func (budget *statelessUpdateBudget) lookup() error {
	budget.pathLookups++
	return checkStatelessUpdateResource(
		StatelessUpdateResourcePathLookups,
		uint64(budget.limits.MaxPathLookups),
		budget.pathLookups,
	)
}

func (budget *statelessUpdateBudget) commitmentUpdate() error {
	budget.commitmentUpdates++

	return checkStatelessUpdateResource(
		StatelessUpdateResourceCommitmentUpdates,
		uint64(budget.limits.MaxCommitmentUpdates),
		budget.commitmentUpdates,
	)
}

func (budget *statelessUpdateBudget) mapCommitment(
	commitment backend.VectorCommitment,
) ([32]byte, error) {
	budget.fieldMappings++
	if err := checkStatelessUpdateResource(
		StatelessUpdateResourceFieldMappings,
		uint64(budget.limits.MaxFieldMappings),
		budget.fieldMappings,
	); err != nil {
		return [32]byte{}, err
	}

	return commitment.ScalarBytes()
}

func checkStatelessUpdateResource(
	resource StatelessUpdateResource,
	limit uint64,
	actual uint64,
) error {
	if actual <= limit {
		return nil
	}

	return &StatelessUpdateResourceError{Resource: resource, Limit: limit, Actual: actual}
}
