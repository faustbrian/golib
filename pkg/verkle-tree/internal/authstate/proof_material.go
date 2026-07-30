package authstate

import (
	"context"
	"errors"
	"fmt"
	"math/bits"
	"slices"

	verkletree "github.com/faustbrian/golib/pkg/verkle-tree"
	"github.com/faustbrian/golib/pkg/verkle-tree/internal/backend"
	"github.com/faustbrian/golib/pkg/verkle-tree/internal/committedtree"
)

const (
	proofMaterialKeyWorkingBytes = uint64(32)
	proofPathMaximumCommitments  = uint64(32)
	proofPathMaximumBytes        = uint64(528)
	proofPathCommitmentBytes     = uint64(256)
)

var (
	errInvalidProofMaterial       = errors.New("invalid snapshot proof material")
	errInvalidProofMaterialLimits = errors.New(
		"invalid snapshot proof-material limits",
	)
	errProofMaterialResource = errors.New(
		"snapshot proof-material resource limit exceeded",
	)
)

// ProofMaterialLimits bounds aggregate snapshot proof-material assembly. Every
// field must be positive and no field denotes an unbounded resource.
type ProofMaterialLimits struct {
	MaxKeys            uint32
	MaxStemPaths       uint32
	MaxNodeReads       uint64
	MaxPathCommitments uint32
	MaxPathBytes       uint64
	MaxTemporaryBytes  uint64
}

func (limits ProofMaterialLimits) validate() error {
	if limits.MaxKeys == 0 ||
		limits.MaxKeys > maxClaimCount ||
		limits.MaxStemPaths == 0 ||
		limits.MaxStemPaths > maxTreeProofStemPaths ||
		limits.MaxNodeReads == 0 ||
		limits.MaxNodeReads > uint64(maxTreeProofPathDerivations) ||
		limits.MaxPathCommitments == 0 ||
		limits.MaxPathCommitments > maxTreeProofPathCommitments ||
		limits.MaxPathBytes == 0 ||
		limits.MaxPathBytes >
			uint64(maxTreeProofPathCommitments)*maxProofPathLength ||
		limits.MaxTemporaryBytes == 0 {
		return errInvalidProofMaterialLimits
	}

	return nil
}

// ProofMaterialResource identifies one bounded assembly resource.
type ProofMaterialResource uint8

const (
	// ProofMaterialResourceKeys counts distinct requested keys.
	ProofMaterialResourceKeys ProofMaterialResource = iota + 1

	// ProofMaterialResourceStemPaths counts distinct requested stems.
	ProofMaterialResourceStemPaths

	// ProofMaterialResourceNodeReads counts immutable tree nodes inspected.
	ProofMaterialResourceNodeReads

	// ProofMaterialResourcePathCommitments counts extracted path records before
	// deterministic deduplication.
	ProofMaterialResourcePathCommitments

	// ProofMaterialResourcePathBytes counts extracted canonical path bytes
	// before deterministic deduplication.
	ProofMaterialResourcePathBytes

	// ProofMaterialResourceTemporaryBytes counts conservative owned and sorting
	// storage.
	ProofMaterialResourceTemporaryBytes
)

// ProofMaterialResourceError reports one rejected assembly bound without
// disclosing keys, values, paths, or commitments.
type ProofMaterialResourceError struct {
	Resource ProofMaterialResource
	Limit    uint64
	Actual   uint64
}

// Error implements error.
func (err *ProofMaterialResourceError) Error() string {
	return fmt.Sprintf(
		"%v: resource %d has value %d, limit %d",
		errProofMaterialResource,
		err.Resource,
		err.Actual,
		err.Limit,
	)
}

// Unwrap makes ProofMaterialResourceError match the resource sentinel.
func (err *ProofMaterialResourceError) Unwrap() error {
	return errProofMaterialResource
}

// ProofMaterial is immutable canonical claim, topology, commitment, and root
// material derived from one snapshot. It contains no aggregate opening and
// establishes no cryptographic verification.
type ProofMaterial struct {
	root        backend.Root
	claims      ClaimSet
	stemPaths   []StemPath
	commitments []PathCommitment
	valid       bool
}

