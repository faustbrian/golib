package authstate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"

	verkletree "github.com/faustbrian/golib/pkg/verkle-tree"
	"github.com/faustbrian/golib/pkg/verkle-tree/internal/backend"
)

const (
	maxProofPathLength          = 32
	maxTreeProofStemPaths       = uint32(65_536)
	maxTreeProofPathCommitments = uint32(2_097_152)
	maxTreeProofPathDerivations = uint32(2_097_152)

	stemPathWorkingBytes       = uint64(96)
	pathCommitmentWorkingBytes = uint64(128)
	pathMarkerWorkingBytes     = uint64(96)
)

var (
	errInvalidStemPath         = errors.New("invalid tree-proof stem path")
	errInvalidPathCommitment   = errors.New("invalid tree-proof path commitment")
	errInvalidTreeProof        = errors.New("invalid tree proof")
	errInvalidTreeProofLimits  = errors.New("invalid tree-proof limits")
	errInvalidTreeProofContext = errors.New(
		"invalid tree-proof context",
	)
	errTreeProofCancelled = errors.New("tree-proof operation cancelled")
	errTreeProofResource  = errors.New("tree-proof resource limit exceeded")
)

// Stem is the fixed 31-byte path portion of one experimental-profile key.
type Stem [31]byte

// StemPathKind identifies how one queried stem terminates in the committed
// topology.
type StemPathKind uint8

const (
	// StemPathPresent means the exact queried stem is committed at Depth.
	StemPathPresent StemPathKind = iota + 1

	// StemPathMissing means the selected child at Depth is absent.
	StemPathMissing

	// StemPathDifferent means the selected child commits to another stem.
	StemPathDifferent
)

// StemPath is one immutable topology assertion for all claims sharing a stem.
// Its zero value is invalid.
type StemPath struct {
	stem     Stem
	existing Stem
	depth    uint8
	kind     StemPathKind
	valid    bool
}

// PresentStemPath returns a path asserting that stem is present at depth.
func PresentStemPath(stem Stem, depth uint8) StemPath {
	return StemPath{
		stem:  stem,
		depth: depth,
		kind:  StemPathPresent,
		valid: true,
	}
}

// MissingStemPath returns a path asserting that a child is missing at depth.
func MissingStemPath(stem Stem, depth uint8) StemPath {
	return StemPath{
		stem:  stem,
		depth: depth,
		kind:  StemPathMissing,
		valid: true,
	}
}

// DifferentStemPath returns a path asserting that the queried stem reaches a
// different existing stem at depth.
func DifferentStemPath(stem Stem, depth uint8, existing Stem) StemPath {
	return StemPath{
		stem:     stem,
		existing: existing,
		depth:    depth,
		kind:     StemPathDifferent,
		valid:    true,
	}
}

// Stem returns the exact queried stem.
func (path StemPath) Stem() (Stem, error) {
	if err := path.validate(); err != nil {
		return Stem{}, err
	}

	return path.stem, nil
}

// Depth returns the number of committed tree edges traversed.
func (path StemPath) Depth() (uint8, error) {
	if err := path.validate(); err != nil {
		return 0, err
	}

	return path.depth, nil
}

// Kind returns how the stem path terminates.
func (path StemPath) Kind() (StemPathKind, error) {
	if err := path.validate(); err != nil {
		return 0, err
	}

	return path.kind, nil
}

// ExistingStem returns the encountered stem and present true for a different
// stem path. Other path kinds return a zero stem and present false.
func (path StemPath) ExistingStem() (Stem, bool, error) {
	if err := path.validate(); err != nil {
		return Stem{}, false, err
	}
	if path.kind != StemPathDifferent {
		return Stem{}, false, nil
	}

	return path.existing, true, nil
}

func (path StemPath) validate() error {
	if !path.valid || path.depth == 0 || path.depth > uint8(len(path.stem)) {
		return errInvalidStemPath
	}
	switch path.kind {
	case StemPathPresent, StemPathMissing:
		if path.existing != (Stem{}) {
			return errInvalidStemPath
		}
	case StemPathDifferent:
		if path.existing == path.stem ||
			!bytes.Equal(
				path.existing[:path.depth],
				path.stem[:path.depth],
			) {
			return errInvalidStemPath
		}
	default:
		return errInvalidStemPath
	}

	return nil
}

