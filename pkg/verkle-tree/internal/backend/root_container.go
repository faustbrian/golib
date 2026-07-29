package backend

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"

	verkletree "github.com/faustbrian/golib/pkg/verkle-tree"
)

const (
	rootMagicSize      = 4
	rootProfileIDSize  = 1
	rootVersionSize    = 2
	rootKindSize       = 1
	rootProfileIDIndex = rootMagicSize
	rootVersionOffset  = rootProfileIDIndex + rootProfileIDSize
	rootEncodingOffset = rootVersionOffset + rootVersionSize
	rootKindIndex      = rootEncodingOffset + rootVersionSize
	rootPayloadOffset  = rootKindIndex + rootKindSize

	// RootSize is the exact canonical root-container length for the
	// experimental profile.
	RootSize = rootPayloadOffset + commitmentSize
)

var (
	rootMagic = [rootMagicSize]byte{'V', 'K', 'R', 'T'}

	errInvalidRoot        = errors.New("invalid root container")
	errInvalidRootContext = errors.New("invalid root-container context")
	errInvalidRootLimits  = errors.New("invalid root-container limits")
	errRootCancelled      = errors.New("root-container operation cancelled")
	errRootResource       = errors.New("root-container resource limit exceeded")
)

// RootKind distinguishes the explicit empty root from a non-identity root
// commitment.
type RootKind uint8

const (
	// RootKindEmpty identifies the mathematical identity without serializing an
	// identity point.
	RootKindEmpty RootKind = iota + 1

	// RootKindCommitment identifies one strict non-identity commitment payload.
	RootKindCommitment
)

// RootLimits bounds hostile root-container decoding. MaxPointDecodes may be
// zero so callers can accept an explicit empty root without permitting point
// decoding. MaxRootBytes must be positive.
type RootLimits struct {
	MaxRootBytes    uint32
	MaxPointDecodes uint32
}

func (limits RootLimits) validate() error {
	if limits.MaxRootBytes == 0 {
		return errInvalidRootLimits
	}

	return nil
}

// RootResource identifies one bounded root-decoding resource.
type RootResource uint8

const (
	// RootResourceBytes counts untrusted root-container bytes.
	RootResourceBytes RootResource = iota + 1

	// RootResourcePointDecodes counts strict group-point decodings.
	RootResourcePointDecodes
)

// RootResourceError reports one rejected root-decoding bound without
// disclosing the root payload.
type RootResourceError struct {
	Resource RootResource
	Limit    uint64
	Actual   uint64
}

// Error implements error.
func (err *RootResourceError) Error() string {
	return fmt.Sprintf(
		"%v: resource %d has value %d, limit %d",
		errRootResource,
		err.Resource,
		err.Actual,
		err.Limit,
	)
}

// Unwrap makes RootResourceError match the root-resource sentinel.
func (err *RootResourceError) Unwrap() error {
	return errRootResource
}

// Root is one immutable profile-bound root container. Its zero value is
// invalid. Empty roots are represented by a kind tag rather than an identity
// point encoding.
type Root struct {
	profile    verkletree.Profile
	kind       RootKind
	commitment VectorCommitment
	valid      bool
}

// NewRoot binds one validated commitment to the exact package-owned profile.
// Profile validation precedes commitment inspection.
func NewRoot(
	ctx context.Context,
	profile verkletree.Profile,
	commitment VectorCommitment,
) (Root, error) {
	if err := checkRootContext(ctx); err != nil {
		return Root{}, err
	}
	if err := profile.Validate(); err != nil {
		return Root{}, fmt.Errorf("%w: root profile", err)
	}
	identity, err := commitment.IsIdentity()
	if err != nil {
		return Root{}, fmt.Errorf("%w: commitment state", errInvalidRoot)
	}
	if err := checkRootContext(ctx); err != nil {
		return Root{}, err
	}
	if identity {
		return Root{
			profile: profile,
			kind:    RootKindEmpty,
			valid:   true,
		}, nil
	}

	return Root{
		profile:    profile,
		kind:       RootKindCommitment,
		commitment: commitment,
		valid:      true,
	}, nil
}

