package authstate

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/faustbrian/golib/pkg/verkle-tree/internal/backend"
	internalprofile "github.com/faustbrian/golib/pkg/verkle-tree/internal/profile"
)

const (
	treeProofMagicBytes        = 4
	treeProofProfileIDBytes    = 1
	treeProofVersionBytes      = 2
	treeProofEncodingBytes     = 2
	treeProofCountBytes        = 4
	treeProofCountFields       = 3
	treeProofContainerVersion  = uint16(2)
	claimEncodedBytes          = 32 + 1 + 32
	stemPathEncodedBytes       = 31 + 1 + 1 + 31
	pathCommitmentEncodedBytes = 1 + maxProofPathLength +
		backend.CommitmentSize

	treeProofHeaderBytes = treeProofMagicBytes +
		treeProofProfileIDBytes +
		treeProofVersionBytes +
		treeProofEncodingBytes +
		backend.RootSize +
		treeProofCountFields*treeProofCountBytes
	treeProofFixedBytes      = treeProofHeaderBytes + backend.OpeningProofSize
	maxTreeProofEncodedBytes = treeProofFixedBytes +
		int(maxClaimCount)*claimEncodedBytes +
		int(maxTreeProofStemPaths)*stemPathEncodedBytes +
		int(maxTreeProofPathCommitments)*pathCommitmentEncodedBytes
)

var (
	treeProofMagic = [treeProofMagicBytes]byte{'V', 'K', 'P', 'F'}

	errInvalidTreeProofEncodingContext = errors.New(
		"invalid tree-proof encoding context",
	)
	errInvalidTreeProofEncodingLimits = errors.New(
		"invalid tree-proof encoding limits",
	)
	errTreeProofEncodingCancelled = errors.New(
		"tree-proof encoding operation cancelled",
	)
	errTreeProofEncodingResource = errors.New(
		"tree-proof encoding resource limit exceeded",
	)
	errInvalidTreeProofEncoding = errors.New(
		"invalid canonical tree-proof encoding",
	)
	errInvalidTreeProofDecodingLimits = errors.New(
		"invalid tree-proof decoding limits",
	)
	errTreeProofDecodingResource = errors.New(
		"tree-proof decoding resource limit exceeded",
	)
)

// TreeProofEncodingLimits bounds canonical tree-proof serialization. Every
// field must be positive and no field denotes an unbounded resource.
type TreeProofEncodingLimits struct {
	MaxProofBytes     uint64
	MaxTemporaryBytes uint64
}

// TreeProofDecodingLimits bounds hostile canonical proof decoding.
// MaxPointDecodes and MaxScalarDecodes may be zero to reject every proof
// before cryptographic decoding; every other field must be positive. No field
// denotes an unbounded resource.
type TreeProofDecodingLimits struct {
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

func (limits TreeProofDecodingLimits) validate() error {
	if limits.MaxProofBytes == 0 ||
		limits.MaxProofBytes > uint64(maxTreeProofEncodedBytes) ||
		limits.MaxClaims == 0 ||
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
		return errInvalidTreeProofDecodingLimits
	}

	return nil
}

func (limits TreeProofEncodingLimits) validate() error {
	if limits.MaxProofBytes == 0 ||
		limits.MaxProofBytes > uint64(maxTreeProofEncodedBytes) ||
		limits.MaxTemporaryBytes == 0 {
		return errInvalidTreeProofEncodingLimits
	}

	return nil
}

// TreeProofEncodingResource identifies one bounded serialization resource.
type TreeProofEncodingResource uint8

const (
	// TreeProofEncodingResourceBytes counts canonical encoded proof bytes.
	TreeProofEncodingResourceBytes TreeProofEncodingResource = iota + 1

	// TreeProofEncodingResourceTemporaryBytes counts the owned output buffer.
	TreeProofEncodingResourceTemporaryBytes
)

// TreeProofEncodingResourceError reports one rejected serialization bound
// without disclosing keys, values, paths, commitments, or proof bytes.
type TreeProofEncodingResourceError struct {
	Resource TreeProofEncodingResource
	Limit    uint64
	Actual   uint64
}

// Error implements error.
func (err *TreeProofEncodingResourceError) Error() string {
	return fmt.Sprintf(
		"%v: resource %d has value %d, limit %d",
		errTreeProofEncodingResource,
		err.Resource,
		err.Actual,
		err.Limit,
	)
}