// PathCommitment binds one non-root canonical tree path to its non-identity
// vector commitment. Its zero value is invalid.
type PathCommitment struct {
	path       [maxProofPathLength]byte
	commitment backend.VectorCommitment
	length     uint8
	valid      bool
}

// NewPathCommitment validates and owns a non-empty path and commitment.
func NewPathCommitment(
	path []byte,
	commitment backend.VectorCommitment,
) (PathCommitment, error) {
	if len(path) == 0 || len(path) > maxProofPathLength {
		return PathCommitment{}, errInvalidPathCommitment
	}
	if _, err := commitment.Bytes(); err != nil {
		return PathCommitment{}, errInvalidPathCommitment
	}

	value := PathCommitment{
		commitment: commitment,
		length:     uint8(len(path)),
		valid:      true,
	}
	copy(value.path[:], path)

	return value, nil
}

// Path returns an owned copy of the canonical path bytes.
func (value PathCommitment) Path() ([]byte, error) {
	if err := value.validate(); err != nil {
		return nil, err
	}

	path := make([]byte, value.length)
	copy(path, value.path[:value.length])

	return path, nil
}

// Commitment returns the immutable vector commitment at Path.
func (value PathCommitment) Commitment() (backend.VectorCommitment, error) {
	if err := value.validate(); err != nil {
		return backend.VectorCommitment{}, err
	}

	return value.commitment, nil
}

func (value PathCommitment) validate() error {
	if !value.valid ||
		value.length == 0 ||
		value.length > maxProofPathLength {
		return errInvalidPathCommitment
	}
	if _, err := value.commitment.Bytes(); err != nil {
		return errInvalidPathCommitment
	}

	return nil
}

// TreeProofLimits bounds canonical unverified proof construction. Every field
// must be positive and no field denotes an unbounded resource.
type TreeProofLimits struct {
	MaxClaims          uint32
	MaxStemPaths       uint32
	MaxPathCommitments uint32
	MaxPathDerivations uint32
	MaxPathBytes       uint64
	MaxTemporaryBytes  uint64
}

func (limits TreeProofLimits) validate() error {
	if limits.MaxClaims == 0 ||
		limits.MaxClaims > maxClaimCount ||
		limits.MaxStemPaths == 0 ||
		limits.MaxStemPaths > maxTreeProofStemPaths ||
		limits.MaxPathCommitments == 0 ||
		limits.MaxPathCommitments > maxTreeProofPathCommitments ||
		limits.MaxPathDerivations == 0 ||
		limits.MaxPathDerivations > maxTreeProofPathDerivations ||
		limits.MaxPathBytes == 0 ||
		limits.MaxPathBytes >
			uint64(maxTreeProofPathCommitments)*maxProofPathLength ||
		limits.MaxTemporaryBytes == 0 {
		return errInvalidTreeProofLimits
	}

	return nil
}

// TreeProofResource identifies one bounded proof-container resource.
type TreeProofResource uint8

const (
	// TreeProofResourceClaims counts claimed keys.
	TreeProofResourceClaims TreeProofResource = iota + 1

	// TreeProofResourceStemPaths counts distinct queried stems.
	TreeProofResourceStemPaths

	// TreeProofResourcePathCommitments counts non-root commitments.
	TreeProofResourcePathCommitments

	// TreeProofResourcePathDerivations counts topology markers derived from
	// claims and stem paths.
	TreeProofResourcePathDerivations

	// TreeProofResourcePathBytes counts retained path bytes.
	TreeProofResourcePathBytes

	// TreeProofResourceTemporaryBytes counts conservative owned copies and
	// deterministic sorting scratch.
	TreeProofResourceTemporaryBytes
)

// TreeProofResourceError reports one rejected proof-container bound without
// disclosing keys, values, paths, commitments, or proof bytes.
type TreeProofResourceError struct {
	Resource TreeProofResource
	Limit    uint64
	Actual   uint64
}

// Error implements error.
func (err *TreeProofResourceError) Error() string {
	return fmt.Sprintf(
		"%v: resource %d has value %d, limit %d",
		errTreeProofResource,
		err.Resource,
		err.Actual,
		err.Limit,
	)
}