// DecodeRoot validates the exact profile header and kind before any point
// decoding. It returns an immutable canonical root and rejects trailing bytes.
func DecodeRoot(
	ctx context.Context,
	encoded []byte,
	limits RootLimits,
) (Root, error) {
	if err := checkRootContext(ctx); err != nil {
		return Root{}, err
	}
	if err := limits.validate(); err != nil {
		return Root{}, err
	}
	if err := checkRootResource(
		RootResourceBytes,
		uint64(limits.MaxRootBytes),
		uint64(len(encoded)),
	); err != nil {
		return Root{}, err
	}
	if len(encoded) != RootSize {
		return Root{}, fmt.Errorf("%w: encoded length", errInvalidRoot)
	}

	var owned [RootSize]byte
	copy(owned[:], encoded)
	if [rootMagicSize]byte(owned[:rootMagicSize]) != rootMagic {
		return Root{}, fmt.Errorf("%w: magic", errInvalidRoot)
	}
	profile := verkletree.ExperimentalBandersnatchIPA256V0()
	if owned[rootProfileIDIndex] != byte(profile.ID()) ||
		binary.BigEndian.Uint16(owned[rootVersionOffset:rootEncodingOffset]) != profile.Version() ||
		binary.BigEndian.Uint16(owned[rootEncodingOffset:rootKindIndex]) != profile.EncodingVersion() {
		return Root{}, fmt.Errorf("%w: root profile", verkletree.ErrUnsupportedProfile)
	}

	switch RootKind(owned[rootKindIndex]) {
	case RootKindEmpty:
		if [commitmentSize]byte(owned[rootPayloadOffset:]) != ([commitmentSize]byte{}) {
			return Root{}, fmt.Errorf("%w: empty payload", errInvalidRoot)
		}

		return Root{
			profile: profile,
			kind:    RootKindEmpty,
			valid:   true,
		}, nil
	case RootKindCommitment:
		if err := checkRootResource(
			RootResourcePointDecodes,
			uint64(limits.MaxPointDecodes),
			1,
		); err != nil {
			return Root{}, err
		}
		if err := checkRootContext(ctx); err != nil {
			return Root{}, err
		}
		decoded, err := decodeCommitment(owned[rootPayloadOffset:])
		if err != nil {
			return Root{}, fmt.Errorf("%w: commitment", errInvalidRoot)
		}
		if err := checkRootContext(ctx); err != nil {
			return Root{}, err
		}

		return Root{
			profile: profile,
			kind:    RootKindCommitment,
			commitment: VectorCommitment{
				value: decoded,
				valid: true,
			},
			valid: true,
		}, nil
	default:
		return Root{}, fmt.Errorf("%w: root kind", errInvalidRoot)
	}
}

// Bytes returns the exact canonical root container.
func (root Root) Bytes() ([RootSize]byte, error) {
	if err := root.validate(); err != nil {
		return [RootSize]byte{}, err
	}

	var encoded [RootSize]byte
	copy(encoded[:rootMagicSize], rootMagic[:])
	encoded[rootProfileIDIndex] = byte(root.profile.ID())
	binary.BigEndian.PutUint16(
		encoded[rootVersionOffset:rootEncodingOffset],
		root.profile.Version(),
	)
	binary.BigEndian.PutUint16(
		encoded[rootEncodingOffset:rootKindIndex],
		root.profile.EncodingVersion(),
	)
	encoded[rootKindIndex] = byte(root.kind)
	if root.kind == RootKindCommitment {
		commitmentBytes := encodeCommitment(root.commitment.value)
		copy(encoded[rootPayloadOffset:], commitmentBytes[:])
	}

	return encoded, nil
}

// Profile returns the immutable profile bound into the root container.
func (root Root) Profile() (verkletree.Profile, error) {
	if err := root.validate(); err != nil {
		return verkletree.Profile{}, err
	}

	return root.profile, nil
}

// IsEmpty reports whether the root is the explicit mathematical identity.
func (root Root) IsEmpty() (bool, error) {
	if err := root.validate(); err != nil {
		return false, err
	}

	return root.kind == RootKindEmpty, nil
}

// CommitmentBytes returns the canonical commitment payload and whether it is
// present. An empty root returns a zero array with present false.
func (root Root) CommitmentBytes() ([commitmentSize]byte, bool, error) {
	if err := root.validate(); err != nil {
		return [commitmentSize]byte{}, false, err
	}
	if root.kind == RootKindEmpty {
		return [commitmentSize]byte{}, false, nil
	}
	encoded := encodeCommitment(root.commitment.value)

	return encoded, true, nil
}

func (root Root) validate() error {
	if !root.valid || root.profile.Validate() != nil {
		return errInvalidRoot
	}
	switch root.kind {
	case RootKindEmpty:
		if root.commitment != (VectorCommitment{}) {
			return errInvalidRoot
		}
	case RootKindCommitment:
		identity, err := root.commitment.IsIdentity()
		if err != nil || identity {
			return errInvalidRoot
		}
	default:
		return errInvalidRoot
	}

	return nil
}

func checkRootContext(ctx context.Context) error {
	if ctx == nil {
		return errInvalidRootContext
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(errRootCancelled, err)
	}

	return nil
}

func checkRootResource(
	resource RootResource,
	limit uint64,
	actual uint64,
) error {
	if actual <= limit {
		return nil
	}

	return &RootResourceError{
		Resource: resource,
		Limit:    limit,
		Actual:   actual,
	}
}