// Unwrap makes TreeProofEncodingResourceError match the encoding-resource
// sentinel.
func (err *TreeProofEncodingResourceError) Unwrap() error {
	return errTreeProofEncodingResource
}

// TreeProofDecodingResource identifies one bounded decoder resource.
type TreeProofDecodingResource uint8

const (
	// TreeProofDecodingResourceBytes counts untrusted proof bytes.
	TreeProofDecodingResourceBytes TreeProofDecodingResource = iota + 1

	// TreeProofDecodingResourceClaims counts encoded claims.
	TreeProofDecodingResourceClaims

	// TreeProofDecodingResourceStemPaths counts encoded stem paths.
	TreeProofDecodingResourceStemPaths

	// TreeProofDecodingResourcePathCommitments counts encoded non-root
	// commitments.
	TreeProofDecodingResourcePathCommitments

	// TreeProofDecodingResourcePathDerivations counts conservatively derived
	// topology paths.
	TreeProofDecodingResourcePathDerivations

	// TreeProofDecodingResourcePathBytes counts retained path bytes.
	TreeProofDecodingResourcePathBytes

	// TreeProofDecodingResourcePointDecodes counts strict group-point decodes.
	TreeProofDecodingResourcePointDecodes

	// TreeProofDecodingResourceScalarDecodes counts strict field-scalar
	// decodes.
	TreeProofDecodingResourceScalarDecodes

	// TreeProofDecodingResourceTemporaryBytes counts conservative decoder,
	// constructor, and deterministic-sort scratch.
	TreeProofDecodingResourceTemporaryBytes
)

// TreeProofDecodingResourceError reports one rejected decoder bound without
// disclosing proof contents.
type TreeProofDecodingResourceError struct {
	Resource TreeProofDecodingResource
	Limit    uint64
	Actual   uint64
}

// Error implements error.
func (err *TreeProofDecodingResourceError) Error() string {
	return fmt.Sprintf(
		"%v: resource %d has value %d, limit %d",
		errTreeProofDecodingResource,
		err.Resource,
		err.Actual,
		err.Limit,
	)
}

// Unwrap makes TreeProofDecodingResourceError match the decoder-resource
// sentinel.
func (err *TreeProofDecodingResourceError) Unwrap() error {
	return errTreeProofDecodingResource
}