// ProofMaterial derives complete canonical proof inputs for unordered distinct
// keys from this exact immutable snapshot. It rejects empty roots until their
// opening-free non-membership form is specified.
func (snapshot Snapshot) ProofMaterial(
	ctx context.Context,
	keys []Key,
	limits ProofMaterialLimits,
) (ProofMaterial, error) {
	if err := snapshot.validate(); err != nil {
		return ProofMaterial{}, err
	}
	if err := checkContext(ctx); err != nil {
		return ProofMaterial{}, err
	}
	if err := limits.validate(); err != nil {
		return ProofMaterial{}, err
	}
	if len(keys) == 0 {
		return ProofMaterial{}, errInvalidProofMaterial
	}
	root, err := snapshot.RootContainer(ctx)
	if err != nil {
		return ProofMaterial{}, err
	}
	empty, err := root.IsEmpty()
	if err != nil || empty {
		return ProofMaterial{}, errInvalidProofMaterial
	}
	keyCount := uint64(len(keys))
	if err := checkProofMaterialResource(
		ProofMaterialResourceKeys,
		uint64(limits.MaxKeys),
		keyCount,
	); err != nil {
		return ProofMaterial{}, err
	}
	pathCapacity := min(
		keyCount*proofPathMaximumCommitments,
		uint64(limits.MaxPathCommitments),
	)
	temporaryBytes := keyCount*2*proofMaterialKeyWorkingBytes +
		keyCount*3*claimWorkingBytes +
		keyCount*2*stemPathWorkingBytes +
		pathCapacity*2*pathCommitmentWorkingBytes
	if err := checkProofMaterialResource(
		ProofMaterialResourceTemporaryBytes,
		limits.MaxTemporaryBytes,
		temporaryBytes,
	); err != nil {
		return ProofMaterial{}, err
	}

	ordered := slices.Clone(keys)
	if err := sortTreeProofValues(ctx, ordered, compareKey); err != nil {
		return ProofMaterial{}, err
	}
	claims := make([]Claim, len(ordered))
	for index := range ordered {
		if err := checkContext(ctx); err != nil {
			return ProofMaterial{}, err
		}
		if index > 0 && ordered[index-1] == ordered[index] {
			return ProofMaterial{}, errDuplicateClaimKey
		}
		entryIndex, found := findEntry(snapshot.entries, ordered[index])
		if found {
			claims[index] = Membership(
				ordered[index],
				snapshot.entries[entryIndex].Value,
			)
		} else {
			claims[index] = Absence(ordered[index])
		}
	}
	claimSet, err := NewClaimSet(
		ctx,
		verkletree.ExperimentalBandersnatchIPA256V0(),
		claims,
		ClaimLimits{
			MaxClaims:         limits.MaxKeys,
			MaxTemporaryBytes: limits.MaxTemporaryBytes,
		},
	)
	if err != nil {
		return ProofMaterial{}, err
	}
	stemPaths := make(
		[]StemPath,
		0,
		min(len(ordered), int(limits.MaxStemPaths)),
	)
	commitments := make([]PathCommitment, 0, int(pathCapacity))
	nodeReads := uint64(0)
	pathBytes := uint64(0)
	for start := 0; start < len(ordered); {
		if err := checkContext(ctx); err != nil {
			return ProofMaterial{}, err
		}
		end := start + 1
		stem := proofMaterialStemFromKey(ordered[start])
		for end < len(ordered) && proofMaterialStemFromKey(ordered[end]) == stem {
			end++
		}
		if err := checkProofMaterialResource(
			ProofMaterialResourceStemPaths,
			uint64(limits.MaxStemPaths),
			uint64(len(stemPaths)+1),
		); err != nil {
			return ProofMaterial{}, err
		}
		path, err := snapshot.extractProofPath(
			ctx,
			ordered[start],
			limits,
			&nodeReads,
			&pathBytes,
			&commitments,
		)
		if err != nil {
			return ProofMaterial{}, err
		}
		stemPaths = append(stemPaths, stemPathFromCommitted(stem, path))
		if path.Kind == committedtree.ProofPathPresent &&
			ordered[start][31] < 128 {
			secondHalf := start + 1
			for secondHalf < end && ordered[secondHalf][31] < 128 {
				secondHalf++
			}
			if secondHalf < end {
				if _, err := snapshot.extractProofPath(
					ctx,
					ordered[secondHalf],
					limits,
					&nodeReads,
					&pathBytes,
					&commitments,
				); err != nil {
					return ProofMaterial{}, err
				}
			}
		}
		start = end
	}

	if err := sortTreeProofValues(
		ctx,
		commitments,
		comparePathCommitments,
	); err != nil {
		return ProofMaterial{}, err
	}
	unique, err := deduplicatePathCommitments(ctx, commitments)
	if err != nil {
		return ProofMaterial{}, err
	}

	return newProofMaterial(root, claimSet, stemPaths, unique)
}