// Unwrap makes TreeProofResourceError match the proof-resource sentinel.
func (err *TreeProofResourceError) Unwrap() error {
	return errTreeProofResource
}

// TreeProof is one immutable canonical but unverified tree-proof container. It
// binds claims and path commitments to an exact root and raw opening payload;
// successful construction does not establish cryptographic verification.
type TreeProof struct {
	profile     verkletree.Profile
	root        backend.Root
	claims      ClaimSet
	stemPaths   []StemPath
	commitments []PathCommitment
	opening     backend.OpeningProof
	valid       bool
}

// NewTreeProof validates, canonicalizes, and owns all tree-proof components.
// It rejects omitted, surplus, duplicate, or structurally conflicting paths
// but performs no cryptographic opening verification.
func NewTreeProof(
	ctx context.Context,
	root backend.Root,
	claims ClaimSet,
	stemPaths []StemPath,
	commitments []PathCommitment,
	opening backend.OpeningProof,
	limits TreeProofLimits,
) (TreeProof, error) {
	if err := checkTreeProofContext(ctx); err != nil {
		return TreeProof{}, err
	}
	profile, err := root.Profile()
	if err != nil {
		return TreeProof{}, errInvalidTreeProof
	}
	emptyRoot, err := root.IsEmpty()
	if err != nil || emptyRoot {
		return TreeProof{}, errInvalidTreeProof
	}
	if err := claims.validate(); err != nil {
		return TreeProof{}, errInvalidTreeProof
	}
	if _, err := opening.Bytes(); err != nil {
		return TreeProof{}, errInvalidTreeProof
	}
	if err := limits.validate(); err != nil {
		return TreeProof{}, err
	}
	claimCount := uint32(len(claims.claims))
	stemPathCount := uint64(len(stemPaths))
	commitmentCount := uint64(len(commitments))
	if err := checkTreeProofResource(
		TreeProofResourceClaims,
		uint64(limits.MaxClaims),
		uint64(claimCount),
	); err != nil {
		return TreeProof{}, err
	}
	if err := checkTreeProofResource(
		TreeProofResourceStemPaths,
		uint64(limits.MaxStemPaths),
		stemPathCount,
	); err != nil {
		return TreeProof{}, err
	}
	if err := checkTreeProofResource(
		TreeProofResourcePathCommitments,
		uint64(limits.MaxPathCommitments),
		commitmentCount,
	); err != nil {
		return TreeProof{}, err
	}
	derivationBound := stemPathCount*31 + uint64(claimCount)
	if err := checkTreeProofResource(
		TreeProofResourcePathDerivations,
		uint64(limits.MaxPathDerivations),
		derivationBound,
	); err != nil {
		return TreeProof{}, err
	}

	pathBytes := uint64(0)
	for index := range stemPaths {
		if err := checkTreeProofContext(ctx); err != nil {
			return TreeProof{}, err
		}
		if err := stemPaths[index].validate(); err != nil {
			return TreeProof{}, err
		}
	}
	for index := range commitments {
		if err := checkTreeProofContext(ctx); err != nil {
			return TreeProof{}, err
		}
		if err := commitments[index].validate(); err != nil {
			return TreeProof{}, err
		}
		pathBytes += uint64(commitments[index].length)
	}
	if err := checkTreeProofResource(
		TreeProofResourcePathBytes,
		limits.MaxPathBytes,
		pathBytes,
	); err != nil {
		return TreeProof{}, err
	}
	temporaryBytes :=
		uint64(claimCount)*claimWorkingBytes +
			stemPathCount*2*stemPathWorkingBytes +
			commitmentCount*2*pathCommitmentWorkingBytes +
			derivationBound*2*pathMarkerWorkingBytes
	if err := checkTreeProofResource(
		TreeProofResourceTemporaryBytes,
		limits.MaxTemporaryBytes,
		temporaryBytes,
	); err != nil {
		return TreeProof{}, err
	}

	canonicalClaims, err := claims.Claims(ctx)
	if err != nil {
		return TreeProof{}, errors.Join(errTreeProofCancelled, err)
	}
	if err := checkTreeProofContext(ctx); err != nil {
		return TreeProof{}, err
	}
	ownedStemPaths := append([]StemPath(nil), stemPaths...)
	if err := sortTreeProofValues(
		ctx,
		ownedStemPaths,
		func(left StemPath, right StemPath) int {
			return bytes.Compare(left.stem[:], right.stem[:])
		},
	); err != nil {
		return TreeProof{}, err
	}
	for index := 1; index < len(ownedStemPaths); index++ {
		if err := checkTreeProofContext(ctx); err != nil {
			return TreeProof{}, err
		}
		if ownedStemPaths[index-1].stem == ownedStemPaths[index].stem {
			return TreeProof{}, errInvalidTreeProof
		}
	}
	if err := checkTreeProofContext(ctx); err != nil {
		return TreeProof{}, err
	}
	ownedCommitments := append([]PathCommitment(nil), commitments...)
	if err := sortTreeProofValues(
		ctx,
		ownedCommitments,
		comparePathCommitments,
	); err != nil {
		return TreeProof{}, err
	}
	for index := 1; index < len(ownedCommitments); index++ {
		if err := checkTreeProofContext(ctx); err != nil {
			return TreeProof{}, err
		}
		if comparePathCommitments(
			ownedCommitments[index-1],
			ownedCommitments[index],
		) == 0 {
			return TreeProof{}, errInvalidTreeProof
		}
	}

	markers, err := derivePathMarkers(
		ctx,
		canonicalClaims,
		ownedStemPaths,
		int(derivationBound),
	)
	if err != nil {
		return TreeProof{}, err
	}
	if err := sortTreeProofValues(ctx, markers, comparePathMarkers); err != nil {
		return TreeProof{}, err
	}
	if err := matchExpectedCommitments(
		ctx,
		markers,
		ownedCommitments,
	); err != nil {
		return TreeProof{}, err
	}
	if err := checkTreeProofContext(ctx); err != nil {
		return TreeProof{}, err
	}

	return TreeProof{
		profile:     profile,
		root:        root,
		claims:      claims,
		stemPaths:   ownedStemPaths,
		commitments: ownedCommitments,
		opening:     opening,
		valid:       true,
	}, nil
}

