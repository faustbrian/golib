package backend

import (
	"context"
	"errors"
	"fmt"
)

const (
	// OpeningProofSize is the exact byte length of the experimental profile's
	// raw aggregate opening proof: D, eight L points, eight R points, and one
	// canonical scalar.
	OpeningProofSize = openingProofPointCount*commitmentSize + scalarSize

	openingProofPointCount = 17

	// OpeningProofPointDecodes is the exact number of strict point decodings
	// required by one aggregate-opening payload.
	OpeningProofPointDecodes = openingProofPointCount

	// OpeningProofScalarDecodes is the exact number of strict scalar decodings
	// required by one aggregate-opening payload.
	OpeningProofScalarDecodes = 1
)

var (
	errInvalidOpeningProof        = errors.New("invalid aggregate opening proof")
	errInvalidOpeningProofContext = errors.New("invalid aggregate opening proof context")
	errInvalidOpeningProofLimits  = errors.New("invalid aggregate opening proof limits")
	errOpeningProofCancelled      = errors.New("aggregate opening proof operation cancelled")
	errOpeningProofResource       = errors.New("aggregate opening proof resource limit exceeded")
)

// OpeningProof is one opaque canonical aggregate-opening proof for the fixed
// experimental profile. It does not include tree claims or imply verification.
type OpeningProof struct {
	encoded [OpeningProofSize]byte
	valid   bool
}

// OpeningProofLimits bounds hostile proof decoding. MaxScalarDecodes may be
// zero to reject all proofs before scalar decoding; the other fields must be
// positive. No field denotes an unbounded resource.
type OpeningProofLimits struct {
	MaxProofBytes    uint32
	MaxPointDecodes  uint32
	MaxScalarDecodes uint32
}

func (limits OpeningProofLimits) validate() error {
	if limits.MaxProofBytes == 0 || limits.MaxPointDecodes == 0 {
		return errInvalidOpeningProofLimits
	}

	return nil
}

// OpeningProofResource identifies one bounded aggregate-proof decoder
// resource.
type OpeningProofResource uint8

const (
	// OpeningProofResourceBytes counts untrusted proof bytes.
	OpeningProofResourceBytes OpeningProofResource = iota + 1

	// OpeningProofResourcePointDecodes counts strict group-point decodings.
	OpeningProofResourcePointDecodes

	// OpeningProofResourceScalarDecodes counts strict field-scalar decodings.
	OpeningProofResourceScalarDecodes
)

// OpeningProofResourceError reports one rejected proof-decoding bound without
// disclosing proof contents.
type OpeningProofResourceError struct {
	Resource OpeningProofResource
	Limit    uint64
	Actual   uint64
}

// Error implements error.
func (err *OpeningProofResourceError) Error() string {
	return fmt.Sprintf(
		"%v: resource %d has value %d, limit %d",
		errOpeningProofResource,
		err.Resource,
		err.Actual,
		err.Limit,
	)
}

// Unwrap makes OpeningProofResourceError match the proof-resource sentinel.
func (err *OpeningProofResourceError) Unwrap() error {
	return errOpeningProofResource
}

// DecodeOpeningProof validates resource declarations, exact length, every
// non-identity point, and the final canonical scalar before returning an owned
// opaque proof. It performs no cryptographic verification.
func DecodeOpeningProof(
	ctx context.Context,
	encoded []byte,
	limits OpeningProofLimits,
) (OpeningProof, error) {
	if err := checkOpeningProofContext(ctx); err != nil {
		return OpeningProof{}, err
	}
	if err := limits.validate(); err != nil {
		return OpeningProof{}, err
	}
	if err := checkOpeningProofResource(
		OpeningProofResourceBytes,
		uint64(limits.MaxProofBytes),
		uint64(len(encoded)),
	); err != nil {
		return OpeningProof{}, err
	}
	if len(encoded) != OpeningProofSize {
		return OpeningProof{}, fmt.Errorf("%w: encoded length", errInvalidOpeningProof)
	}
	if err := checkOpeningProofResource(
		OpeningProofResourcePointDecodes,
		uint64(limits.MaxPointDecodes),
		openingProofPointCount,
	); err != nil {
		return OpeningProof{}, err
	}
	if err := checkOpeningProofResource(
		OpeningProofResourceScalarDecodes,
		uint64(limits.MaxScalarDecodes),
		1,
	); err != nil {
		return OpeningProof{}, err
	}

	var owned [OpeningProofSize]byte
	copy(owned[:], encoded)
	for index := 0; index < openingProofPointCount; index++ {
		if err := checkOpeningProofContext(ctx); err != nil {
			return OpeningProof{}, err
		}
		start := index * commitmentSize
		if _, err := decodeCommitment(owned[start : start+commitmentSize]); err != nil {
			return OpeningProof{}, fmt.Errorf(
				"%w: point %d",
				errInvalidOpeningProof,
				index,
			)
		}
	}
	if err := checkOpeningProofContext(ctx); err != nil {
		return OpeningProof{}, err
	}
	if _, err := decodeScalar(owned[OpeningProofSize-scalarSize:]); err != nil {
		return OpeningProof{}, fmt.Errorf("%w: final scalar", errInvalidOpeningProof)
	}

	return OpeningProof{encoded: owned, valid: true}, nil
}

// Bytes returns the canonical owned proof bytes.
func (proof OpeningProof) Bytes() ([OpeningProofSize]byte, error) {
	if !proof.valid {
		return [OpeningProofSize]byte{}, errInvalidOpeningProof
	}

	return proof.encoded, nil
}

func checkOpeningProofContext(ctx context.Context) error {
	if ctx == nil {
		return errInvalidOpeningProofContext
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(errOpeningProofCancelled, err)
	}

	return nil
}

func checkOpeningProofResource(
	resource OpeningProofResource,
	limit uint64,
	actual uint64,
) error {
	if actual <= limit {
		return nil
	}

	return &OpeningProofResourceError{
		Resource: resource,
		Limit:    limit,
		Actual:   actual,
	}
}