func proofMaterialStemFromKey(key Key) Stem {
	return Stem(key[:31])
}

func (snapshot Snapshot) extractProofPath(
	ctx context.Context,
	key Key,
	limits ProofMaterialLimits,
	nodeReads *uint64,
	pathBytes *uint64,
	commitments *[]PathCommitment,
) (committedtree.ProofPath, error) {
	remainingReads, err := remainingProofMaterialResource(
		ProofMaterialResourceNodeReads,
		limits.MaxNodeReads,
		*nodeReads,
	)
	if err != nil {
		return committedtree.ProofPath{}, err
	}
	commitmentCount := uint64(len(*commitments))
	remainingCommitments, err := remainingProofMaterialResource(
		ProofMaterialResourcePathCommitments,
		uint64(limits.MaxPathCommitments),
		commitmentCount,
	)
	if err != nil {
		return committedtree.ProofPath{}, err
	}
	remainingPathBytes, err := remainingProofMaterialResource(
		ProofMaterialResourcePathBytes,
		limits.MaxPathBytes,
		*pathBytes,
	)
	if err != nil {
		return committedtree.ProofPath{}, err
	}
	path, err := snapshot.tree.ProofPath(
		ctx,
		key,
		committedtree.ProofPathLimits{
			MaxNodeReads: uint32(min(
				remainingReads,
				proofPathMaximumCommitments,
			)),
			MaxCommitments: uint32(min(
				remainingCommitments,
				proofPathMaximumCommitments,
			)),
			MaxPathBytes: min(
				remainingPathBytes,
				proofPathMaximumBytes,
			),
			MaxTemporaryBytes: min(
				remainingCommitments,
				proofPathMaximumCommitments,
			) * proofPathCommitmentBytes,
		},
	)
	if err != nil {
		return committedtree.ProofPath{}, translateProofPathResourceError(
			err,
			limits,
			*nodeReads,
			commitmentCount,
			*pathBytes,
		)
	}
	reads := uint64(path.Depth)
	if path.Kind != committedtree.ProofPathMissing {
		reads++
	}
	*nodeReads += reads
	converted, addedPathBytes := convertProofPathCommitments(path.Commitments)
	*commitments = append(*commitments, converted...)
	*pathBytes += addedPathBytes

	return path, nil
}

func stemPathFromCommitted(
	stem Stem,
	path committedtree.ProofPath,
) StemPath {
	switch path.Kind {
	case committedtree.ProofPathPresent:
		return PresentStemPath(stem, path.Depth)
	case committedtree.ProofPathMissing:
		return MissingStemPath(stem, path.Depth)
	case committedtree.ProofPathDifferent:
		return DifferentStemPath(
			stem,
			path.Depth,
			Stem(path.ExistingStem),
		)
	default:
		return StemPath{}
	}
}

func convertProofPathCommitments(
	values []committedtree.ProofPathCommitment,
) ([]PathCommitment, uint64) {
	converted := make([]PathCommitment, len(values))
	pathBytes := uint64(0)
	for index := range values {
		length := int(values[index].Length)
		pathBytes = saturatingProofMaterialAdd(pathBytes, uint64(length))
		if length == 0 || length > len(values[index].Path) {
			continue
		}
		value, err := NewPathCommitment(
			values[index].Path[:length],
			values[index].Commitment,
		)
		if err != nil {
			continue
		}
		converted[index] = value
	}

	return converted, pathBytes
}

func deduplicatePathCommitments(
	ctx context.Context,
	commitments []PathCommitment,
) ([]PathCommitment, error) {
	unique := commitments[:0]
	for index := range commitments {
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		if len(unique) > 0 &&
			comparePathCommitments(unique[len(unique)-1], commitments[index]) == 0 {
			left, leftErr := unique[len(unique)-1].commitment.ScalarBytes()
			right, rightErr := commitments[index].commitment.ScalarBytes()
			if leftErr != nil {
				return nil, errInvalidProofMaterial
			}
			if rightErr != nil {
				return nil, errInvalidProofMaterial
			}
			if left != right {
				return nil, errInvalidProofMaterial
			}
			continue
		}
		unique = append(unique, commitments[index])
	}

	return unique, nil
}