// Profile returns the immutable profile bound to every proof component.
func (proof TreeProof) Profile() (verkletree.Profile, error) {
	if err := proof.validate(); err != nil {
		return verkletree.Profile{}, err
	}

	return proof.profile, nil
}

// Root returns the exact root container authenticated by a future verifier.
func (proof TreeProof) Root() (backend.Root, error) {
	if err := proof.validate(); err != nil {
		return backend.Root{}, err
	}

	return proof.root, nil
}

// Claims returns the immutable canonical claim set.
func (proof TreeProof) Claims() (ClaimSet, error) {
	if err := proof.validate(); err != nil {
		return ClaimSet{}, err
	}

	return proof.claims, nil
}

// StemPaths returns a cancellation-aware owned copy in canonical stem order.
func (proof TreeProof) StemPaths(ctx context.Context) ([]StemPath, error) {
	if err := proof.validate(); err != nil {
		return nil, err
	}

	return copyTreeProofValues(ctx, proof.stemPaths)
}

// PathCommitments returns a cancellation-aware owned copy in canonical path
// order.
func (proof TreeProof) PathCommitments(
	ctx context.Context,
) ([]PathCommitment, error) {
	if err := proof.validate(); err != nil {
		return nil, err
	}

	return copyTreeProofValues(ctx, proof.commitments)
}

// OpeningProof returns the canonical raw aggregate-opening payload. It remains
// unverified.
func (proof TreeProof) OpeningProof() (backend.OpeningProof, error) {
	if err := proof.validate(); err != nil {
		return backend.OpeningProof{}, err
	}

	return proof.opening, nil
}

func (proof TreeProof) validate() error {
	if !proof.valid ||
		proof.profile.Validate() != nil ||
		len(proof.stemPaths) == 0 {
		return errInvalidTreeProof
	}
	if _, err := proof.root.Profile(); err != nil {
		return errInvalidTreeProof
	}
	emptyRoot, _ := proof.root.IsEmpty()
	if emptyRoot {
		return errInvalidTreeProof
	}
	if _, err := proof.claims.Profile(); err != nil {
		return errInvalidTreeProof
	}
	if _, err := proof.opening.Bytes(); err != nil {
		return errInvalidTreeProof
	}

	return nil
}

