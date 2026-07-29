package authstate

import (
	"context"
	"errors"
	"fmt"
	"slices"

	verkletree "github.com/faustbrian/golib/pkg/verkle-tree"
)

const (
	claimWorkingBytes = uint64(96)
	maxClaimCount     = uint32(65_536)
)

var (
	errInvalidClaimContext = errors.New("invalid claim-set context")
	errInvalidClaimLimits  = errors.New("invalid claim-set limits")
	errInvalidClaim        = errors.New("invalid tree claim")
	errInvalidClaimSet     = errors.New("invalid tree claim set")
	errDuplicateClaimKey   = errors.New("duplicate tree-claim key")
	errClaimCancelled      = errors.New("tree-claim operation cancelled")
	errClaimResource       = errors.New("tree-claim resource limit exceeded")
)

// ClaimKind distinguishes a present value from an absent key.
type ClaimKind uint8

const (
	// ClaimMembership states that one exact value is present. The all-zero
	// value remains present.
	ClaimMembership ClaimKind = iota + 1

	// ClaimAbsence states that the key is absent.
	ClaimAbsence
)

// Claim is one immutable fixed-size membership or absence assertion. Its zero
// value is invalid.
type Claim struct {
	kind  ClaimKind
	key   Key
	value Value
}

// Membership returns a present-value claim, including for an all-zero value.
func Membership(key Key, value Value) Claim {
	return Claim{kind: ClaimMembership, key: key, value: value}
}

// Absence returns an absent-key claim with no value.
func Absence(key Key) Claim {
	return Claim{kind: ClaimAbsence, key: key}
}

// Kind returns whether the claim asserts membership or absence.
func (claim Claim) Kind() (ClaimKind, error) {
	if err := claim.validate(); err != nil {
		return 0, err
	}

	return claim.kind, nil
}

// Key returns the exact fixed-length key.
func (claim Claim) Key() (Key, error) {
	if err := claim.validate(); err != nil {
		return Key{}, err
	}

	return claim.key, nil
}

// Value returns the exact value and present true for membership. Absence
// returns a zero value and present false.
func (claim Claim) Value() (Value, bool, error) {
	if err := claim.validate(); err != nil {
		return Value{}, false, err
	}
	if claim.kind == ClaimAbsence {
		return Value{}, false, nil
	}

	return claim.value, true, nil
}

func (claim Claim) validate() error {
	switch claim.kind {
	case ClaimMembership:
		return nil
	case ClaimAbsence:
		if claim.value == (Value{}) {
			return nil
		}
	}

	return errInvalidClaim
}

// ClaimLimits bounds canonical claim-set construction. MaxClaims must not
// exceed 65,536. Zero fields are invalid and no field denotes an unbounded
// resource.
type ClaimLimits struct {
	MaxClaims         uint32
	MaxTemporaryBytes uint64
}

func (limits ClaimLimits) validate() error {
	if limits.MaxClaims == 0 ||
		limits.MaxClaims > maxClaimCount ||
		limits.MaxTemporaryBytes == 0 {
		return errInvalidClaimLimits
	}

	return nil
}

// ClaimResource identifies one bounded claim-set resource.
type ClaimResource uint8

const (
	// ClaimResourceClaims counts distinct claimed keys.
	ClaimResourceClaims ClaimResource = iota + 1

	// ClaimResourceTemporaryBytes counts conservative deterministic copy and
	// sort scratch space.
	ClaimResourceTemporaryBytes
)

// ClaimResourceError reports one rejected claim-set bound without disclosing
// keys or values.
type ClaimResourceError struct {
	Resource ClaimResource
	Limit    uint64
	Actual   uint64
}

// Error implements error.
func (err *ClaimResourceError) Error() string {
	return fmt.Sprintf(
		"%v: resource %d has value %d, limit %d",
		errClaimResource,
		err.Resource,
		err.Actual,
		err.Limit,
	)
}

// Unwrap makes ClaimResourceError match the claim-resource sentinel.
func (err *ClaimResourceError) Unwrap() error {
	return errClaimResource
}

// ClaimSet is one immutable non-empty set of claims in ascending raw-key
// order. Copies are safe for concurrent reads.
type ClaimSet struct {
	profile verkletree.Profile
	claims  []Claim
	valid   bool
}