// Root returns the exact non-empty profile-bound snapshot root.
func (material ProofMaterial) Root() (backend.Root, error) {
	if err := material.validate(); err != nil {
		return backend.Root{}, err
	}

	return material.root, nil
}

// Claims returns the immutable canonical claims derived from the snapshot.
func (material ProofMaterial) Claims() (ClaimSet, error) {
	if err := material.validate(); err != nil {
		return ClaimSet{}, err
	}

	return material.claims, nil
}

// StemPaths returns a cancellation-aware owned copy in canonical stem order.
func (material ProofMaterial) StemPaths(
	ctx context.Context,
) ([]StemPath, error) {
	if err := material.validate(); err != nil {
		return nil, err
	}

	return copyTreeProofValues(ctx, material.stemPaths)
}

// PathCommitments returns a cancellation-aware owned copy in canonical path
// order.
func (material ProofMaterial) PathCommitments(
	ctx context.Context,
) ([]PathCommitment, error) {
	if err := material.validate(); err != nil {
		return nil, err
	}

	return copyTreeProofValues(ctx, material.commitments)
}

func (material ProofMaterial) validate() error {
	if !material.valid ||
		material.claims.validate() != nil ||
		len(material.stemPaths) == 0 {
		return errInvalidProofMaterial
	}
	if _, err := material.root.Profile(); err != nil {
		return errInvalidProofMaterial
	}
	empty, _ := material.root.IsEmpty()
	if empty {
		return errInvalidProofMaterial
	}

	return nil
}

func newProofMaterial(
	root backend.Root,
	claims ClaimSet,
	stemPaths []StemPath,
	commitments []PathCommitment,
) (ProofMaterial, error) {
	material := ProofMaterial{
		root:        root,
		claims:      claims,
		stemPaths:   stemPaths,
		commitments: commitments,
		valid:       true,
	}
	if err := material.validate(); err != nil {
		return ProofMaterial{}, err
	}
	for index := range stemPaths {
		if err := stemPaths[index].validate(); err != nil {
			return ProofMaterial{}, errInvalidProofMaterial
		}
	}
	for index := range commitments {
		if err := commitments[index].validate(); err != nil {
			return ProofMaterial{}, errInvalidProofMaterial
		}
	}

	return material, nil
}

func checkProofMaterialResource(
	resource ProofMaterialResource,
	limit uint64,
	actual uint64,
) error {
	if actual <= limit {
		return nil
	}

	return newProofMaterialResourceError(resource, limit, actual)
}

func newProofMaterialResourceError(
	resource ProofMaterialResource,
	limit uint64,
	actual uint64,
) error {
	return &ProofMaterialResourceError{
		Resource: resource,
		Limit:    limit,
		Actual:   actual,
	}
}

func remainingProofMaterialResource(
	resource ProofMaterialResource,
	limit uint64,
	used uint64,
) (uint64, error) {
	if used < limit {
		return limit - used, nil
	}

	return 0, newProofMaterialResourceError(
		resource,
		limit,
		saturatingProofMaterialAdd(used, 1),
	)
}

func translateProofPathResourceError(
	err error,
	limits ProofMaterialLimits,
	nodeReads uint64,
	commitmentCount uint64,
	pathBytes uint64,
) error {
	var resourceErr *committedtree.ProofPathResourceError
	if !errors.As(err, &resourceErr) {
		return err
	}
	switch resourceErr.Resource {
	case committedtree.ProofPathResourceNodeReads:
		return newProofMaterialResourceError(
			ProofMaterialResourceNodeReads,
			limits.MaxNodeReads,
			saturatingProofMaterialAdd(nodeReads, resourceErr.Actual),
		)
	case committedtree.ProofPathResourceCommitments:
		return newProofMaterialResourceError(
			ProofMaterialResourcePathCommitments,
			uint64(limits.MaxPathCommitments),
			saturatingProofMaterialAdd(
				commitmentCount,
				resourceErr.Actual,
			),
		)
	case committedtree.ProofPathResourcePathBytes:
		return newProofMaterialResourceError(
			ProofMaterialResourcePathBytes,
			limits.MaxPathBytes,
			saturatingProofMaterialAdd(pathBytes, resourceErr.Actual),
		)
	default:
		return errInvalidProofMaterial
	}
}

func saturatingProofMaterialAdd(left uint64, right uint64) uint64 {
	result, carry := bits.Add64(left, right, 0)
	if carry != 0 {
		return ^uint64(0)
	}

	return result
}