type pathMarkerKind uint8

const (
	pathMarkerInternal pathMarkerKind = iota + 1
	pathMarkerStem
	pathMarkerSuffix
	pathMarkerMissing
)

type pathMarker struct {
	path     [maxProofPathLength]byte
	leafStem Stem
	length   uint8
	kind     pathMarkerKind
}

func derivePathMarkers(
	ctx context.Context,
	claims []Claim,
	stemPaths []StemPath,
	capacity int,
) ([]pathMarker, error) {
	if len(stemPaths) == 0 {
		return nil, errInvalidTreeProof
	}
	if err := checkTreeProofContext(ctx); err != nil {
		return nil, err
	}
	markers := make([]pathMarker, 0, capacity)
	claimIndex := 0
	for pathIndex := range stemPaths {
		if err := checkTreeProofContext(ctx); err != nil {
			return nil, err
		}
		path := stemPaths[pathIndex]
		if claimIndex == len(claims) {
			return nil, errInvalidTreeProof
		}
		firstKey := claims[claimIndex].key
		stem := Stem(firstKey[:31])
		if stem != path.stem {
			return nil, errInvalidTreeProof
		}
		claimEnd := claimIndex + 1
		for claimEnd < len(claims) &&
			Stem(claims[claimEnd].key[:31]) == stem {
			if err := checkTreeProofContext(ctx); err != nil {
				return nil, err
			}
			claimEnd++
		}
		for depth := uint8(1); depth < path.depth; depth++ {
			if err := checkTreeProofContext(ctx); err != nil {
				return nil, err
			}
			markers = append(markers, newPathMarker(
				path.stem[:depth],
				pathMarkerInternal,
				Stem{},
			))
		}
		switch path.kind {
		case StemPathPresent:
			markers = append(markers, newPathMarker(
				path.stem[:path.depth],
				pathMarkerStem,
				path.stem,
			))
			for index := claimIndex; index < claimEnd; index++ {
				if err := checkTreeProofContext(ctx); err != nil {
					return nil, err
				}
				key := claims[index].key
				suffixPath := make([]byte, path.depth+1)
				copy(suffixPath, path.stem[:path.depth])
				suffixPath[path.depth] = 2 + key[31]/128
				markers = append(markers, newPathMarker(
					suffixPath,
					pathMarkerSuffix,
					Stem{},
				))
			}
		case StemPathMissing:
			if err := requireAbsenceClaims(
				ctx,
				claims[claimIndex:claimEnd],
			); err != nil {
				return nil, err
			}
			markers = append(markers, newPathMarker(
				path.stem[:path.depth],
				pathMarkerMissing,
				Stem{},
			))
		case StemPathDifferent:
			if err := requireAbsenceClaims(
				ctx,
				claims[claimIndex:claimEnd],
			); err != nil {
				return nil, err
			}
			markers = append(markers, newPathMarker(
				path.stem[:path.depth],
				pathMarkerStem,
				path.existing,
			))
		default:
			return nil, errInvalidTreeProof
		}
		claimIndex = claimEnd
	}
	if claimIndex != len(claims) {
		return nil, errInvalidTreeProof
	}

	return markers, nil
}

func requireAbsenceClaims(
	ctx context.Context,
	claims []Claim,
) error {
	for index := range claims {
		if err := checkTreeProofContext(ctx); err != nil {
			return err
		}
		if claims[index].kind != ClaimAbsence {
			return errInvalidTreeProof
		}
	}

	return nil
}

func newPathMarker(
	path []byte,
	kind pathMarkerKind,
	leafStem Stem,
) pathMarker {
	marker := pathMarker{
		leafStem: leafStem,
		length:   uint8(len(path)),
		kind:     kind,
	}
	copy(marker.path[:], path)

	return marker
}