// NewClaimSet validates every claim before allocation, rejects duplicate keys,
// and owns the claims in canonical ascending raw-key order.
func NewClaimSet(
	ctx context.Context,
	profile verkletree.Profile,
	claims []Claim,
	limits ClaimLimits,
) (ClaimSet, error) {
	if err := checkClaimContext(ctx); err != nil {
		return ClaimSet{}, err
	}
	if err := profile.Validate(); err != nil {
		return ClaimSet{}, fmt.Errorf("%w: claim profile", err)
	}
	if err := limits.validate(); err != nil {
		return ClaimSet{}, err
	}
	if len(claims) == 0 {
		return ClaimSet{}, errInvalidClaimSet
	}
	count := uint64(len(claims))
	if err := checkClaimResource(
		ClaimResourceClaims,
		uint64(limits.MaxClaims),
		count,
	); err != nil {
		return ClaimSet{}, err
	}
	temporaryBytes := count * 2 * claimWorkingBytes
	if err := checkClaimResource(
		ClaimResourceTemporaryBytes,
		limits.MaxTemporaryBytes,
		temporaryBytes,
	); err != nil {
		return ClaimSet{}, err
	}
	for index := range claims {
		if err := checkClaimContext(ctx); err != nil {
			return ClaimSet{}, err
		}
		if err := claims[index].validate(); err != nil {
			return ClaimSet{}, err
		}
	}

	owned := make([]Claim, len(claims))
	for index := range claims {
		if err := checkClaimContext(ctx); err != nil {
			return ClaimSet{}, err
		}
		owned[index] = claims[index]
	}
	if err := sortClaims(ctx, owned); err != nil {
		return ClaimSet{}, err
	}
	if err := checkClaimContext(ctx); err != nil {
		return ClaimSet{}, err
	}

	return ClaimSet{
		profile: profile,
		claims:  owned,
		valid:   true,
	}, nil
}

// Count returns the number of distinct claimed keys.
func (set ClaimSet) Count() (uint32, error) {
	if err := set.validate(); err != nil {
		return 0, err
	}

	return uint32(len(set.claims)), nil
}

// Profile returns the immutable profile bound to the claims.
func (set ClaimSet) Profile() (verkletree.Profile, error) {
	if err := set.validate(); err != nil {
		return verkletree.Profile{}, err
	}

	return set.profile, nil
}

// Claims returns a cancellation-aware owned copy in canonical key order.
func (set ClaimSet) Claims(ctx context.Context) ([]Claim, error) {
	if err := set.validate(); err != nil {
		return nil, err
	}
	if err := checkClaimContext(ctx); err != nil {
		return nil, err
	}
	owned := make([]Claim, len(set.claims))
	for index := range set.claims {
		if err := checkClaimContext(ctx); err != nil {
			return nil, err
		}
		owned[index] = set.claims[index]
	}

	return owned, nil
}

// Lookup returns the claim for key and whether the canonical set contains it.
// A found absence claim remains distinct from a key not included in the set.
func (set ClaimSet) Lookup(key Key) (Claim, bool, error) {
	if err := set.validate(); err != nil {
		return Claim{}, false, err
	}
	index, found := slices.BinarySearchFunc(
		set.claims,
		key,
		func(claim Claim, key Key) int {
			return compareKey(claim.key, key)
		},
	)
	if !found {
		return Claim{}, false, nil
	}

	return set.claims[index], true, nil
}

func (set ClaimSet) validate() error {
	if !set.valid ||
		set.profile.Validate() != nil ||
		len(set.claims) == 0 ||
		uint64(len(set.claims)) > uint64(maxClaimCount) {
		return errInvalidClaimSet
	}

	return nil
}

func sortClaims(ctx context.Context, claims []Claim) error {
	if len(claims) < 2 {
		return checkClaimContext(ctx)
	}
	scratch := make([]Claim, len(claims))

	return mergeSortClaims(ctx, claims, scratch, 0, len(claims))
}

func mergeSortClaims(
	ctx context.Context,
	claims []Claim,
	scratch []Claim,
	start int,
	end int,
) error {
	if err := checkClaimContext(ctx); err != nil {
		return err
	}
	if end-start < 2 {
		return nil
	}
	middle := start + (end-start)/2
	if err := mergeSortClaims(ctx, claims, scratch, start, middle); err != nil {
		return err
	}
	if err := mergeSortClaims(ctx, claims, scratch, middle, end); err != nil {
		return err
	}
	left := start
	right := middle
	for output := start; output < end; output++ {
		if err := checkClaimContext(ctx); err != nil {
			return err
		}
		if right == end {
			scratch[output] = claims[left]
			left++
		} else if left == middle {
			scratch[output] = claims[right]
			right++
		} else {
			comparison := compareKey(claims[left].key, claims[right].key)
			switch {
			case comparison < 0:
				scratch[output] = claims[left]
				left++
			case comparison > 0:
				scratch[output] = claims[right]
				right++
			default:
				return errDuplicateClaimKey
			}
		}
	}
	for index := start; index < end; index++ {
		if err := checkClaimContext(ctx); err != nil {
			return err
		}
		claims[index] = scratch[index]
	}

	return nil
}

func checkClaimContext(ctx context.Context) error {
	if ctx == nil {
		return errInvalidClaimContext
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(errClaimCancelled, err)
	}

	return nil
}

func checkClaimResource(
	resource ClaimResource,
	limit uint64,
	actual uint64,
) error {
	if actual <= limit {
		return nil
	}

	return &ClaimResourceError{
		Resource: resource,
		Limit:    limit,
		Actual:   actual,
	}
}