// Bytes returns the exact canonical profile-bound tree-proof encoding. The
// returned bytes are caller-owned and the proof remains cryptographically
// unverified.
func (proof TreeProof) Bytes(
	ctx context.Context,
	limits TreeProofEncodingLimits,
) ([]byte, error) {
	if err := checkTreeProofEncodingContext(ctx); err != nil {
		return nil, err
	}
	if err := proof.validate(); err != nil {
		return nil, err
	}
	if err := limits.validate(); err != nil {
		return nil, err
	}

	encodedSize := uint64(treeProofFixedBytes) +
		uint64(len(proof.claims.claims))*claimEncodedBytes +
		uint64(len(proof.stemPaths))*stemPathEncodedBytes +
		uint64(len(proof.commitments))*pathCommitmentEncodedBytes
	if err := checkTreeProofEncodingResource(
		TreeProofEncodingResourceBytes,
		limits.MaxProofBytes,
		encodedSize,
	); err != nil {
		return nil, err
	}
	if err := checkTreeProofEncodingResource(
		TreeProofEncodingResourceTemporaryBytes,
		limits.MaxTemporaryBytes,
		encodedSize,
	); err != nil {
		return nil, err
	}

	rootBytes, _ := proof.root.Bytes()
	openingBytes, _ := proof.opening.Bytes()
	encoded := make([]byte, int(encodedSize))
	copy(encoded, treeProofMagic[:])
	offset := treeProofMagicBytes
	encoded[offset] = byte(proof.profile.ID())
	offset += treeProofProfileIDBytes
	binary.BigEndian.PutUint16(
		encoded[offset:offset+treeProofVersionBytes],
		proof.profile.Version(),
	)
	offset += treeProofVersionBytes
	binary.BigEndian.PutUint16(
		encoded[offset:offset+treeProofEncodingBytes],
		treeProofContainerVersion,
	)
	offset += treeProofEncodingBytes
	copy(encoded[offset:], rootBytes[:])
	offset += backend.RootSize
	binary.BigEndian.PutUint32(
		encoded[offset:offset+treeProofCountBytes],
		uint32(len(proof.claims.claims)),
	)
	offset += treeProofCountBytes
	binary.BigEndian.PutUint32(
		encoded[offset:offset+treeProofCountBytes],
		uint32(len(proof.stemPaths)),
	)
	offset += treeProofCountBytes
	binary.BigEndian.PutUint32(
		encoded[offset:offset+treeProofCountBytes],
		uint32(len(proof.commitments)),
	)
	offset += treeProofCountBytes

	for index := range proof.claims.claims {
		if err := checkTreeProofEncodingContext(ctx); err != nil {
			return nil, err
		}
		claim := proof.claims.claims[index]
		copy(encoded[offset:], claim.key[:])
		offset += len(claim.key)
		encoded[offset] = byte(claim.kind)
		offset++
		copy(encoded[offset:], claim.value[:])
		offset += len(claim.value)
	}
	for index := range proof.stemPaths {
		if err := checkTreeProofEncodingContext(ctx); err != nil {
			return nil, err
		}
		path := proof.stemPaths[index]
		copy(encoded[offset:], path.stem[:])
		offset += len(path.stem)
		encoded[offset] = path.depth
		offset++
		encoded[offset] = byte(path.kind)
		offset++
		copy(encoded[offset:], path.existing[:])
		offset += len(path.existing)
	}
	for index := range proof.commitments {
		if err := checkTreeProofEncodingContext(ctx); err != nil {
			return nil, err
		}
		path := proof.commitments[index]
		encoded[offset] = path.length
		offset++
		copy(encoded[offset:], path.path[:])
		offset += maxProofPathLength
		identity, identityErr := path.commitment.IsIdentity()
		if identityErr != nil {
			return nil, errInvalidTreeProof
		}
		if !identity {
			commitmentBytes, _ := path.commitment.Bytes()
			copy(encoded[offset:], commitmentBytes[:])
		}
		offset += backend.CommitmentSize
	}
	copy(encoded[offset:], openingBytes[:])
	if err := checkTreeProofEncodingContext(ctx); err != nil {
		return nil, err
	}

	return encoded, nil
}