func matchExpectedCommitments(
	ctx context.Context,
	markers []pathMarker,
	commitments []PathCommitment,
) error {
	expected := markers[:0]
	for markerIndex := 0; markerIndex < len(markers); {
		if err := checkTreeProofContext(ctx); err != nil {
			return err
		}
		end := markerIndex + 1
		for end < len(markers) &&
			comparePathMarkers(markers[markerIndex], markers[end]) == 0 {
			if err := checkTreeProofContext(ctx); err != nil {
				return err
			}
			if markers[markerIndex].kind != markers[end].kind ||
				markers[markerIndex].leafStem != markers[end].leafStem {
				return errInvalidTreeProof
			}
			end++
		}
		if markers[markerIndex].kind != pathMarkerMissing {
			expected = append(expected, markers[markerIndex])
		}
		markerIndex = end
	}
	if !slices.EqualFunc(
		expected,
		commitments,
		equalMarkerCommitmentPath,
	) {
		return errInvalidTreeProof
	}

	return nil
}

func equalMarkerCommitmentPath(
	marker pathMarker,
	commitment PathCommitment,
) bool {
	return marker.length == commitment.length &&
		[maxProofPathLength]byte(marker.path) == commitment.path
}

func comparePathCommitments(
	left PathCommitment,
	right PathCommitment,
) int {
	return compareFixedPaths(
		left.path,
		left.length,
		right.path,
		right.length,
	)
}

func comparePathMarkers(left pathMarker, right pathMarker) int {
	return compareFixedPaths(
		left.path,
		left.length,
		right.path,
		right.length,
	)
}

func compareFixedPaths(
	left [maxProofPathLength]byte,
	leftLength uint8,
	right [maxProofPathLength]byte,
	rightLength uint8,
) int {
	if comparison := bytes.Compare(
		left[:leftLength],
		right[:rightLength],
	); comparison != 0 {
		return comparison
	}

	return 0
}

func sortTreeProofValues[T any](
	ctx context.Context,
	values []T,
	compare func(T, T) int,
) error {
	if len(values) < 2 {
		return checkTreeProofContext(ctx)
	}
	if err := checkTreeProofContext(ctx); err != nil {
		return err
	}
	scratch := make([]T, len(values))

	return mergeSortTreeProofValues(
		ctx,
		values,
		scratch,
		0,
		len(values),
		compare,
	)
}

func mergeSortTreeProofValues[T any](
	ctx context.Context,
	values []T,
	scratch []T,
	start int,
	end int,
	compare func(T, T) int,
) error {
	if err := checkTreeProofContext(ctx); err != nil {
		return err
	}
	if end-start < 2 {
		return nil
	}
	middle := start + (end-start)/2
	if err := mergeSortTreeProofValues(
		ctx,
		values,
		scratch,
		start,
		middle,
		compare,
	); err != nil {
		return err
	}
	if err := mergeSortTreeProofValues(
		ctx,
		values,
		scratch,
		middle,
		end,
		compare,
	); err != nil {
		return err
	}
	left := start
	right := middle
	for output := start; output < end; output++ {
		if err := checkTreeProofContext(ctx); err != nil {
			return err
		}
		if right == end {
			scratch[output] = values[left]
			left++
		} else if left == middle ||
			compare(values[left], values[right]) > 0 {
			scratch[output] = values[right]
			right++
		} else {
			scratch[output] = values[left]
			left++
		}
	}
	for index := start; index < end; index++ {
		if err := checkTreeProofContext(ctx); err != nil {
			return err
		}
		values[index] = scratch[index]
	}

	return nil
}

func copyTreeProofValues[T any](
	ctx context.Context,
	values []T,
) ([]T, error) {
	if err := checkTreeProofContext(ctx); err != nil {
		return nil, err
	}
	owned := make([]T, len(values))
	for index := range values {
		if err := checkTreeProofContext(ctx); err != nil {
			return nil, err
		}
		owned[index] = values[index]
	}

	return owned, nil
}

func checkTreeProofContext(ctx context.Context) error {
	if ctx == nil {
		return errInvalidTreeProofContext
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(errTreeProofCancelled, err)
	}

	return nil
}

func checkTreeProofResource(
	resource TreeProofResource,
	limit uint64,
	actual uint64,
) error {
	if actual <= limit {
		return nil
	}

	return &TreeProofResourceError{
		Resource: resource,
		Limit:    limit,
		Actual:   actual,
	}
}
