package backend

import (
	"context"
	"errors"
	"fmt"
)

var (
	errInvalidVectorCommitmentDecodingContext = errors.New(
		"invalid vector-commitment decoding context",
	)
	errInvalidVectorCommitmentDecodingLimits = errors.New(
		"invalid vector-commitment decoding limits",
	)
	errVectorCommitmentDecodingCancelled = errors.New(
		"vector-commitment decoding operation cancelled",
	)
	errVectorCommitmentDecodingResource = errors.New(
		"vector-commitment decoding resource limit exceeded",
	)
)

// VectorCommitmentDecodingLimits bounds hostile commitment decoding.
// MaxPointDecodes may be zero to reject every commitment before point
// decoding; MaxCommitmentBytes must be positive. No field denotes an unbounded
// resource.
type VectorCommitmentDecodingLimits struct {
	MaxCommitmentBytes uint32
	MaxPointDecodes    uint32
}

func (limits VectorCommitmentDecodingLimits) validate() error {
	if limits.MaxCommitmentBytes == 0 {
		return errInvalidVectorCommitmentDecodingLimits
	}

	return nil
}

// VectorCommitmentDecodingResource identifies one bounded decoder resource.
type VectorCommitmentDecodingResource uint8

const (
	// VectorCommitmentDecodingResourceBytes counts untrusted commitment bytes.
	VectorCommitmentDecodingResourceBytes VectorCommitmentDecodingResource = iota + 1

	// VectorCommitmentDecodingResourcePointDecodes counts strict point
	// decodings.
	VectorCommitmentDecodingResourcePointDecodes
)

// VectorCommitmentDecodingResourceError reports one rejected decoder bound
// without disclosing the commitment payload.
type VectorCommitmentDecodingResourceError struct {
	Resource VectorCommitmentDecodingResource
	Limit    uint64
	Actual   uint64
}

// Error implements error.
func (err *VectorCommitmentDecodingResourceError) Error() string {
	return fmt.Sprintf(
		"%v: resource %d has value %d, limit %d",
		errVectorCommitmentDecodingResource,
		err.Resource,
		err.Actual,
		err.Limit,
	)
}

// Unwrap makes VectorCommitmentDecodingResourceError match the decoder
// resource sentinel.
func (err *VectorCommitmentDecodingResourceError) Unwrap() error {
	return errVectorCommitmentDecodingResource
}

// DecodeVectorCommitment returns one immutable canonical non-identity
// commitment after enforcing byte and point-decoding budgets.
func DecodeVectorCommitment(
	ctx context.Context,
	encoded []byte,
	limits VectorCommitmentDecodingLimits,
) (VectorCommitment, error) {
	if err := checkVectorCommitmentDecodingContext(ctx); err != nil {
		return VectorCommitment{}, err
	}
	if err := limits.validate(); err != nil {
		return VectorCommitment{}, err
	}
	if err := checkVectorCommitmentDecodingResource(
		VectorCommitmentDecodingResourceBytes,
		uint64(limits.MaxCommitmentBytes),
		uint64(len(encoded)),
	); err != nil {
		return VectorCommitment{}, err
	}
	if len(encoded) != commitmentSize {
		return VectorCommitment{}, fmt.Errorf(
			"%w: encoded length",
			errInvalidCommitment,
		)
	}
	if err := checkVectorCommitmentDecodingResource(
		VectorCommitmentDecodingResourcePointDecodes,
		uint64(limits.MaxPointDecodes),
		1,
	); err != nil {
		return VectorCommitment{}, err
	}
	decoded, err := decodeCommitment(encoded)
	if err != nil {
		return VectorCommitment{}, err
	}
	if err := checkVectorCommitmentDecodingContext(ctx); err != nil {
		return VectorCommitment{}, err
	}

	return VectorCommitment{value: decoded, valid: true}, nil
}

func checkVectorCommitmentDecodingContext(ctx context.Context) error {
	if ctx == nil {
		return errInvalidVectorCommitmentDecodingContext
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(errVectorCommitmentDecodingCancelled, err)
	}

	return nil
}

func checkVectorCommitmentDecodingResource(
	resource VectorCommitmentDecodingResource,
	limit uint64,
	actual uint64,
) error {
	if actual <= limit {
		return nil
	}

	return &VectorCommitmentDecodingResourceError{
		Resource: resource,
		Limit:    limit,
		Actual:   actual,
	}
}