// DecodeTreeProof validates one exact canonical profile-bound encoding,
// defensively owns every component, and returns an unverified proof container.
// It rejects trailing bytes and checks all aggregate resource limits before
// point or scalar decoding.
func DecodeTreeProof(
	ctx context.Context,
	encoded []byte,
	limits TreeProofDecodingLimits,
) (TreeProof, error) {
	if err := checkTreeProofEncodingContext(ctx); err != nil {
		return TreeProof{}, err
	}
	if err := limits.validate(); err != nil {
		return TreeProof{}, err
	}
	if err := checkTreeProofDecodingResource(
		TreeProofDecodingResourceBytes,
		limits.MaxProofBytes,
		uint64(len(encoded)),
	); err != nil {
		return TreeProof{}, err
	}
	if len(encoded) < treeProofFixedBytes {
		return TreeProof{}, fmt.Errorf(
			"%w: minimum length",
			errInvalidTreeProofEncoding,
		)
	}
	if [treeProofMagicBytes]byte(encoded[:treeProofMagicBytes]) !=
		treeProofMagic {
		return TreeProof{}, errInvalidTreeProofEncoding
	}
	profile := internalprofile.ExperimentalBandersnatchIPA256V0()
	if encoded[treeProofMagicBytes] != byte(profile.ID()) ||
		binary.BigEndian.Uint16(encoded[5:7]) != profile.Version() ||
		binary.BigEndian.Uint16(encoded[7:9]) != treeProofContainerVersion {
		return TreeProof{}, fmt.Errorf(
			"%w: proof profile",
			internalprofile.ErrUnsupported,
		)
	}

	countOffset := treeProofMagicBytes + treeProofProfileIDBytes +
		treeProofVersionBytes + treeProofEncodingBytes + backend.RootSize
	claimCount := binary.BigEndian.Uint32(
		encoded[countOffset : countOffset+treeProofCountBytes],
	)
	countOffset += treeProofCountBytes
	stemPathCount := binary.BigEndian.Uint32(
		encoded[countOffset : countOffset+treeProofCountBytes],
	)
	countOffset += treeProofCountBytes
	commitmentCount := binary.BigEndian.Uint32(
		encoded[countOffset : countOffset+treeProofCountBytes],
	)
	if err := checkTreeProofDecodingResource(
		TreeProofDecodingResourceClaims,
		uint64(limits.MaxClaims),
		uint64(claimCount),
	); err != nil {
		return TreeProof{}, err
	}
	if err := checkTreeProofDecodingResource(
		TreeProofDecodingResourceStemPaths,
		uint64(limits.MaxStemPaths),
		uint64(stemPathCount),
	); err != nil {
		return TreeProof{}, err
	}
	if err := checkTreeProofDecodingResource(
		TreeProofDecodingResourcePathCommitments,
		uint64(limits.MaxPathCommitments),
		uint64(commitmentCount),
	); err != nil {
		return TreeProof{}, err
	}
	derivationBound := uint64(stemPathCount)*31 + uint64(claimCount)
	if err := checkTreeProofDecodingResource(
		TreeProofDecodingResourcePathDerivations,
		uint64(limits.MaxPathDerivations),
		derivationBound,
	); err != nil {
		return TreeProof{}, err
	}
	encodedSize := uint64(treeProofFixedBytes) +
		uint64(claimCount)*claimEncodedBytes +
		uint64(stemPathCount)*stemPathEncodedBytes +
		uint64(commitmentCount)*pathCommitmentEncodedBytes
	if encodedSize != uint64(len(encoded)) {
		return TreeProof{}, fmt.Errorf(
			"%w: declared length",
			errInvalidTreeProofEncoding,
		)
	}
	claimsOffset := treeProofHeaderBytes
	stemPathsOffset := claimsOffset + int(claimCount)*claimEncodedBytes
	commitmentsOffset := stemPathsOffset +
		int(stemPathCount)*stemPathEncodedBytes
	openingOffset := len(encoded) - backend.OpeningProofSize
	pathBytes := uint64(0)
	pointDecodes := uint64(1 + backend.OpeningProofPointDecodes)
	for index := uint32(0); index < commitmentCount; index++ {
		if err := checkTreeProofEncodingContext(ctx); err != nil {
			return TreeProof{}, err
		}
		offset := commitmentsOffset + int(index)*pathCommitmentEncodedBytes
		length := encoded[offset]
		if length == 0 || length > maxProofPathLength {
			return TreeProof{}, errInvalidTreeProofEncoding
		}
		pathEnd := offset + 1 + maxProofPathLength
		for _, value := range encoded[offset+1+int(length) : pathEnd] {
			if value != 0 {
				return TreeProof{}, errInvalidTreeProofEncoding
			}
		}
		if index > 0 {
			previousOffset := commitmentsOffset +
				int(index-1)*pathCommitmentEncodedBytes
			previousLength := encoded[previousOffset]
			if bytes.Compare(
				encoded[previousOffset+1:previousOffset+1+
					int(previousLength)],
				encoded[offset+1:offset+1+int(length)],
			) >= 0 {
				return TreeProof{}, fmt.Errorf(
					"%w: path-commitment order",
					errInvalidTreeProofEncoding,
				)
			}
		}
		commitmentOffset := pathEnd
		commitmentEnd := commitmentOffset + backend.CommitmentSize
		if [backend.CommitmentSize]byte(
			encoded[commitmentOffset:commitmentEnd],
		) != ([backend.CommitmentSize]byte{}) {
			pointDecodes++
		}
		pathBytes += uint64(length)
	}
	if err := checkTreeProofDecodingResource(
		TreeProofDecodingResourcePathBytes,
		limits.MaxPathBytes,
		pathBytes,
	); err != nil {
		return TreeProof{}, err
	}
	if err := checkTreeProofDecodingResource(
		TreeProofDecodingResourcePointDecodes,
		uint64(limits.MaxPointDecodes),
		pointDecodes,
	); err != nil {
		return TreeProof{}, err
	}
	if err := checkTreeProofDecodingResource(
		TreeProofDecodingResourceScalarDecodes,
		uint64(limits.MaxScalarDecodes),
		backend.OpeningProofScalarDecodes,
	); err != nil {
		return TreeProof{}, err
	}
	temporaryBytes := encodedSize +
		uint64(claimCount)*3*claimWorkingBytes +
		uint64(stemPathCount)*3*stemPathWorkingBytes +
		uint64(commitmentCount)*3*pathCommitmentWorkingBytes +
		derivationBound*2*pathMarkerWorkingBytes
	if err := checkTreeProofDecodingResource(
		TreeProofDecodingResourceTemporaryBytes,
		limits.MaxTemporaryBytes,
		temporaryBytes,
	); err != nil {
		return TreeProof{}, err
	}

	rootOffset := treeProofMagicBytes + treeProofProfileIDBytes +
		treeProofVersionBytes + treeProofEncodingBytes
	root, err := backend.DecodeRoot(
		ctx,
		encoded[rootOffset:rootOffset+backend.RootSize],
		backend.RootLimits{
			MaxRootBytes:    backend.RootSize,
			MaxPointDecodes: 1,
		},
	)
	if err != nil {
		return TreeProof{}, normalizeTreeProofDecodingError(err)
	}
	claims, err := decodeTreeProofClaims(
		ctx,
		encoded[claimsOffset:stemPathsOffset],
		claimCount,
		profile,
		limits,
	)
	if err != nil {
		return TreeProof{}, err
	}
	stemPaths, err := decodeTreeProofStemPaths(
		ctx,
		encoded[stemPathsOffset:commitmentsOffset],
		stemPathCount,
	)
	if err != nil {
		return TreeProof{}, err
	}
	commitments, err := decodeTreeProofPathCommitments(
		ctx,
		encoded[commitmentsOffset:openingOffset],
		commitmentCount,
	)
	if err != nil {
		return TreeProof{}, err
	}
	opening, err := backend.DecodeOpeningProof(
		ctx,
		encoded[openingOffset:],
		backend.OpeningProofLimits{
			MaxProofBytes:    backend.OpeningProofSize,
			MaxPointDecodes:  backend.OpeningProofPointDecodes,
			MaxScalarDecodes: backend.OpeningProofScalarDecodes,
		},
	)
	if err != nil {
		return TreeProof{}, normalizeTreeProofDecodingError(err)
	}

	proof, err := NewTreeProof(
		ctx,
		root,
		claims,
		stemPaths,
		commitments,
		opening,
		TreeProofLimits{
			MaxClaims:          limits.MaxClaims,
			MaxStemPaths:       limits.MaxStemPaths,
			MaxPathCommitments: limits.MaxPathCommitments,
			MaxPathDerivations: limits.MaxPathDerivations,
			MaxPathBytes:       limits.MaxPathBytes,
			MaxTemporaryBytes:  limits.MaxTemporaryBytes,
		},
	)
	if err != nil {
		return TreeProof{}, normalizeTreeProofDecodingError(err)
	}

	return proof, nil
}

func decodeTreeProofClaims(
	ctx context.Context,
	encoded []byte,
	count uint32,
	profile internalprofile.Profile,
	limits TreeProofDecodingLimits,
) (ClaimSet, error) {
	claims := make([]Claim, count)
	for index := range claims {
		if err := checkTreeProofEncodingContext(ctx); err != nil {
			return ClaimSet{}, err
		}
		offset := index * claimEncodedBytes
		var key Key
		copy(key[:], encoded[offset:offset+len(key)])
		if index > 0 &&
			bytes.Compare(claims[index-1].key[:], key[:]) >= 0 {
			return ClaimSet{}, fmt.Errorf(
				"%w: claim order",
				errInvalidTreeProofEncoding,
			)
		}
		kindOffset := offset + len(key)
		var value Value
		copy(value[:], encoded[kindOffset+1:kindOffset+1+len(value)])
		switch ClaimKind(encoded[kindOffset]) {
		case ClaimMembership:
			claims[index] = Membership(key, value)
		case ClaimAbsence:
			if value != (Value{}) {
				return ClaimSet{}, errInvalidTreeProofEncoding
			}
			claims[index] = Absence(key)
		default:
			return ClaimSet{}, errInvalidTreeProofEncoding
		}
	}

	set, err := NewClaimSet(
		ctx,
		profile,
		claims,
		ClaimLimits{
			MaxClaims:         limits.MaxClaims,
			MaxTemporaryBytes: limits.MaxTemporaryBytes,
		},
	)
	if err != nil {
		return ClaimSet{}, normalizeTreeProofDecodingError(err)
	}

	return set, nil
}

func decodeTreeProofStemPaths(
	ctx context.Context,
	encoded []byte,
	count uint32,
) ([]StemPath, error) {
	paths := make([]StemPath, count)
	for index := range paths {
		if err := checkTreeProofEncodingContext(ctx); err != nil {
			return nil, err
		}
		offset := index * stemPathEncodedBytes
		var stem Stem
		copy(stem[:], encoded[offset:offset+len(stem)])
		if index > 0 &&
			bytes.Compare(paths[index-1].stem[:], stem[:]) >= 0 {
			return nil, fmt.Errorf(
				"%w: stem-path order",
				errInvalidTreeProofEncoding,
			)
		}
		depthOffset := offset + len(stem)
		depth := encoded[depthOffset]
		kind := StemPathKind(encoded[depthOffset+1])
		var existing Stem
		copy(
			existing[:],
			encoded[depthOffset+2:depthOffset+2+len(existing)],
		)
		switch kind {
		case StemPathPresent:
			paths[index] = PresentStemPath(stem, depth)
		case StemPathMissing:
			paths[index] = MissingStemPath(stem, depth)
		case StemPathDifferent:
			paths[index] = DifferentStemPath(stem, depth, existing)
		default:
			return nil, errInvalidTreeProofEncoding
		}
		if paths[index].existing != existing ||
			paths[index].validate() != nil {
			return nil, errInvalidTreeProofEncoding
		}
	}

	return paths, nil
}

func decodeTreeProofPathCommitments(
	ctx context.Context,
	encoded []byte,
	count uint32,
) ([]PathCommitment, error) {
	commitments := make([]PathCommitment, count)
	for index := range commitments {
		if err := checkTreeProofEncodingContext(ctx); err != nil {
			return nil, err
		}
		offset := index * pathCommitmentEncodedBytes
		length := encoded[offset]
		commitmentOffset := offset + 1 + maxProofPathLength
		commitmentEnd := commitmentOffset + backend.CommitmentSize
		payload := [backend.CommitmentSize]byte(
			encoded[commitmentOffset:commitmentEnd],
		)
		commitment := backend.EmptyVectorCommitment()
		if payload != ([backend.CommitmentSize]byte{}) {
			var err error
			commitment, err = backend.DecodeVectorCommitment(
				ctx,
				payload[:],
				backend.VectorCommitmentDecodingLimits{
					MaxCommitmentBytes: backend.CommitmentSize,
					MaxPointDecodes:    1,
				},
			)
			if err != nil {
				return nil, normalizeTreeProofDecodingError(err)
			}
		}
		value, _ := NewPathCommitment(
			encoded[offset+1:offset+1+int(length)],
			commitment,
		)
		commitments[index] = value
	}

	return commitments, nil
}

func checkTreeProofEncodingContext(ctx context.Context) error {
	if ctx == nil {
		return errInvalidTreeProofEncodingContext
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(errTreeProofEncodingCancelled, err)
	}

	return nil
}

func normalizeTreeProofDecodingError(err error) error {
	if errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) {
		return errors.Join(errTreeProofEncodingCancelled, err)
	}

	return errInvalidTreeProofEncoding
}

func checkTreeProofEncodingResource(
	resource TreeProofEncodingResource,
	limit uint64,
	actual uint64,
) error {
	if actual <= limit {
		return nil
	}

	return &TreeProofEncodingResourceError{
		Resource: resource,
		Limit:    limit,
		Actual:   actual,
	}
}

func checkTreeProofDecodingResource(
	resource TreeProofDecodingResource,
	limit uint64,
	actual uint64,
) error {
	if actual <= limit {
		return nil
	}

	return &TreeProofDecodingResourceError{
		Resource: resource,
		Limit:    limit,
		Actual:   actual,
	}
}

// IsInvalidProofEncodingError reports malformed canonical proof bytes.
func IsInvalidProofEncodingError(err error) bool {
	return errors.Is(err, errInvalidTreeProofEncoding)
}

// IsInvalidProofLimitsError reports invalid proof construction or codec
// limits, as distinct from malformed proof material.
func IsInvalidProofLimitsError(err error) bool {
	return errors.Is(err, errInvalidTreeProofLimits) ||
		errors.Is(err, errInvalidTreeProofEncodingLimits) ||
		errors.Is(err, errInvalidTreeProofDecodingLimits)
}
